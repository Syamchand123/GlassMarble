package akg

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

const CurrentSchemaVersion = 2

type ArchitecturalSummary struct {
	PrimaryPatterns   []string       `json:"primary_patterns"`
	LayerDistribution map[string]int `json:"layer_distribution"`
	HotspotNodes      []string       `json:"hotspot_nodes"`
	EntryPoints       []string       `json:"entry_points"`
	ExternalDeps      []string       `json:"external_deps"`
	GeneratedAt       time.Time      `json:"generated_at"`
}

// CodePropertyGraph represents the live in-memory persistent database of the Architectural Knowledge Graph (AKG).
type CodePropertyGraph struct {
	mu            sync.RWMutex                            `json:"-"`
	SchemaVersion int                                     `json:"schema_version"`
	CommitHash    string                                  `json:"commit_hash"`
	Version       uint64                                  `json:"version"`
	Nodes         *CowMap[string, *stage4.ResolvedNode]   `json:"-"`
	macroCache    *CowMap[string, []string]               `json:"-"`
	macroHash     string                                  `json:"-"`
	OutboundEdges *CowMap[string, []stage4.ResolvedEdge]  `json:"-"`
	InboundEdges  *CowMap[string, []stage4.ResolvedEdge]  `json:"-"`
	FileNodeIndex *CowMap[string, map[string]bool]        `json:"-"`
	LineIndex     *CowMap[string, []*stage4.ResolvedNode] `json:"-"`
	MacroRules    *CowMap[string, []string]               `json:"-"`
	KindIndex     *CowMap[string, map[string]bool]        `json:"-"`
	HashIndex     *CowMap[string, []string]               `json:"-"`
	Summary       *ArchitecturalSummary                   `json:"summary,omitempty"`
	Errors        []DanglingReferenceError                `json:"errors,omitempty"`
	Entrypoints   []string                                `json:"entrypoints,omitempty"`
	FolderZones   *CowMap[string, string]                 `json:"-"`
	// Verified and VerificationMsg are set by the post-write verification step
	// (AUDIT Issue 5 Phase 5A-1): Verified=false means the persisted TTL
	// contained findings (currently: dangling edges) and should be inspected.
	Verified        bool
	VerificationMsg string
}

// DanglingReferenceError records structural edge breakage during invariant audit.
type DanglingReferenceError struct {
	SourceID   string `json:"source_id"`
	TargetID   string `json:"target_id"`
	EdgeType   string `json:"edge_type"`
	LineNumber int    `json:"line_number"`
	Message    string `json:"message"`
}

// NewCodePropertyGraph creates a fresh CodePropertyGraph.
func NewCodePropertyGraph(commitHash string) *CodePropertyGraph {
	return &CodePropertyGraph{
		SchemaVersion: CurrentSchemaVersion,
		CommitHash:    commitHash,
		Version:       1,
		Nodes:         NewCowMap[string, *stage4.ResolvedNode](),
		OutboundEdges: NewCowMap[string, []stage4.ResolvedEdge](),
		InboundEdges:  NewCowMap[string, []stage4.ResolvedEdge](),
		FileNodeIndex: NewCowMap[string, map[string]bool](),
		LineIndex:     NewCowMap[string, []*stage4.ResolvedNode](),
		MacroRules:    NewCowMap[string, []string](),
		KindIndex:     NewCowMap[string, map[string]bool](),
		HashIndex:     NewCowMap[string, []string](),
		Entrypoints:   make([]string, 0),
		FolderZones:   NewCowMap[string, string](),
		macroCache:    NewCowMap[string, []string](),
		Errors:        nil,
	}
}

// GetNode implements stage4.GraphDB interface for incremental lookups.
func (c *CodePropertyGraph) GetNode(id string) (*stage4.ResolvedNode, bool) {
	if c.Nodes == nil {
		return nil, false
	}
	return c.Nodes.Get(id)
}

// GetNodesByKind implements stage4.GraphDB interface for bulk interface/struct lookups.
func (c *CodePropertyGraph) GetNodesByKind(kind string) []*stage4.ResolvedNode {
	var results []*stage4.ResolvedNode
	if c.KindIndex == nil || c.Nodes == nil {
		return results
	}
	nodeSet, exists := c.KindIndex.Get(kind)
	if !exists {
		return results
	}
	for nodeID := range nodeSet {
		if node, ok := c.Nodes.Get(nodeID); ok {
			results = append(results, node)
		}
	}
	return results
}

