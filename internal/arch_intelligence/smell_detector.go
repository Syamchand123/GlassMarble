package arch_intelligence

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// SmellRule defines a deterministic rule for identifying an architectural smell.
type SmellRule interface {
	ID() string
	Name() string
	Evaluate(ctx *RuleContext) []archmodel.ArchSmell
}

// newSmellEvidence builds the standard evidence bundle for a smell match.
func newSmellEvidence(ctx *RuleContext, ruleID, excerpt string, confidence float64) evidence.Bundle {
	b := evidence.Bundle{PrimarySource: evidence.SourceRule}
	b.Add(evidence.EvidenceItem{
		Source:     evidence.SourceRule,
		Reference:  ruleID,
		Excerpt:    excerpt,
		Confidence: confidence,
		Timestamp:  ctx.now(),
	})
	return b
}

// sortSmells orders smells deterministically: by kind, then title.
func sortSmells(smells []archmodel.ArchSmell) {
	sort.Slice(smells, func(i, j int) bool {
		if smells[i].Kind != smells[j].Kind {
			return smells[i].Kind < smells[j].Kind
		}
		return smells[i].Title < smells[j].Title
	})
}

// SD01GodObject detects God Objects: STRUCT/CLASS nodes exceeding both the
// fan-in and method-count thresholds.
type SD01GodObject struct{}

func (r *SD01GodObject) ID() string   { return "SD-01" }
func (r *SD01GodObject) Name() string { return "God Object" }
func (r *SD01GodObject) Evaluate(ctx *RuleContext) []archmodel.ArchSmell {
	if ctx.Graph == nil || ctx.Graph.Len() == 0 {
		return nil
	}
	cfg := ctx.cfgOrDefault()
	coupling := NodeMetricsSnapshot(ctx.Graph)

	var smells []archmodel.ArchSmell
	for _, id := range ctx.Graph.NodeIDs {
		node := ctx.Graph.Nodes[id]
		if node == nil || (node.Kind != "STRUCT" && node.Kind != "CLASS") {
			continue
		}
		if coupling[id].FanIn <= cfg.GodObjectFanInThreshold {
			continue
		}
		methodCount := 0
		for _, e := range ctx.Graph.Outbound[id] {
			if e.Type == stage4.EdgeHasReceiver || e.Type == stage4.EdgeContains {
				if t, ok := ctx.Graph.Nodes[e.TargetID]; ok && t.Kind == "FUNCTION" {
					methodCount++
				}
			}
		}
		if methodCount <= cfg.GodObjectMethodThreshold {
			continue
		}
		smells = append(smells, archmodel.ArchSmell{
			Kind:        archmodel.SmellGodObject,
			Title:       "God Object Detected: " + node.Name,
			Severity:    archmodel.SeverityHigh,
			AffectedIDs: []string{id},
			Evidence: newSmellEvidence(ctx, r.ID(),
				fmt.Sprintf("%s has fan-in %d and %d methods", node.Name, coupling[id].FanIn, methodCount), 0.9),
			Suggestion: "Split this large class into smaller, more cohesive components with focused responsibilities.",
		})
	}
	sortSmells(smells)
	return smells
}

// SD02CyclicDependency detects cyclic dependencies using SCC over structural
// edges. Severity scales with component size.
type SD02CyclicDependency struct{}

func (r *SD02CyclicDependency) ID() string   { return "SD-02" }
func (r *SD02CyclicDependency) Name() string { return "Cyclic Dependency" }
func (r *SD02CyclicDependency) Evaluate(ctx *RuleContext) []archmodel.ArchSmell {
	if ctx.Graph == nil || ctx.Graph.Len() == 0 {
		return nil
	}
	cfg := ctx.cfgOrDefault()
	sccs := SCCIterative(ctx.Graph)

	var smells []archmodel.ArchSmell
	for _, scc := range sccs {
		if len(scc) <= 1 {
			continue
		}
		sev := archmodel.SeverityMedium
		if len(scc) > cfg.LargeCycleThreshold {
			sev = archmodel.SeverityHigh
		} else if len(scc) <= cfg.SmallCycleThreshold {
			sev = archmodel.SeverityLow
		}
		smells = append(smells, archmodel.ArchSmell{
			Kind:        archmodel.SmellCyclicDependency,
			Title:       fmt.Sprintf("Cyclic Dependency Detected (%d nodes)", len(scc)),
			Severity:    sev,
			AffectedIDs: scc,
			Evidence: newSmellEvidence(ctx, r.ID(),
				fmt.Sprintf("Strongly Connected Component of %d nodes detected", len(scc)), 1.0),
			Suggestion: "Break the cycle by introducing interfaces (Dependency Inversion) or extracting shared logic to a new component.",
		})
	}
	sortSmells(smells)
	return smells
}

