package arch_timeline

import (
	"os"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

var snapBaseTime = time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)

func buildSnapshotGraph() *akg.CodePropertyGraph {
	g := akg.NewCodePropertyGraph("c1")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "A"})
	g.Nodes = g.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "B"})
	edge := link.ResolvedEdge{SourceID: "a", TargetID: "b", Type: link.EdgeCalls, LineNumber: 1}
	g.OutboundEdges = g.OutboundEdges.Set("a", []link.ResolvedEdge{edge})
	g.InboundEdges = g.InboundEdges.Set("b", []link.ResolvedEdge{edge})
	return g
}

var evidenceItem = evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceCode, Reference: "a", Confidence: 0.95})

func snapshotInput(commit string) SnapshotInput {
	return SnapshotInput{
		Graph:      buildSnapshotGraph(),
		CommitHash: commit,
		Version:    "1.0",
		Timestamp:  snapBaseTime,
		Components: []archmodel.DetectedComponent{{
			ID: "c1", Name: "PaymentService", Kind: archmodel.ComponentService,
			NodeIDs:  []string{"a", "b"},
			Evidence: evidenceItem,
		}},
		Patterns: []archmodel.DetectedPattern{{
			Kind: archmodel.PatternHexagonal, Name: "hex", Components: []string{"c1"},
			Evidence: evidenceItem,
		}},
		Smells: []archmodel.ArchSmell{{
			Kind: archmodel.SmellGodObject, Title: "god", Severity: archmodel.SeverityHigh,
			Evidence: evidenceItem,
		}},
		Metrics: archmodel.ArchMetrics{TotalNodes: 2, TotalEdges: 1, CycleCount: 0, LCOM4: 2.0},
	}
}

func TestBuildSnapshot_DeterministicID(t *testing.T) {
	a := snapshotInput("c1")
	b := snapshotInput("c1")
	snapA, err := BuildSnapshot(a)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	snapB, err := BuildSnapshot(b)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if snapA.ID != snapB.ID {
		t.Errorf("ID not deterministic: %q vs %q", snapA.ID, snapB.ID)
	}
	if len(snapA.ID) != 21 || snapA.ID[:5] != "snap_" {
		t.Errorf("unexpected snapshot ID format: %q", snapA.ID)
	}

	c := snapshotInput("c2")
	snapC, err := BuildSnapshot(c)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if snapC.ID == snapA.ID {
		t.Errorf("different commits must produce different snapshot IDs")
	}
}

func TestBuildSnapshot_EmbedsGraphAndCounts(t *testing.T) {
	snap, err := BuildSnapshot(snapshotInput("c1"))
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if snap.NodeCount != 2 || snap.EdgeCount != 1 {
		t.Errorf("node/edge counts = %d/%d, want 2/1", snap.NodeCount, snap.EdgeCount)
	}
	if len(snap.AKGJSON) == 0 {
		t.Errorf("AKGJSON not embedded")
	}
	if snap.CommitHash != "c1" || snap.Timestamp.IsZero() {
		t.Errorf("commit/timestamp not carried: %+v", snap)
	}
	if len(snap.Components) != 1 || len(snap.Patterns) != 1 || len(snap.Smells) != 1 {
		t.Errorf("analysis results not carried through: %+v", snap)
	}
	// BuildSnapshot must leave topology computation to SnapshotStore.
	if snap.TopologyHash != "" {
		t.Errorf("TopologyHash = %q, want empty (computed by SnapshotStore)", snap.TopologyHash)
	}
}

func TestBuildSnapshot_NilGraph(t *testing.T) {
	in := snapshotInput("c1")
	in.Graph = nil
	if _, err := BuildSnapshot(in); err == nil {
		t.Fatal("expected an error building a snapshot without a graph when NoGraph is not set")
	}

	in.NoGraph = true
	snap, err := BuildSnapshot(in)
	if err != nil {
		t.Fatalf("BuildSnapshot with NoGraph: %v", err)
	}
	if snap.NodeCount != 0 || len(snap.AKGJSON) != 0 {
		t.Errorf("NoGraph snapshot must have zero counts and no AKGJSON")
	}
	if snap.ID == "" {
		t.Errorf("NoGraph snapshot must still get a deterministic ID")
	}
}

func TestBuildSnapshot_NoGraphIDDiffersFromFull(t *testing.T) {
	full, err := BuildSnapshot(snapshotInput("c1"))
	if err != nil {
		t.Fatalf("BuildSnapshot(full): %v", err)
	}
	ng := snapshotInput("c1")
	ng.NoGraph = true
	noGraph, err := BuildSnapshot(ng)
	if err != nil {
		t.Fatalf("BuildSnapshot(no-graph): %v", err)
	}
	if full.ID == noGraph.ID {
		t.Errorf("full and --no-graph snapshots of the same commit must not share an ID")
	}
}

func TestBuildSnapshot_ZeroTimestamp(t *testing.T) {
	in := snapshotInput("c1")
	in.Timestamp = time.Time{}
	if _, err := BuildSnapshot(in); err == nil {
		t.Fatal("expected an error for a zero timestamp")
	}
}

