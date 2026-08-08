package arch_timeline

import (
	"os"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

var snapBaseTime = time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)

func buildSnapshotGraph() *akg.CodePropertyGraph {
	g := akg.NewCodePropertyGraph("c1")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "A"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "B"})
	edge := stage4.ResolvedEdge{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls, LineNumber: 1}
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{edge})
	g.InboundEdges = g.InboundEdges.Set("b", []stage4.ResolvedEdge{edge})
	return g
}

func snapshotInput(commit string) SnapshotInput {
	return SnapshotInput{
		Graph:      buildSnapshotGraph(),
		CommitHash: commit,
		Version:    "1.0",
		Timestamp:  snapBaseTime,
		Components: []archmodel.DetectedComponent{{
			ID: "c1", Name: "PaymentService", Kind: archmodel.ComponentService,
			Evidence: evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceCode, Reference: "a", Confidence: 0.95}),
		}},
		Patterns: []archmodel.DetectedPattern{{Kind: archmodel.PatternHexagonal, Name: "hex", Components: []string{"c1"}}},
		Smells:   []archmodel.ArchSmell{{Kind: archmodel.SmellGodObject, Title: "god", Severity: archmodel.SeverityHigh}},
		Metrics:  archmodel.ArchMetrics{TotalNodes: 2, TotalEdges: 1, CycleCount: 0},
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
	if len(snapA.ID) < 13 || snapA.ID[:5] != "snap_" {
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
	snap, err := BuildSnapshot(in)
	if err != nil {
		t.Fatalf("BuildSnapshot with nil graph: %v", err)
	}
	if snap.NodeCount != 0 || len(snap.AKGJSON) != 0 {
		t.Errorf("nil graph must produce zero counts/empty AKGJSON")
	}
	if snap.ID == "" {
		t.Errorf("nil graph snapshot must still get a deterministic ID")
	}
}

func TestBuildSnapshot_StoreIntegration(t *testing.T) {
	dir, err := os.MkdirTemp("", "snapshot_build_test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)
	store := NewSnapshotStore(dir)

	// Same topology, two different commits → snapshot created only once.
	in1 := snapshotInput("commit-aaaa1")
	snap1, err := BuildSnapshot(in1)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if err := store.Create(snap1); err != nil {
		t.Fatalf("Create: %v", err)
	}

	in2 := snapshotInput("commit-aaaa2")
	snap2, err := BuildSnapshot(in2)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if err := store.Create(snap2); err != nil {
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
	in3.Graph.Nodes = in3.Graph.Nodes.Set("c", &stage4.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "C"})
	edge := stage4.ResolvedEdge{SourceID: "a", TargetID: "c", Type: stage4.EdgeCalls, LineNumber: 2}
	in3.Graph.OutboundEdges = in3.Graph.OutboundEdges.Set("a", []stage4.ResolvedEdge{
		{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls, LineNumber: 1},
		edge,
	})
	in3.Graph.InboundEdges = in3.Graph.InboundEdges.Set("c", []stage4.ResolvedEdge{edge})
	snap3, err := BuildSnapshot(in3)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if err := store.Create(snap3); err != nil {
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
