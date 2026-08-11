package stages_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// Note: the reference mentions archmodel.BuildSnapshot(graph, now) and
// archmodel.BuildSnapshotFromGraph; neither exists. The real API is
// arch_timeline.BuildSnapshot(SnapshotInput) — used here. Component "tagging"
// is expressed through DetectedComponent.Kind inside the snapshot.

func evidenceBundle() evidence.Bundle {
	return evidence.NewBundle(evidence.EvidenceItem{
		Source:     evidence.SourceCode,
		Reference:  "internal/service/service.go",
		Excerpt:    "service component",
		Confidence: 0.95,
		Timestamp:  time.Now().UTC(),
	})
}

func sampleGraph(t *testing.T, commit string) *akg.CodePropertyGraph {
	t.Helper()
	g := akg.NewCodePropertyGraph(commit)
	g.Nodes = g.Nodes.Set("cmd/api/main.go::Main", &stage4.ResolvedNode{
		ID:   "cmd/api/main.go::Main",
		Kind: "FUNCTION",
		Name: "Main",
		FileSpec: stage4.LocationMeta{
			Path:      "cmd/api/main.go",
			LineStart: 1,
			LineEnd:   9,
		},
	})
	g.Nodes = g.Nodes.Set("internal/service/service.go::New", &stage4.ResolvedNode{
		ID:   "internal/service/service.go::New",
		Kind: "FUNCTION",
		Name: "New",
		FileSpec: stage4.LocationMeta{
			Path:      "internal/service/service.go",
			LineStart: 1,
			LineEnd:   3,
		},
	})
	return g
}

func buildSnapshot(t *testing.T, g *akg.CodePropertyGraph, commit string, ts time.Time, components ...archmodel.DetectedComponent) *archmodel.ArchSnapshot {
	t.Helper()
	snap, err := arch_timeline.BuildSnapshot(arch_timeline.SnapshotInput{
		Graph:      g,
		CommitHash: commit,
		Timestamp:  ts,
		Components: components,
	})
	if err != nil {
		t.Fatalf("arch_timeline.BuildSnapshot(%s): %v", commit, err)
	}
	return snap
}

func TestSnapshotStoreLifecycle(t *testing.T) {
	sb := harness.NewSandbox(t)
	store, err := arch_timeline.NewSnapshotStore(sb.Path(".glassmarble", "snapshots"))
	if err != nil {
		t.Fatalf("arch_timeline.NewSnapshotStore: %v", err)
	}

	now := time.Now().UTC()
	snap := buildSnapshot(t, sampleGraph(t, "abcd1234abcd1234"), "abcd1234abcd1234", now)

	created, err := store.Create(snap)
	if err != nil {
		t.Fatalf("store.Create: %v", err)
	}
	if !created {
		t.Fatal("first Create returned false, want a write")
	}

	// Identical snapshot (same ID) must be skipped, not duplicated.
	created, err = store.Create(snap)
	if err != nil {
		t.Fatalf("duplicate store.Create: %v", err)
	}
	if created {
		t.Error("duplicate Create returned true, want skip-write")
	}

	entries := store.List()
	if len(entries) != 1 {
		t.Fatalf("store.List() = %d entries, want 1", len(entries))
	}
	if !sb.Exists(filepath.Join(".glassmarble", "snapshots", entries[0].SnapshotFile)) {
		t.Errorf("snapshot file %q missing on disk", entries[0].SnapshotFile)
	}

	latest, err := store.Latest()
	if err != nil {
		t.Fatalf("store.Latest: %v", err)
	}
	if latest.ID != snap.ID || latest.CommitHash != "abcd1234abcd1234" {
		t.Errorf("Latest = %q/%q, want %q/abcd1234abcd1234", latest.ID, latest.CommitHash, snap.ID)
	}

	got, err := store.Get("abcd1234")
	if err != nil {
		t.Fatalf("store.Get(prefix): %v", err)
	}
	if got.ID != snap.ID {
		t.Errorf("Get(prefix) snapshot ID = %q, want %q", got.ID, snap.ID)
	}

	nearest, err := store.NearestAt(now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("store.NearestAt: %v", err)
	}
	if nearest.ID != snap.ID {
		t.Errorf("NearestAt(now-1h) = %q, want %q", nearest.ID, snap.ID)
	}
}