// SD03DeadCode detects nodes unreachable from entrypoints (excluding tests,
// mocks, vendored code and exported API surface).
type SD03DeadCode struct{}

func (r *SD03DeadCode) ID() string   { return "SD-03" }
func (r *SD03DeadCode) Name() string { return "Dead Code" }
func (r *SD03DeadCode) Evaluate(ctx *RuleContext) []archmodel.ArchSmell {
	if ctx.Graph == nil {
		return nil
	}
	dead := DeadCodeNodesSnapshot(ctx.Graph)
	if len(dead) == 0 {
		return nil
	}
	sort.Strings(dead)
	return []archmodel.ArchSmell{{
		Kind:        archmodel.SmellDeadCode,
		Title:       fmt.Sprintf("Dead Code Detected (%d nodes)", len(dead)),
		Severity:    archmodel.SeverityLow,
		AffectedIDs: dead,
		Evidence: newSmellEvidence(ctx, r.ID(),
			fmt.Sprintf("%d nodes unreachable from any entrypoint", len(dead)), 0.85),
		Suggestion: "Verify these components are truly unused and remove them to simplify the codebase.",
	}}
}

// SD04LayerViolation detects edges that violate declared layering: upward
// edges (deeper layer depending on a shallower one) and declared forbidden
// pairs. Requires config.arch_layers; otherwise no evidence of intent exists.
type SD04LayerViolation struct{}

func (r *SD04LayerViolation) ID() string   { return "SD-04" }
func (r *SD04LayerViolation) Name() string { return "Layer Violation" }
func (r *SD04LayerViolation) Evaluate(ctx *RuleContext) []archmodel.ArchSmell {
	if ctx.Graph == nil || ctx.LayerAssigner == nil || !ctx.LayerAssigner.Configured() {
		return nil
	}
	nodeLayer := make(map[string]string, ctx.Graph.Len())
	for _, id := range ctx.Graph.NodeIDs {
		node := ctx.Graph.Nodes[id]
		if node != nil {
			nodeLayer[id] = ctx.LayerAssigner.Assign(node.FileSpec.Path)
		}
	}

	var violations []archmodel.ArchSmell
	byPair := make(map[string][]string) // "src\x00tgt" -> node ids
	for _, id := range ctx.Graph.NodeIDs {
		srcLayer := nodeLayer[id]
		if srcLayer == "" {
			continue
		}
		for _, e := range ctx.Graph.structuralOutbound(id) {
			tgtLayer := nodeLayer[e.TargetID]
			if tgtLayer == "" || tgtLayer == srcLayer {
				continue
			}
			if ctx.LayerAssigner.IsForbidden(srcLayer, tgtLayer) ||
				ctx.LayerAssigner.IsUpward(srcLayer, tgtLayer) {
				key := srcLayer + "\x00" + tgtLayer
				byPair[key] = append(byPair[key], id)
			}
		}
	}
	keys := make([]string, 0, len(byPair))
	for k := range byPair {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts := strings.SplitN(key, "\x00", 2)
		ids := byPair[key]
		sort.Strings(ids)
		violations = append(violations, archmodel.ArchSmell{
			Kind:        archmodel.SmellLayerViolation,
			Title:       fmt.Sprintf("Layer Violation: %s depends on %s", parts[0], parts[1]),
			Severity:    archmodel.SeverityMedium,
			AffectedIDs: ids,
			Evidence: newSmellEvidence(ctx, r.ID(),
				fmt.Sprintf("%d edges from layer %q to layer %q violate the declared layering", len(ids), parts[0], parts[1]), 0.9),
			Suggestion: "Resolve the dependency in the correct direction or move the affected code to a lower layer.",
		})
	}
	sortSmells(violations)
	return violations
}

// SD05GodPackage detects god packages: a component holding more than
// GodPackageTrafficPct of the graph's nodes with incoming coupling from many
// other components.
type SD05GodPackage struct{}

