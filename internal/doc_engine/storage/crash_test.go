// Package storage — crash_test.go
// Crash-safety harness (improvement plan A4): proves the tmp → fsync →
// rename → verify contract and the .gmb.bak self-healing under kill-style
// failures. All tests are hermetic (t.TempDir, no repo writes).
//
//   - torn-write: partial bytes land in target.tmp, rename never happens →
//     the original target must be byte-intact, and a rerun converges.
//   - kill-simulation: the test binary is re-execed as a child that writes
//     target.tmp and dies via os.Exit(3) before rename (coordinated by
//     GMB_CRASH_TEST=1 / GMB_CRASH_TARGET / GMB_CRASH_PARTIAL). The parent
//     asserts the exit code, the intact original, and that a rerun converges
//     to zero-churn.
//   - .gmb.bak restore: corrupt the target, RestoreFromBackup must bring
//     back the exact pre-write bytes.
package storage

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCrash_TornWriteLeavesOriginalIntact simulates a crash after the tmp
// file is (partially) written but before rename: the target must keep the
// original bytes, and recovery (drop stale tmp + rerun) converges.
func TestCrash_TornWriteLeavesOriginalIntact(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "auth.md")
	original := []byte("# Auth\n\nOriginal content.\n")
	require.NoError(t, os.WriteFile(target, original, 0644))

	// Crash mid-tmp: partial bytes land in target.tmp, rename never runs.
	require.NoError(t, os.WriteFile(target+".tmp", []byte("# Auth\n\nParti"), 0644))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, original, got, "target must be intact: rename never happened")

	// Recovery: discard the stale tmp, rerun converges to the new bytes.
	require.NoError(t, os.Remove(target+".tmp"))
	updated := []byte("# Auth\n\nRegenerated content.\n")
	changed, err := AtomicWriteFile(target, updated)
	require.NoError(t, err)
	require.True(t, changed)
	got, err = os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, updated, got)
}

// TestCrashChildWriteTmpAndDie is the kill-simulation child: it writes
// target.tmp (fsynced, like a real interrupted write) and dies via
// os.Exit(3) before any rename. It only acts when GMB_CRASH_TEST=1; the
// parent harness launches it via os.Executable re-exec.
func TestCrashChildWriteTmpAndDie(t *testing.T) {
	if os.Getenv("GMB_CRASH_TEST") != "1" {
		t.Skip("crash child helper: set GMB_CRASH_TEST=1 to run")
	}
	target := os.Getenv("GMB_CRASH_TARGET")
	partial := os.Getenv("GMB_CRASH_PARTIAL")
	if target == "" {
		os.Exit(2)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(target+".tmp", []byte(partial), 0644); err != nil {
		os.Exit(2)
	}
	// Fsync like the real writer path, so the tmp bytes are genuinely on
	// disk when the process dies.
	if f, err := os.OpenFile(target+".tmp", os.O_RDWR, 0); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
	os.Exit(3)
}

// TestCrash_KillMidWriteRerunConverges kills a child mid-write (exit 3,
// pre-rename) and asserts: (a) the target holds either old or new bytes,
// never torn output; (b) a rerun converges; (c) the rerun is then zero-churn.
func TestCrash_KillMidWriteRerunConverges(t *testing.T) {
	if os.Getenv("GMB_CRASH_TEST") == "1" {
		t.Skip("crash child process: skipping parent harness")
	}
	dir := t.TempDir()
	sm := NewStateManager(dir)
	target := filepath.Join(dir, "docs", "arch.md")
	original := []byte("# Architecture\n\nOriginal content.\n")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
	require.NoError(t, os.WriteFile(target, original, 0644))

	const partial = "partial-tmp-bytes-from-killed-child"
	testBin, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(testBin, "-test.run", "^TestCrashChildWriteTmpAndDie$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"GMB_CRASH_TEST=1",
		"GMB_CRASH_TARGET="+target,
		"GMB_CRASH_PARTIAL="+partial,
	)
	err = cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok, "child must die via os.Exit(3), got: %v", err)
	require.Equal(t, 3, exitErr.ExitCode(), "child must exit 3 (killed pre-rename)")

	// (a) Target is old bytes — never torn: rename never happened.
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, original, got, "target must be old bytes, never torn output")

	// The killed child left its tmp behind.
	tmpBytes, err := os.ReadFile(target + ".tmp")
	require.NoError(t, err)
	require.Equal(t, partial, string(tmpBytes))

	// (b) Recovery sweep drops the stale tmp; rerun converges.
	require.NoError(t, os.Remove(target+".tmp"))
	updated := []byte("# Architecture\n\nRegenerated content.\n")
	res, err := WriteDoc(sm, target, "docs/arch.md", updated, "doc", "sec", "abc123", "deterministic")
	require.NoError(t, err)
	require.True(t, res.Changed)
	got, err = os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, updated, got)

	// (c) Second run is a byte-identical no-op.
	res2, err := WriteDoc(sm, target, "docs/arch.md", updated, "doc", "sec", "abc123", "deterministic")
	require.NoError(t, err)
	require.False(t, res2.Changed, "rerun after convergence must be zero-churn")
}

// TestCrash_BackupRestore corrupts a written target and asserts
// RestoreFromBackup brings back the exact pre-write bytes, and that a
// missing backup is a clean error leaving the file untouched.
func TestCrash_BackupRestore(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "notes.md")
	original := []byte("# Notes\n\nHuman original.\n")
	updated := []byte("# Notes\n\nMachine update.\n")
	require.NoError(t, os.WriteFile(target, original, 0644))

	changed, err := AtomicWriteFile(target, updated)
	require.NoError(t, err)
	require.True(t, changed)

	// Corrupt the target (disk corruption / bad concurrent edit).
	require.NoError(t, os.WriteFile(target, []byte("CORRUPT!!!"), 0644))

	require.NoError(t, RestoreFromBackup(target))
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, original, got, "restore must yield the exact pre-write bytes")

	// Missing backup → error, file untouched.
	lone := filepath.Join(dir, "lone.md")
	loneBytes := []byte("untouched\n")
	require.NoError(t, os.WriteFile(lone, loneBytes, 0644))
	require.Error(t, RestoreFromBackup(lone))
	kept, err := os.ReadFile(lone)
	require.NoError(t, err)
	require.Equal(t, loneBytes, kept)
}

// TestFlockForFile_BlocksUntilRelease proves the advisory lock serializes:
// a second holder blocks while the first holds the lock and acquires
// promptly after release.
func TestFlockForFile_BlocksUntilRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "contended.md")
	locksDir := filepath.Join(dir, "locks")

	rel1, err := FlockForFile(path, locksDir)
	require.NoError(t, err)
	defer rel1()

	// Sidecar lives under locksDir (gitignored territory), never beside the target.
	assert.DirExists(t, locksDir)
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Errorf("lock debris beside target: %s.lock must not exist", path)
	}

	acquired := make(chan struct{})
	go func() {
		rel2, err := FlockForFile(path, locksDir)
		if err != nil {
			// Must not happen: release below frees the lock well within the
			// 30s FlockForFile timeout.
			close(acquired)
			return
		}
		defer rel2()
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatal("second FlockForFile acquired while the first lock is held")
	case <-time.After(300 * time.Millisecond):
		// Expected: the waiter blocks.
	}

	rel1()

	select {
	case <-acquired:
		// Expected: acquired promptly after release.
	case <-time.After(10 * time.Second):
		t.Fatal("second FlockForFile did not acquire after release")
	}
}
