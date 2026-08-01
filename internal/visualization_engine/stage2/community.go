package stage2

import (
	"math"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// DetectCommunities runs weighted modularity-based community detection on the subgraph.
func DetectCommunities(sub *types.VirtualSubgraph) map[string]string {
	return ComputeWeightedModularity(sub)
}

// ComputeWeightedModularity performs Louvain-like community detection using predicate-based edge weights.
func ComputeWeightedModularity(sub *types.VirtualSubgraph) map[string]string {
	communities := make(map[string]string)
	for id := range sub.Nodes {
		communities[id] = id
	}

	adj := buildWeightedAdjacency(sub)

	var totalWeight float64
	for _, neighbors := range adj {
		for _, n := range neighbors {
			totalWeight += n.weight
		}
	}
	totalWeight /= 2
	if totalWeight == 0 {
		totalWeight = 1.0 // avoid div by zero
	}

	for iter := 0; iter < 30; iter++ {
		changed := false
		for id := range sub.Nodes {
			bestCommunity := communities[id]
			neighborCommunities := make(map[string]float64)

			for _, neighbor := range adj[id] {
				nc := communities[neighbor.id]
				neighborCommunities[nc] += neighbor.weight
			}

			var nodeDegree float64
			for _, neighbor := range adj[id] {
				nodeDegree += neighbor.weight
			}

			bestCommunity, _ = modularityGain(communities[id], neighborCommunities, totalWeight, nodeDegree)

			if bestCommunity != communities[id] {
				communities[id] = bestCommunity
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	return communities
}

type weightedNeighbor struct {
	id     string
	weight float64
}

func buildWeightedAdjacency(sub *types.VirtualSubgraph) map[string][]weightedNeighbor {
	adj := make(map[string][]weightedNeighbor)
	addEdge := func(src, tgt, pred string) {
		w := communityEdgeWeight(pred)
		adj[src] = append(adj[src], weightedNeighbor{id: tgt, weight: w})
		adj[tgt] = append(adj[tgt], weightedNeighbor{id: src, weight: w})
	}

	for _, e := range sub.Edges {
		addEdge(e.SourceID, e.TargetID, e.Predicate)
	}

	return adj
}

// communityEdgeWeight assigns a heuristic weight to different semantic edge types.
func communityEdgeWeight(predicate string) float64 {
	switch predicate {
	case "gm:belongsToFile", "gm:belongsToNamespace", "gm:belongsTo", "gm:dependsOn", "gm:references", "gm:imports":
		return 3.0
	case "gm:calls", "gm:spawnsConcurrent", "gm:dispatchesEvent", "gm:ffiCall":
		return 2.0
	case "gm:dataFlowTo", "gm:pointsTo", "gm:heapAlias", "gm:aliasesPointer", "gm:vulnerableTaint":
		return 1.0
	case "gm:controlFlowTo", "gm:controlFlowToTrue", "gm:controlFlowToFalse", "gm:catchesException", "gm:defersExecution":
		return 0.5
	default:
		return 1.0
	}
}

// modularityGain returns the best neighbor community for a node along with its
// modularity gain, or the current community with gain 0 when no move improves
// modularity. Never returns an empty-string community (AUDIT Issue 2 §2.4).
func modularityGain(currentCommunity string, neighborCommunities map[string]float64, totalWeight float64, nodeDegree float64) (string, float64) {
	bestCommunity := currentCommunity
	bestGain := 0.0

	for nc, weightToCommunity := range neighborCommunities {
		if nc == "" {
			continue
		}
		gain := weightToCommunity/totalWeight - (nodeDegree*nodeDegree)/(2*totalWeight*totalWeight)
		if gain > bestGain {
			bestGain = gain
			bestCommunity = nc
		}
	}

	return bestCommunity, bestGain
}

func ComputeModularity(communities map[string]string, adj map[string][]weightedNeighbor) float64 {
	var totalWeight float64
	for _, neighbors := range adj {
		for _, n := range neighbors {
			totalWeight += n.weight
		}
	}
	totalWeight /= 2
	if totalWeight == 0 {
		return 0
	}

	var modularity float64
	communityWeights := make(map[string]float64)
	communityDegrees := make(map[string]float64)

	for node, neighbors := range adj {
		comm := communities[node]
		for _, n := range neighbors {
			if communities[n.id] == comm {
				communityWeights[comm] += n.weight
			}
		}
		var degree float64
		for _, n := range neighbors {
			degree += n.weight
		}
		communityDegrees[comm] += degree
	}

	for comm := range communityWeights {
		kw := communityWeights[comm] / 2
		d := communityDegrees[comm]
		modularity += (kw / totalWeight) - math.Pow(d/(2*totalWeight), 2)
	}

	return modularity
}
