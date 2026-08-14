package arch_intelligence

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

func TestCalculatePageRank(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("n1", &link.ResolvedNode{ID: "n1", Name: "n1"})

	ranks := PageRank(graph, 10, 0.85)
	if len(ranks) != 1 {
		t.Errorf("Expected 1 rank, got %d", len(ranks))
	}
}

func TestCalculateLCOM4(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	node := &link.ResolvedNode{ID: "n1", Name: "n1"}
	graph.Nodes = graph.Nodes.Set("n1", node)

	lcom4 := LCOM4(node, graph)
	if lcom4 != 0 {
		t.Errorf("Expected 0 LCOM4")
	}
}
