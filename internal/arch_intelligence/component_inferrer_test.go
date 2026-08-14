package arch_intelligence

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

func TestInferComponents(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("n1", &link.ResolvedNode{
		ID:   "n1",
		Name: "testNode",
		FileSpec: link.LocationMeta{
			Path: "internal/service/foo.go",
		},
	})

	components := InferComponents(graph)
	if len(components) != 1 {
		t.Errorf("Expected 1 component, got %d", len(components))
	}
}
