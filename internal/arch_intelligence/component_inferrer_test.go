package arch_intelligence

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

func TestInferComponents(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("n1", &stage4.ResolvedNode{
		ID:   "n1",
		Name: "testNode",
		FileSpec: stage4.LocationMeta{
			Path: "internal/service/foo.go",
		},
	})

	components := InferComponents(graph)
	if len(components) != 1 {
		t.Errorf("Expected 1 component, got %d", len(components))
	}
}
