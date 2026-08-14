package akg

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

const (
	// RuleTierHeuristic identifies rules based only on name patterns with no structural evidence.
	// These are speculative and may produce false positives.
	RuleTierHeuristic = "heuristic"

	// RuleTierStructural identifies rules backed by graph edges or primitives.
	// These require structural evidence to fire.
	RuleTierStructural = "structural"

	// RuleTierArchitectural identifies rules derived from graph topology analysis
	// (PageRank, cycle detection, centrality, etc.).
	RuleTierArchitectural = "architectural"
)

// maxMacroCacheEntries bounds the macro-inference cache (AUDIT Issue 4
// Phase 4B-7): one entry per distinct node key, capped to prevent unbounded
// memory growth on large graphs.
const maxMacroCacheEntries = 10000

// capMacroCache resets the macro-inference cache when it exceeds the bound.
func capMacroCache(cache *CowMap[string, []string]) *CowMap[string, []string] {
	if cache != nil && cache.Len() > maxMacroCacheEntries {
		return NewCowMap[string, []string]()
	}
	return cache
}

// RunTopologicalMacroInference executes Sub-Step D.2: Topological Macro-Inference Parsing.
// It walks execution paths across functional primitives to infer high-level C4 component rules.
// An optional LinkerConfig controls which tiers of rules are applied.
func RunTopologicalMacroInference(graph *CodePropertyGraph, config ...link.LinkerConfig) {
	if graph == nil || graph.Nodes.Len() == 0 {
		return
	}

	// Detach node Properties/PrimitiveScores so derived-metric writes below
	// never mutate maps shared with the active graph snapshot (concurrent
	// readers via SafeQuery / the AI bridge would otherwise race). The shadow
	// snapshot shares unchanged node pointers with the active graph
	// (transaction_manager.go graft phase); deep-copying before inference
	// preserves MVCC isolation.
	graph.detachNodesForWrite()

	// Determine macro inference mode from config
	macroMode := "all"
	if len(config) > 0 {
		if config[0].MacroInference != "" {
			macroMode = config[0].MacroInference
		}
	}

	if macroMode == "disabled" {
		return
	}

	// Extract disabled rules from config
	disabledRules := make(map[string]bool)
	if len(config) > 0 {
		for _, ruleID := range config[0].DisabledRules {
			disabledRules[ruleID] = true
		}
	}

	if graph.macroCache == nil {
		graph.macroCache = NewCowMap[string, []string]()
	}
	graph.macroCache = capMacroCache(graph.macroCache)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)
	var mu sync.Mutex

	// Collect node info first to avoid concurrent map iteration with goroutines
	type nodeTask struct {
		id       string
		node     *link.ResolvedNode
		cacheKey string
	}
	var tasks []nodeTask
	mu.Lock()
	graph.Nodes.Iterate(func(nodeID string, node *link.ResolvedNode) {
		if node == nil {
			return
		}
		if node.Kind == "MODULE" || node.Kind == "FILE" || node.Kind == "STRUCT" || node.Kind == "CLASS" || node.Kind == "FUNCTION" {
			key := nodeMacroKey(node, graph, macroMode, disabledRules)
			if cached, ok := graph.macroCache.Get(key); ok {
				graph.MacroRules = graph.MacroRules.Set(nodeID, cached)
				if node.Properties == nil {
					node.Properties = make(map[string]string)
				}
				node.Properties["macro_rules"] = strings.Join(cached, " | ")
				return
			}
			tasks = append(tasks, nodeTask{id: nodeID, node: node, cacheKey: key})
		}
	})
	mu.Unlock()

	for _, task := range tasks {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string, n *link.ResolvedNode, key string) {
			defer wg.Done()
			defer func() { <-sem }()
			inferMacroRulesForNode(id, n, graph, &mu, macroMode, disabledRules)

			mu.Lock()
			graph.macroCache = capMacroCache(graph.macroCache)
			rules, _ := graph.MacroRules.Get(id)
			graph.macroCache = graph.macroCache.Set(key, rules)
			mu.Unlock()
		}(task.id, task.node, task.cacheKey)
	}

	wg.Wait()

	// Architectural rules (graph topology) — always run if not disabled
	if macroMode == "disabled" {
		return
	}

	// Rule 29: Dead Code / Unreachable Component
	orphans := graph.GetOrphanNodes()
	for _, id := range orphans {
		if node, ok := graph.Nodes.Get(id); ok {
			if node.Kind != "MODULE" && node.Kind != "FILE" && node.Kind != "STRUCT" && node.Kind != "CLASS" && node.Kind != "FUNCTION" {
				continue
			}
			rule := fmt.Sprintf("Component %s is unreachable/dead code (0 Inbound Edges) [%s]", node.Name, RuleTierArchitectural)
			existing, _ := graph.MacroRules.Get(id)
			graph.MacroRules = graph.MacroRules.Set(id, append(existing, rule))
			if node.Properties == nil {
				node.Properties = make(map[string]string)
			}
			updated, _ := graph.MacroRules.Get(id)
			node.Properties["macro_rules"] = strings.Join(updated, " | ")
		}
	}

	// Rule 30: Circular Dependency Detected (Tarjan's SCC Algorithm)
	cycles := graph.DetectCycles()
	for _, cycle := range cycles {
		for _, id := range cycle {
			if node, ok := graph.Nodes.Get(id); ok {
				if node.Kind == "MODULE" || node.Kind == "FILE" || node.Kind == "STRUCT" || node.Kind == "CLASS" || node.Kind == "PACKAGE" {
					rule := fmt.Sprintf("Architectural Flaw: Component %s is part of a Circular Dependency (Cycle Size: %d) [%s]", node.Name, len(cycle), RuleTierArchitectural)
					existing, _ := graph.MacroRules.Get(id)
					graph.MacroRules = graph.MacroRules.Set(id, append(existing, rule))
					if node.Properties == nil {
						node.Properties = make(map[string]string)
					}
					updated, _ := graph.MacroRules.Get(id)
					node.Properties["macro_rules"] = strings.Join(updated, " | ")
				}
			}
		}
	}

	// Rule 31: Single Point of Failure (Articulation Point Detection)
	aps := graph.FindArticulationPoints()
	for _, id := range aps {
		if node, ok := graph.Nodes.Get(id); ok {
			if node.Kind == "MODULE" || node.Kind == "FILE" || node.Kind == "STRUCT" || node.Kind == "CLASS" || node.Kind == "PACKAGE" {
				rule := fmt.Sprintf("Architectural Bottleneck: Component %s is a Single Point of Failure (Cut Vertex) [%s]", node.Name, RuleTierArchitectural)
				existing, _ := graph.MacroRules.Get(id)
				graph.MacroRules = graph.MacroRules.Set(id, append(existing, rule))
				if node.Properties == nil {
					node.Properties = make(map[string]string)
				}
				updated, _ := graph.MacroRules.Get(id)
				node.Properties["macro_rules"] = strings.Join(updated, " | ")
			}
		}
	}

	// Tag Blast Radius, Instability, PageRank, and Betweenness Centrality globally
	pageRanks := graph.CalculatePageRank(20, 0.85)
	betweenness := graph.CalculateBetweennessCentrality(graph.Nodes.Len() < 100000)

	graph.Nodes.Iterate(func(id string, node *link.ResolvedNode) {
		if node.Kind == "MODULE" || node.Kind == "PACKAGE" {
			cohesion := graph.CalculatePackageCohesion(id)
			if node.Properties == nil {
				node.Properties = make(map[string]string)
			}
			node.Properties["cohesion"] = fmt.Sprintf("%.2f", cohesion)

			// Rule 37: Low Relational Cohesion (Dump Package)
			if cohesion < 1.0 && cohesion > 0 {
				rule := fmt.Sprintf("Low Cohesion Package: %s has weak internal relationships (Cohesion: %.2f) and may be a Dump Package. [%s]", node.Name, cohesion, RuleTierArchitectural)
				existing, _ := graph.MacroRules.Get(id)
				graph.MacroRules = graph.MacroRules.Set(id, append(existing, rule))
			}
		}

		if node.Kind == "MODULE" || node.Kind == "FILE" || node.Kind == "STRUCT" || node.Kind == "CLASS" || node.Kind == "PACKAGE" {
			radius := graph.CalculateImpactRadius(id)
			instability := graph.CalculateInstability(id)
			pr := pageRanks[id]
			bc := betweenness[id]

			if node.Properties == nil {
				node.Properties = make(map[string]string)
			}
			node.Properties["instability"] = fmt.Sprintf("%.2f", instability)
			node.Properties["blast_radius"] = fmt.Sprintf("%d", radius)
			node.Properties["pagerank"] = fmt.Sprintf("%.6f", pr)
			node.Properties["betweenness_centrality"] = fmt.Sprintf("%.2f", bc)

			// Rule 32: High Blast Radius Warning
			if radius > 50 {
				rule := fmt.Sprintf("High Blast Radius: Modifying %s will impact %d other components downstream. [%s]", node.Name, radius, RuleTierArchitectural)
				existing, _ := graph.MacroRules.Get(id)
				graph.MacroRules = graph.MacroRules.Set(id, append(existing, rule))
			}

			// Rule 33: Core Foundational Component (High PageRank)
			if pr > (5.0 / float64(graph.Nodes.Len())) {
				rule := fmt.Sprintf("Core Component: %s is highly authoritative (PageRank: %.6f). Modifications require extreme care. [%s]", node.Name, pr, RuleTierArchitectural)
				existing, _ := graph.MacroRules.Get(id)
				graph.MacroRules = graph.MacroRules.Set(id, append(existing, rule))
			}

			// Rule 36: Communication Hub (High Betweenness Centrality)
			if bc > 100.0 {
				rule := fmt.Sprintf("Communication Hub: %s is an architectural bridge (Betweenness: %.2f). Decoupling is recommended. [%s]", node.Name, bc, RuleTierArchitectural)
				existing, _ := graph.MacroRules.Get(id)
				graph.MacroRules = graph.MacroRules.Set(id, append(existing, rule))
			}

			existingAll, _ := graph.MacroRules.Get(id)
			if len(existingAll) > 0 {
				node.Properties["macro_rules"] = strings.Join(existingAll, " | ")
			}
		}
	})

	// Rule 34: Isolated Architecture Island (Dead Sub-system)
	islands := graph.FindIsolatedIslands()
	for _, island := range islands {
		for _, id := range island {
			if node, ok := graph.Nodes.Get(id); ok {
				rule := fmt.Sprintf("Dead Sub-system: Component %s is part of an isolated architectural island (Size: %d) with no entrypoints. [%s]", node.Name, len(island), RuleTierArchitectural)
				existing, _ := graph.MacroRules.Get(id)
				graph.MacroRules = graph.MacroRules.Set(id, append(existing, rule))
				if node.Properties == nil {
					node.Properties = make(map[string]string)
				}
				updated, _ := graph.MacroRules.Get(id)
				node.Properties["macro_rules"] = strings.Join(updated, " | ")
			}
		}
	}

	// Rule 35: God Object Anti-Pattern (Statistical Outlier)
	godObjects := graph.DetectGodObjects()
	for _, id := range godObjects {
		if node, ok := graph.Nodes.Get(id); ok {
			rule := fmt.Sprintf("God Object Anti-Pattern: Component %s has assumed too much responsibility (>2 StdDev Fan-in/Fan-out). [%s]", node.Name, RuleTierArchitectural)
			existing, _ := graph.MacroRules.Get(id)
			graph.MacroRules = graph.MacroRules.Set(id, append(existing, rule))
			if node.Properties == nil {
				node.Properties = make(map[string]string)
			}
			updated, _ := graph.MacroRules.Get(id)
			node.Properties["macro_rules"] = strings.Join(updated, " | ")
		}
	}

	buildArchitecturalSummary(graph)
}

