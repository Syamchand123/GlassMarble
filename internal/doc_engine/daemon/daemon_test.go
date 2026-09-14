package daemon

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting: %s", msg)
}

func TestCoalescing(t *testing.T) {
	resetDroppedBatches()
	dir := t.TempDir()
	var calls atomic.Int64
	var mu sync.Mutex
	var batches [][]string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = Run(ctx, Config{RepoRoot: dir, DebounceMs: 200, Workers: 2}, func(ctx context.Context, changed []string) {
			calls.Add(1)
			mu.Lock()
			batches = append(batches, changed)
			mu.Unlock()
		})
	}()
	time.Sleep(200 * time.Millisecond) // let the watcher register

	// 5 rapid writes inside one debounce window → 1 onChange call.
	for i := 0; i < 5; i++ {
		writeFile(t, filepath.Join(dir, "f.go"), "package f\n// v\n")
	}
	waitFor(t, 5*time.Second, func() bool { return calls.Load() >= 1 }, "first batch")
	// Wait past the window to prove no second call arrives for the burst.
	time.Sleep(600 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 coalesced call for 5 rapid writes, got %d (%v)", got, batches)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(batches) != 1 || len(batches[0]) == 0 {
		t.Fatalf("expected one non-empty batch, got %v", batches)
	}
}

func TestNeverConcurrent(t *testing.T) {
	resetDroppedBatches()
	dir := t.TempDir()
	var current, maxSeen atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = Run(ctx, Config{RepoRoot: dir, DebounceMs: 50, Workers: 4}, func(ctx context.Context, changed []string) {
			n := current.Add(1)
			for {
				m := maxSeen.Load()
				if n <= m || maxSeen.CompareAndSwap(m, n) {
					break
				}
			}
			time.Sleep(400 * time.Millisecond)
			current.Add(-1)
		})
	}()
	time.Sleep(200 * time.Millisecond)

	// Distinct batches arriving faster than onChange completes: without the
	// run mutex the 4 workers would overlap.
	for i := 0; i < 6; i++ {
		writeFile(t, filepath.Join(dir, "f.go"), "package f\n// v\n")
		time.Sleep(150 * time.Millisecond)
	}
	waitFor(t, 10*time.Second, func() bool { return maxSeen.Load() >= 1 }, "any run")
	time.Sleep(time.Second)
	cancel()
	if got := maxSeen.Load(); got != 1 {
		t.Fatalf("onChange ran concurrently: max parallelism %d", got)
	}
}

func TestDropOldestCounter(t *testing.T) {
	resetDroppedBatches()
	dir := t.TempDir()
	release := make(chan struct{})
	var started atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = Run(ctx, Config{RepoRoot: dir, DebounceMs: 50, Workers: 1}, func(ctx context.Context, changed []string) {
			started.Add(1)
			<-release // block so the queue backs up
		})
	}()
	time.Sleep(200 * time.Millisecond)

	// First batch occupies the worker; subsequent distinct batches fill the
	// queue (cap 4) and overflow it, forcing drop-oldest.
	writeFile(t, filepath.Join(dir, "a.go"), "package a\n")
	waitFor(t, 5*time.Second, func() bool { return started.Load() >= 1 }, "worker occupied")
	for i := 0; i < 10; i++ {
		writeFile(t, filepath.Join(dir, "b.go"), "package b\n")
		time.Sleep(120 * time.Millisecond) // > debounce → distinct batches
	}
	if got := DroppedBatches(); got < 1 {
		t.Fatalf("expected drop-oldest counter > 0 under overflow, got %d", got)
	}
	close(release)
}

func TestGracefulCancel(t *testing.T) {
	resetDroppedBatches()
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{RepoRoot: dir, DebounceMs: 100}, func(ctx context.Context, changed []string) {})
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil on graceful cancel, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// TestOnChangePanicDoesNotKillDaemon guards against a regression where a
// panic anywhere inside onChange (a malformed docs.yaml, a nil map access on
// a rarely-hit path) escaped the worker goroutine and silently stopped the
// whole daemon — contradicting the documented "the daemon survives a failed
// batch and retries on the next one" contract, which only covered a
// returned error, not a panic. A batch that panics must not prevent a
// later, unrelated batch from still triggering onChange.
func TestOnChangePanicDoesNotKillDaemon(t *testing.T) {
	resetDroppedBatches()
	dir := t.TempDir()
	var calls atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{RepoRoot: dir, DebounceMs: 100, Workers: 1}, func(ctx context.Context, changed []string) {
			n := calls.Add(1)
			if n == 1 {
				panic("simulated onChange panic")
			}
		})
	}()
	time.Sleep(200 * time.Millisecond) // let the watcher register

	writeFile(t, filepath.Join(dir, "a.go"), "package f\n// v1\n")
	waitFor(t, 5*time.Second, func() bool { return calls.Load() >= 1 }, "first (panicking) batch")

	time.Sleep(300 * time.Millisecond) // past the debounce window
	writeFile(t, filepath.Join(dir, "b.go"), "package f\n// v2\n")
	waitFor(t, 5*time.Second, func() bool { return calls.Load() >= 2 }, "second batch after panic recovery")

	select {
	case err := <-done:
		t.Fatalf("Run exited after a panicking batch, want it to keep running: %v", err)
	default:
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil on graceful cancel, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// TestCancelReachesInFlightOnChange guards against a regression where the
// context passed to onChange (runCtx) was derived from context.Background()
// instead of the caller's ctx, so cancelling ctx (e.g. SIGINT/SIGTERM during
// `gmb docserve`) never reached an in-flight onChange call at all — only the
// deferred stopWorkers(), which itself only ran AFTER wg.Wait() had already
// returned, i.e. after every worker had already exited on its own. An
// operator hitting Ctrl+C during active generation would block until that
// run finished, however long it took, defeating the point of a shutdown
// signal. onChange here blocks on <-runCtx.Done() the way the real LLM
// actuator's backoff loop does; it must observe cancellation almost
// immediately once the caller cancels ctx, not merely "eventually."
func TestCancelReachesInFlightOnChange(t *testing.T) {
	resetDroppedBatches()
	dir := t.TempDir()
	started := make(chan struct{})
	cancelObserved := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{RepoRoot: dir, DebounceMs: 100, Workers: 1}, func(runCtx context.Context, changed []string) {
			close(started)
			<-runCtx.Done()
			close(cancelObserved)
		})
	}()
	time.Sleep(200 * time.Millisecond) // let the watcher register

	writeFile(t, filepath.Join(dir, "a.go"), "package f\n")
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("onChange never started")
	}

	cancelStart := time.Now()
	cancel()

	select {
	case <-cancelObserved:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight onChange never observed ctx cancellation (runCtx not derived from caller's ctx)")
	}
	if elapsed := time.Since(cancelStart); elapsed > time.Second {
		t.Errorf("cancellation took %s to reach in-flight onChange, want near-instant", elapsed)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil on cancel, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestIgnoredDirs(t *testing.T) {
	for _, p := range []string{"/repo/.git/HEAD", "/repo/vendor/x.go", "/repo/node_modules/y.js", "/repo/.glassmarble/state.json"} {
		if !isIgnored(p) {
			t.Errorf("expected %s to be ignored", p)
		}
	}
	if isIgnored("/repo/internal/app.go") {
		t.Errorf("source file must not be ignored")
	}
}
