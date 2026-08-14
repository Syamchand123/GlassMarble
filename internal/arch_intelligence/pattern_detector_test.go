package arch_intelligence

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

func TestRunPatternDetection(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("n1", &link.ResolvedNode{
		ID:   "n1",
		Name: "UserRepository",
	})

	metrics := archmodel.ArchMetrics{}
	patterns := RunPatternDetection(graph, metrics)

	if len(patterns) != 0 {
		t.Errorf("Expected 0 Patterns in mock graph")
	}
}
