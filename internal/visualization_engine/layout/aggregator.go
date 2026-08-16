package normalize

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// BuildLayoutTree computes all metrics, detects communities, and builds a hierarchical LayoutTree from the subgraph.
func BuildLayoutTree(sub *types.VirtualSubgraph, opts types.QueryOptions) *types.LayoutTree {
	metrics := ComputeAllMetrics(sub)
	return BuildLayoutTreeEx(sub, metrics, metrics.Communities, opts, opts.DiagramType)
}

// BuildLayoutTreeEx builds a hierarchical LayoutTree with precomputed metrics, community assignments, scoping, and boundary grouping.
func BuildLayoutTreeEx(sub *types.VirtualSubgraph, metrics *DiagramMetrics, clustering map[string]string, opts types.QueryOptions, t types.DiagramType) *types.LayoutTree {
	if sub == nil {
		return &types.LayoutTree{BoundaryName: "Root", Summary: &types.GraphSummary{}}
	}

	if metrics == nil {
		metrics = &DiagramMetrics{}
	}

	pr := metrics.PageRank
	bc := metrics.Betweenness
	inDeg := metrics.DegreeIn
	outDeg := metrics.DegreeOut
	godObjects := metrics.GodObjects

	communities := clustering
	if communities == nil {
		communities = metrics.Communities
	}

	godSet := make(map[string]bool)
	for _, g := range godObjects {
		godSet[g] = true
	}

	filteredNodes := pruneDeadComponents(sub, opts)

	if opts.MaxDepth > 0 && opts.EntryPointID != "" {
		depthMap := make(map[string]int)
		queue := []string{opts.EntryPointID}
		depthMap[opts.EntryPointID] = 0

		adj := make(map[string][]string)
		for _, e := range sub.Edges {
			adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
		}

		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			currDepth := depthMap[curr]

			if currDepth < opts.MaxDepth {
				for _, next := range adj[curr] {
					if _, visited := depthMap[next]; !visited {
						depthMap[next] = currDepth + 1
						queue = append(queue, next)
					}
				}
			}
		}

		kept := make(map[string]*types.TTLNode)
		boundaryCount := 0
		for id, node := range filteredNodes {
			if d, ok := depthMap[id]; ok && d <= opts.MaxDepth {
				kept[id] = node
			} else {
				boundaryCount++
			}
		}

		if boundaryCount > 0 {
			boundaryID := "boundary_ports_depth"
			kept[boundaryID] = &types.TTLNode{
				ID:   boundaryID,
				Name: fmt.Sprintf("[+%d more callees]", boundaryCount),
				Kind: "External",
			}
		}
		filteredNodes = kept
	}

	summary := metrics.Summary
	if summary == nil {
		summary = ComputeGraphSummary(sub)
	}

	if opts.MaxNodes > 0 && len(filteredNodes) > opts.MaxNodes {
		type nodeDegree struct {
			id     string
			degree int
		}
		var list []nodeDegree
		for id := range filteredNodes {
			deg := inDeg[id] + outDeg[id]
			list = append(list, nodeDegree{id: id, degree: deg})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].degree != list[j].degree {
				return list[i].degree > list[j].degree
			}
			return list[i].id < list[j].id
		})

		kept := make(map[string]*types.TTLNode)
		for i := 0; i < opts.MaxNodes && i < len(list); i++ {
			id := list[i].id
			kept[id] = filteredNodes[id]
		}
		filteredNodes = kept
		summary.Truncated = true
	}

	root := &types.LayoutTree{BoundaryName: "Root", Summary: summary}
	treeMap := make(map[string]*types.LayoutTree)
	treeMap[""] = root

	for id, node := range filteredNodes {
		dir := getDirectoryPath(id, node.Kind, t, communities)
		nodeTree := getOrCreateBoundary(dir, treeMap, root)

		nodeName := node.Name
		if node.IsEntrypoint {
			nodeName += " [ENTRYPOINT]"
		}
		if node.PrimitiveZone != "" {
			nodeName += " [ZONE: " + node.PrimitiveZone + "]"
		}

		nodePR := pr[id]
		if prStr, ok := node.Properties["pagerank"]; ok {
			if parsedPR, err := strconv.ParseFloat(prStr, 64); err == nil {
				nodePR = parsedPR
			}
		}

		nodeBC := bc[id]
		if bcStr, ok := node.Properties["betweenness_centrality"]; ok {
			if parsedBC, err := strconv.ParseFloat(bcStr, 64); err == nil {
				nodeBC = parsedBC
			}
		}

		layoutNode := &types.LayoutNode{
			ID:            node.ID,
			Kind:          node.Kind,
			Name:          nodeName,
			PrimitiveType: node.PrimitiveType,
			LineStart:     node.LineStart,
			LineEnd:       node.LineEnd,
			Code:          node.Code,
			IsEntrypoint:  node.IsEntrypoint,
			PrimitiveZone: node.PrimitiveZone,
			PageRank:      nodePR,
			Betweenness:   nodeBC,
			Community:     communities[id],
			InDegree:      inDeg[id],
			OutDegree:     outDeg[id],
			IsGodObject:   godSet[id],
			Visibility:    node.Properties["visibility"],
			Properties:    node.Properties,
		}
		if inDeg[id] > 5 {
			layoutNode.IsHotspot = true
		}
		if nodeBC > 0.1 {
			layoutNode.IsBottleneck = true
		}
		nodeTree.Nodes = append(nodeTree.Nodes, layoutNode)
	}

	collapsedEdges := collapseEdges(sub.Edges)
	flaggedEdges := detectAndMarkCycles(sub.Nodes, collapsedEdges)
	root.Edges = flaggedEdges

	sortLayoutTree(root)

	return root
}