func TestBuildSnapshot_EvidenceValidation(t *testing.T) {
	in := snapshotInput("c1")
	in.Components[0].Evidence = evidence.Bundle{}
	if _, err := BuildSnapshot(in); err == nil {
		t.Fatal("expected an error when a component carries no evidence")
	}

	in = snapshotInput("c1")
	in.Patterns[0].Evidence = evidence.Bundle{}
	if _, err := BuildSnapshot(in); err == nil {
		t.Fatal("expected an error when a pattern carries no evidence")
	}

	in = snapshotInput("c1")
	in.Smells[0].Evidence = evidence.Bundle{}
	if _, err := BuildSnapshot(in); err == nil {
		t.Fatal("expected an error when a smell carries no evidence")
	}
}

// TestBuildSnapshot_FingerprintSensitivity: fingerprints must fold in
// evidence, confidence, node membership, dependencies and metrics, so any of
// them changes the snapshot ID.
func TestBuildSnapshot_FingerprintSensitivity(t *testing.T) {
	ids := make(map[string]bool)
	base, err := BuildSnapshot(snapshotInput("c1"))
	if err != nil {
		t.Fatalf("base: %v", err)
	}
	ids[base.ID] = true

	mutate := func(f func(*SnapshotInput)) string {
		in := snapshotInput("c1")
		f(&in)
		snap, err := BuildSnapshot(in)
		if err != nil {
			t.Fatalf("mutated build: %v", err)
		}
		ids[snap.ID] = true
		return snap.ID
	}

	// Evidence change.
	if id := mutate(func(in *SnapshotInput) {
		in.Components[0].Evidence = evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Reference: "z", Confidence: 0.5})
	}); id == base.ID {
		t.Error("evidence change must change the ID")
	}
	// Confidence change.
	if id := mutate(func(in *SnapshotInput) {
		in.Components[0].Confidence = 0.42
	}); id == base.ID {
		t.Error("confidence change must change the ID")
	}
	// Node membership change.
	if id := mutate(func(in *SnapshotInput) {
		in.Components[0].NodeIDs = []string{"a"}
	}); id == base.ID {
		t.Error("node membership change must change the ID")
	}
	// Dependency change.
	if id := mutate(func(in *SnapshotInput) {
		in.Components[0].Dependencies = []string{"c2"}
	}); id == base.ID {
		t.Error("dependency change must change the ID")
	}
	// Metric change.
	if id := mutate(func(in *SnapshotInput) {
		in.Metrics.CycleCount = 3
	}); id == base.ID {
		t.Error("metrics change must change the ID")
	}
	if len(ids) != 6 {
		t.Errorf("expected 6 distinct IDs across base+5 mutations, got %d", len(ids))
	}
}

func TestBuildSnapshot_StoreIntegration(t *testing.T) {
	dir, err := os.MkdirTemp("", "snapshot_build_test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)
	store, err := NewSnapshotStore(dir)
	if err != nil {
		t.Fatalf("NewSnapshotStore: %v", err)
	}

	// Same topology, two different commits → snapshot created only once.
	in1 := snapshotInput("commit-aaaa1")
	snap1, err := BuildSnapshot(in1)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if _, err := store.Create(snap1); err != nil {
		t.Fatalf("Create: %v", err)
	}

	in2 := snapshotInput("commit-aaaa2")
	snap2, err := BuildSnapshot(in2)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if _, err := store.Create(snap2); err != nil {
		t.Fatalf("Create: %v", err)
	}

	entries := store.List()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 (unchanged topology must skip the write)", len(entries))
	}
	if entries[0].TopologyHash == "" {
		t.Errorf("SnapshotStore must compute the topology hash")
	}

	// Changing the graph must produce a new snapshot.
	in3 := snapshotInput("commit-aaaa3")
	in3.Graph.Nodes = in3.Graph.Nodes.Set("c", &link.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "C"})
	edge := link.ResolvedEdge{SourceID: "a", TargetID: "c", Type: link.EdgeCalls, LineNumber: 2}
	in3.Graph.OutboundEdges = in3.Graph.OutboundEdges.Set("a", []link.ResolvedEdge{
		{SourceID: "a", TargetID: "b", Type: link.EdgeCalls, LineNumber: 1},
		edge,
	})
	in3.Graph.InboundEdges = in3.Graph.InboundEdges.Set("c", []link.ResolvedEdge{edge})
	snap3, err := BuildSnapshot(in3)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if _, err := store.Create(snap3); err != nil {
		t.Fatalf("Create: %v", err)
	}
	entries = store.List()
	if len(entries) != 2 {
		t.Errorf("entries = %d, want 2 after topology change", len(entries))
	}

	latest, err := store.Latest()
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest.CommitHash != "commit-aaaa3" {
		t.Errorf("latest commit = %q, want commit-aaaa3", latest.CommitHash)
	}
}
