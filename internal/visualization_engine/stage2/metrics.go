package stage2

import (
	"math"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// ComputePageRank computes PageRank values for all nodes in the subgraph using the given damping factor and iteration count.
func ComputePageRank(sub *types.VirtualSubgraph, damping float64, iterations int) map[string]float64 {
	pr := make(map[string]float64)
	n := len(sub.Nodes)
	if n == 0 {
		return pr
	}
	for id := range sub.Nodes {
		pr[id] = 1.0 / float64(n)
	}
	outDegree := make(map[string]int)
	for _, e := range sub.Edges {
		outDegree[e.SourceID]++
	}
	for iter := 0; iter < iterations; iter++ {
		newPR := make(map[string]float64)
		var danglingSum float64
		for id := range sub.Nodes {
			if outDegree[id] == 0 {
				danglingSum += pr[id]
			}
		}
		for id := range sub.Nodes {
			newPR[id] = (1.0 - damping) / float64(n)
		}
		for _, e := range sub.Edges {
			if outDegree[e.SourceID] > 0 {
				contrib := damping * pr[e.SourceID] / float64(outDegree[e.SourceID])
				newPR[e.TargetID] += contrib
			}
		}
		danglingContrib := damping * danglingSum / float64(n)
		for id := range sub.Nodes {
			newPR[id] += danglingContrib
		}
		pr = newPR
	}
	return pr
}

// ComputeBetweenness computes betweenness centrality for all nodes using Brandes' algorithm.
func ComputeBetweenness(sub *types.VirtualSubgraph) map[string]float64 {
	bc := make(map[string]float64)
	for id := range sub.Nodes {
		bc[id] = 0
	}
	adj := make(map[string][]string)
	for _, e := range sub.Edges {
		adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
	}
	for s := range sub.Nodes {
		stack := make([]string, 0)
		pred := make(map[string][]string)
		sigma := make(map[string]float64)
		dist := make(map[string]int)
		for id := range sub.Nodes {
			pred[id] = nil
			sigma[id] = 0
			dist[id] = -1
		}
		sigma[s] = 1
		dist[s] = 0
		queue := []string{s}
		for len(queue) > 0 {
			v := queue[0]
			queue = queue[1:]
			stack = append(stack, v)
			for _, w := range adj[v] {
				if _, ok := sub.Nodes[w]; !ok {
					continue
				}
				if dist[w] < 0 {
					queue = append(queue, w)
					dist[w] = dist[v] + 1
				}
				if dist[w] == dist[v]+1 {
					sigma[w] += sigma[v]
					pred[w] = append(pred[w], v)
				}
			}
		}
		delta := make(map[string]float64)
		for id := range sub.Nodes {
			delta[id] = 0
		}
		for len(stack) > 0 {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, v := range pred[w] {
				if sigma[w] > 0 {
					delta[v] += (sigma[v] / sigma[w]) * (1 + delta[w])
				}
			}
			if w != s {
				bc[w] += delta[w]
			}
		}
	}
	return bc
}

// ComputeDegrees returns in-degree and out-degree maps for all nodes in the subgraph.
func ComputeDegrees(sub *types.VirtualSubgraph) (map[string]int, map[string]int) {
	inDeg := make(map[string]int)
	outDeg := make(map[string]int)
	for id := range sub.Nodes {
		inDeg[id] = 0
		outDeg[id] = 0
	}
	for _, e := range sub.Edges {
		outDeg[e.SourceID]++
		inDeg[e.TargetID]++
	}
	return inDeg, outDeg
}

// DetectGodObjects identifies nodes whose total degree exceeds the mean by more than 3 standard deviations.
func DetectGodObjects(sub *types.VirtualSubgraph, inDeg, outDeg map[string]int) []string {
	if len(sub.Nodes) < 3 {
		return nil
	}
	var totalDegrees []float64
	for id := range sub.Nodes {
		totalDegrees = append(totalDegrees, float64(inDeg[id]+outDeg[id]))
	}
	if len(totalDegrees) == 0 {
		return nil
	}
	var sum float64
	for _, d := range totalDegrees {
		sum += d
	}
	mean := sum / float64(len(totalDegrees))
	var varianceSum float64
	for _, d := range totalDegrees {
		diff := d - mean
		varianceSum += diff * diff
	}
	variance := varianceSum / float64(len(totalDegrees)-1)
	std := math.Sqrt(variance)
	if std == 0 {
		std = 1
	}
	threshold := mean + 3*std
	var gods []string
	for id := range sub.Nodes {
		if float64(inDeg[id]+outDeg[id]) > threshold {
			gods = append(gods, id)
		}
	}
	return gods
}

// ComputeInDegree returns the in-degree map by delegating to ComputeDegrees.
func ComputeInDegree(sub *types.VirtualSubgraph) map[string]int {
	inDeg, _ := ComputeDegrees(sub)
	return inDeg
}

// ComputeAllMetrics runs all metric computations (PageRank, betweenness, degrees, god-objects, k-core, SCCs, summary, communities).
func ComputeAllMetrics(sub *types.VirtualSubgraph) *DiagramMetrics {
	return ComputeAllMetricsWithOptions(sub, true)
}

// ComputeAllMetricsWithOptions is ComputeAllMetrics with an explicit SCC toggle;
// the PipelineConfig.EnableSCC flag was previously dead (AUDIT Issue 2 §2.4).
func ComputeAllMetricsWithOptions(sub *types.VirtualSubgraph, includeSCC bool) *DiagramMetrics {
	pr := ComputePageRank(sub, 0.85, 100)
	bc := ComputeBetweenness(sub)
	inDeg, outDeg := ComputeDegrees(sub)
	godObjects := DetectGodObjects(sub, inDeg, outDeg)
	kcore := ComputeKCores(sub)
	var sccs [][]string
	if includeSCC {
		sccs, _ = CountSCCs(sub)
	}
	summary := ComputeGraphSummary(sub)
	communities := ComputeWeightedModularity(sub)

	return &DiagramMetrics{
		PageRank:    pr,
		Betweenness: bc,
		DegreeIn:    inDeg,
		DegreeOut:   outDeg,
		Communities: communities,
		GodObjects:  godObjects,
		KCore:       kcore,
		SCCs:        sccs,
		Summary:     summary,
	}
}

// ComputeGraphSummary builds a GraphSummary with node/edge counts, density, diameter, average path length, SCC stats, and god-object count.
func ComputeGraphSummary(sub *types.VirtualSubgraph) *types.GraphSummary {
	summary := &types.GraphSummary{
		NodeCount: len(sub.Nodes),
		EdgeCount: len(sub.Edges),
	}
	if summary.NodeCount > 1 {
		// Directed density is clamped to [0,1]; dangling edges (endpoints
		// outside the node set) must not inflate it (AUDIT Issue 2 §2.4).
		maxEdges := summary.NodeCount * (summary.NodeCount - 1)
		if maxEdges > 0 {
			summary.Density = float64(summary.EdgeCount) / float64(maxEdges)
			if summary.Density > 1 {
				summary.Density = 1
			}
		}
		summary.Diameter = ComputeDiameter(sub)
		summary.AvgPathLength = ComputeAvgPathLength(sub)
		summary.BipartiteScore = ComputeBipartiteScore(sub)
	}
	sccs, largestSize := CountSCCs(sub)
	summary.ClusterCount = len(sccs)
	summary.LargestSCCSize = largestSize

	inDeg, outDeg := ComputeDegrees(sub)
	godObjects := DetectGodObjects(sub, inDeg, outDeg)
	summary.GodObjectCount = len(godObjects)
	summary.ConnectedComponents = CountConnectedComponents(sub)

	return summary
}

// CountConnectedComponents returns the number of weakly connected components,
// so disconnected graphs are reported instead of silently presenting
// intra-component values as global metrics (AUDIT Issue 2 §2.4).
func CountConnectedComponents(sub *types.VirtualSubgraph) int {
	adj := make(map[string][]string, len(sub.Nodes))
	for _, e := range sub.Edges {
		adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
		adj[e.TargetID] = append(adj[e.TargetID], e.SourceID)
	}
	seen := make(map[string]bool, len(sub.Nodes))
	components := 0
	for id := range sub.Nodes {
		if seen[id] {
			continue
		}
		components++
		queue := []string{id}
		seen[id] = true
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, nb := range adj[cur] {
				if !seen[nb] {
					seen[nb] = true
					queue = append(queue, nb)
				}
			}
		}
	}
	return components
}
