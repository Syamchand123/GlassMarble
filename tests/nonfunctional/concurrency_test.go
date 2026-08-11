package nonfunctional_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestCommitLockBlocksSecondWriter verifies the cross-instance db.lock:
// a second transaction manager must block its commit until the first holder
// releases the lock, and the commit must then apply exactly once.
func TestCommitLockBlocksSecondWriter(t *testing.T) {
	sb := harness.NewSandbox(t)
	tm1, err := akg.NewAKGTransactionManager(sb.GmDir)
	if err != nil {
		t.Fatalf("New tm1: %v", err)
	}
	defer tm1.Close()
	tm2, err := akg.NewAKGTransactionManager(sb.GmDir)
	if err != nil {
		t.Fatalf("New tm2: %v", err)
	}
	defer tm2.Close()

	if err := tm1.AcquireLock(); err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- tm2.ExecuteDeltaTransaction(commitPayload("c2", "pkg/a.go::A", "pkg/b.go::B"), []string{"pkg/a.go"})
	}()

	select {
	case err := <-done:
		t.Fatalf("commit completed while lock held: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	tm1.ReleaseLock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("commit after release: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("commit did not proceed after lock release")
	}

	if v := gmVersion(t, sb); v != 1 {
		t.Errorf("version = %d after one commit, want 1", v)
	}
}

// TestSequentialCommitsAdvanceVersion verifies the transaction counter is
// monotonic and persisted: each commit bumps the graph version exactly once.
func TestSequentialCommitsAdvanceVersion(t *testing.T) {
	sb := harness.NewSandbox(t)
	tm, err := akg.NewAKGTransactionManager(sb.GmDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tm.Close()

	if err := tm.ExecuteDeltaTransaction(commitPayload("c1", "pkg/a.go::A"), []string{"pkg/a.go"}); err != nil {
		t.Fatalf("commit 1: %v", err)
	}
	if v := gmVersion(t, sb); v != 1 {
		t.Errorf("version after commit 1 = %d, want 1", v)
	}
	if err := tm.ExecuteDeltaTransaction(commitPayload("c2", "pkg/b.go::B"), []string{"pkg/b.go"}); err != nil {
		t.Fatalf("commit 2: %v", err)
	}
	if v := gmVersion(t, sb); v != 2 {
		t.Errorf("version after commit 2 = %d, want 2", v)
	}

	// A fresh manager must observe the persisted version (no reset).
	tm2, err := akg.NewAKGTransactionManager(sb.GmDir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer tm2.Close()
	if v := gmVersion(t, sb); v != 2 {
		t.Errorf("version after reopen = %d, want 2", v)
	}
}

// TestSubscribersReceiveEveryCommit verifies Subscribe: every subscriber
// channel receives exactly one event per commit, with the payload fields
// populated. Subscriptions are registered concurrently.
func TestSubscribersReceiveEveryCommit(t *testing.T) {
	sb := harness.NewSandbox(t)
	tm, err := akg.NewAKGTransactionManager(sb.GmDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tm.Close()

	const n = 3
	var mu sync.Mutex
	subs := make([]chan akg.AKGCommitEvent, 0, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := tm.Subscribe()
			mu.Lock()
			subs = append(subs, ch)
			mu.Unlock()
		}()
	}
	wg.Wait()

	if err := tm.ExecuteDeltaTransaction(commitPayload("c1", "pkg/a.go::A", "pkg/b.go::B"), []string{"pkg/a.go"}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	for i, ch := range subs {
		select {
		case ev := <-ch:
			if ev.CommitHash != "c1" {
				t.Errorf("subscriber %d event commit = %s, want c1", i, ev.CommitHash)
			}
			if ev.NodeCount != 2 {
				t.Errorf("subscriber %d event nodes = %d, want 2", i, ev.NodeCount)
			}
			if len(ev.ModifiedFiles) != 1 {
				t.Errorf("subscriber %d event modified files = %v, want [pkg/a.go]", i, ev.ModifiedFiles)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("subscriber %d missed the commit event", i)
		}
		select {
		case ev := <-ch:
			t.Fatalf("subscriber %d received a duplicate event: %+v", i, ev)
		default:
		}
	}
}

// TestConcurrentQueryNode verifies read-only node queries are safe under
// concurrent access to the same state file.
func TestConcurrentQueryNode(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteAKGState(harness.TinyGraph())

	const workers = 4
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 3; j++ {
				node, _, _, err := akg.QueryNode(sb.GmDir, "cmd/app/main.go::Main")
				if err != nil {
					errs <- fmt.Errorf("QueryNode: %w", err)
					return
				}
				if node == nil || node.ID != "cmd/app/main.go::Main" {
					errs <- fmt.Errorf("QueryNode returned %v", node)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestConcurrentEventAppend verifies the developer-memory WAL is safe for
// concurrent appends: no interleaving corruption, and every event survives
// a rebuild.
func TestConcurrentEventAppend(t *testing.T) {
	sb := harness.NewSandbox(t)
	store := developer_memory.NewStoreForRepo(sb.Root)

	const n = 5
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := store.AppendEvent(memEvent(fmt.Sprintf("evt_conc_%d", i), time.Now())); err != nil {
				errs <- fmt.Errorf("AppendEvent %d: %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	events, err := store.LoadEvents()
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(events) != n {
		t.Errorf("LoadEvents returned %d events, want %d", len(events), n)
	}
	mem, err := store.Rebuild()
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if mem.TotalEvents != n {
		t.Errorf("Rebuild total_events = %d, want %d", mem.TotalEvents, n)
	}
}
