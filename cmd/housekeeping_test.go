package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestHousekeepingPrune deletes only stale marbles/sessions files and never
// the AKG state file (AUDIT Issue 4 Phase 4B-8).
func TestHousekeepingPrune(t *testing.T) {
	root := t.TempDir()
	storage := filepath.Join(root, ".glassmarble")
	marbles := filepath.Join(storage, "marbles")
	sessions := filepath.Join(storage, "ai", "sessions")
	for _, d := range []string{marbles, sessions, filepath.Join(storage, "wal")} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	old := time.Now().Add(-90 * 24 * time.Hour)
	stale := filepath.Join(marbles, "stale_c4.md")
	fresh := filepath.Join(marbles, "fresh_c4.md")
	staleSess := filepath.Join(sessions, "old.json")
	state := filepath.Join(storage, "akg_state.ttl")
	for _, p := range []string{stale, fresh, staleSess, state} {
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	os.Chtimes(stale, old, old)
	os.Chtimes(staleSess, old, old)
	os.Chtimes(state, old, old) // the state file must survive even if stale

	command := RootCmdForTesting()
	command.SetArgs([]string{"housekeeping", "--prune", "--dir", root})
	if err := command.Execute(); err != nil {
		t.Fatalf("housekeeping failed: %v", err)
	}

	for _, gone := range []string{stale, staleSess} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("expected %s to be pruned", gone)
		}
	}
	for _, kept := range []string{fresh, state} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("expected %s to be kept, got %v", kept, err)
		}
	}
}
