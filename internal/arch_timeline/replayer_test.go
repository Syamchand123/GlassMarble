package arch_timeline

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// TestReplay_RoundTrip: the graph embedded in a snapshot must come back
// structurally identical (no node/edge drift through JSON).
func TestReplay_RoundTrip(t *testing.T) {
	graph := akg.NewCodePropertyGraph("c1")
	graph.Nodes = graph.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "A"})
	graph.Nodes = graph.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "B"})
	e1 := link.ResolvedEdge{SourceID: "a", TargetID: "b", Type: link.EdgeCalls, LineNumber: 1}
	graph.OutboundEdges = graph.OutboundEdges.Set("a", []link.ResolvedEdge{e1})
	graph.InboundEdges = graph.InboundEdges.Set("b", []link.ResolvedEdge{e1})

	snap, err := BuildSnapshot(SnapshotInput{Graph: graph, CommitHash: "c1", Timestamp: snapBaseTime})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	restored, err := Replay(snap)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	diff := akg.DiffGraphs(graph, restored)
	if len(diff.NodesAdded) != 0 || len(diff.NodesRemoved) != 0 ||
		len(diff.EdgesAdded) != 0 || len(diff.EdgesRemoved) != 0 {
		t.Errorf("round-trip drift: %+v", diff)
	}
	if restored.Nodes.Len() != 2 {
		t.Errorf("restored node count = %d, want 2", restored.Nodes.Len())
	}
}

// TestReplay_Errors: nil snapshots and --no-graph snapshots must error
// clearly, never panic.
func TestReplay_Errors(t *testing.T) {
	if _, err := Replay(nil); err == nil {
		t.Error("Replay(nil) must error")
	}

	noGraph, err := BuildSnapshot(SnapshotInput{
		Graph:      nil,
		CommitHash: "c1",
		Timestamp:  snapBaseTime,
		NoGraph:    true,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot(no-graph): %v", err)
	}
	if _, err := Replay(noGraph); err == nil {
		t.Error("Replay on a --no-graph snapshot must error")
	}
}
