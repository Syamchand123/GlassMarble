package stage1

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// RunIngestion is the full-scan entry point for Stage 1. It discovers every
// source file under rootDir that matches a known language grammar and parses
// them concurrently through a bound worker pool.
//
// When the engine is triggered by a Git commit, callers should prefer
// RunIngestionForDelta so that unchanged files are skipped entirely.
func RunIngestion(cfg Config) (*StageOutput, error) {
	normalize(&cfg)

	pathCh := make(chan string, cfg.BufferSize)
	errorCh := make(chan error, 1)
	skipWarnCh := make(chan string, cfg.BufferSize)

	go Discover(cfg, pathCh, errorCh, skipWarnCh)

	return streamTasks(pathCh, errorCh, skipWarnCh, cfg)
}

// RunIngestionForDelta is the incremental entry point for Stage 1.
// It accepts a pre-filtered diff (from a Git diff or equivalent) and parses
// only the ADDED/MODIFIED files through the worker pool. DELETED files
// bypass the parser and are emitted directly as DeleteEvent rows.
//
// Callers own the diff slice; the engine applies it as-is.
func RunIngestionForDelta(cfg Config, diff []FileTask) (*StageOutput, error) {
	normalize(&cfg)

	reg := Registry()

	var parseTasks []FileTask
	var deletes []*DeleteEvent
	var skipped, warnings []string

	for _, t := range diff {
		if t.Change == ChangeDeleted || t.Change == ChangeRenamed {
			deletes = append(deletes, &DeleteEvent{
				FilePath: t.FilePath,
				RelPath:  t.RelPath,
				Language: t.Language,
				Commit:   t.Commit,
				Author:   t.Author,
				Time:     t.Time,
			})
			continue
		}

		if _, _, ok := DetectLanguage(t.FilePath, reg); !ok {
			skipped = append(skipped, t.RelPath+" (no matching grammar)")
			continue
		}
		parseTasks = append(parseTasks, t)
	}

	idxTasks := make([]indexTask, len(parseTasks))
	for i, t := range parseTasks {
		idxTasks[i] = indexTask{
			idx:  i,
			task: t,
		}
	}

	out, err := runTasks(idxTasks, cfg, warnings, skipped)
	if err != nil {
		return nil, err
	}
	out.Deleted = deletes
	return out, nil
}

// streamTasks fans string paths out to the worker pool directly, streaming them.
func streamTasks(pathCh <-chan string, errorCh <-chan error, skipWarnCh <-chan string, cfg Config) (*StageOutput, error) {
	var warnings []string
	var skipped []string
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Collect skip/warnings in the background
	var collectWg sync.WaitGroup
	collectWg.Add(1)
	go func() {
		defer collectWg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("stage1: collector panicked: %v", r)
			}
		}()
		for msg := range skipWarnCh {
			if strings.Contains(msg, "exceeds") || strings.Contains(msg, "no matching grammar") {
				skipped = append(skipped, msg)
			} else {
				warnings = append(warnings, msg)
			}
		}
	}()

	taskCh := make(chan indexTask, cfg.BufferSize)
	resultCh := make(chan *IngestionResult, cfg.BufferSize)
	reg := Registry()
	root := cfg.RootDir

	var workerWg sync.WaitGroup
	workerWg.Add(cfg.WorkerCount)

	// Launch workers
	for w := 0; w < cfg.WorkerCount; w++ {
		go func() {
			defer workerWg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("stage1: worker panicked: %v", r)
				}
			}()
			p := newParser()
			defer p.Close()
			for it := range taskCh {
				select {
				case <-ctx.Done():
					return
				default:
				}
				spec := lookupSpec(it.task.Language, reg)
				if spec == nil {
					continue
				}
				res := processFile(p, it.task, spec)
				if res != nil {
					resultCh <- res
				}
			}
		}()
	}

	// Wait for workers and close result channel
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("stage1: result closer panicked: %v", r)
			}
		}()
		workerWg.Wait()
		close(resultCh)
	}()

	// Feeder goroutine: reads pathCh, builds indexTask, feeds taskCh
	var discovered int64
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("stage1: feeder panicked: %v", r)
			}
		}()
		idx := 0
		for p := range pathCh {
			select {
			case <-ctx.Done():
				close(taskCh)
				return
			default:
			}
			lang, _, ok := DetectLanguage(p, reg)
			if !ok || lang == LangUnknown {
				continue
			}
			atomic.AddInt64(&discovered, 1)
			rel, err := filepath.Rel(root, p)
			if err != nil {
				rel = p
			}
			rel = filepath.ToSlash(rel)
			taskCh <- indexTask{
				idx: idx,
				task: FileTask{
					FilePath: p,
					RelPath:  rel,
					Language: lang,
					Change:   ChangeModified,
				},
			}
			idx++
		}
		close(taskCh)
	}()

	// Collect results
	var updated []*IngestionResult
	onProgress := cfg.OnProgress
	var emitted int64
	for r := range resultCh {
		updated = append(updated, r)
		if onProgress != nil {
			onProgress(int(atomic.AddInt64(&emitted, 1)), int(atomic.LoadInt64(&discovered)))
		}
	}

	collectWg.Wait()

	// Check discovery error
	select {
	case err := <-errorCh:
		if err != nil {
			return nil, fmt.Errorf("stage1: discover stream: %w", err)
		}
	default:
	}

	return &StageOutput{
		Updated:  updated,
		Skipped:  skipped,
		Warnings: warnings,
	}, nil
}

// runTasks is preserved for RunIngestionForDelta to fan out pre-defined FileTasks.
func runTasks(tasks []indexTask, cfg Config, warnings, skipped []string) (*StageOutput, error) {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	n := len(tasks)
	if n == 0 {
		return &StageOutput{
			Skipped:  skipped,
			Warnings: warnings,
		}, nil
	}

	taskCh := make(chan indexTask, cfg.BufferSize)
	results := make([]*IngestionResult, n)
	reg := Registry()
	onProgress := cfg.OnProgress
	var done int64

	var wg sync.WaitGroup
	wg.Add(cfg.WorkerCount)

	for w := 0; w < cfg.WorkerCount; w++ {
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("stage1: runTasks worker panicked: %v", r)
				}
			}()
			p := newParser()
			defer p.Close()
			for it := range taskCh {
				select {
				case <-ctx.Done():
					return
				default:
				}
				spec := lookupSpec(it.task.Language, reg)
				if spec == nil {
					continue
				}
				results[it.idx] = processFile(p, it.task, spec)
				if onProgress != nil {
					onProgress(int(atomic.AddInt64(&done, 1)), n)
				}
			}
		}()
	}

	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	wg.Wait()

	updated := make([]*IngestionResult, 0, n)
	for _, r := range results {
		if r != nil {
			updated = append(updated, r)
		}
	}

	return &StageOutput{
		Updated:  updated,
		Skipped:  skipped,
		Warnings: warnings,
	}, nil
}

// normalize sets conservative defaults on a zero-valued Config so callers
// do not have to populate every field explicitly.
func normalize(cfg *Config) {
	cfg.RootDir = mustAbs(cfg.RootDir)
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = resolveWorkerCount(0)
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 128
	}
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = defaultMaxFileBytes
	}
}

func mustAbs(p string) string {
	if p == "" {
		return "."
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// indexTask pairs an integer submission-order index with a FileTask so that
// the concurrent worker pool can write results back into a pre-allocated
// slice without locks.
type indexTask struct {
	idx  int
	task FileTask
}
