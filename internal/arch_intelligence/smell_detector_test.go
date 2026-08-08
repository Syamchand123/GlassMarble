package arch_intelligence

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

func TestRunSmellDetection(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("god", &stage4.ResolvedNode{
		ID: "god",
		Name: "GodClass",
	})
	for i := 0; i < 20; i++ {
		strID := string(rune(i))
		graph.Nodes = graph.Nodes.Set(strID, &stage4.ResolvedNode{ID: strID})
		edges, _ := graph.OutboundEdges.Get(strID)
		edges = append(edges, stage4.ResolvedEdge{
			SourceID: strID,
			TargetID: "god",
			Type:     "CALLS",
		})
		graph.OutboundEdges = graph.OutboundEdges.Set(strID, edges)
	}

	metrics := archmodel.ArchMetrics{
		MaxFanIn: 20,
	}
	smells := RunSmellDetection(graph, metrics)

	if len(smells) != 0 {
		t.Errorf("Expected 0 smells in mock graph")
	}
}
