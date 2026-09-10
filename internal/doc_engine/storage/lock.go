// Package storage — lock.go
// Cross-process advisory locking via gofrs/flock.
//
// The legacy docs_state.lock O_EXCL file is kept as a best-effort guard for
// the pure-JSON backend (see state.go). This file adds FlockForFile, a
// blocking OS-advisory lock (LockFileEx on Windows, flock(2) on Unix) with a
// 30s timeout, used by WriteDoc (writer.go) and by the JSON→SQLite migration
// (store_sqlite.go). Unlike the O_EXCL file, a held flock blocks — it never
// fails a run under contention — and its semantics are correct across
// processes on all three OSes.
package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// flockTimeout is how long FlockForFile blocks waiting for a contended lock.
const flockTimeout = 30 * time.Second

// flockRetry is the poll interval while waiting for a contended lock.
const flockRetry = 50 * time.Millisecond

// FlockForFile takes an exclusive advisory lock for path by locking the
// sidecar file path+".lock". It blocks up to 30s, then returns an error.
//
// The returned release function must be called (typically via defer) to drop
// the lock. The sidecar ".lock" file is intentionally left on disk after
// release: deleting it would break mutual exclusion for waiters on Unix
// (unlink races) and fail on Windows (open-handle delete semantics).
func FlockForFile(path string) (release func(), err error) {
	// The lock sidecar lives next to the guarded file, so ensure its parent
	// exists first (WriteDoc creates target parents later inside
	// AtomicWriteFile — locking must not fail for a not-yet-created path).
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("doc_engine: flock mkdir for %s: %w", path, err)
	}
	fl := flock.New(path + ".lock")
	ctx, cancel := context.WithTimeout(context.Background(), flockTimeout)
	defer cancel()
	locked, err := fl.TryLockContext(ctx, flockRetry)
	if err != nil {
		return nil, fmt.Errorf("doc_engine: flock %s: %w", path, err)
	}
	if !locked {
		return nil, fmt.Errorf("doc_engine: flock %s: timeout after %s", path, flockTimeout)
	}
	return func() { _ = fl.Unlock() }, nil
}
