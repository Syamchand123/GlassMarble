package arch_intelligence

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
)

func TestEngine_Run(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")

	engine := NewEngine(graph)
	res := engine.Run()

	if res.Metrics.TotalNodes != 0 {
		t.Errorf("expected 0 total nodes, got %d", res.Metrics.TotalNodes)
	}
}
