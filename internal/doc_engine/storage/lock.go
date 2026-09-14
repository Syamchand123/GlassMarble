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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

// flockTimeout is how long FlockForFile blocks waiting for a contended lock.
const flockTimeout = 30 * time.Second

// flockRetry is the poll interval while waiting for a contended lock.
const flockRetry = 50 * time.Millisecond

// FlockForFile takes an exclusive advisory lock for path by locking a
// sidecar file inside locksDir (NOT alongside the target: lock debris must
// never land in the user's worktree where it could be committed —
// locksDir is expected to be .glassmarble/locks/, which is gitignored).
// It blocks up to 30s, then returns an error.
//
// The sidecar name is deterministic per path (basename + sha256 prefix),
// so independent processes locking the same target rendezvous on the same
// file. The returned release function must be called (typically via defer)
// to drop the lock.
//
// The sidecar file is intentionally left on disk after release: deleting
// it would break mutual exclusion for waiters on Unix (unlink races) and
// fail on Windows (open-handle delete semantics). Inside .glassmarble the
// residue is invisible to git and harmless.
func FlockForFile(path, locksDir string) (release func(), err error) {
	if err := os.MkdirAll(locksDir, 0755); err != nil {
		return nil, fmt.Errorf("doc_engine: flock mkdir for %s: %w", path, err)
	}
	sidecar := lockSidecarPath(path, locksDir)
	fl := flock.New(sidecar)
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

// lockSidecarPath maps a guarded target path to its deterministic sidecar
// inside locksDir: "<basename>-<sha256hex(path)[:16]>.lock". The hash makes
// distinct targets (including same-basename files in different dirs)
// rendezvous on distinct sidecars.
func lockSidecarPath(path, locksDir string) string {
	sum := sha256.Sum256([]byte(path))
	base := filepath.Base(path)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "lock"
	}
	// Keep the sidecar name filesystem-safe.
	base = strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || r == '_' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, base)
	return filepath.Join(locksDir, base+"-"+hex.EncodeToString(sum[:])[:16]+".lock")
}