// GetOutboundEdges implements stage4.GraphDB interface for incremental edge traversal.
func (c *CodePropertyGraph) GetOutboundEdges(id string) []stage4.ResolvedEdge {
	if c.OutboundEdges == nil {
		return nil
	}
	edges, _ := c.OutboundEdges.Get(id)
	return edges
}

// GetInboundEdges allows backwards traversal for dependency impact analysis.
func (c *CodePropertyGraph) GetInboundEdges(id string) []stage4.ResolvedEdge {
	if c.InboundEdges == nil {
		return nil
	}
	edges, _ := c.InboundEdges.Get(id)
	return edges
}

// FindPath executes a Breadth-First Search (BFS) to find the shortest path between two architectural components.
// Essential for Reachability Analysis (e.g. checking if API route can hit a secure database sink).
func (c *CodePropertyGraph) FindPath(startID, targetID string, maxDepth int) []string {
	if c.Nodes == nil {
		return nil
	}
	_, startOK := c.Nodes.Get(startID)
	_, targetOK := c.Nodes.Get(targetID)
	if !startOK || !targetOK {
		return nil
	}

	queue := [][]string{{startID}}
	visited := make(map[string]bool)

	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]

		curr := path[len(path)-1]
		if curr == targetID {
			return path
		}

		if len(path) >= maxDepth {
			continue
		}

		if !visited[curr] {
			visited[curr] = true
			for _, edge := range c.GetOutboundEdges(curr) {
				if !visited[edge.TargetID] {
					newPath := make([]string, len(path))
					copy(newPath, path)
					newPath = append(newPath, edge.TargetID)
					queue = append(queue, newPath)
				}
			}
		}
	}
	return nil
}

// GetOrphanNodes identifies dead code or unreachable components.
// Returns a list of node IDs that have 0 inbound edges and are not flagged as entrypoints.
func (c *CodePropertyGraph) GetOrphanNodes() []string {
	var orphans []string
	entrypointMap := make(map[string]bool)
	for _, ep := range c.Entrypoints {
		entrypointMap[ep] = true
	}

	c.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
		if len(c.GetInboundEdges(id)) == 0 && !entrypointMap[id] {
			orphans = append(orphans, id)
		}
	})
	return orphans
}

// DetectCycles uses Tarjan's strongly connected components algorithm to find circular dependencies.
// It returns a list of cycles, where each cycle is a list of node IDs involved in the circular dependency.
func (c *CodePropertyGraph) DetectCycles() [][]string {
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

		for _, edge := range c.GetOutboundEdges(v) {
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
			if len(component) > 1 {
				cycles = append(cycles, component)
			}
		}
	}

	c.Nodes.Iterate(func(v string, _ *stage4.ResolvedNode) {
		if _, ok := indices[v]; !ok {
			strongconnect(v)
		}
	})

	return cycles
}

// GetTopologicalSort performs a topological sort of the graph using Kahn's algorithm.
func (c *CodePropertyGraph) GetTopologicalSort() ([]string, bool) {
	inDegree := make(map[string]int)
	var queue []string
	var sorted []string

	// Initialize in-degrees
	c.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
		inDegree[id] = 0
	})
	c.OutboundEdges.Iterate(func(_ string, edges []stage4.ResolvedEdge) {
		for _, edge := range edges {
			inDegree[edge.TargetID]++
		}
	})

	// Find nodes with 0 in-degree
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		sorted = append(sorted, curr)

		for _, edge := range c.GetOutboundEdges(curr) {
			inDegree[edge.TargetID]--
			if inDegree[edge.TargetID] == 0 {
				queue = append(queue, edge.TargetID)
			}
		}
	}

	if len(sorted) == c.Nodes.Len() {
		return sorted, true
	}
	return sorted, false
}

// GetStructuralSimilarity computes the Jaccard similarity index between two nodes.
func (c *CodePropertyGraph) GetStructuralSimilarity(nodeA, nodeB string) float64 {
	edgesA := c.GetOutboundEdges(nodeA)
	edgesB := c.GetOutboundEdges(nodeB)

	if len(edgesA) == 0 && len(edgesB) == 0 {
		return 1.0
	}

	setA := make(map[string]bool)
	for _, e := range edgesA {
		setA[e.TargetID] = true
	}

	intersection := 0
	unionMap := make(map[string]bool)
	for k := range setA {
		unionMap[k] = true
	}

	for _, e := range edgesB {
		if setA[e.TargetID] {
			intersection++
		}
		unionMap[e.TargetID] = true
	}

	if len(unionMap) == 0 {
		return 1.0
	}
	return float64(intersection) / float64(len(unionMap))
}

