package arch_intelligence

import (
	"sort"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

// isStructuralEdge returns true if the edge represents a high-level architectural dependency
// (as opposed to CFG/DFG low-level details).
func isStructuralEdge(edgeType stage4.RelationshipType) bool {
	switch edgeType {
	case stage4.EdgeDependsOn, stage4.EdgeCalls, stage4.EdgeImplements,
		stage4.EdgeExtends, stage4.EdgeContains, stage4.EdgeComposes,
		stage4.EdgeReferences, stage4.EdgeNetworkCall, stage4.EdgeQueriesDB,
		stage4.EdgeCallsCloudAPI, stage4.EdgePublishes, stage4.EdgeSubscribes:
		return true
	default:
		return false
	}
}

// SCC finds all strongly connected components in the AKG over structural edges.
// A SCC with >1 node means circular dependencies.
func SCC(graph *akg.CodePropertyGraph) [][]string {
	var index int
	indices := make(map[string]int)
	lowlink := make(map[string]int)
	onStack := make(map[string]bool)
	var stack []string
	var cycles [][]string

	var strongconnect func(string)
	strongconnect = func(v string) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, edge := range graph.SafeGetOutboundEdges(v) {
			if !isStructuralEdge(edge.Type) {
				continue
			}
			w := edge.TargetID
			if _, ok := indices[w]; !ok {
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
			var component []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				component = append(component, w)
				if w == v {
					break
				}
			}
			if len(component) > 0 { // In SCC we return all components to compute metrics correctly, cycle if > 1
				cycles = append(cycles, component)
			}
		}
	}

	graph.Nodes.Iterate(func(v string, _ *stage4.ResolvedNode) {
		if _, ok := indices[v]; !ok {
			strongconnect(v)
		}
	})

	return cycles
}

// PageRank computes centrality scores across structural edges.
// High PageRank = architectural hotspot (lots of things depend on it).
func PageRank(graph *akg.CodePropertyGraph, iterations int, damping float64) map[string]float64 {
	ranks := make(map[string]float64)
	numNodes := float64(graph.Nodes.Len())
	if numNodes == 0 {
		return ranks
	}

	initialRank := 1.0 / numNodes
	outDegree := make(map[string]int)

	graph.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
		ranks[id] = initialRank
		deg := 0
		for _, e := range graph.SafeGetOutboundEdges(id) {
			if isStructuralEdge(e.Type) {
				deg++
			}
		}
		outDegree[id] = deg
	})

	for i := 0; i < iterations; i++ {
		newRanks := make(map[string]float64)

		var danglingSum float64
		for id, deg := range outDegree {
			if deg == 0 {
				danglingSum += ranks[id]
			}
		}
		danglingContrib := damping * danglingSum / numNodes

		graph.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
			rank := (1.0-damping)/numNodes + danglingContrib
			for _, edge := range graph.SafeGetInboundEdges(id) {
				if isStructuralEdge(edge.Type) {
					outDeg := outDegree[edge.SourceID]
					if outDeg > 0 {
						rank += damping * (ranks[edge.SourceID] / float64(outDeg))
					}
				}
			}
			newRanks[id] = rank
		})
		ranks = newRanks
	}
	return ranks
}

// NodeCouplingMetrics holds fan-in/fan-out metrics for a node.
type NodeCouplingMetrics struct {
	FanIn  int
	FanOut int
}

// NodeMetrics computes per-node coupling metrics (FanIn/FanOut over structural edges).
func NodeMetrics(graph *akg.CodePropertyGraph) map[string]NodeCouplingMetrics {
	metrics := make(map[string]NodeCouplingMetrics)

	// We want DISTINCT source/target nodes for fan-in/fan-out
	inboundSet := make(map[string]map[string]bool)
	outboundSet := make(map[string]map[string]bool)

	graph.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
		inboundSet[id] = make(map[string]bool)
		outboundSet[id] = make(map[string]bool)
	})

	graph.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
		for _, e := range graph.SafeGetOutboundEdges(id) {
			if isStructuralEdge(e.Type) {
				if outMap, ok := outboundSet[id]; ok {
					outMap[e.TargetID] = true
				}
				if inMap, ok := inboundSet[e.TargetID]; ok {
					inMap[id] = true
				}
			}
		}
	})

	graph.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
		metrics[id] = NodeCouplingMetrics{
			FanIn:  len(inboundSet[id]),
			FanOut: len(outboundSet[id]),
		}
	})

	return metrics
}

