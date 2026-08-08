package arch_intelligence

import (
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

// CalculateMetrics runs all graph analytics and produces ArchMetrics.
func CalculateMetrics(graph *akg.CodePropertyGraph) archmodel.ArchMetrics {
	metrics := archmodel.ArchMetrics{
		TotalNodes: graph.Nodes.Len(),
	}

	edgeCount := 0
	graph.OutboundEdges.Iterate(func(_ string, edges []stage4.ResolvedEdge) {
		edgeCount += len(edges)
	})
	metrics.TotalEdges = edgeCount

	if metrics.TotalNodes > 1 {
		maxEdges := float64(metrics.TotalNodes * (metrics.TotalNodes - 1))
		metrics.GraphDensity = float64(edgeCount) / maxEdges
	}

	// 1. Fan-In / Fan-Out
	coupling := NodeMetrics(graph)
	totalFanIn := 0
	totalFanOut := 0
	maxFanIn := 0
	maxFanOut := 0

	for _, c := range coupling {
		totalFanIn += c.FanIn
		totalFanOut += c.FanOut
		if c.FanIn > maxFanIn {
			maxFanIn = c.FanIn
		}
		if c.FanOut > maxFanOut {
			maxFanOut = c.FanOut
		}
	}
	metrics.MaxFanIn = maxFanIn
	metrics.MaxFanOut = maxFanOut
	if metrics.TotalNodes > 0 {
		metrics.AvgFanIn = float64(totalFanIn) / float64(metrics.TotalNodes)
		metrics.AvgFanOut = float64(totalFanOut) / float64(metrics.TotalNodes)
	}

	// Afferent/Efferent mapping to Fan-In/Fan-Out
	metrics.AfferentCoupling = float64(totalFanIn)
	metrics.EfferentCoupling = float64(totalFanOut)
	if totalFanIn+totalFanOut > 0 {
		metrics.Instability = float64(totalFanOut) / float64(totalFanIn+totalFanOut)
	}

	// 2. LCOM4
	var totalLCOM4 float64
	var classCount int
	graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		if node.Kind == "STRUCT" || node.Kind == "CLASS" {
			lcom := LCOM4(node, graph)
			totalLCOM4 += lcom
			classCount++
		}
	})
	if classCount > 0 {
		metrics.LCOM4 = totalLCOM4 / float64(classCount)
	}

	// 3. Cyclomatic Complexity
	complexities := CyclomaticComplexity(graph)
	var totalCC int
	var maxCC int
	for _, cc := range complexities {
		totalCC += cc
		if cc > maxCC {
			maxCC = cc
		}
	}
	metrics.CyclomaticMax = maxCC
	if len(complexities) > 0 {
		metrics.CyclomaticAvg = float64(totalCC) / float64(len(complexities))
	}

	// 4. SCC & Cycles
	sccs := SCC(graph)
	metrics.StronglyConnectedComponents = len(sccs)
	cycleCount := 0
	maxCycle := 0
	for _, scc := range sccs {
		if len(scc) > 1 {
			cycleCount++
			if len(scc) > maxCycle {
				maxCycle = len(scc)
			}
		}
	}
	metrics.CycleCount = cycleCount
	metrics.MaxCycleLength = maxCycle

	// 5. Dead Code
	dead := DeadCodeNodes(graph)
	// Filter out test files and generated files from dead code count
	deadCount := 0
	for _, id := range dead {
		if node, ok := graph.SafeGetNode(id); ok {
			if !strings.HasSuffix(node.FileSpec.Path, "_test.go") && !strings.Contains(node.FileSpec.Path, "mock") {
				deadCount++
			}
		}
	}
	metrics.DeadCodeNodeCount = deadCount

	if metrics.TotalNodes > 0 {
		reachable := metrics.TotalNodes - deadCount
		metrics.ReachableFromEntrypoints = float64(reachable) / float64(metrics.TotalNodes)
	}

	// 6. PageRank Hotspots
	ranks := PageRank(graph, 20, 0.85)
	var hotspots []archmodel.HotspotEntry
	for id, rank := range ranks {
		if node, ok := graph.SafeGetNode(id); ok {
			if node.Kind != "FUNCTION" && node.Kind != "VARIABLE" {
				c := coupling[id]
				hotspots = append(hotspots, archmodel.HotspotEntry{
					NodeID:   id,
					Name:     node.Name,
					PageRank: rank,
					FanIn:    c.FanIn,
					FanOut:   c.FanOut,
				})
			}
		}
	}

	// Sort by PageRank descending
	sort.Slice(hotspots, func(i, j int) bool {
		return hotspots[i].PageRank > hotspots[j].PageRank
	})

	if len(hotspots) > 10 {
		metrics.TopHotspots = hotspots[:10]
	} else {
		metrics.TopHotspots = hotspots
	}

	return metrics
}
