package arch_intelligence

import (
	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// Stage5Result holds all insights derived from a single graph snapshot.
type Stage5Result struct {
	Metrics    archmodel.ArchMetrics
	Components []archmodel.DetectedComponent
	Patterns   []archmodel.DetectedPattern
	Smells     []archmodel.ArchSmell
}

// Engine coordinates the execution of graph analytics, pattern detection,
// and component inference (Stage 5A-5D).
type Engine struct {
	graph *akg.CodePropertyGraph
}

// NewEngine creates a new Stage 5 engine.
func NewEngine(graph *akg.CodePropertyGraph) *Engine {
	return &Engine{
		graph: graph,
	}
}

// Run executes the full Stage 5 pipeline.
func (e *Engine) Run() Stage5Result {
	metrics := CalculateMetrics(e.graph)
	components := InferComponents(e.graph)
	patterns := RunPatternDetection(e.graph, metrics)
	smells := RunSmellDetection(e.graph, metrics)

	return Stage5Result{
		Metrics:    metrics,
		Components: components,
		Patterns:   patterns,
		Smells:     smells,
	}
}