// FindArticulationPoints uses Hopcroft-Tarjan's algorithm to find "Cut Vertices" in the graph.
func (c *CodePropertyGraph) FindArticulationPoints() []string {
	var articulationPoints []string
	visited := make(map[string]bool)
	discoveryTime := make(map[string]int)
	low := make(map[string]int)
	parent := make(map[string]string)
	apSet := make(map[string]bool)
	timeCounter := 0

	var dfs func(u string)
	dfs = func(u string) {
		visited[u] = true
		discoveryTime[u] = timeCounter
		low[u] = timeCounter
		timeCounter++
		children := 0

		neighbors := make(map[string]bool)
		for _, edge := range c.GetOutboundEdges(u) {
			neighbors[edge.TargetID] = true
		}
		for _, edge := range c.GetInboundEdges(u) {
			neighbors[edge.SourceID] = true
		}

		for v := range neighbors {
			if !visited[v] {
				parent[v] = u
				children++
				dfs(v)

				if low[v] < low[u] {
					low[u] = low[v]
				}

				if parent[u] == "" && children > 1 {
					apSet[u] = true
				}
				if parent[u] != "" && low[v] >= discoveryTime[u] {
					apSet[u] = true
				}
			} else if v != parent[u] {
				if discoveryTime[v] < low[u] {
					low[u] = discoveryTime[v]
				}
			}
		}
	}

	c.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
		if !visited[id] {
			dfs(id)
		}
	})

	for ap := range apSet {
		articulationPoints = append(articulationPoints, ap)
	}
	return articulationPoints
}

// CalculateInstability calculates Martin's Instability Metric (I = Ce / (Ca + Ce)).
func (c *CodePropertyGraph) CalculateInstability(nodeID string) float64 {
	ce := float64(len(c.GetOutboundEdges(nodeID)))
	ca := float64(len(c.GetInboundEdges(nodeID)))

	if ca+ce == 0 {
		return 0.0
	}
	return ce / (ca + ce)
}

// CalculateImpactRadius determines the "Blast Radius" of a component if it were to change.
func (c *CodePropertyGraph) CalculateImpactRadius(nodeID string) int {
	visited := make(map[string]bool)
	queue := []string{nodeID}
	visited[nodeID] = true
	count := 0

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		for _, edge := range c.GetInboundEdges(curr) {
			if !visited[edge.SourceID] {
				visited[edge.SourceID] = true
				queue = append(queue, edge.SourceID)
				count++
			}
		}
	}
	return count
}

// CalculatePageRank computes the PageRank of all nodes in the graph to determine architectural importance.
func (c *CodePropertyGraph) CalculatePageRank(iterations int, dampingFactor float64) map[string]float64 {
	ranks := make(map[string]float64)
	numNodes := float64(c.Nodes.Len())
	if numNodes == 0 {
		return ranks
	}

	initialRank := 1.0 / numNodes
	c.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
		ranks[id] = initialRank
	})

	// Pre-compute out-degree once so the per-iteration loops do not re-scan
	// every node's edge list (also keeps dangling detection O(1) per node).
	outDegree := make(map[string]int)
	c.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
		outDegree[id] = len(c.GetOutboundEdges(id))
	})

	for i := 0; i < iterations; i++ {
		newRanks := make(map[string]float64)

		// Dangling nodes (no outbound edges) leak mass: their rank never
		// propagates. Redistribute that mass evenly, matching the parallel
		// implementation in visualization_engine/stage2/metrics.go, so rank
		// stays conserved and pageRanks remain comparable across engines.
		var danglingSum float64
		for id, deg := range outDegree {
			if deg == 0 {
				danglingSum += ranks[id]
			}
		}
		danglingContrib := dampingFactor * danglingSum / numNodes

		c.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
			rank := (1.0-dampingFactor)/numNodes + danglingContrib
			for _, edge := range c.GetInboundEdges(id) {
				outDegree := outDegree[edge.SourceID]
				if outDegree > 0 {
					rank += dampingFactor * (ranks[edge.SourceID] / float64(outDegree))
				}
			}
			newRanks[id] = rank
		})
		ranks = newRanks
	}
	return ranks
}