func buildArchitecturalSummary(graph *CodePropertyGraph) {
	if graph == nil {
		return
	}

	summary := &ArchitecturalSummary{
		PrimaryPatterns:   []string{},
		LayerDistribution: make(map[string]int),
		HotspotNodes:      []string{},
		EntryPoints:       []string{},
		ExternalDeps:      []string{},
		GeneratedAt:       time.Now().UTC(),
	}

	patternCounts := make(map[string]int)
	inboundCounts := make(map[string]int)
	outboundCounts := make(map[string]int)

	graph.MacroRules.Iterate(func(_ string, rules []string) {
		for _, r := range rules {
			patternCounts[r]++
		}
	})

	for p := range patternCounts {
		summary.PrimaryPatterns = append(summary.PrimaryPatterns, p)
	}

	graph.OutboundEdges.Iterate(func(id string, edges []link.ResolvedEdge) {
		outboundCounts[id] += len(edges)
	})
	graph.InboundEdges.Iterate(func(id string, edges []link.ResolvedEdge) {
		inboundCounts[id] += len(edges)
	})

	graph.Nodes.Iterate(func(id string, node *link.ResolvedNode) {
		summary.LayerDistribution[node.Kind]++

		if node.Primitive == "EXTERNAL" || node.Primitive == "NETWORK_IO" {
			summary.ExternalDeps = append(summary.ExternalDeps, node.Name)
		}

		if inboundCounts[id] == 0 && outboundCounts[id] > 0 {
			if len(summary.EntryPoints) < 50 {
				summary.EntryPoints = append(summary.EntryPoints, node.Name)
			}
		}
	})

	type NodeRank struct {
		ID    string
		Score int
	}
	var rankings []NodeRank
	graph.Nodes.Iterate(func(id string, _ *link.ResolvedNode) {
		score := inboundCounts[id] + outboundCounts[id]
		if score > 0 {
			rankings = append(rankings, NodeRank{ID: id, Score: score})
		}
	})
	sort.Slice(rankings, func(i, j int) bool {
		return rankings[i].Score > rankings[j].Score
	})

	for i := 0; i < len(rankings) && i < 10; i++ {
		if node, ok := graph.Nodes.Get(rankings[i].ID); ok {
			summary.HotspotNodes = append(summary.HotspotNodes, fmt.Sprintf("%s (Degree: %d)", node.Name, rankings[i].Score))
		}
	}

	graph.Summary = summary
}

