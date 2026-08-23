package layout

import (
	"sort"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// FindShortestPath returns the shortest path (by BFS) between source and target nodes, or nil if unreachable.
func FindShortestPath(sub *types.VirtualSubgraph, src, tgt string) []string {
	adj := make(map[string][]string)
	for _, e := range sub.Edges {
		adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
	}

	dist := make(map[string]int)
	prev := make(map[string]string)
	queue := []string{src}
	dist[src] = 0

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current == tgt {
			var path []string
			for at := tgt; at != ""; at = prev[at] {
				path = append([]string{at}, path...)
				if at == src {
					break
				}
			}
			return path
		}

		for _, neighbor := range adj[current] {
			if _, visited := dist[neighbor]; !visited {
				dist[neighbor] = dist[current] + 1
				prev[neighbor] = current
				queue = append(queue, neighbor)
			}
		}
	}

	return nil
}

// FindAllPaths returns all unique paths from src to tgt up to maxDepth using depth-first search.
func FindAllPaths(sub *types.VirtualSubgraph, src, tgt string, maxDepth int) [][]string {
	adj := make(map[string][]string)
	for _, e := range sub.Edges {
		adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
	}

	var result [][]string
	var current []string
	visited := make(map[string]bool)

	var dfs func(node string, depth int)
	dfs = func(node string, depth int) {
		if depth > maxDepth {
			return
		}
		if visited[node] {
			return
		}
		if node == tgt {
			path := make([]string, len(current)+1)
			copy(path, current)
			path[len(current)] = node
			result = append(result, path)
			return
		}

		visited[node] = true
		current = append(current, node)

		for _, neighbor := range adj[node] {
			if _, ok := sub.Nodes[neighbor]; ok {
				dfs(neighbor, depth+1)
			}
		}

		current = current[:len(current)-1]
		visited[node] = false
	}

	dfs(src, 0)
	return result
}

// FindCriticalPath returns the longest path in a DAG using topological ordering. Returns nil if the graph contains a cycle.
func FindCriticalPath(sub *types.VirtualSubgraph) []string {
	adj := make(map[string][]string)
	inDegree := make(map[string]int)
	for id := range sub.Nodes {
		inDegree[id] = 0
	}
	for _, e := range sub.Edges {
		adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
		inDegree[e.TargetID]++
	}

	var queue []string
	for id := range sub.Nodes {
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}

	topoOrder := make([]string, 0, len(sub.Nodes))
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		topoOrder = append(topoOrder, node)
		for _, neighbor := range adj[node] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(topoOrder) != len(sub.Nodes) {
		return nil
	}

	dist := make(map[string]int)
	prev := make(map[string]string)
	for _, node := range topoOrder {
		dist[node] = 0
	}

	for _, node := range topoOrder {
		for _, neighbor := range adj[node] {
			if dist[node]+1 > dist[neighbor] {
				dist[neighbor] = dist[node] + 1
				prev[neighbor] = node
			}
		}
	}

	var endNode string
	maxDist := -1
	for id, d := range dist {
		if d > maxDist {
			maxDist = d
			endNode = id
		}
	}

	if endNode == "" {
		return nil
	}

	var path []string
	for at := endNode; at != ""; at = prev[at] {
		path = append([]string{at}, path...)
	}

	return path
}

// pathMetricSourceCap caps the number of BFS sources used by the all-pairs
// metrics (diameter, average path length) on very large graphs. Exact
// all-pairs work is O(V·(V+E)) with a fresh distance map per source, which
// made `gmb visualize` take 20+ seconds on subgraphs of a few thousand nodes.
// Below the cap the values are exact; above it they are computed from a
// deterministically spread sample of sources (a lower bound for the diameter,
// a sampled average for the path length) so interactive diagrams stay fast.
const pathMetricSourceCap = 1200

// sampleSources returns up to cap node IDs spread evenly across nodes in
// deterministic order, or all of them when the graph is small enough.
// It sorts a copy of nodes to guarantee determinism even if the caller
// supplied a map-order slice (C3-3).
func sampleSources(nodes []string, cap int) []string {
	if len(nodes) == 0 {
		return nodes
	}
	// Deterministic: work on a sorted copy.
	sorted := make([]string, len(nodes))
	copy(sorted, nodes)
	sort.Strings(sorted)
	if len(sorted) <= cap {
		return sorted
	}
	res := make([]string, 0, cap)
	for i := 0; i < cap; i++ {
		res = append(res, sorted[int(float64(i)*float64(len(sorted))/float64(cap))])
	}
	return res
}