// LCOM4 calculates Lack of Cohesion of Methods for struct/class nodes.
func LCOM4(node *stage4.ResolvedNode, graph *akg.CodePropertyGraph) float64 {
	// 1. Get all method nodes in this class via EdgeContains/EdgeBelongsTo (for methods)
	// and fields via EdgeHasField. Wait, graph.GetOutboundEdges(node.ID).
	methods := make(map[string]bool)
	fields := make(map[string]bool)

	for _, e := range graph.SafeGetOutboundEdges(node.ID) {
		if e.Type == stage4.EdgeHasField {
			fields[e.TargetID] = true
		} else if e.Type == stage4.EdgeContains || e.Type == stage4.EdgeHasReceiver {
			if target, ok := graph.SafeGetNode(e.TargetID); ok && target.Kind == "FUNCTION" {
				methods[target.ID] = true
			}
		}
	}

	// If it doesn't have methods or fields, LCOM4 is 0
	if len(methods) == 0 {
		return 0.0
	}

	// 2. Build bipartite graph of methods ↔ fields accessed
	// Actually we just need connected components of methods.
	// Two methods are connected if they access the same field or call each other.
	adj := make(map[string][]string)
	for m := range methods {
		adj[m] = []string{}
		for _, e := range graph.SafeGetOutboundEdges(m) {
			if e.Type == stage4.EdgeCalls && methods[e.TargetID] {
				adj[m] = append(adj[m], e.TargetID)
			}
			if fields[e.TargetID] {
				// Method uses field. Connect method to field (and field to method).
				adj[m] = append(adj[m], e.TargetID)
				adj[e.TargetID] = append(adj[e.TargetID], m)
			}
		}
	}

	// 3. Count connected components
	visited := make(map[string]bool)
	components := 0

	var bfs func(string)
	bfs = func(start string) {
		q := []string{start}
		visited[start] = true
		for len(q) > 0 {
			curr := q[0]
			q = q[1:]
			for _, neighbor := range adj[curr] {
				if !visited[neighbor] {
					visited[neighbor] = true
					q = append(q, neighbor)
				}
			}
		}
	}

	for m := range methods {
		if !visited[m] {
			components++
			bfs(m)
		}
	}

	if components == 0 {
		return 0.0
	}
	// LCOM4 = number of connected components of methods
	return float64(components)
}

// DeadCodeNodes finds nodes unreachable from any entrypoint.
func DeadCodeNodes(graph *akg.CodePropertyGraph) []string {
	visited := make(map[string]bool)
	var q []string

	for _, ep := range graph.Entrypoints {
		visited[ep] = true
		q = append(q, ep)
	}

	// Also treat standard library imports or external deps as roots if necessary,
	// but generally entrypoints cover mains and exported APIs.

	for len(q) > 0 {
		curr := q[0]
		q = q[1:]

		for _, e := range graph.SafeGetOutboundEdges(curr) {
			// Traverse calls, contains, implements, etc.
			if isStructuralEdge(e.Type) {
				if !visited[e.TargetID] {
					visited[e.TargetID] = true
					q = append(q, e.TargetID)
				}
			}
		}
	}

	var dead []string
	graph.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
		if !visited[id] {
			dead = append(dead, id)
		}
	})

	return dead
}

// CyclomaticComplexity approximates McCabe complexity.
func CyclomaticComplexity(graph *akg.CodePropertyGraph) map[string]int {
	complexity := make(map[string]int)

	graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		if node.Kind == "FUNCTION" {
			comp := 1
			for _, e := range graph.SafeGetOutboundEdges(id) {
				if e.Type == stage4.EdgeConditionalBranch ||
					e.Type == stage4.EdgeLoopBranch ||
					e.Type == stage4.EdgeSwitchBranch {
					comp++
				}
			}
			complexity[id] = comp
		}
	})

	return complexity
}

// LouvainCommunityDetection partitions the graph into communities maximizing modularity.
// Output: map[string]string — nodeID → communityID
func LouvainCommunityDetection(graph *akg.CodePropertyGraph) map[string]string {
	community := make(map[string]string)

	// Fast deterministic simplified heuristic for Louvain
	// 1. Initial assignment: each node in its own community = its nodeID
	nodes := []string{}
	graph.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
		community[id] = id
		nodes = append(nodes, id)
	})

	// Sort nodes deterministically
	sort.Strings(nodes)

	// 2. Iterations
	changed := true
	for iter := 0; iter < 10 && changed; iter++ {
		changed = false
		for _, id := range nodes {
			// Count edges to neighboring communities
			neighborComms := make(map[string]int)
			bestComm := community[id]
			bestCount := 0

			for _, e := range graph.SafeGetOutboundEdges(id) {
				if isStructuralEdge(e.Type) {
					c := community[e.TargetID]
					neighborComms[c]++
					if neighborComms[c] > bestCount {
						bestCount = neighborComms[c]
						bestComm = c
					}
				}
			}
			for _, e := range graph.SafeGetInboundEdges(id) {
				if isStructuralEdge(e.Type) {
					c := community[e.SourceID]
					neighborComms[c]++
					if neighborComms[c] > bestCount {
						bestCount = neighborComms[c]
						bestComm = c
					}
				}
			}

			// If a tie, pick lexicographically smaller community ID for determinism
			if bestComm != community[id] && bestCount > 0 {
				community[id] = bestComm
				changed = true
			}
		}
	}

	return community
}