// shouldApplyRule returns true if a rule with the given tier should fire in the given macro mode.
func shouldApplyRule(tier, macroMode string) bool {
	switch macroMode {
	case "disabled":
		return false
	case "structural":
		return tier == RuleTierStructural || tier == RuleTierArchitectural
	default: // "all"
		return true
	}
}

func inferMacroRulesForNode(nodeID string, node *link.ResolvedNode, graph *CodePropertyGraph, mu *sync.Mutex, macroMode string, disabledRules map[string]bool) {
	visited := make(map[string]bool)
	primitivesFound := make(map[string]bool)
	var flags ruleFlags

	dfsWalkPrimitives(nodeID, graph, visited, primitivesFound, &flags.hasSecurityGate, &flags.hasAsyncProcessing, &flags.hasContextPass, &flags.hasEventPubSub, &flags.hasDependencyInjection, &flags.hasHeapEscape, &flags.hasFFI, &flags.hasConstraint, 0, 5)

	var inferredRules []string

	for _, rule := range RuleRegistry {
		if !shouldApplyRule(rule.Tier, macroMode) {
			continue
		}
		if rule.Enabled(node, graph, disabledRules, primitivesFound, flags) {
			result := rule.Apply(node, graph, primitivesFound, flags)
			if result != "" {
				inferredRules = append(inferredRules, result)
			}
		}
	}

	if len(inferredRules) > 0 {
		mu.Lock()
		graph.MacroRules = graph.MacroRules.Set(nodeID, inferredRules)
		if node.Properties == nil {
			node.Properties = make(map[string]string)
		}
		node.Properties["macro_rules"] = strings.Join(inferredRules, " | ")
		mu.Unlock()
	}
}

