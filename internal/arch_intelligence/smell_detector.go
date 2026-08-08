package arch_intelligence

import (
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// SmellRule defines a deterministic rule for identifying an architectural smell.
type SmellRule interface {
	ID() string
	Name() string
	Evaluate(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) []archmodel.ArchSmell
}

// SD01GodObject detects God Objects.
type SD01GodObject struct{}

func (r *SD01GodObject) ID() string   { return "SD-01" }
func (r *SD01GodObject) Name() string { return "God Object" }
func (r *SD01GodObject) Evaluate(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) []archmodel.ArchSmell {
	var smells []archmodel.ArchSmell

	// Thresholds: FanIn > 15 AND method count > 30 (adjust as needed)
	fanInThreshold := 15
	methodCountThreshold := 30

	coupling := NodeMetrics(graph)

	graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		if node.Kind != "STRUCT" && node.Kind != "CLASS" {
			return
		}

		metrics := coupling[id]
		if metrics.FanIn > fanInThreshold {
			methodCount := 0
			for _, e := range graph.SafeGetOutboundEdges(id) {
				if e.Type == stage4.EdgeHasReceiver || e.Type == stage4.EdgeContains {
					if t, ok := graph.SafeGetNode(e.TargetID); ok && t.Kind == "FUNCTION" {
						methodCount++
					}
				}
			}

			if methodCount > methodCountThreshold {
				b := evidence.Bundle{}
				b.Add(evidence.EvidenceItem{
					Source:     evidence.SourceRule,
					Reference:  "SD-01",
					Excerpt:    "Node exceeds God Object thresholds.",
					Confidence: 0.9,
					Timestamp:  time.Now(),
				})

				smells = append(smells, archmodel.ArchSmell{
					Kind:        archmodel.SmellGodObject,
					Title:       "God Object Detected: " + node.Name,
					Severity:    archmodel.SeverityHigh,
					AffectedIDs: []string{id},
					Evidence:    b,
					Suggestion:  "Split this large class into smaller, more cohesive components with focused responsibilities.",
				})
			}
		}
	})

	return smells
}

// SD02CyclicDependency detects cyclic dependencies using SCC.
type SD02CyclicDependency struct{}

func (r *SD02CyclicDependency) ID() string   { return "SD-02" }
func (r *SD02CyclicDependency) Name() string { return "Cyclic Dependency" }
func (r *SD02CyclicDependency) Evaluate(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) []archmodel.ArchSmell {
	var smells []archmodel.ArchSmell
	sccs := SCC(graph)

	for _, scc := range sccs {
		if len(scc) > 1 {
			sev := archmodel.SeverityMedium
			if len(scc) > 5 {
				sev = archmodel.SeverityHigh
			}

			b := evidence.Bundle{}
			b.Add(evidence.EvidenceItem{
				Source:     evidence.SourceRule,
				Reference:  "SD-02",
				Excerpt:    "Strongly Connected Component of size > 1 detected.",
				Confidence: 1.0,
				Timestamp:  time.Now(),
			})

			smells = append(smells, archmodel.ArchSmell{
				Kind:        archmodel.SmellCyclicDependency,
				Title:       "Cyclic Dependency Detected",
				Severity:    sev,
				AffectedIDs: scc,
				Evidence:    b,
				Suggestion:  "Break the cycle by introducing interfaces (Dependency Inversion) or extracting shared logic to a new component.",
			})
		}
	}

	return smells
}

// SD03DeadCode detects unreachable nodes.
type SD03DeadCode struct{}

func (r *SD03DeadCode) ID() string   { return "SD-03" }
func (r *SD03DeadCode) Name() string { return "Dead Code" }
func (r *SD03DeadCode) Evaluate(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) []archmodel.ArchSmell {
	var smells []archmodel.ArchSmell
	deadNodes := DeadCodeNodes(graph)

	var actualDead []string
	for _, id := range deadNodes {
		if node, ok := graph.SafeGetNode(id); ok {
			// Exclude tests and generated code
			if !strings.HasSuffix(node.FileSpec.Path, "_test.go") && !strings.Contains(node.FileSpec.Path, "mock") {
				if node.Kind == "FUNCTION" || node.Kind == "STRUCT" || node.Kind == "CLASS" {
					actualDead = append(actualDead, id)
				}
			}
		}
	}

	if len(actualDead) > 0 {
		b := evidence.Bundle{}
		b.Add(evidence.EvidenceItem{
			Source:     evidence.SourceRule,
			Reference:  "SD-03",
			Excerpt:    "Nodes unreachable from any known entrypoint.",
			Confidence: 0.85, // Could be false positive if entrypoints are incomplete
			Timestamp:  time.Now(),
		})

		smells = append(smells, archmodel.ArchSmell{
			Kind:        archmodel.SmellDeadCode,
			Title:       "Dead Code Detected",
			Severity:    archmodel.SeverityLow,
			AffectedIDs: actualDead,
			Evidence:    b,
			Suggestion:  "Verify these components are truly unused and remove them to simplify the codebase.",
		})
	}

	return smells
}

// SD04LayerViolation detects edges violating the standard layered architecture.
type SD04LayerViolation struct{}

func (r *SD04LayerViolation) ID() string   { return "SD-04" }
func (r *SD04LayerViolation) Name() string { return "Layer Violation" }
func (r *SD04LayerViolation) Evaluate(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) []archmodel.ArchSmell {
	// (Simplified for now, real logic would use drift.layerIndex)
	return nil
}

// SD05GodPackage detects god packages/services.
type SD05GodPackage struct{}

func (r *SD05GodPackage) ID() string   { return "SD-05" }
func (r *SD05GodPackage) Name() string { return "God Package" }
func (r *SD05GodPackage) Evaluate(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) []archmodel.ArchSmell {
	// (Simplified)
	return nil
}

// RunSmellDetection runs all defined smell rules against the graph.
func RunSmellDetection(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) []archmodel.ArchSmell {
	rules := []SmellRule{
		&SD01GodObject{},
		&SD02CyclicDependency{},
		&SD03DeadCode{},
		&SD04LayerViolation{},
		&SD05GodPackage{},
	}

	var smells []archmodel.ArchSmell
	for _, rule := range rules {
		detected := rule.Evaluate(graph, metrics)
		smells = append(smells, detected...)
	}

	return smells
}
