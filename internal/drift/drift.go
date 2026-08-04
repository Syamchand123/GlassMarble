// Package drift compares the committed AKG against declared architecture
// invariants (layering, forbidden dependencies, and cycle budgets) so `gmb
// drift` can report where the codebase has deviated from its intent.
package drift

import (
	"path"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/config"
)

// ViolationKind discriminates drift findings.
type ViolationKind string

const (
	// KindForbiddenDep is an edge that crosses a layer boundary declared
	// forbidden by config.drift.forbidden_deps.
	KindForbiddenDep ViolationKind = "FORBIDDEN_DEPENDENCY"
	// KindCycle is a dependency cycle that exceeds the declared cycle budget.
	KindCycle ViolationKind = "CYCLE"
)

// Violation is a single drift finding with enough context to be actionable.
type Violation struct {
	Kind        ViolationKind `json:"kind"`
	SourceID    string        `json:"source_id"`
	TargetID    string        `json:"target_id"`
	SourceLayer string        `json:"source_layer,omitempty"`
	TargetLayer string        `json:"target_layer,omitempty"`
	EdgeType    string        `json:"edge_type,omitempty"`
	Message     string        `json:"message"`
}

// Report is the complete drift assessment of one AKG snapshot.
type Report struct {
	CheckedAt      string      `json:"-"`
	LayersDefined  int         `json:"layers_defined"`
	Violations     []Violation `json:"violations"`
	CycleCount     int         `json:"cycle_count"`
	CycleBudget    int         `json:"cycle_budget"`
	ForbiddenEdges int         `json:"forbidden_dependencies"`
}

// LayerIndex assigns nodes to layers by path glob.
type layerIndex struct {
	layers []config.DriftLayer
}

// Assign returns the name of the first layer whose glob matches relPath, or "".
func (li *layerIndex) Assign(relPath string) string {
	clean := path.Clean(strings.ReplaceAll(relPath, "\\", "/"))
	for _, layer := range li.layers {
		for _, pat := range layer.Paths {
			if matched, err := path.Match(pat, clean); err == nil && matched {
				return layer.Name
			}
			// Also match a prefix glob against every parent directory so
			// "internal/**" catches "internal/db/db.go".
			if strings.HasSuffix(pat, "/**") {
				prefix := strings.TrimSuffix(pat, "/**")
				if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
					return layer.Name
				}
			}
		}
	}
	return ""
}

// Analyze evaluates the snapshot against the declared drift configuration.
func Analyze(graph *akg.CodePropertyGraph, driftCfg config.DriftConfig) *Report {
	rep := &Report{
		LayersDefined: len(driftCfg.Layers),
		CycleBudget:   driftCfg.CycleBudget,
	}
	if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
		return rep
	}

	li := &layerIndex{layers: driftCfg.Layers}

	// nodeLayer caches file->layer assignment per node id. Node file paths are
	// absolute on disk; strip any project prefix by matching the trailing path.
	nodeLayer := make(map[string]string)
	layerGraph := make(map[string]map[string]bool) // layer -> set of layers it depends on
	layerEdgeCount := make(map[string]int)         // "src\x00tgt" -> count

	graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		nodeLayer[id] = li.Assign(node.FileSpec.Path)
	})

	graph.OutboundEdges.Iterate(func(srcID string, edges []stage4.ResolvedEdge) {
		for _, e := range edges {
			srcLayer := nodeLayer[srcID]
			tgtLayer := nodeLayer[e.TargetID]
			if srcLayer == "" || tgtLayer == "" || srcLayer == tgtLayer {
				continue
			}
			key := srcLayer + "\x00" + tgtLayer
			layerEdgeCount[key]++
			if layerGraph[srcLayer] == nil {
				layerGraph[srcLayer] = make(map[string]bool)
			}
			layerGraph[srcLayer][tgtLayer] = true
		}
	})

	// Forbidden dependencies: every layer pair that appears in the config and
	// actually has edges must be reported (one violation per source node edge).
	forbidden := make(map[string]bool)
	for _, rule := range driftCfg.ForbiddenDeps {
		if rule.Source == "" || rule.Target == "" {
			continue
		}
		forbidden[rule.Source+"\x00"+rule.Target] = true
	}

	graph.OutboundEdges.Iterate(func(srcID string, edges []stage4.ResolvedEdge) {
		srcLayer := nodeLayer[srcID]
		for _, e := range edges {
			tgtLayer := nodeLayer[e.TargetID]
			if forbidden[srcLayer+"\x00"+tgtLayer] {
				rep.Violations = append(rep.Violations, Violation{
					Kind:        KindForbiddenDep,
					SourceID:    srcID,
					TargetID:    e.TargetID,
					SourceLayer: srcLayer,
					TargetLayer: tgtLayer,
					EdgeType:    string(e.Type),
					Message:     "layer " + srcLayer + " must not depend on layer " + tgtLayer,
				})
			}
		}
	})
	rep.ForbiddenEdges = len(rep.Violations)

	// Cycle detection over the coarse layer graph: count distinct SCCs with
	// more than one member, plus self-loop counts in single-node SCCs. A
	// simplified approach treats each back-edge found by DFS as one cycle.
	rep.CycleCount = countLayerCycles(layerGraph)

	sort.Slice(rep.Violations, func(i, j int) bool {
		if rep.Violations[i].SourceLayer != rep.Violations[j].SourceLayer {
			return rep.Violations[i].SourceLayer < rep.Violations[j].SourceLayer
		}
		return rep.Violations[i].TargetLayer < rep.Violations[j].TargetLayer
	})

	return rep
}

// countLayerCycles counts strongly connected components with cycles using
// Tarjan's SCC algorithm over the coarse layer graph. Each multi-member SCC
// contributes one cycle; each single-member SCC with a self-edge contributes
// one. This matches the "cycle budget" notion: how many independent cyclic
// clusters exist between architectural layers.
func countLayerCycles(graph map[string]map[string]bool) int {
	index := 0
	var stack []string
	onStack := make(map[string]bool)
	indices := make(map[string]int)
	low := make(map[string]int)
	cycleCount := 0

	var strongconnect func(v string)
	strongconnect = func(v string) {
		indices[v] = index
		low[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for w := range graph[v] {
			if _, seen := indices[w]; !seen {
				strongconnect(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] && indices[w] < low[v] {
				low[v] = indices[w]
			}
		}

		if low[v] == indices[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			if len(scc) > 1 {
				cycleCount++
			} else if graph[v] != nil && graph[v][v] {
				cycleCount++
			}
		}
	}

	nodes := make([]string, 0, len(graph))
	for n := range graph {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		if _, seen := indices[n]; !seen {
			strongconnect(n)
		}
	}
	return cycleCount
}

// ExceedsBudget reports whether the detected cycles violate the declared budget.
func (r *Report) ExceedsBudget() bool {
	return r.CycleCount > r.CycleBudget
}