func pruneDeadComponents(sub *types.VirtualSubgraph, opts types.QueryOptions) map[string]*types.TTLNode {
	if opts.IncludeUnused {
		return sub.Nodes
	}

	if opts.Scope != types.ScopeGlobal && opts.ScopePath != "" {
		filtered := make(map[string]*types.TTLNode)
		for id, n := range sub.Nodes {
			switch opts.Scope {
			case types.ScopeFolder:
				if isWithinFolder(n.FileURI, opts.ScopePath) || isWithinFolder(id, opts.ScopePath) {
					filtered[id] = n
				}
			case types.ScopeFile:
				if isWithinFile(n.FileURI, opts.ScopePath) || isWithinFile(id, opts.ScopePath) {
					filtered[id] = n
				}
			default:
				filtered[id] = n
			}
		}
		return filtered
	}

	referenced := make(map[string]bool)
	for _, edge := range sub.Edges {
		referenced[edge.SourceID] = true
		referenced[edge.TargetID] = true
	}

	filtered := make(map[string]*types.TTLNode)
	for id, node := range sub.Nodes {
		k := strings.TrimPrefix(node.Kind, ont.PrefixGM)
		switch k {
		case "Struct", "Class", "Interface", "TypeDecl", "Module", "Namespace", "File",
			"STRUCT", "CLASS", "INTERFACE", "TYPE_DECL", "MODULE", "NAMESPACE", "FILE":
			filtered[id] = node
		default:
			if referenced[id] {
				filtered[id] = node
			}
		}
	}
	return filtered
}

// isWithinFolder reports whether candidate (a file path, FileURI or node ID)
// lives inside the folder scope. Matching is boundary-aware: the candidate
// must equal the scope path or start with scope + "/" — never a bare
// substring, so scope "api" cannot match "internal/api" or "api-utils"
// (GAP-H-01 / §8.1).
func isWithinFolder(candidate, scopePath string) bool {
	if candidate == "" || scopePath == "" {
		return false
	}
	c := normalizeScopePath(candidate)
	s := normalizeScopePath(scopePath)
	return c == s || strings.HasPrefix(c, s+"/")
}

// isWithinFile reports whether candidate names the scoped file (or a node
// inside it). Boundary-aware: the candidate must equal the scope path or end
// with "/" + scope path — never a bare substring (GAP-H-01).
func isWithinFile(candidate, scopePath string) bool {
	if candidate == "" || scopePath == "" {
		return false
	}
	c := normalizeScopePath(candidate)
	s := normalizeScopePath(scopePath)
	return c == s || strings.HasSuffix(c, "/"+s)
}