// FindIsolatedIslands detects completely disconnected clusters of nodes (Islands).
func (c *CodePropertyGraph) FindIsolatedIslands() [][]string {
	visited := make(map[string]bool)
	var islands [][]string

	isEntrypoint := make(map[string]bool)
	for _, ep := range c.Entrypoints {
		isEntrypoint[ep] = true
	}

	c.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
		if visited[id] {
			return
		}
		var island []string
		queue := []string{id}
		visited[id] = true
		hasEntrypoint := false

		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			island = append(island, curr)
			if isEntrypoint[curr] {
				hasEntrypoint = true
			}

			for _, edge := range c.GetOutboundEdges(curr) {
				if !visited[edge.TargetID] {
					visited[edge.TargetID] = true
					queue = append(queue, edge.TargetID)
				}
			}
			for _, edge := range c.GetInboundEdges(curr) {
				if !visited[edge.SourceID] {
					visited[edge.SourceID] = true
					queue = append(queue, edge.SourceID)
				}
			}
		}

		if !hasEntrypoint && len(island) > 1 {
			islands = append(islands, island)
		}
	})
	return islands
}

// DetectGodObjects identifies components that have assumed too much responsibility using statistical analysis.
func (c *CodePropertyGraph) DetectGodObjects() []string {
	var godObjects []string
	var totalFanIn, totalFanOut float64
	var count float64

	relevantKinds := map[string]bool{"STRUCT": true, "CLASS": true, "MODULE": true, "FILE": true}

	c.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		if relevantKinds[node.Kind] {
			totalFanIn += float64(len(c.GetInboundEdges(id)))
			totalFanOut += float64(len(c.GetOutboundEdges(id)))
			count++
		}
	})

	if count == 0 {
		return godObjects
	}

	meanFanIn := totalFanIn / count
	meanFanOut := totalFanOut / count

	var varFanIn, varFanOut float64
	c.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		if relevantKinds[node.Kind] {
			fanIn := float64(len(c.GetInboundEdges(id)))
			fanOut := float64(len(c.GetOutboundEdges(id)))
			varFanIn += math.Pow(fanIn-meanFanIn, 2)
			varFanOut += math.Pow(fanOut-meanFanOut, 2)
		}
	})

	stdDevFanIn := math.Sqrt(varFanIn / count)
	stdDevFanOut := math.Sqrt(varFanOut / count)

	thresholdIn := meanFanIn + (2 * stdDevFanIn)
	thresholdOut := meanFanOut + (2 * stdDevFanOut)

	totalRelevant := count
	adaptiveMin := math.Sqrt(totalRelevant)
	if thresholdIn < adaptiveMin {
		thresholdIn = adaptiveMin
	}
	if thresholdOut < adaptiveMin {
		thresholdOut = adaptiveMin
	}

	c.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		if relevantKinds[node.Kind] {
			fanIn := float64(len(c.GetInboundEdges(id)))
			fanOut := float64(len(c.GetOutboundEdges(id)))
			if fanIn > thresholdIn && fanOut > thresholdOut {
				godObjects = append(godObjects, id)
			}
		}
	})
	return godObjects
}

