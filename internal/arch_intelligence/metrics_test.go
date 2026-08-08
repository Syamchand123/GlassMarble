package arch_intelligence

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
)

func TestCalculateMetrics(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	metrics := CalculateMetrics(graph)

	if metrics.TotalNodes != 0 {
		t.Errorf("Expected 0 nodes, got %d", metrics.TotalNodes)
	}
	if metrics.TotalEdges != 0 {
		t.Errorf("Expected 0 edges, got %d", metrics.TotalEdges)
	}
}