func TestSnapshotComponentCategoryRoundTrip(t *testing.T) {
	sb := harness.NewSandbox(t)
	store, err := arch_timeline.NewSnapshotStore(sb.Path(".glassmarble", "snapshots"))
	if err != nil {
		t.Fatalf("arch_timeline.NewSnapshotStore: %v", err)
	}

	comp := archmodel.DetectedComponent{
		ID:          "comp_service",
		Name:        "service",
		Kind:        archmodel.ComponentService,
		Directories: []string{"internal/service"},
		NodeIDs:     []string{"internal/service/service.go::New"},
		Evidence:    evidenceBundle(),
	}
	now := time.Now().UTC()
	snap := buildSnapshot(t, sampleGraph(t, "deadbeefdeadbeef"), "deadbeefdeadbeef", now, comp)

	if _, err := store.Create(snap); err != nil {
		t.Fatalf("store.Create: %v", err)
	}
	got, err := store.Get("deadbeef")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if len(got.Components) != 1 {
		t.Fatalf("snapshot components = %d, want 1", len(got.Components))
	}
	c := got.Components[0]
	if c.Kind != archmodel.ComponentService {
		t.Errorf("component Kind = %q, want %q", c.Kind, archmodel.ComponentService)
	}
	if len(c.Directories) != 1 || c.Directories[0] != "internal/service" {
		t.Errorf("component Directories = %v, want [internal/service]", c.Directories)
	}
}

func TestSnapshotDiffChangedComponents(t *testing.T) {
	sb := harness.NewSandbox(t)
	store, err := arch_timeline.NewSnapshotStore(sb.Path(".glassmarble", "snapshots"))
	if err != nil {
		t.Fatalf("arch_timeline.NewSnapshotStore: %v", err)
	}

	baseGraph := sampleGraph(t, "base")
	baseGraph.Nodes = baseGraph.Nodes.Delete("internal/service/service.go::New")
	baseGraph.Nodes = baseGraph.Nodes.Set("internal/service/service.go::New", &stage4.ResolvedNode{
		ID:   "internal/service/service.go::New",
		Kind: "FUNCTION",
		Name: "New",
		FileSpec: stage4.LocationMeta{Path: "internal/service/service.go", LineStart: 1, LineEnd: 3},
	})

	headGraph := baseGraph.Clone()
	headGraph.CommitHash = "head"
	headGraph.Nodes = headGraph.Nodes.Set("internal/cache/cache.go::Get", &stage4.ResolvedNode{
		ID:   "internal/cache/cache.go::Get",
		Kind: "METHOD",
		Name: "Get",
		FileSpec: stage4.LocationMeta{Path: "internal/cache/cache.go", LineStart: 1, LineEnd: 4},
	})
	headGraph.OutboundEdges = headGraph.OutboundEdges.Set("internal/service/service.go::New", []stage4.ResolvedEdge{{
		SourceID: "internal/service/service.go::New",
		TargetID: "internal/cache/cache.go::Get",
		Type:     stage4.EdgeCalls,
		LineNumber: 2,
	}})

	now := time.Now().UTC()
	baseSnap := buildSnapshot(t, baseGraph, "basecommit", now,
		archmodel.DetectedComponent{ID: "auth", Name: "auth", Evidence: evidenceBundle()},
	)
	headSnap := buildSnapshot(t, headGraph, "headcommit", now.Add(time.Minute),
		archmodel.DetectedComponent{ID: "auth", Name: "auth", Evidence: evidenceBundle()},
		archmodel.DetectedComponent{ID: "payment", Name: "payment", Evidence: evidenceBundle()},
	)

	// Distinct topologies -> both snapshots must actually be written.
	if created, err := store.Create(baseSnap); err != nil || !created {
		t.Fatalf("Create(baseSnap) = %v, %v", created, err)
	}
	if created, err := store.Create(headSnap); err != nil || !created {
		t.Fatalf("Create(headSnap) = %v, %v", created, err)
	}

	result := arch_timeline.Diff(baseSnap, headSnap)
	if result == nil {
		t.Fatal("Diff returned nil")
	}
	found := false
	for _, added := range result.Delta.AddedComponents {
		if added == "payment" {
			found = true
		}
	}
	if !found {
		t.Errorf("Diff AddedComponents = %v, want to include payment", result.Delta.AddedComponents)
	}
	if result.Graph == nil {
		t.Fatal("Diff produced no structural graph diff")
	}
	if len(result.Graph.NodesAdded) == 0 {
		t.Errorf("Graph diff NodesAdded empty, want cache.go::Get")
	}
}