// CalculateBetweennessCentrality calculates Brandes' Betweenness Centrality for major components.
func (c *CodePropertyGraph) CalculateBetweennessCentrality(includeAll ...bool) map[string]float64 {
	bc := make(map[string]float64)

	includeAllNodes := false
	if len(includeAll) > 0 {
		includeAllNodes = includeAll[0]
	}

	var majorNodes []string
	c.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		if includeAllNodes {
			majorNodes = append(majorNodes, id)
			bc[id] = 0.0
		} else if node.Kind == "STRUCT" || node.Kind == "CLASS" || node.Kind == "MODULE" || node.Kind == "PACKAGE" || node.Kind == "FILE" {
			majorNodes = append(majorNodes, id)
			bc[id] = 0.0
		}
	})

	for _, s := range majorNodes {
		var stack []string
		var queue []string

		sigma := make(map[string]float64)
		dist := make(map[string]int)
		pred := make(map[string][]string)

		sigma[s] = 1
		dist[s] = 0
		queue = append(queue, s)

		for len(queue) > 0 {
			v := queue[0]
			queue = queue[1:]
			stack = append(stack, v)

			for _, edge := range c.GetOutboundEdges(v) {
				w := edge.TargetID
				// bc is keyed by exactly the major nodes (see the init loop
				// above); edges to non-major targets are excluded from the
				// BFS, matching the original pre-populated-sigma filter.
				if _, isMajor := bc[w]; !isMajor {
					continue
				}
				if _, ok := dist[w]; ok {
					if dist[w] == dist[v]+1 {
						sigma[w] += sigma[v]
						pred[w] = append(pred[w], v)
					}
					continue
				}
				dist[w] = dist[v] + 1
				sigma[w] = sigma[v]
				pred[w] = append(pred[w], v)
				queue = append(queue, w)
			}
		}

		delta := make(map[string]float64)
		for i := len(stack) - 1; i >= 0; i-- {
			w := stack[i]
			for _, v := range pred[w] {
				if sigma[w] > 0 {
					delta[v] += (sigma[v] / sigma[w]) * (1.0 + delta[w])
				}
			}
			if w != s {
				bc[w] += delta[w]
			}
		}
	}

	// The AKG is a DIRECTED graph (edges have a source and target). Brandes'
	// algorithm accumulates betweenness per directed traversal; the "/2"
	// correction is only valid for UNDIRECTED graphs where each edge is counted
	// in both directions. Matching the visualization engine's directed
	// implementation (stage2/metrics.go ComputeBetweenness), no halving is
	// applied here.
	return bc
}

// CalculatePackageCohesion calculates the Relational Cohesion of a module or package.
func (c *CodePropertyGraph) CalculatePackageCohesion(packageID string) float64 {
	var components []string
	componentSet := make(map[string]bool)

	foundBelongsTo := false
	for _, e := range c.GetInboundEdges(packageID) {
		if e.Type == stage4.EdgeBelongsTo {
			components = append(components, e.SourceID)
			componentSet[e.SourceID] = true
			foundBelongsTo = true
		}
	}

	if !foundBelongsTo {
		c.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
			for _, e := range c.GetOutboundEdges(id) {
				if e.TargetID == packageID {
					components = append(components, id)
					componentSet[id] = true
					return
				}
			}
		})
	}

	numComponents := float64(len(components))
	if numComponents == 0 {
		return 0.0
	}

	internalEdges := 0.0
	for _, id := range components {
		for _, e := range c.GetOutboundEdges(id) {
			if componentSet[e.TargetID] {
				internalEdges++
			}
		}
	}

	return internalEdges / numComponents
}

// Clone performs a deep shadow copy of the CodePropertyGraph for MVCC transaction isolation.
func (cpg *CodePropertyGraph) Clone() *CodePropertyGraph {
	if cpg == nil {
		return NewCodePropertyGraph("")
	}

	cpg.mu.RLock()
	defer cpg.mu.RUnlock()

	clone := &CodePropertyGraph{
		SchemaVersion: cpg.SchemaVersion,
		CommitHash:    cpg.CommitHash,
		Version:       cpg.Version,
		Nodes:         cpg.Nodes.Clone(),
		OutboundEdges: cpg.OutboundEdges.Clone(),
		InboundEdges:  cpg.InboundEdges.Clone(),
		FileNodeIndex: cpg.FileNodeIndex.Clone(),
		LineIndex:     cpg.LineIndex.Clone(),
		MacroRules:    cpg.MacroRules.Clone(),
		KindIndex:     cpg.KindIndex.Clone(),
		HashIndex:     cpg.HashIndex.Clone(),
		FolderZones:   cpg.FolderZones.Clone(),
		Errors:        cpg.Errors[:len(cpg.Errors):len(cpg.Errors)],
		macroCache:    cpg.macroCache.Clone(),
		macroHash:     cpg.macroHash,
	}
	clone.Entrypoints = append([]string(nil), cpg.Entrypoints...)

	if cpg.Summary != nil {
		s := *cpg.Summary
		clone.Summary = &s
	}
	clone.Verified = cpg.Verified
	clone.VerificationMsg = cpg.VerificationMsg

	return clone
}

