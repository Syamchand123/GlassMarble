package stage2

import (
	"sort"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// ComputeKCores computes the k-core decomposition for all nodes using iterative degree-based pruning.
func ComputeKCores(sub *types.VirtualSubgraph) map[string]int {
	adj := make(map[string]map[string]bool)
	for id := range sub.Nodes {
		adj[id] = make(map[string]bool)
	}
	for _, e := range sub.Edges {
		adj[e.SourceID][e.TargetID] = true
		adj[e.TargetID][e.SourceID] = true
	}

	degree := make(map[string]int)
	for id := range sub.Nodes {
		degree[id] = len(adj[id])
	}

	kCore := make(map[string]int)
	for id := range sub.Nodes {
		kCore[id] = 0
	}

	queue := make([]string, 0)
	for id, d := range degree {
		if d == 0 {
			kCore[id] = 0
		}
	}

	currentK := 1
	for {
		for len(queue) == 0 && currentK <= len(sub.Nodes) {
			for id, d := range degree {
				if d > 0 && d <= currentK {
					queue = append(queue, id)
				}
			}
			if len(queue) == 0 {
				currentK++
			}
		}
		if len(queue) == 0 {
			break
		}
		for len(queue) > 0 {
			node := queue[0]
			queue = queue[1:]
			kCore[node] = currentK
			degree[node] = 0
			for neighbor := range adj[node] {
				if degree[neighbor] > 0 {
					degree[neighbor]--
					if degree[neighbor] <= currentK {
						queue = append(queue, neighbor)
					}
				}
			}
		}
	}

	for id := range sub.Nodes {
		if kCore[id] == 0 && len(adj[id]) > 0 {
			kCore[id] = currentK
		}
	}

	return kCore
}

type weightedEdge struct {
	src, tgt string
	weight   int
}

// ComputeMST returns a minimum spanning tree using Kruskal's algorithm with edge weights derived from predicate multiplicity.
func ComputeMST(sub *types.VirtualSubgraph) []types.LayoutEdge {
	edgeWeights := make(map[string]int)
	for _, e := range sub.Edges {
		key := e.SourceID + "|" + e.TargetID
		edgeWeights[key]++
	}

	var edges []weightedEdge
	for _, e := range sub.Edges {
		key := e.SourceID + "|" + e.TargetID
		w := edgeWeights[key]
		edges = append(edges, weightedEdge{src: e.SourceID, tgt: e.TargetID, weight: -w})
	}

	sort.Slice(edges, func(i, j int) bool {
		return edges[i].weight < edges[j].weight
	})

	parent := make(map[string]string)
	find := func(x string) string {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(x, y string) {
		rx, ry := find(x), find(y)
		if rx != ry {
			parent[rx] = ry
		}
	}

	for id := range sub.Nodes {
		parent[id] = id
	}

	var mst []types.LayoutEdge
	for _, e := range edges {
		if find(e.src) != find(e.tgt) {
			mst = append(mst, types.LayoutEdge{
				SourceID:  e.src,
				TargetID:  e.tgt,
				Weight:    -e.weight,
				Predicate: "mst",
			})
			union(e.src, e.tgt)
		}
	}

	return mst
}
