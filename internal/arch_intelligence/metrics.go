package arch_intelligence

import (
	"sort"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// ComponentCoupling holds afferent/efferent coupling for one component.
type ComponentCoupling struct {
	ComponentID string  `json:"component_id"`
	Name        string  `json:"name"`
	Ca          int     `json:"ca"` // distinct components depending on this one
	Ce          int     `json:"ce"` // distinct components this one depends on
	Instability float64 `json:"instability"`
	Weight      int     `json:"weight"` // node count
}

// CalculateMetrics runs all graph analytics and produces ArchMetrics.
// Compatibility wrapper for graph callers.
func CalculateMetrics(graph *akg.CodePropertyGraph) archmodel.ArchMetrics {
	if graph == nil {
		return archmodel.ArchMetrics{}
	}
	return CalculateMetricsFromSnapshot(NewGraphSnapshot(graph))
}

// CalculateMetricsFromSnapshot computes global architecture metrics on a
// snapshot. Deterministic: node iteration follows sorted IDs.
func CalculateMetricsFromSnapshot(snap *GraphSnapshot) archmodel.ArchMetrics {
	metrics := archmodel.ArchMetrics{
		TotalNodes: snap.Len(),
		TotalEdges: snap.EdgeCount,
	}
	if snap.Len() <= 1 {
		return metrics
	}

	maxEdges := float64(snap.Len() * (snap.Len() - 1))
	metrics.GraphDensity = float64(snap.EdgeCount) / maxEdges

	// 1. Per-node fan-in / fan-out (distinct structural endpoints).
	coupling := NodeMetricsSnapshot(snap)
	totalFanIn := 0
	totalFanOut := 0
	for _, id := range snap.NodeIDs {
		c := coupling[id]
		totalFanIn += c.FanIn
		totalFanOut += c.FanOut
		if c.FanIn > metrics.MaxFanIn {
			metrics.MaxFanIn = c.FanIn
		}
		if c.FanOut > metrics.MaxFanOut {
			metrics.MaxFanOut = c.FanOut
		}
	}
	metrics.AvgFanIn = float64(totalFanIn) / float64(snap.Len())
	metrics.AvgFanOut = float64(totalFanOut) / float64(snap.Len())

	// 2. LCOM4 (mean over STRUCT/CLASS nodes).
	var totalLCOM4 float64
	var classCount int
	for _, id := range snap.NodeIDs {
		node := snap.Nodes[id]
		if node == nil || (node.Kind != "STRUCT" && node.Kind != "CLASS") {
			continue
		}
		totalLCOM4 += LCOM4Snapshot(node, snap)
		classCount++
	}
	if classCount > 0 {
		metrics.LCOM4 = totalLCOM4 / float64(classCount)
	}

	// 3. Cyclomatic complexity.
	complexities := CyclomaticComplexitySnapshot(snap)
	totalCC := 0
	for _, cc := range complexities {
		totalCC += cc
		if cc > metrics.CyclomaticMax {
			metrics.CyclomaticMax = cc
		}
	}
	if len(complexities) > 0 {
		metrics.CyclomaticAvg = float64(totalCC) / float64(len(complexities))
	}

	// 4. SCCs and cycles.
	sccs := SCCIterative(snap)
	metrics.StronglyConnectedComponents = len(sccs)
	for _, scc := range sccs {
		if len(scc) > 1 {
			metrics.CycleCount++
			if len(scc) > metrics.MaxCycleLength {
				metrics.MaxCycleLength = len(scc)
			}
		}
	}

	// 5. Dead code.
	dead := DeadCodeNodesSnapshot(snap)
	metrics.DeadCodeNodeCount = len(dead)
	if snap.Len() > 0 {
		metrics.ReachableFromEntrypoints = 1.0 - float64(len(dead))/float64(snap.Len())
	}

	// 6. PageRank hotspots (top 10 non-function nodes).
	ranks := PageRankSnapshot(snap, 20, 0.85)
	var hotspots []archmodel.HotspotEntry
	for _, id := range snap.NodeIDs {
		rank := ranks[id]
		node := snap.Nodes[id]
		if node == nil {
			continue
		}
		if node.Kind == "FUNCTION" || node.Kind == "VARIABLE" {
			continue
		}
		c := coupling[id]
		hotspots = append(hotspots, archmodel.HotspotEntry{
			NodeID:   id,
			Name:     node.Name,
			PageRank: rank,
			FanIn:    c.FanIn,
			FanOut:   c.FanOut,
		})
	}
	sort.Slice(hotspots, func(i, j int) bool {
		if hotspots[i].PageRank != hotspots[j].PageRank {
			return hotspots[i].PageRank > hotspots[j].PageRank
		}
		return hotspots[i].NodeID < hotspots[j].NodeID
	})
	if len(hotspots) > 10 {
		hotspots = hotspots[:10]
	}
	metrics.TopHotspots = hotspots

	return metrics
}

// ComputeComponentCoupling computes per-component afferent/efferent coupling
// and instability over distinct structural edges between components. Also
// returns the global afferent/efferent coupling and instability summed over
// the component graph. The result is sorted by component ID.
func ComputeComponentCoupling(snap *GraphSnapshot, components []archmodel.DetectedComponent) ([]ComponentCoupling, float64, float64, float64) {
	if snap == nil {
		return nil, 0, 0, 0
	}
	nodeToComp := make(map[string]int, snap.Len())
	for i, c := range components {
		for _, id := range c.NodeIDs {
			nodeToComp[id] = i
		}
	}
	compCount := len(components)
	// depMatrix[src][tgt] = true for distinct component-level edges.
	depMatrix := make([]map[int]bool, compCount)
	for i := range depMatrix {
		depMatrix[i] = make(map[int]bool)
	}
	for _, id := range snap.NodeIDs {
		srcIdx, ok := nodeToComp[id]
		if !ok {
			continue
		}
		for _, e := range snap.structuralOutbound(id) {
			tgtIdx, ok := nodeToComp[e.TargetID]
			if !ok || tgtIdx == srcIdx {
				continue
			}
			depMatrix[srcIdx][tgtIdx] = true
		}
	}

	couplings := make([]ComponentCoupling, 0, compCount)
	var totalCa, totalCe int
	for i, c := range components {
		ca := 0
		for j := 0; j < compCount; j++ {
			if depMatrix[j][i] {
				ca++
			}
		}
		ce := len(depMatrix[i])
		instability := 0.0
		if ca+ce > 0 {
			instability = float64(ce) / float64(ca+ce)
		}
		totalCa += ca
		totalCe += ce
		couplings = append(couplings, ComponentCoupling{
			ComponentID: c.ID,
			Name:        c.Name,
			Ca:          ca,
			Ce:          ce,
			Instability: instability,
			Weight:      len(c.NodeIDs),
		})
	}
	sort.Slice(couplings, func(i, j int) bool { return couplings[i].ComponentID < couplings[j].ComponentID })

	globalInstability := 0.0
	if totalCa+totalCe > 0 {
		globalInstability = float64(totalCe) / float64(totalCa+totalCe)
	}
	return couplings, float64(totalCa), float64(totalCe), globalInstability
}

// applyComponentMetrics folds component-level coupling into the global
// ArchMetrics so AfferentCoupling/EfferentCoupling/Instability reflect the
// component graph rather than raw node counts.
func applyComponentMetrics(metrics *archmodel.ArchMetrics, ca, ce, instability float64) {
	metrics.AfferentCoupling = ca
	metrics.EfferentCoupling = ce
	metrics.Instability = instability
}

// countLayerViolations counts structural edges that cross declared layers
// against the layer order (upward edges) or that match forbidden pairs.
// Returns the violation edge count.
func countLayerViolations(snap *GraphSnapshot, layerIndex *LayerAssigner) int {
	if snap == nil || layerIndex == nil || !layerIndex.Configured() {
		return 0
	}
	nodeLayer := make(map[string]string, snap.Len())
	for _, id := range snap.NodeIDs {
		node := snap.Nodes[id]
		if node != nil {
			nodeLayer[id] = layerIndex.Assign(node.FileSpec.Path)
		}
	}
	violations := 0
	for _, id := range snap.NodeIDs {
		srcLayer := nodeLayer[id]
		if srcLayer == "" {
			continue
		}
		for _, e := range snap.structuralOutbound(id) {
			tgtLayer := nodeLayer[e.TargetID]
			if tgtLayer == "" || tgtLayer == srcLayer {
				continue
			}
			if layerIndex.IsForbidden(srcLayer, tgtLayer) ||
				layerIndex.IsUpward(srcLayer, tgtLayer) {
				violations++
			}
		}
	}
	return violations
}