func dfsWalkPrimitives(currentID string, graph *CodePropertyGraph, visited map[string]bool, primitivesFound map[string]bool, hasSecurity *bool, hasAsync *bool, hasContext *bool, hasPubSub *bool, hasDI *bool, hasHeapEscape *bool, hasFFI *bool, hasConstraint *bool, depth, maxDepth int) {
	if depth > maxDepth || visited[currentID] {
		return
	}
	visited[currentID] = true

	if currNode, ok := graph.Nodes.Get(currentID); ok && currNode != nil {
		if currNode.Primitive != "" {
			for _, prim := range strings.Split(currNode.Primitive, ",") {
				primitivesFound[strings.TrimSpace(prim)] = true
			}
		}

		lowerName := strings.ToLower(currNode.Name)
		if strings.Contains(lowerName, "auth") || strings.Contains(lowerName, "jwt") || strings.Contains(lowerName, "security") || strings.Contains(lowerName, "validate") {
			*hasSecurity = true
		}
	}

	edges, _ := graph.OutboundEdges.Get(currentID)
	for _, edge := range edges {
		if edge.Type == link.EdgeSpawnsConcurrent || edge.Type == link.EdgeDispatchesEvent {
			*hasAsync = true
		}
		if edge.Type == link.EdgePublishes || edge.Type == link.EdgeSubscribes || edge.Type == link.EdgeDispatchesEvent {
			*hasPubSub = true
		}
		if edge.Type == link.EdgeInjects {
			*hasDI = true
		}
		if edge.Type == link.EdgeContextCall {
			*hasContext = true
		}
		if edge.Type == link.EdgeEscapesToHeap || edge.Type == link.EdgeHeapAlias {
			*hasHeapEscape = true
		}
		if edge.Type == link.EdgeFFICall {
			*hasFFI = true
		}
		if edge.Type == link.EdgeConstraint {
			*hasConstraint = true
		}

		if edge.Type == link.EdgeCalls || edge.Type == link.EdgeSpawnsConcurrent || edge.Type == link.EdgeControlFlow || edge.Type == link.EdgeContextCall {
			dfsWalkPrimitives(edge.TargetID, graph, visited, primitivesFound, hasSecurity, hasAsync, hasContext, hasPubSub, hasDI, hasHeapEscape, hasFFI, hasConstraint, depth+1, maxDepth)
		}
	}
}

