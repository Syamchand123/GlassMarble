package akgbridge_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/akgbridge"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/testutil"
)

func TestSnapshotNoAKG(t *testing.T) {
	dir := t.TempDir()
	b := akgbridge.New(dir)

	st := b.Status()
	if st.Exists {
		t.Fatal("status reports an AKG where none exists")
	}

	snap, err := b.Snapshot()
	if err == nil {
		t.Fatal("expected error for missing AKG")
	}
	if snap != nil {
		t.Fatalf("expected nil snapshot, got %v", snap)
	}
	if !strings.Contains(err.Error(), "gmb analyze") {
		t.Errorf("error should recommend `gmb analyze`: %v", err)
	}
}

func TestSnapshotLoadsAndCaches(t *testing.T) {
	dir := t.TempDir()
	testutil.SeedAKG(t, dir)
	b := akgbridge.New(dir)

	st := b.Status()
	if !st.Exists || st.Size == 0 {
		t.Fatalf("unexpected status: %+v", st)
	}

	snap, err := b.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap == nil {
		t.Fatal("nil snapshot")
	}
	if got := snap.Nodes.Len(); got != 4 {
		t.Errorf("nodes = %d, want 4", got)
	}
	// The TTL metadata now carries the commit hash and the entrypoint
	// registry, so both survive restore (AUDIT Issue 3 Phase 3B-7/3B-8).
	if got := snap.CommitHash; got != "abc1234" {
		t.Errorf("commit = %q, want abc1234 (TTL restore behavior)", got)
	}
	if got := akgbridge.EdgeCount(snap); got != 3 {
		t.Errorf("edges = %d, want 3", got)
	}
	if len(snap.Entrypoints) != 1 || snap.Entrypoints[0] != "src/app.go::main" {
		t.Errorf("entrypoints = %v, want [src/app.go::main] (registry restored from TTL)", snap.Entrypoints)
	}

	// Same pointer from cache.
	snap2, err := b.Snapshot()
	if err != nil {
		t.Fatalf("second Snapshot: %v", err)
	}
	if snap != snap2 {
		t.Error("expected cached snapshot pointer to be reused")
	}
}

func TestSnapshotReloadsAfterTTLChange(t *testing.T) {
	dir := t.TempDir()
	testutil.SeedAKG(t, dir)
	b := akgbridge.New(dir)

	if _, err := b.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Remove the state: the next call must notice and report the missing DB.
	if err := os.Remove(filepath.Join(dir, ".glassmarble", "akg.json")); err != nil {
		t.Fatalf("remove state: %v", err)
	}
	if _, err := b.Snapshot(); err == nil || !strings.Contains(err.Error(), "gmb analyze") {
		t.Fatalf("expected missing-AKG error after state removal, got %v", err)
	}

	// Re-seed: the bridge must reload the fresh state.
	testutil.SeedAKG(t, dir)
	snap, err := b.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot after re-seed: %v", err)
	}
	if snap == nil || snap.Nodes.Len() != 4 {
		t.Fatalf("expected reloaded graph, nodes = %d", snap.Nodes.Len())
	}
}

func TestClearForcesReload(t *testing.T) {
	dir := t.TempDir()
	testutil.SeedAKG(t, dir)
	b := akgbridge.New(dir)

	snap, err := b.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	b.Clear()
	snap2, err := b.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot after Clear: %v", err)
	}
	if snap == snap2 {
		t.Error("expected a fresh load after Clear")
	}
}

func TestGitCommit(t *testing.T) {
	dir := t.TempDir()
	if _, err := akgbridge.GitCommit(context.Background(), dir); err == nil {
		t.Skip("unexpectedly a git repo")
	}
}