// normalizeScopePath converts a file path, FileURI or node ID into a
// slash-normalized relative path: grammar prefixes (file:/module:/virt:) and
// full glassmarble.org IRIs are stripped, node IDs are reduced to their path
// segment (both the legacy `path::Symbol` and canonical `type:path:Symbol`
// dialects via parseIDParts), backslashes become slashes, and leading
// "./" / trailing "/" are removed.
func normalizeScopePath(p string) string {
	res := p
	res = strings.TrimPrefix(res, "http://glassmarble.org/node/")
	res = strings.TrimPrefix(res, "http://glassmarble.org/file/")
	res = strings.TrimPrefix(res, "http://glassmarble.org/namespace/")
	res = strings.TrimPrefix(res, "file:")
	res = strings.TrimPrefix(res, "module:")
	res = strings.TrimPrefix(res, "virt:")
	res = strings.TrimPrefix(res, "./")
	if path, _, _ := parseIDParts(res); path != "" && path != res {
		res = path
	}
	res = strings.ReplaceAll(res, "\\", "/")
	return strings.Trim(res, "/")
}

func getDirectoryPath(nodeID string, kind string, diagramType types.DiagramType, communities map[string]string) string {
	// Path segment is extracted with the same dual-grammar parser as the
	// rest of the engine, so canonical IDs (type:path:owner:symbol, §4.1)
	// and legacy path::symbol IDs resolve identically (GAP-C-03).
	filePath, _, _ := parseIDParts(nodeID)

	var dir string
	if kind == ont.PredNamespace || kind == ont.PredModule {
		dir = filePath
	} else {
		dir = filepath.Dir(filePath)
	}

	if dir == "." || dir == "/" || dir == "\\" {
		return ""
	}

	slashPath := filepath.ToSlash(dir)

	switch diagramType {
	case types.UMLPackage, types.DependencyGraph, types.Mindmap:
		return slashPath
	case types.C4Context, types.C4Landscape:
		if comm, ok := communities[nodeID]; ok && comm != nodeID {
			return comm + " Community"
		}
		return slashPath
	case types.LayeredArchitecture:
		// §8.1 grouping uses the schema-v3-migrated kind set (STRUCT /
		// FUNCTION / CLASS / INTERFACE); the legacy TypeDecl/Executable
		// values are retained for stale TTL files (GAP-H-02).
		if isLayeredKind(kind) {
			if strings.HasPrefix(slashPath, "internal/") {
				subParts := strings.Split(strings.TrimPrefix(slashPath, "internal/"), "/")
				if len(subParts) > 0 && subParts[0] != "" {
					return subParts[0] + " Layer"
				}
			} else if strings.HasPrefix(slashPath, "cmd") {
				return "CLI Layer"
			}
		}
		return "Core Layer"
	case types.UMLComponent, types.C4Container:
		if comm, ok := communities[nodeID]; ok && comm != nodeID {
			return comm + " Component"
		}
		if strings.HasPrefix(slashPath, "internal/") {
			subParts := strings.Split(strings.TrimPrefix(slashPath, "internal/"), "/")
			if len(subParts) > 0 && subParts[0] != "" {
				return subParts[0] + " Subsystem"
			}
		} else if strings.HasPrefix(slashPath, "cmd") {
			return "CLI Subsystem"
		}
		return slashPath
	default:
		return slashPath
	}
}

// isLayeredKind reports whether a node kind participates in
// LayeredArchitecture boundary grouping. The schema-v3 migration reclassified
// TYPE_DECL → STRUCT and EXECUTABLE → FUNCTION, so both the migrated
// constants and the stale legacy values are accepted (GAP-H-02).
func isLayeredKind(kind string) bool {
	switch kind {
	case ont.PredStruct, ont.PredClass, ont.PredInterface, ont.PredTypeDecl,
		ont.PredFunction, ont.PredMethod, ont.PredExecutable:
		return true
	}
	return false
}

