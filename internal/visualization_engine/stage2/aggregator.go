package stage2

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

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

	summary := metrics.Summary
	if summary == nil {
		summary = ComputeGraphSummary(sub)
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
				if strings.Contains(n.FileURI, opts.ScopePath) || strings.HasPrefix(id, opts.ScopePath) {
					filtered[id] = n
				}
			case types.ScopeFile:
				if strings.HasSuffix(n.FileURI, opts.ScopePath) || strings.HasPrefix(id, opts.ScopePath) {
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
		if referenced[id] || node.Kind == "gm:Namespace" || node.Kind == "gm:File" {
			filtered[id] = node
		}
	}
	return filtered
}

func getDirectoryPath(nodeID string, kind string, diagramType types.DiagramType, communities map[string]string) string {
	parts := strings.Split(nodeID, "::")
	filePath := parts[0]
	if strings.HasPrefix(filePath, "file:") {
		filePath = filePath[5:]
	} else if strings.HasPrefix(filePath, "module:") {
		filePath = filePath[7:]
	}

	var dir string
	if kind == "gm:Namespace" || kind == "gm:Module" {
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
		if kind == "gm:TypeDecl" || kind == "gm:Executable" {
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

func getOrCreateBoundary(dir string, treeMap map[string]*types.LayoutTree, root *types.LayoutTree) *types.LayoutTree {
	if tree, exists := treeMap[dir]; exists {
		return tree
	}

	parentDir := filepath.Dir(dir)
	if parentDir == "." || parentDir == "/" || parentDir == "\\" {
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
