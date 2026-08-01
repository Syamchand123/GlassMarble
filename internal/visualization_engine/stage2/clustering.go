package stage2

import (
	"sort"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// ComputeKCores computes the k-core decomposition for all nodes using the
// standard peeling algorithm with a min-priority queue: a node's core number
// is its degree at removal time. This gives exact core numbers and fixes the
// old tail bug where leftover nodes could be assigned N+1 (AUDIT Issue 2 §2.4).
func ComputeKCores(sub *types.VirtualSubgraph) map[string]int {
	adj := make(map[string]map[string]bool)
	for id := range sub.Nodes {
		adj[id] = make(map[string]bool)
	}
	for _, e := range sub.Edges {
		if e.SourceID == e.TargetID {
			continue
		}
		adj[e.SourceID][e.TargetID] = true
		adj[e.TargetID][e.SourceID] = true
	}

	degree := make(map[string]int)
	for id := range sub.Nodes {
		degree[id] = len(adj[id])
	}

	kCore := make(map[string]int, len(sub.Nodes))
	removed := make(map[string]bool)

	h := &degreeHeap{deg: degree}
	for id := range sub.Nodes {
		h.push(id)
	}

	for h.len() > 0 {
		v := h.pop()
		if removed[v] {
			continue
		}
		removed[v] = true
		kCore[v] = degree[v]
		for nb := range adj[v] {
			if removed[nb] {
				continue
			}
			degree[nb]--
			h.push(nb)
		}
	}

	return kCore
}

// degreeHeap is a binary min-heap over node IDs keyed by their current degree.
type degreeHeap struct {
	deg map[string]int
	ids []string
}

func (h *degreeHeap) len() int { return len(h.ids) }

func (h *degreeHeap) push(id string) {
	h.ids = append(h.ids, id)
	i := len(h.ids) - 1
	for i > 0 {
		parent := (i - 1) / 2
		if h.deg[h.ids[parent]] <= h.deg[h.ids[i]] {
			break
		}
		h.ids[parent], h.ids[i] = h.ids[i], h.ids[parent]
		i = parent
	}
}

func (h *degreeHeap) pop() string {
	top := h.ids[0]
	h.ids[0] = h.ids[len(h.ids)-1]
	h.ids = h.ids[:len(h.ids)-1]
	i := 0
	for {
		left, right := 2*i+1, 2*i+2
		smallest := i
		if left < len(h.ids) && h.deg[h.ids[left]] < h.deg[h.ids[smallest]] {
			smallest = left
		}
		if right < len(h.ids) && h.deg[h.ids[right]] < h.deg[h.ids[smallest]] {
			smallest = right
		}
		if smallest == i {
			break
		}
		h.ids[i], h.ids[smallest] = h.ids[smallest], h.ids[i]
		i = smallest
	}
	return top
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