func (r *SD05GodPackage) ID() string   { return "SD-05" }
func (r *SD05GodPackage) Name() string { return "God Package" }
func (r *SD05GodPackage) Evaluate(ctx *RuleContext) []archmodel.ArchSmell {
	if ctx.Graph == nil || len(ctx.Components) == 0 || len(ctx.ComponentCoupling) == 0 {
		return nil
	}
	cfg := ctx.cfgOrDefault()
	total := ctx.Graph.Len()
	if total == 0 {
		return nil
	}

	var smells []archmodel.ArchSmell
	for _, cc := range ctx.ComponentCoupling {
		share := float64(cc.Weight) / float64(total)
		if share*100 < cfg.GodPackageTrafficPct || cc.Ca < 3 {
			continue
		}
		smells = append(smells, archmodel.ArchSmell{
			Kind:        archmodel.SmellGodPackage,
			Title:       "God Package Detected: " + cc.Name,
			Severity:    archmodel.SeverityHigh,
			AffectedIDs: []string{cc.ComponentID},
			Evidence: newSmellEvidence(ctx, r.ID(),
				fmt.Sprintf("%s holds %.0f%% of the graph nodes with %d afferent components", cc.Name, share*100, cc.Ca), 0.85),
			Suggestion: "Split the package into focused modules to reduce coupling and improve cohesion.",
		})
	}
	sortSmells(smells)
	return smells
}

// SD06TightCoupling detects components coupled in both directions: high
// afferent and efferent coupling at the same time.
type SD06TightCoupling struct{}

func (r *SD06TightCoupling) ID() string   { return "SD-06" }
func (r *SD06TightCoupling) Name() string { return "Tight Coupling" }
func (r *SD06TightCoupling) Evaluate(ctx *RuleContext) []archmodel.ArchSmell {
	if len(ctx.ComponentCoupling) == 0 {
		return nil
	}
	var smells []archmodel.ArchSmell
	for _, cc := range ctx.ComponentCoupling {
		if cc.Ca < 5 || cc.Ce < 5 {
			continue
		}
		smells = append(smells, archmodel.ArchSmell{
			Kind:        archmodel.SmellTightCoupling,
			Title:       "Tight Coupling Detected: " + cc.Name,
			Severity:    archmodel.SeverityMedium,
			AffectedIDs: []string{cc.ComponentID},
			Evidence: newSmellEvidence(ctx, r.ID(),
				fmt.Sprintf("%s has %d afferent and %d efferent component dependencies", cc.Name, cc.Ca, cc.Ce), 0.85),
			Suggestion: "Reduce cross-component coupling by introducing interfaces or event-driven communication.",
		})
	}
	sortSmells(smells)
	return smells
}

// SD07UnstableAbstraction detects components with near-maximal instability:
// they depend on many components but nothing depends on them.
type SD07UnstableAbstraction struct{}

func (r *SD07UnstableAbstraction) ID() string   { return "SD-07" }
func (r *SD07UnstableAbstraction) Name() string { return "Unstable Abstraction" }
func (r *SD07UnstableAbstraction) Evaluate(ctx *RuleContext) []archmodel.ArchSmell {
	if len(ctx.ComponentCoupling) == 0 {
		return nil
	}
	cfg := ctx.cfgOrDefault()
	var smells []archmodel.ArchSmell
	for _, cc := range ctx.ComponentCoupling {
		if cc.Ce < 3 || cc.Instability < cfg.UnstableThreshold {
			continue
		}
		smells = append(smells, archmodel.ArchSmell{
			Kind:        archmodel.SmellUnstableAbstraction,
			Title:       "Unstable Abstraction: " + cc.Name,
			Severity:    archmodel.SeverityLow,
			AffectedIDs: []string{cc.ComponentID},
			Evidence: newSmellEvidence(ctx, r.ID(),
				fmt.Sprintf("%s has instability %.2f (%d efferent, %d afferent)", cc.Name, cc.Instability, cc.Ce, cc.Ca), 0.7),
			Suggestion: "Make the component more abstract (inverted dependencies) so its interface stabilizes.",
		})
	}
	sortSmells(smells)
	return smells
}

// RunSmellDetection runs all defined smell rules against the graph.
// Compatibility wrapper: builds a snapshot-based context from the graph.
func RunSmellDetection(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) []archmodel.ArchSmell {
	if graph == nil {
		return nil
	}
	ctx := &RuleContext{
		Graph:   NewGraphSnapshot(graph),
		Metrics: metrics,
		Cfg:     config.DefaultIntelligenceConfig(),
		Clock:   time.Now,
	}
	return RunSmellDetectionContext(ctx)
}

// RunSmellDetectionContext runs all smell rules against a shared context.
func RunSmellDetectionContext(ctx *RuleContext) []archmodel.ArchSmell {
	rules := []SmellRule{
		&SD01GodObject{},
		&SD02CyclicDependency{},
		&SD03DeadCode{},
		&SD04LayerViolation{},
		&SD05GodPackage{},
		&SD06TightCoupling{},
		&SD07UnstableAbstraction{},
	}
	var smells []archmodel.ArchSmell
	for _, rule := range rules {
		smells = append(smells, rule.Evaluate(ctx)...)
	}
	sortSmells(smells)
	return smells
}