func getOrCreateBoundary(dir string, treeMap map[string]*types.LayoutTree, root *types.LayoutTree) *types.LayoutTree {
	if tree, exists := treeMap[dir]; exists {
		return tree
	}

	parentDir := filepath.Dir(dir)
	if parentDir == "." || parentDir == "/" || parentDir == "\\" || parentDir == dir {
		parentDir = ""
	}

	parentTree := getOrCreateBoundary(parentDir, treeMap, root)
	newTree := &types.LayoutTree{
		BoundaryName: filepath.Base(dir),
	}

	parentTree.Children = append(parentTree.Children, newTree)
	treeMap[dir] = newTree
	return newTree
}

func collapseEdges(edges []types.TTLEdge) []types.LayoutEdge {
	type edgeKey struct {
		src  string
		pred string
		tgt  string
	}

	counts := make(map[edgeKey]*types.LayoutEdge)

	for _, edge := range edges {
		key := edgeKey{src: edge.SourceID, pred: edge.Predicate, tgt: edge.TargetID}
		if layoutEdge, exists := counts[key]; exists {
			layoutEdge.Weight++
		} else {
			counts[key] = &types.LayoutEdge{
				SourceID:   edge.SourceID,
				Predicate:  edge.Predicate,
				TargetID:   edge.TargetID,
				LineNumber: edge.LineNumber,
				Weight:     1,
			}
		}
	}

	var result []types.LayoutEdge
	for _, edge := range counts {
		result = append(result, *edge)
	}
	return result
}

// sortLayoutTree sorts nodes and children topologically using strongly connected component hints to avoid overlapping visual layout lines.
func sortLayoutTree(tree *types.LayoutTree) {
	if tree == nil {
		return
	}

	// Sort layout nodes based on names and lines
	sortNodes(tree.Nodes)

	// Sort child boundaries recursively
	for _, child := range tree.Children {
		sortLayoutTree(child)
	}
}

func sortNodes(nodes []*types.LayoutNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		aIsSink := a.PrimitiveType == "DATABASE" || a.PrimitiveType == "DISK_IO" || a.PrimitiveType == "NETWORK_IO"
		bIsSink := b.PrimitiveType == "DATABASE" || b.PrimitiveType == "DISK_IO" || b.PrimitiveType == "NETWORK_IO"
		if aIsSink != bIsSink {
			return bIsSink
		}
		if a.LineStart != b.LineStart {
			return a.LineStart < b.LineStart
		}
		return a.Name < b.Name
	})
}

func detectAndMarkCycles(nodes map[string]*types.TTLNode, edges []types.LayoutEdge) []types.LayoutEdge {
	// Build graph representation: Node ID -> List of Target Node IDs
	adj := make(map[string][]string)
	edgeMap := make(map[string]*types.LayoutEdge)

	for i := range edges {
		e := &edges[i]
		key := e.SourceID + "->" + e.TargetID
		edgeMap[key] = e
		adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
	}

	// Tarjan's SCC state variables
	index := 0
	indices := make(map[string]int)
	lowlink := make(map[string]int)
	onStack := make(map[string]bool)
	var stack []string

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

		// If v is a root node, pop the stack and generate an SCC
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

			// If SCC contains multiple nodes, or a single node with a self-loop, it is a cycle!
			isCycle := len(scc) > 1
			if len(scc) == 1 {
				// Check for self-loop
				for _, w := range adj[v] {
					if w == v {
						isCycle = true
						break
					}
				}
			}

			if isCycle {
				sccSet := make(map[string]bool)
				for _, node := range scc {
					sccSet[node] = true
				}

				// Mark all edges connecting nodes inside this cycle component
				for _, src := range scc {
					for _, tgt := range adj[src] {
						if sccSet[tgt] {
							key := src + "->" + tgt
							if e, ok := edgeMap[key]; ok {
								e.IsCycle = true
							}
						}
					}
				}
			}
		}
	}

	for id := range nodes {
		if _, exists := indices[id]; !exists {
			strongconnect(id)
		}
	}

	return edges
}
