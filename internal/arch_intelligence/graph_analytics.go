package arch_intelligence

import (
	"sort"
	"strconv"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// isStructuralEdge returns true if the edge represents a high-level architectural
// dependency (as opposed to CFG/DFG low-level details).
func isStructuralEdge(edgeType link.RelationshipType) bool {
	switch edgeType {
	case link.EdgeDependsOn, link.EdgeCalls, link.EdgeImplements,
		link.EdgeExtends, link.EdgeContains, link.EdgeComposes,
		link.EdgeReferences, link.EdgeNetworkCall, link.EdgeQueriesDB,
		link.EdgeCallsCloudAPI, link.EdgePublishes, link.EdgeSubscribes,
		link.EdgeSendsTo, link.EdgeReceivesFrom, link.EdgeDispatchesEvent:
		return true
	default:
		return false
	}
}

// GraphSnapshot is an immutable, lock-free copy of the CPG taken once before
// analytics run. All analytics functions operate on the snapshot so repeated
// reads never touch the live (mutex-guarded) AKG and every phase sees a
// consistent view of the graph.
type GraphSnapshot struct {
	// NodeIDs is the sorted node id list — iteration order is deterministic.
	NodeIDs []string
	Nodes   map[string]*link.ResolvedNode
	// Outbound and Inbound hold the per-node edge lists with stable ordering
	// (sorted by type, then target/source id).
	Outbound    map[string][]link.ResolvedEdge
	Inbound     map[string][]link.ResolvedEdge
	Entrypoints []string
	EdgeCount   int
}

// NewGraphSnapshot captures the current CPG state into a GraphSnapshot.
func NewGraphSnapshot(graph *akg.CodePropertyGraph) *GraphSnapshot {
	snap := &GraphSnapshot{
		Nodes:       make(map[string]*link.ResolvedNode),
		Outbound:    make(map[string][]link.ResolvedEdge),
		Inbound:     make(map[string][]link.ResolvedEdge),
		Entrypoints: nil,
	}
	if graph == nil {
		return snap
	}
	if graph.Entrypoints != nil {
		snap.Entrypoints = append([]string(nil), graph.Entrypoints...)
	}
	if graph.Nodes != nil {
		graph.Nodes.Iterate(func(id string, node *link.ResolvedNode) {
			snap.Nodes[id] = node
			snap.NodeIDs = append(snap.NodeIDs, id)
			snap.Outbound[id] = graph.SafeGetOutboundEdges(id)
			snap.Inbound[id] = graph.SafeGetInboundEdges(id)
		})
	}
	sort.Strings(snap.NodeIDs)
	for id := range snap.Outbound {
		sortEdges(snap.Outbound[id])
	}
	for id := range snap.Inbound {
		sortEdges(snap.Inbound[id])
	}
	for _, edges := range snap.Outbound {
		snap.EdgeCount += len(edges)
	}
	return snap
}

func sortEdges(edges []link.ResolvedEdge) {
	sort.SliceStable(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.TargetID < b.TargetID
	})
}

// Node returns the node with the given id, or nil.
func (s *GraphSnapshot) Node(id string) *link.ResolvedNode {
	return s.Nodes[id]
}

// OutEdges returns the structural outbound edges of id, sorted.
func (s *GraphSnapshot) OutEdges(id string) []link.ResolvedEdge {
	return s.Outbound[id]
}

// InEdges returns the structural inbound edges of id, sorted.
func (s *GraphSnapshot) InEdges(id string) []link.ResolvedEdge {
	return s.Inbound[id]
}

// Len returns the number of nodes in the snapshot.
func (s *GraphSnapshot) Len() int {
	return len(s.NodeIDs)
}

// structuralOutbound filters the outbound edges of id to structural edges
// whose target exists in the snapshot (dangling edges are ignored).
func (s *GraphSnapshot) structuralOutbound(id string) []link.ResolvedEdge {
	edges := s.Outbound[id]
	out := make([]link.ResolvedEdge, 0, len(edges))
	for _, e := range edges {
		if isStructuralEdge(e.Type) {
			if _, ok := s.Nodes[e.TargetID]; ok {
				out = append(out, e)
			}
		}
	}
	return out
}