// affectedReasonerDepth bounds the reverse reachability walk that expands the
// changed-file seed set to its transitive inbound dependents. A caller one or
// two hops up may change its inferred rules when a callee it reaches changes
// primitives; beyond that the risk of a stale inference is outweighed by the
// cost of walking the whole graph.
const affectedReasonerDepth = 2

// RunIncrementalMacroInference re-infers macro rules for ONLY the changed
// subgraph: nodes whose files are in modifiedFiles, plus their bounded inbound
// dependents (callers/composers that reach a changed node). Unchanged nodes
// keep their previously inferred rules, so an incremental delta transaction no
// longer re-walks the entire graph (AUDIT Issue 1 Phase 1C-9 companion to the
// delta linker). The macro cache is honored: nodes whose content hash is
// unchanged fall through to the cached result instead of a full rule pass.
//
// Architectural topology rules (dead code, cycles, articulation points,
// centrality) are graph-global and are NOT re-derived here; callers that need
// them (e.g. `gmb analyze --full`) use RunTopologicalMacroInference. The
// summary is refreshed from the merged graph so status/queries stay current.
func RunIncrementalMacroInference(graph *CodePropertyGraph, modifiedFiles []string, changedNodeIDs []string, config ...link.LinkerConfig) {
	if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
		return
	}

	macroMode := "all"
	disabledRules := make(map[string]bool)
	if len(config) > 0 {
		if config[0].MacroInference != "" {
			macroMode = config[0].MacroInference
		}
		for _, ruleID := range config[0].DisabledRules {
			disabledRules[ruleID] = true
		}
	}
	if macroMode == "disabled" {
		return
	}

	// Detach node properties before writing so concurrent readers via
	// SafeQuery / the AI bridge never race the in-place mutation (same
	// isolation the full reasoner provides).
	graph.detachNodesForWrite()

	if graph.macroCache == nil {
		graph.macroCache = NewCowMap[string, []string]()
	}
	graph.macroCache = capMacroCache(graph.macroCache)

	// Seed the affected set from the delta payload node IDs (authoritative:
	// these are the nodes that actually changed, even if their file path is
	// unset) and from the modified files. FileNodeIndex maps normalized paths
	// to their node IDs (kept in sync by the delta graft).
	affected := make(map[string]bool)
	for _, id := range changedNodeIDs {
		affected[id] = true
	}
	for _, filePath := range modifiedFiles {
		normPath := normalizePath(filePath)
		if nodeSet, ok := graph.FileNodeIndex.Get(normPath); ok {
			for id := range nodeSet {
				affected[id] = true
			}
		}
	}
	// Fallback: if the file index was not populated (e.g. direct callers in
	// tests), match by scanning node file paths.
	if len(affected) == 0 {
		need := make(map[string]bool)
		for _, filePath := range modifiedFiles {
			need[normalizePath(filePath)] = true
		}
		graph.Nodes.Iterate(func(id string, node *link.ResolvedNode) {
			if node != nil && need[normalizePath(node.FileSpec.Path)] {
				affected[id] = true
			}
		})
	}

	// Expand to bounded inbound dependents: a node that reaches a changed node
	// may infer different rules (its primitivesFound/flags depend on the DFS
	// walk), so it must be re-inferred too.
	frontier := make(map[string]bool)
	for id := range affected {
		frontier[id] = true
	}
	for depth := 0; depth < affectedReasonerDepth && len(frontier) > 0; depth++ {
		next := make(map[string]bool)
		for id := range frontier {
			// InboundEdges is keyed by target, so the inbound dependents of
			// `id` are the SourceIDs of its OWN edge list — a direct Get. The
			// previous implementation re-iterated the ENTIRE InboundEdges
			// index for every frontier id (O(affected × E)); on a --full
			// commit every node is affected, making the expansion quadratic
			// in the graph size (minutes on a 17k-node/24k-edge graph).
			inbound, _ := graph.InboundEdges.Get(id)
			for _, e := range inbound {
				if !affected[e.SourceID] {
					if node, ok := graph.Nodes.Get(e.SourceID); ok && node != nil {
						if isReasonerRelevantKind(node.Kind) {
							affected[e.SourceID] = true
							next[e.SourceID] = true
						}
					}
				}
			}
		}
		frontier = next
	}

	if len(affected) == 0 {
		buildArchitecturalSummary(graph)
		return
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)

	// Collect tasks first so no goroutine mutates the macro cache while the
	// collection loop is still reading it (same pattern as the full reasoner).
	type task struct {
		id   string
		node *link.ResolvedNode
		key  string
	}
	var tasks []task
	for id := range affected {
		node, ok := graph.Nodes.Get(id)
		if !ok || node == nil {
			continue
		}
		key := nodeMacroKey(node, graph, macroMode, disabledRules)
		if cached, ok := graph.macroCache.Get(key); ok {
			graph.MacroRules = graph.MacroRules.Set(id, cached)
			if node.Properties == nil {
				node.Properties = make(map[string]string)
			}
			node.Properties["macro_rules"] = strings.Join(cached, " | ")
			continue
		}
		tasks = append(tasks, task{id: id, node: node, key: key})
	}

	for _, tk := range tasks {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string, n *link.ResolvedNode, key string) {
			defer wg.Done()
			defer func() { <-sem }()
			inferMacroRulesForNode(id, n, graph, &mu, macroMode, disabledRules)

			mu.Lock()
			graph.macroCache = capMacroCache(graph.macroCache)
			rules, _ := graph.MacroRules.Get(id)
			graph.macroCache = graph.macroCache.Set(key, rules)
			mu.Unlock()
		}(tk.id, tk.node, tk.key)
	}
	wg.Wait()

	buildArchitecturalSummary(graph)
}

// isReasonerRelevantKind reports whether a node kind participates in macro
// inference (mirrors the kind filter in RunTopologicalMacroInference).
func isReasonerRelevantKind(kind string) bool {
	switch kind {
	case "MODULE", "FILE", "STRUCT", "CLASS", "FUNCTION", "PACKAGE":
		return true
	}
	return false
}
