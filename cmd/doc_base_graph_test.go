package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// TestLoadPersistedBaseGraph_TwoSnapshots guards against the regression
// where standalone `gmb doc` (the officially-documented flow — `doc init`'s
// own success message says "Run `gmb doc --doc %s`") never had a real
// BaseGraph: only the git-hook-triggered pipeline (cmd/analyze.go's
// runDocEngine) captured one, by grabbing the pre-commit graph directly
// before ExecuteDeltaTransaction promoted it. With two analyze snapshots on
// disk (simulating two real commits), loadPersistedBaseGraph must return
// the FIRST one's graph — "whatever came before the latest" — not the
// latest one and not nil.
func TestLoadPersistedBaseGraph_TwoSnapshots(t *testing.T) {
	absDir := t.TempDir()
	snapDir := filepath.Join(absDir, ".glassmarble", "snapshots")
	store, err := arch_timeline.NewSnapshotStore(snapDir)
	if err != nil {
		t.Fatalf("NewSnapshotStore: %v", err)
	}

	baseGraph := akg.NewCodePropertyGraph("c1")
	baseGraph.Nodes = baseGraph.Nodes.Set("pkg/a.go::Foo", &link.ResolvedNode{
		ID: "pkg/a.go::Foo", Name: "Foo", Kind: "FUNCTION",
		FileSpec: link.LocationMeta{Path: "pkg/a.go", LineStart: 1, LineEnd: 1},
	})
	baseSnap, err := arch_timeline.BuildSnapshot(arch_timeline.SnapshotInput{
		Graph: baseGraph, CommitHash: "c1", Timestamp: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("BuildSnapshot(base): %v", err)
	}
	if _, err := store.Create(baseSnap); err != nil {
		t.Fatalf("store.Create(base): %v", err)
	}

	headGraph := akg.NewCodePropertyGraph("c2")
	headGraph.Nodes = headGraph.Nodes.Set("pkg/a.go::Foo", &link.ResolvedNode{
		ID: "pkg/a.go::Foo", Name: "Foo", Kind: "FUNCTION",
		FileSpec: link.LocationMeta{Path: "pkg/a.go", LineStart: 1, LineEnd: 1},
	})
	headGraph.Nodes = headGraph.Nodes.Set("pkg/a.go::Bar", &link.ResolvedNode{
		ID: "pkg/a.go::Bar", Name: "Bar", Kind: "FUNCTION",
		FileSpec: link.LocationMeta{Path: "pkg/a.go", LineStart: 5, LineEnd: 5},
	})
	headSnap, err := arch_timeline.BuildSnapshot(arch_timeline.SnapshotInput{
		Graph: headGraph, CommitHash: "c2", Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("BuildSnapshot(head): %v", err)
	}
	if _, err := store.Create(headSnap); err != nil {
		t.Fatalf("store.Create(head): %v", err)
	}

	got, ok := loadPersistedBaseGraph(absDir)
	if !ok {
		t.Fatal("loadPersistedBaseGraph returned ok=false with two snapshots present")
	}
	if got.Nodes.Len() != 1 {
		t.Errorf("expected the BASE (pre-latest-commit) graph with 1 node, got %d nodes", got.Nodes.Len())
	}
	if _, ok := got.Nodes.Get("pkg/a.go::Bar"); ok {
		t.Errorf("BaseGraph must not contain Bar (added only in the later commit)")
	}
}

// TestLoadPersistedBaseGraph_FewerThanTwoSnapshots confirms the genesis
// fallback: with 0 or 1 snapshot on disk, there is nothing to diff against
// yet, so loadPersistedBaseGraph must report ok=false (callers leave
// RunOptions.BaseGraph nil, which invalidator.BuildDossier treats as the
// correct "everything is new" genesis case) rather than erroring.
func TestLoadPersistedBaseGraph_FewerThanTwoSnapshots(t *testing.T) {
	absDir := t.TempDir()
	if _, ok := loadPersistedBaseGraph(absDir); ok {
		t.Error("expected ok=false with no .glassmarble directory at all")
	}

	snapDir := filepath.Join(absDir, ".glassmarble", "snapshots")
	store, err := arch_timeline.NewSnapshotStore(snapDir)
	if err != nil {
		t.Fatalf("NewSnapshotStore: %v", err)
	}
	graph := akg.NewCodePropertyGraph("c1")
	snap, err := arch_timeline.BuildSnapshot(arch_timeline.SnapshotInput{
		Graph: graph, CommitHash: "c1", Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if _, err := store.Create(snap); err != nil {
		t.Fatalf("store.Create: %v", err)
	}
	if _, ok := loadPersistedBaseGraph(absDir); ok {
		t.Error("expected ok=false with only 1 snapshot present (nothing to diff against yet)")
	}
}
