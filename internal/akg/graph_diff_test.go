package akg

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

func buildDiffBase() *CodePropertyGraph {
	g := NewCodePropertyGraph("basehash")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "A", FileSpec: stage4.LocationMeta{Path: "a.go"}})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "B", FileSpec: stage4.LocationMeta{Path: "b.go"}})
	addEdgeToGraph(g, "a", "b", stage4.EdgeCalls, 1)
	return g
}

// TestDiffGraphsDetectsAddRemove verifies node and edge additions/removals.
func TestDiffGraphsDetectsAddRemove(t *testing.T) {
	base := buildDiffBase()

	head := buildDiffBase()
	head.CommitHash = "headhash"
	head.Nodes = head.Nodes.Set("c", &stage4.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "C", FileSpec: stage4.LocationMeta{Path: "c.go"}})
	// b removed; edge a->b removed; new edge a->c added.
	head.Nodes = head.Nodes.Delete("b")
	head.OutboundEdges = head.OutboundEdges.Delete("a")
	head.InboundEdges = head.InboundEdges.Delete("b")
	addEdgeToGraph(head, "a", "c", stage4.EdgeCalls, 5)

	diff := DiffGraphs(base, head)
	if len(diff.NodesAdded) != 1 || diff.NodesAdded[0].ID != "c" {
		t.Errorf("NodesAdded = %+v, want [c]", diff.NodesAdded)
	}
	if len(diff.NodesRemoved) != 1 || diff.NodesRemoved[0].ID != "b" {
		t.Errorf("NodesRemoved = %+v, want [b]", diff.NodesRemoved)
	}
	if len(diff.EdgesAdded) != 1 || diff.EdgesAdded[0].TargetID != "c" {
		t.Errorf("EdgesAdded = %+v, want a->c", diff.EdgesAdded)
	}
	if len(diff.EdgesRemoved) != 1 || diff.EdgesRemoved[0].TargetID != "b" {
		t.Errorf("EdgesRemoved = %+v, want a->b", diff.EdgesRemoved)
	}
	if diff.BaseCommit != "basehash" || diff.HeadCommit != "headhash" {
		t.Errorf("commits = %q / %q", diff.BaseCommit, diff.HeadCommit)
	}
}

// TestDiffGraphsIdentical verifies no diff for identical snapshots.
func TestDiffGraphsIdentical(t *testing.T) {
	g := buildDiffBase()
	diff := DiffGraphs(g, g)
	if len(diff.NodesAdded)+len(diff.NodesRemoved)+len(diff.EdgesAdded)+len(diff.EdgesRemoved) != 0 {
		t.Errorf("identical snapshots produced a diff: %+v", diff)
	}
}

// TestDiffGraphsFilesChanged dedups and sorts touched file paths.
func TestDiffGraphsFilesChanged(t *testing.T) {
	base := buildDiffBase()
	head := buildDiffBase()
	head.Nodes = head.Nodes.Set("c", &stage4.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "C", FileSpec: stage4.LocationMeta{Path: "c.go"}})
	head.Nodes = head.Nodes.Delete("b")

	diff := DiffGraphs(base, head)
	want := []string{"b.go", "c.go"}
	if len(diff.FilesChanged) != 2 || diff.FilesChanged[0] != "b.go" || diff.FilesChanged[1] != "c.go" {
		t.Errorf("FilesChanged = %v, want %v", diff.FilesChanged, want)
	}
}

// TestDiffGraphsNil tolerates nil base/head.
func TestDiffGraphsNil(t *testing.T) {
	diff := DiffGraphs(nil, nil)
	if len(diff.NodesAdded) != 0 || len(diff.EdgesAdded) != 0 {
		t.Errorf("nil diff should be empty: %+v", diff)
	}
}