// ComputeDiameter returns the longest shortest-path distance between any two
// reachable nodes, computed on the DIRECTED graph (matching the semantics of
// the architecture graph, AUDIT Issue 2 §2.4). Disconnectedness is reported
// via GraphSummary.ConnectedComponents. On graphs larger than
// pathMetricSourceCap nodes the BFS sources are sampled (see sampleSources).
func ComputeDiameter(sub *types.VirtualSubgraph) int {
	adj := make(map[string][]string)
	for _, e := range sub.Edges {
		adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
	}

	nodes := make([]string, 0, len(sub.Nodes))
	for id := range sub.Nodes {
		nodes = append(nodes, id)
	}
	sort.Strings(nodes)

	maxDist := 0
	for _, s := range sampleSources(nodes, pathMetricSourceCap) {
		dist := make(map[string]int)
		queue := []string{s}
		dist[s] = 0
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			for _, neighbor := range adj[current] {
				if _, visited := dist[neighbor]; !visited {
					dist[neighbor] = dist[current] + 1
					queue = append(queue, neighbor)
					if dist[neighbor] > maxDist {
						maxDist = dist[neighbor]
					}
				}
			}
		}
	}

	return maxDist
}

// ComputeAvgPathLength returns the average shortest-path distance between all
// ordered pairs of reachable nodes on the DIRECTED graph. On graphs larger
// than pathMetricSourceCap nodes the BFS sources are sampled (see
// sampleSources), making the result a sampled average.
func ComputeAvgPathLength(sub *types.VirtualSubgraph) float64 {
	adj := make(map[string][]string)
	for _, e := range sub.Edges {
		adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
	}

	var totalDist int
	var pairCount int
	nodes := make([]string, 0, len(sub.Nodes))
	for id := range sub.Nodes {
		nodes = append(nodes, id)
	}
	sort.Strings(nodes)

	for _, s := range sampleSources(nodes, pathMetricSourceCap) {
		dist := make(map[string]int)
		queue := []string{s}
		dist[s] = 0
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			for _, neighbor := range adj[current] {
				if _, visited := dist[neighbor]; !visited {
					dist[neighbor] = dist[current] + 1
					queue = append(queue, neighbor)
				}
			}
		}
		for _, t := range nodes {
			if s != t {
				if d, ok := dist[t]; ok {
					totalDist += d
					pairCount++
				}
			}
		}
	}

	if pairCount == 0 {
		return 0
	}
	return float64(totalDist) / float64(pairCount)
}

// FindBottlenecks returns nodes whose betweenness centrality exceeds the given threshold.
func FindBottlenecks(sub *types.VirtualSubgraph, threshold float64) []string {
	bc := ComputeBetweenness(sub)
	var bottlenecks []string
	for id, val := range bc {
		if val > threshold {
			bottlenecks = append(bottlenecks, id)
		}
	}
	return bottlenecks
}

// CountSCCs returns strongly connected components using Tarjan's algorithm and the size of the largest SCC.
func CountSCCs(sub *types.VirtualSubgraph) ([][]string, int) {
	adj := make(map[string][]string)
	for _, e := range sub.Edges {
		adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
	}

	index := 0
	indices := make(map[string]int)
	lowlink := make(map[string]int)
	onStack := make(map[string]bool)
	var stack []string

	var sccs [][]string

	var strongconnect func(v string)
	strongconnect = func(v string) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range adj[v] {
			if _, exists := indices[w]; !exists {
				strongconnect(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] {
				if indices[w] < lowlink[v] {
					lowlink[v] = indices[w]
				}
			}
		}

		if lowlink[v] == indices[v] {
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
			sccs = append(sccs, scc)
		}
	}

	for id := range sub.Nodes {
		if _, exists := indices[id]; !exists {
			strongconnect(id)
		}
	}

	largestSize := 0
	for _, scc := range sccs {
		if len(scc) > largestSize {
			largestSize = len(scc)
		}
	}

	return sccs, largestSize
}

// ComputeBipartiteScore returns the fraction of connected components that are bipartite.
func ComputeBipartiteScore(sub *types.VirtualSubgraph) float64 {
	n := len(sub.Nodes)
	if n < 2 {
		return 0
	}

	adj := make(map[string][]string)
	for _, e := range sub.Edges {
		adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
		adj[e.TargetID] = append(adj[e.TargetID], e.SourceID)
	}

	color := make(map[string]int)
	var bfs func(start string) bool
	bfs = func(start string) bool {
		queue := []string{start}
		color[start] = 0
		for len(queue) > 0 {
			v := queue[0]
			queue = queue[1:]
			for _, neighbor := range adj[v] {
				if _, colored := color[neighbor]; !colored {
					color[neighbor] = 1 - color[v]
					queue = append(queue, neighbor)
				} else if color[neighbor] == color[v] {
					return false
				}
			}
		}
		return true
	}

	bipartiteComponents := 0
	totalComponents := 0
	for id := range sub.Nodes {
		if _, colored := color[id]; !colored {
			totalComponents++
			if bfs(id) {
				bipartiteComponents++
			}
		}
	}

	if totalComponents == 0 {
		return 0
	}
	return float64(bipartiteComponents) / float64(totalComponents)
}