// SCC finds all strongly connected components over structural edges using an
// iterative Tarjan implementation (no recursion — safe for very deep graphs).
// Nodes are visited in sorted order and edges in sorted order, so the result
// is deterministic. A component with >1 node is a circular dependency.
func SCC(graph *akg.CodePropertyGraph) [][]string {
	if graph == nil {
		return nil
	}
	return SCCIterative(NewGraphSnapshot(graph))
}

// SCCIterative is the snapshot-based iterative Tarjan.
func SCCIterative(snap *GraphSnapshot) [][]string {
	if snap == nil || snap.Len() == 0 {
		return nil
	}

	index := 0
	indices := make(map[string]int, snap.Len())
	lowlink := make(map[string]int, snap.Len())
	onStack := make(map[string]bool, snap.Len())
	var stack []string
	var result [][]string

	type frame struct {
		v     string
		edges []link.ResolvedEdge
		next  int
	}

	push := func(v string) *frame {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true
		return &frame{v: v, edges: snap.structuralOutbound(v)}
	}

	for _, start := range snap.NodeIDs {
		if _, seen := indices[start]; seen {
			continue
		}
		var frames []*frame
		frames = append(frames, push(start))

		for len(frames) > 0 {
			f := frames[len(frames)-1]
			if f.next < len(f.edges) {
				e := f.edges[f.next]
				f.next++
				w := e.TargetID
				if _, unvisited := indices[w]; !unvisited {
					frames = append(frames, push(w))
				} else if onStack[w] {
					if indices[w] < lowlink[f.v] {
						lowlink[f.v] = indices[w]
					}
				}
				continue
			}

			// Finish node f.v.
			v := f.v
			frames = frames[:len(frames)-1]
			if len(frames) > 0 {
				parent := frames[len(frames)-1]
				if lowlink[v] < lowlink[parent.v] {
					lowlink[parent.v] = lowlink[v]
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
				result = append(result, component)
			}
		}
	}
	return result
}

// PageRank computes centrality scores across structural edges using the
// dangling-mass-corrected formula, then normalizes the result so ranks sum to
// 1. High PageRank = architectural hotspot. Deterministic: nodes and edges are
// visited in sorted order.
func PageRank(graph *akg.CodePropertyGraph, iterations int, damping float64) map[string]float64 {
	if graph == nil {
		return nil
	}
	return PageRankSnapshot(NewGraphSnapshot(graph), iterations, damping)
}

// PageRankSnapshot is the snapshot-based PageRank.
func PageRankSnapshot(snap *GraphSnapshot, iterations int, damping float64) map[string]float64 {
	ranks := make(map[string]float64)
	if snap == nil {
		return ranks
	}
	n := snap.Len()
	if n == 0 || iterations <= 0 {
		return ranks
	}
	if damping <= 0 || damping >= 1 {
		damping = 0.85
	}

	initial := 1.0 / float64(n)
	outDegree := make(map[string]int, n)
	for _, id := range snap.NodeIDs {
		ranks[id] = initial
		outDegree[id] = len(snap.structuralOutbound(id))
	}

	for it := 0; it < iterations; it++ {
		newRanks := make(map[string]float64, n)
		var danglingSum float64
		for _, id := range snap.NodeIDs {
			if outDegree[id] == 0 {
				danglingSum += ranks[id]
			}
		}
		danglingContrib := damping * danglingSum / float64(n)

		for _, id := range snap.NodeIDs {
			rank := (1.0-damping)/float64(n) + danglingContrib
			for _, e := range snap.Inbound[id] {
				if !isStructuralEdge(e.Type) {
					continue
				}
				if _, ok := snap.Nodes[e.SourceID]; !ok {
					continue
				}
				if deg := outDegree[e.SourceID]; deg > 0 {
					rank += damping * ranks[e.SourceID] / float64(deg)
				}
			}
			newRanks[id] = rank
		}
		ranks = newRanks
	}

	var total float64
	for _, id := range snap.NodeIDs {
		total += ranks[id]
	}
	if total > 0 {
		for _, id := range snap.NodeIDs {
			ranks[id] /= total
		}
	}
	return ranks
}

// NodeCouplingMetrics holds fan-in/fan-out metrics for a node.
type NodeCouplingMetrics struct {
	FanIn  int
	FanOut int
}

// NodeMetrics computes per-node coupling metrics (FanIn/FanOut over distinct
// structural edge endpoints).
func NodeMetrics(graph *akg.CodePropertyGraph) map[string]NodeCouplingMetrics {
	if graph == nil {
		return nil
	}
	return NodeMetricsSnapshot(NewGraphSnapshot(graph))
}

// NodeMetricsSnapshot is the snapshot-based per-node coupling.
func NodeMetricsSnapshot(snap *GraphSnapshot) map[string]NodeCouplingMetrics {
	metrics := make(map[string]NodeCouplingMetrics)
	if snap == nil {
		return metrics
	}

	inboundSet := make(map[string]map[string]bool, snap.Len())
	outboundSet := make(map[string]map[string]bool, snap.Len())
	for _, id := range snap.NodeIDs {
		inboundSet[id] = make(map[string]bool)
		outboundSet[id] = make(map[string]bool)
	}
	for _, id := range snap.NodeIDs {
		for _, e := range snap.structuralOutbound(id) {
			if outMap, ok := outboundSet[id]; ok {
				outMap[e.TargetID] = true
			}
			if inMap, ok := inboundSet[e.TargetID]; ok {
				inMap[id] = true
			}
		}
	}
	for _, id := range snap.NodeIDs {
		metrics[id] = NodeCouplingMetrics{
			FanIn:  len(inboundSet[id]),
			FanOut: len(outboundSet[id]),
		}
	}
	return metrics
}

// LCOM4 calculates Lack of Cohesion of Methods for a struct/class node.
// Methods are the only vertices; two methods are connected when they share
// field access or call each other. LCOM4 = number of connected components.
func LCOM4(node *link.ResolvedNode, graph *akg.CodePropertyGraph) float64 {
	if graph == nil {
		return 0
	}
	return LCOM4Snapshot(node, NewGraphSnapshot(graph))
}

// LCOM4Snapshot is the snapshot-based LCOM4.
func LCOM4Snapshot(node *link.ResolvedNode, snap *GraphSnapshot) float64 {
	if node == nil || snap == nil {
		return 0
	}
	methods := make(map[string]bool)
	fields := make(map[string]bool)
	for _, e := range snap.Outbound[node.ID] {
		if e.Type == link.EdgeHasField {
			fields[e.TargetID] = true
		} else if e.Type == link.EdgeContains || e.Type == link.EdgeHasReceiver {
			if t, ok := snap.Nodes[e.TargetID]; ok && t.Kind == "FUNCTION" {
				methods[e.TargetID] = true
			}
		}
	}
	if len(methods) == 0 {
		return 0
	}

	ordered := make([]string, 0, len(methods))
	for m := range methods {
		ordered = append(ordered, m)
	}
	sort.Strings(ordered)

	// Method -> set of fields it accesses (any outbound edge to a field node).
	fieldAccess := make(map[string]map[string]bool, len(ordered))
	for _, m := range ordered {
		access := make(map[string]bool)
		for _, e := range snap.Outbound[m] {
			if fields[e.TargetID] {
				access[e.TargetID] = true
			}
		}
		fieldAccess[m] = access
	}

	// Build the method-level connectivity graph.
	adj := make(map[string]map[string]bool, len(ordered))
	for _, m := range ordered {
		adj[m] = make(map[string]bool)
	}
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			a, b := ordered[i], ordered[j]
			connected := false
			for f := range fieldAccess[a] {
				if fieldAccess[b][f] {
					connected = true
					break
				}
			}
			if !connected && methodsCallEachOther(snap, a, b) {
				connected = true
			}
			if connected {
				adj[a][b] = true
				adj[b][a] = true
			}
		}
	}

	visited := make(map[string]bool, len(ordered))
	components := 0
	for _, m := range ordered {
		if visited[m] {
			continue
		}
		components++
		queue := []string{m}
		visited[m] = true
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for nbr := range adj[cur] {
				if !visited[nbr] {
					visited[nbr] = true
					queue = append(queue, nbr)
				}
			}
		}
	}
	return float64(components)
}