// detachNodesForWrite deep-copies the Properties and PrimitiveScores maps of
// every node in the graph and re-registers the detached copies in the Nodes
// index. The reasoner derives metrics (pagerank, betweenness, macro_rules,
// cohesion, ...) by writing to node.Properties. When inference runs on an MVCC
// shadow (transaction_manager.go applyDeltaToShadow), unchanged nodes are
// still SHARED pointers with the active graph; mutating them in place would
// race with concurrent readers of the active snapshot (SafeQuery, AI bridge).
// Detaching first guarantees every reasoner write lands on shadow-private maps.
func (c *CodePropertyGraph) detachNodesForWrite() {
	if c == nil || c.Nodes == nil {
		return
	}
	c.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		if node == nil {
			return
		}
		clone := *node
		if node.Properties != nil {
			props := make(map[string]string, len(node.Properties))
			for k, v := range node.Properties {
				props[k] = v
			}
			clone.Properties = props
		}
		if node.PrimitiveScores != nil {
			scores := make(map[string]float64, len(node.PrimitiveScores))
			for k, v := range node.PrimitiveScores {
				scores[k] = v
			}
			clone.PrimitiveScores = scores
		}
		c.Nodes = c.Nodes.Set(id, &clone)
	})
}

// SafeGetNode is a concurrency-safe wrapper that acquires the read lock.
func (c *CodePropertyGraph) SafeGetNode(id string) (*stage4.ResolvedNode, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.GetNode(id)
}

// SafeGetOutboundEdges is a concurrency-safe wrapper.
func (c *CodePropertyGraph) SafeGetOutboundEdges(id string) []stage4.ResolvedEdge {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.GetOutboundEdges(id)
}

// SafeGetInboundEdges is a concurrency-safe wrapper.
func (c *CodePropertyGraph) SafeGetInboundEdges(id string) []stage4.ResolvedEdge {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.GetInboundEdges(id)
}

// SafeGetNodesByKind is a concurrency-safe wrapper.
func (c *CodePropertyGraph) SafeGetNodesByKind(kind string) []*stage4.ResolvedNode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.GetNodesByKind(kind)
}

// SafeDetectCycles is a concurrency-safe wrapper.
func (c *CodePropertyGraph) SafeDetectCycles() [][]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.DetectCycles()
}

// SafeFindArticulationPoints is a concurrency-safe wrapper.
func (c *CodePropertyGraph) SafeFindArticulationPoints() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.FindArticulationPoints()
}

// SafeCalculatePageRank is a concurrency-safe wrapper.
func (c *CodePropertyGraph) SafeCalculatePageRank(iterations int, dampingFactor float64) map[string]float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.CalculatePageRank(iterations, dampingFactor)
}

// SafeCalculateBetweennessCentrality is a concurrency-safe wrapper.
func (c *CodePropertyGraph) SafeCalculateBetweennessCentrality(includeAll ...bool) map[string]float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.CalculateBetweennessCentrality(includeAll...)
}

// SafeFindPath is a concurrency-safe wrapper.
func (c *CodePropertyGraph) SafeFindPath(startID, targetID string, maxDepth int) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.FindPath(startID, targetID, maxDepth)
}

// MVCCGraphContainer handles atomic MVCC graph snapshot swapping.
type MVCCGraphContainer struct {
	txCounter   uint64
	mu          sync.RWMutex
	ActiveGraph *CodePropertyGraph
}

// NewMVCCGraphContainer initializes an MVCC container with an empty primary graph.
func NewMVCCGraphContainer() *MVCCGraphContainer {
	return &MVCCGraphContainer{
		ActiveGraph: NewCodePropertyGraph("initial"),
	}
}

// GetSnapshot returns a thread-safe read-only pointer to the active graph state.
func (mc *MVCCGraphContainer) GetSnapshot() *CodePropertyGraph {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.ActiveGraph
}

// AllocateShadowSnapshot creates an isolated shadow clone for writing an incoming transaction.
func (mc *MVCCGraphContainer) AllocateShadowSnapshot() (*CodePropertyGraph, uint64) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	txID := atomic.AddUint64(&mc.txCounter, 1)
	shadow := mc.ActiveGraph.Clone()
	shadow.Version = txID
	return shadow, txID
}

// PromoteShadowSnapshot atomically replaces the active graph state with the validated shadow graph.
func (mc *MVCCGraphContainer) PromoteShadowSnapshot(shadow *CodePropertyGraph) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.ActiveGraph = shadow
}
