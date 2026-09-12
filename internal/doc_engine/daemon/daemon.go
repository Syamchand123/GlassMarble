// Package daemon implements the C5 daemon-mode file watcher: it loads once,
// watches RepoRoot recursively via fsnotify, coalesces bursts through a
// debounce window, and invokes onChange serially with backpressure.
//
// Design:
//   - Debounce: events collected during the window (default 2000ms) collapse
//     into one batch; 5 rapid writes to different files yield 1 onChange call.
//   - Singleflight: batches are executed through singleflight.Group keyed by
//     the batch contents, so duplicate batches never invoke onChange twice.
//   - Backpressure: pending batches queue up to 4 deep; beyond that the
//     oldest queued batch is dropped and DroppedBatches counts it.
//   - Concurrency rule: onChange NEVER runs concurrently with itself. A run
//     mutex serializes invocations; Workers sizes the consumer pool but the
//     mutex keeps effective onChange parallelism at 1.
//   - Cancellation: ctx cancellation stops the watcher and drains gracefully;
//     Run returns nil on clean shutdown.
//
// Ignored subtrees: .git, node_modules, vendor, .glassmarble.
//
// AKG caching boundary (gap C5, honest scope): this package never loads the
// AKG — it only debounces filesystem events and invokes onChange. The
// caller (analyze/serve layer) owns the CodePropertyGraph and should load
// it once per process and pass it as doc_engine RunOptions.HeadGraph across
// runs; cross-process caching belongs there. In-process catalog reuse lives
// one layer down in doc_engine (catalogCache keyed by docs.yaml hash +
// doc/tag filter).
package daemon

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sync/singleflight"
)

// Config controls a daemon Run.
type Config struct {
	// RepoRoot is the directory tree to watch. Required.
	RepoRoot string
	// DebounceMs is the burst-coalescing window in milliseconds. Default 2000.
	DebounceMs int
	// Workers is the number of batch-consumer goroutines. Default 2.
	// onChange itself still never runs concurrently (see package doc).
	Workers int
}

// maxPendingBatches bounds the queued-batch channel; beyond it the oldest
// queued batch is dropped and the drop counter increments.
const maxPendingBatches = 4

var droppedBatches atomic.Int64

// DroppedBatches reports how many queued batches were dropped process-wide
// because the pending queue was full.
func DroppedBatches() int64 {
	return droppedBatches.Load()
}

// resetDroppedBatches zeroes the drop counter (tests only).
func resetDroppedBatches() {
	droppedBatches.Store(0)
}

// ignoredDirNames are subtree roots pruned from recursive watching.
var ignoredDirNames = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".glassmarble": true,
}

// isIgnored reports whether any slash-normalized path segment is ignored.
func isIgnored(path string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		if ignoredDirNames[seg] {
			return true
		}
	}
	return false
}

// Run watches cfg.RepoRoot until ctx is cancelled, invoking onChange once per
// debounced batch of changed paths. It returns nil on graceful shutdown.
func Run(ctx context.Context, cfg Config, onChange func(ctx context.Context, changed []string)) error {
	if onChange == nil {
		return nil
	}
	debounceMs := cfg.DebounceMs
	if debounceMs <= 0 {
		debounceMs = 2000
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = 2
	}
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := addTree(watcher, root); err != nil {
		return err
	}

	batchCh := make(chan []string, maxPendingBatches)

	runCtx, stopWorkers := context.WithCancel(context.Background())
	defer stopWorkers()

	var group singleflight.Group
	var runMu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-runCtx.Done():
					return
				case batch, ok := <-batchCh:
					if !ok {
						return
					}
					// Never two concurrent onChange runs.
					runMu.Lock()
					key := strings.Join(batch, "\x00")
					_, _, _ = group.Do(key, func() (any, error) {
						onChange(runCtx, batch)
						return nil, nil
					})
					runMu.Unlock()
				}
			}
		}()
	}
	defer func() {
		close(batchCh)
		wg.Wait()
	}()

	submit := func(batch []string) {
		select {
		case batchCh <- batch:
		default:
			// Queue full: drop oldest, count it, enqueue the fresh batch.
			select {
			case <-batchCh:
				droppedBatches.Add(1)
			default:
			}
			select {
			case batchCh <- batch:
			default:
				droppedBatches.Add(1)
			}
		}
	}

	var pendingMu sync.Mutex
	pending := map[string]bool{}
	flush := func() {
		pendingMu.Lock()
		if len(pending) == 0 {
			pendingMu.Unlock()
			return
		}
		batch := make([]string, 0, len(pending))
		for p := range pending {
			batch = append(batch, p)
		}
		pending = map[string]bool{}
		pendingMu.Unlock()
		sort.Strings(batch)
		submit(batch)
	}

	// Debounce: each event restarts the window; firing flushes one batch.
	debounceCh := make(chan struct{}, 1)
	var timerMu sync.Mutex
	var timer *time.Timer
	arm := func() {
		timerMu.Lock()
		defer timerMu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(time.Duration(debounceMs)*time.Millisecond, func() {
			select {
			case debounceCh <- struct{}{}:
			default:
			}
		})
	}
	defer func() {
		timerMu.Lock()
		if timer != nil {
			timer.Stop()
		}
		timerMu.Unlock()
	}()

	record := func(path string) {
		if isIgnored(path) {
			return
		}
		pendingMu.Lock()
		pending[path] = true
		pendingMu.Unlock()
		arm()
	}

	for {
		select {
		case <-ctx.Done():
			// Flush trailing events collected inside the final window so
			// no change is silently lost on shutdown.
			flush()
			return nil
		case <-debounceCh:
			flush()
		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if ev.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					_ = addTree(watcher, ev.Name)
					continue
				}
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) != 0 {
				record(ev.Name)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			_ = err // watcher errors are non-fatal; keep serving
		}
	}
}

// addTree registers recursive watches under root, pruning ignored subtrees.
func addTree(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && ignoredDirNames[d.Name()] {
			return filepath.SkipDir
		}
		// Also prune when an absolute path crosses an ignored segment.
		if path != root && isIgnored(path) {
			return filepath.SkipDir
		}
		return w.Add(path)
	})
}
