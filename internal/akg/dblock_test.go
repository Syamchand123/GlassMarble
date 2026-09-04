package akg

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLock_NonOwnerCannotRelease proves the ownership property: a transaction
// manager that never acquired db.lock must not be able to release the lock a
// different holder owns.
//
// Against the pre-fix implementation ReleaseLock was an unconditional
// os.Remove of the lock path, so tmB below silently unlocked tmA's database
// and both processes could then write concurrently.
func TestLock_NonOwnerCannotRelease(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "db.lock")

	tmA, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("new tmA: %v", err)
	}
	defer tmA.Close()
	tmB, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("new tmB: %v", err)
	}
	defer tmB.Close()

	if err := tmA.AcquireLock(); err != nil {
		t.Fatalf("tmA.AcquireLock: %v", err)
	}
	defer tmA.ReleaseLock()

	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock file after acquire: %v", err)
	}

	// tmB holds nothing. Its release must be a no-op.
	tmB.ReleaseLock()

	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("non-owner ReleaseLock destroyed the lock file: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("non-owner ReleaseLock rewrote the lock file:\nbefore=%q\nafter =%q", before, after)
	}
}