// methodsCallEachOther reports whether a and b have a direct CALLS edge in
// either direction.
func methodsCallEachOther(snap *GraphSnapshot, a, b string) bool {
	for _, e := range snap.Outbound[a] {
		if e.Type == link.EdgeCalls && e.TargetID == b {
			return true
		}
	}
	for _, e := range snap.Outbound[b] {
		if e.Type == link.EdgeCalls && e.TargetID == a {
			return true
		}
	}
	return false
}

// isExcludedPath reports whether a file path should be excluded from
// dead-code and component analysis (tests, mocks, vendored/generated code).
func isExcludedPath(p string) bool {
	clean := strings.ReplaceAll(p, "\\", "/")
	lower := strings.ToLower(clean)
	if strings.HasSuffix(lower, "_test.go") {
		return true
	}
	if strings.Contains(lower, "/vendor/") || strings.HasPrefix(lower, "vendor/") {
		return true
	}
	for _, marker := range []string{"/mock", "mock/", "mocks/", "/generated", "generated/"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// isExportedName reports whether a node name looks like exported API surface
// (first rune uppercase). Non-identifier names are not treated as exported.
func isExportedName(name string) bool {
	if name == "" {
		return false
	}
	r := []rune(name)[0]
	return r >= 'A' && r <= 'Z'
}

// isAPISurface reports whether a node is part of the exported public API:
// an exported symbol with no inbound edges. Such nodes are library roots, not
// dead code.
func isAPISurface(snap *GraphSnapshot, id string) bool {
	node := snap.Nodes[id]
	if node == nil || node.Name == "" {
		return false
	}
	if len(snap.Inbound[id]) > 0 {
		return false
	}
	return isExportedName(node.Name)
}

// DeadCodeNodes finds nodes unreachable from any entrypoint. Exported symbols
// with no inbound edges (library API surface) and nodes in test/mock/vendored
// paths are excluded. With no entrypoints at all the result is empty, because
// there is no evidence of deadness.
func DeadCodeNodes(graph *akg.CodePropertyGraph) []string {
	if graph == nil {
		return nil
	}
	return DeadCodeNodesSnapshot(NewGraphSnapshot(graph))
}

// DeadCodeNodesSnapshot is the snapshot-based dead code analysis.
func DeadCodeNodesSnapshot(snap *GraphSnapshot) []string {
	if snap == nil || len(snap.Entrypoints) == 0 {
		return nil
	}
	reachable := make(map[string]bool, snap.Len())
	var queue []string
	for _, ep := range snap.Entrypoints {
		if _, ok := snap.Nodes[ep]; ok && !reachable[ep] {
			reachable[ep] = true
			queue = append(queue, ep)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range snap.structuralOutbound(cur) {
			if !reachable[e.TargetID] {
				reachable[e.TargetID] = true
				queue = append(queue, e.TargetID)
			}
		}
	}

	var dead []string
	for _, id := range snap.NodeIDs {
		if reachable[id] {
			continue
		}
		node := snap.Nodes[id]
		if node == nil || isExcludedPath(node.FileSpec.Path) {
			continue
		}
		switch node.Kind {
		case "FUNCTION", "METHOD", "STRUCT", "CLASS", "INTERFACE", "MODULE":
			if !isAPISurface(snap, id) {
				dead = append(dead, id)
			}
		}
	}
	return dead
}

// CyclomaticComplexity approximates McCabe complexity per function node.
func CyclomaticComplexity(graph *akg.CodePropertyGraph) map[string]int {
	if graph == nil {
		return nil
	}
	return CyclomaticComplexitySnapshot(NewGraphSnapshot(graph))
}

// CyclomaticComplexitySnapshot is the snapshot-based cyclomatic complexity.
func CyclomaticComplexitySnapshot(snap *GraphSnapshot) map[string]int {
	complexity := make(map[string]int)
	if snap == nil {
		return complexity
	}
	for _, id := range snap.NodeIDs {
		node := snap.Nodes[id]
		if node == nil || node.Kind != "FUNCTION" {
			continue
		}
		comp := 1
		for _, e := range snap.Outbound[id] {
			if e.Type == link.EdgeConditionalBranch ||
				e.Type == link.EdgeLoopBranch ||
				e.Type == link.EdgeSwitchBranch {
				comp++
			}
		}
		complexity[id] = comp
	}
	return complexity
}

// LouvainCommunityDetection partitions the graph into communities maximizing
// modularity (Blondel et al.). Deterministic: nodes are processed in sorted
// order and ties break toward the lexicographically smallest community.
func LouvainCommunityDetection(graph *akg.CodePropertyGraph) map[string]string {
	if graph == nil {
		return nil
	}
	return LouvainCommunityDetectionSnapshot(NewGraphSnapshot(graph), 4, 10)
}

// louvainEdge is a weighted undirected adjacency entry.
type louvainEdge struct {
	to int
	w  int
}

// louvainGraph is the symmetrized weighted graph used by Louvain.
type louvainGraph struct {
	size  int            // number of vertices (nodes is nil for aggregated levels)
	nodes []string       // original node ids, sorted (level 0 only)
	idx   map[string]int // node id -> index (level 0 only)
	adj   [][]louvainEdge
	deg   []int // total incident weight, excluding self-loops
	m     int   // total edge weight (each structural edge counts once)
}

func buildLouvainGraph(snap *GraphSnapshot) *louvainGraph {
	g := &louvainGraph{
		size:  snap.Len(),
		nodes: snap.NodeIDs,
		idx:   make(map[string]int, snap.Len()),
		adj:   make([][]louvainEdge, snap.Len()),
		deg:   make([]int, snap.Len()),
	}
	for i, id := range snap.NodeIDs {
		g.idx[id] = i
	}
	seen := make(map[string]int) // "min\x00max" -> weight (symmetrized edges counted once)
	for i, id := range snap.NodeIDs {
		for _, e := range snap.structuralOutbound(id) {
			j, ok := g.idx[e.TargetID]
			if !ok || i == j {
				continue
			}
			a, b := i, j
			if a > b {
				a, b = b, a
			}
			key := strconv.Itoa(a) + "\x00" + strconv.Itoa(b)
			seen[key]++
		}
	}
	for key, w := range seen {
		parts := strings.Split(key, "\x00")
		if len(parts) != 2 {
			continue
		}
		a, errA := strconv.Atoi(parts[0])
		b, errB := strconv.Atoi(parts[1])
		if errA != nil || errB != nil {
			continue
		}
		g.adj[a] = append(g.adj[a], louvainEdge{to: b, w: w})
		g.adj[b] = append(g.adj[b], louvainEdge{to: a, w: w})
		g.m += w
		g.deg[a] += w
		g.deg[b] += w
	}
	// Sort adjacency deterministically.
	for i := range g.adj {
		sort.Slice(g.adj[i], func(x, y int) bool { return g.adj[i][x].to < g.adj[i][y].to })
	}
	return g
}

// louvainDeltaQ is the exact modularity gain of moving node i (weighted
// degree kI) into a community with total degree sumTot, where kIn is the
// weight of edges between i and that community.
func louvainDeltaQ(kIn, sumTot, kI, m float64) float64 {
	inv2m := 1.0 / (2 * m)
	qNew := (sumTot+kIn)*inv2m - (sumTot+kI)*inv2m*(sumTot+kI)*inv2m
	qOld := sumTot*inv2m - sumTot*inv2m*sumTot*inv2m - kI*inv2m*kI*inv2m
	return qNew - qOld
}

// louvainPhase1 runs the local-moving phase; returns true if any node moved.
func louvainPhase1(g *louvainGraph, comm []int, maxPasses int) bool {
	if g.m == 0 {
		return false
	}
	m := float64(g.m)
	commDegree := make([]int, g.size)
	for i, c := range comm {
		commDegree[c] += g.deg[i]
	}
	changed := false
	for pass := 0; pass < maxPasses; pass++ {
		moved := false
		for i := 0; i < g.size; i++ {
			cur := comm[i]
			if g.deg[i] == 0 {
				continue
			}
			// k_i_in per neighbor community (self-loops excluded).
			gains := make(map[int]int)
			for _, e := range g.adj[i] {
				if e.to != i {
					gains[comm[e.to]] += e.w
				}
			}
			best := cur
			bestGain := 0.0
			for c, kIn := range gains {
				if c == cur {
					continue
				}
				gain := louvainDeltaQ(float64(kIn), float64(commDegree[c]), float64(g.deg[i]), m)
				if gain > bestGain+1e-12 {
					bestGain = gain
					best = c
				} else if gain > bestGain-1e-12 && c < best {
					// Tie: prefer the lexicographically smallest community id.
					bestGain = gain
					best = c
				}
			}
			if best != cur {
				comm[i] = best
				commDegree[cur] -= g.deg[i]
				commDegree[best] += g.deg[i]
				moved = true
			}
		}
		if !moved {
			break
		}
		changed = true
	}
	return changed
}

// louvainAggregate builds the next-level graph from a community assignment.
// Returns the new graph and the relabeling (old community -> new vertex).
func louvainAggregate(g *louvainGraph, comm []int) (*louvainGraph, map[int]int) {
	relabel := make(map[int]int)
	order := make([]int, 0, len(g.nodes))
	for _, c := range comm {
		if _, ok := relabel[c]; !ok {
			relabel[c] = len(relabel)
			order = append(order, c)
		}
	}
	sort.Ints(order) // deterministic vertex order: ascending old community id

	newIdx := make(map[int]int, len(order))
	for k, c := range order {
		newIdx[c] = k
	}

	ng := &louvainGraph{
		size: len(order),
		adj:  make([][]louvainEdge, len(order)),
		deg:  make([]int, len(order)),
	}
	for i, c := range comm {
		nc := newIdx[c]
		ng.deg[nc] += g.deg[i]
		for _, e := range g.adj[i] {
			nc2 := newIdx[comm[e.to]]
			ng.adj[nc] = append(ng.adj[nc], louvainEdge{to: nc2, w: e.w})
		}
	}
	ng.m = g.m
	// Deduplicate aggregated adjacency (sum parallel edges).
	for i := range ng.adj {
		wmap := make(map[int]int)
		for _, e := range ng.adj[i] {
			wmap[e.to] += e.w
		}
		merged := make([]louvainEdge, 0, len(wmap))
		for to, w := range wmap {
			merged = append(merged, louvainEdge{to: to, w: w})
		}
		sort.Slice(merged, func(x, y int) bool { return merged[x].to < merged[y].to })
		ng.adj[i] = merged
	}
	return ng, relabel
}

// LouvainCommunityDetectionSnapshot runs the real Louvain algorithm on the
// snapshot with deterministic tie-breaking. maxLevels caps aggregation levels,
// maxPasses caps the local-moving passes per level.
func LouvainCommunityDetectionSnapshot(snap *GraphSnapshot, maxLevels, maxPasses int) map[string]string {
	result := make(map[string]string)
	if snap == nil || snap.Len() == 0 {
		return result
	}
	if snap.Len() == 1 {
		result[snap.NodeIDs[0]] = "0"
		return result
	}
	if maxLevels <= 0 {
		maxLevels = 4
	}
	if maxPasses <= 0 {
		maxPasses = 10
	}

	g := buildLouvainGraph(snap)
	n := len(g.nodes)
	comm := make([]int, n)
	for i := range comm {
		comm[i] = i
	}
	// orig[i] = community (current graph vertex) of original node i.
	orig := make([]int, n)
	for i := range orig {
		orig[i] = i
	}

	for level := 0; level < maxLevels; level++ {
		changed := louvainPhase1(g, comm, maxPasses)
		// Fold the current partition into the original-node assignment.
		for i := range orig {
			orig[i] = comm[orig[i]]
		}
		if !changed || len(comm) <= 1 {
			break
		}
		// Relabel communities to contiguous ids (sorted order — must match
		// the vertex ordering louvainAggregate produces) and aggregate.
		seen := make(map[int]bool)
		order := make([]int, 0, len(comm))
		for _, c := range comm {
			if !seen[c] {
				seen[c] = true
				order = append(order, c)
			}
		}
		sort.Ints(order)
		relabel := make(map[int]int, len(order))
		for k, c := range order {
			relabel[c] = k
		}
		for i := range orig {
			orig[i] = relabel[orig[i]]
		}
		g, _ = louvainAggregate(g, comm)
		comm = make([]int, g.size)
		for i := range comm {
			comm[i] = i
		}
	}

	for i, id := range snap.NodeIDs {
		result[id] = strconv.Itoa(orig[i])
	}
	return result
}
