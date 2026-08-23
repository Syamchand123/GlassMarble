package extract

import (
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/product/ids"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// ApplySemanticZoom applies Level-of-Detail (LoD) aggregation and complexity
// governance based on diagram type and query scope.
// - ScopeGlobal: Aggregates low-level implementation details into Subsystem / Package blocks.
// - ScopeFolder: Extracts module & container-level components with external dependency stubs.
// - ScopeFile: Retains fine-grained AST member & call-level resolution.
func ApplySemanticZoom(graph *types.NativeGraph, diagType types.DiagramType, opts types.QueryOptions) *types.NativeGraph {
	if graph == nil || len(graph.Nodes) == 0 {
		return graph
	}

	switch opts.Scope {
	case types.ScopeGlobal:
		return applyGlobalSemanticZoom(graph, diagType, opts)
	case types.ScopeFolder:
		return applyFolderSemanticZoom(graph, diagType, opts)
	case types.ScopeFile:
		return graph // Full detail within file
	default:
		return graph
	}
}

// applyGlobalSemanticZoom manages global-level complexity by collapsing leaf symbols
// into parent containers and applying intelligent centrality-based node budgets.
func applyGlobalSemanticZoom(graph *types.NativeGraph, diagType types.DiagramType, opts types.QueryOptions) *types.NativeGraph {
	// For high-level architectural diagrams (C4, Landscape, Package, Mindmap, Layered, Infra, Dependency),
	// aggregate fine-grained methods/functions into package/container nodes.
	if shouldAggregateToContainers(diagType) {
		return aggregateToPackageLevel(graph)
	}

	// For detailed entity diagrams (Class, ER, Object) at global scope,
	// enforce the visual complexity budget if node count is excessive.
	maxBudget := opts.MaxNodes
	if maxBudget <= 0 {
		maxBudget = 60 // Default safe visual budget for global entity diagrams
	}

	if len(graph.Nodes) > maxBudget {
		return governGlobalEntityBudget(graph, maxBudget)
	}

	return graph
}

// shouldAggregateToContainers reports whether the diagram type represents high-level architecture.
func shouldAggregateToContainers(diagType types.DiagramType) bool {
	switch diagType {
	case types.C4Context, types.C4Landscape, types.C4Container,
		types.UMLPackage, types.Mindmap, types.LayeredArchitecture,
		types.Infrastructure, types.DependencyGraph:
		return true
	default:
		return false
	}
}

// aggregateToPackageLevel rolls up fine-grained member nodes into package/module boundaries.
func aggregateToPackageLevel(graph *types.NativeGraph) *types.NativeGraph {
	res := &types.NativeGraph{
		Nodes: make(map[string]*types.NativeNode),
		Edges: nil,
	}

	nodeToPkg := make(map[string]string)

	// Deterministic: sorted node IDs so PrimitiveType first-wins is stable (C3-3).
	sortedIDs := make([]string, 0, len(graph.Nodes))
	for id := range graph.Nodes {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Strings(sortedIDs)
	for _, id := range sortedIDs {
		node := graph.Nodes[id]
		pkgID := getPackageIDForNode(id, node)
		nodeToPkg[id] = pkgID

		if pkgID == "" {
			continue
		}
		if _, exists := res.Nodes[pkgID]; !exists {
			pkgName := getPackageNameFromID(pkgID)
			res.Nodes[pkgID] = &types.NativeNode{
				ID:            pkgID,
				Name:          pkgName,
				Kind:          ont.PredPackage,
				PrimitiveType: node.PrimitiveType,
				Properties:    make(map[string]string),
			}
		}
	}

	edgeMap := make(map[string]*types.NativeEdge)
	for _, edge := range graph.Edges {
		srcPkg, srcOk := nodeToPkg[edge.SourceID]
		tgtPkg, tgtOk := nodeToPkg[edge.TargetID]

		if !srcOk || !tgtOk || srcPkg == "" || tgtPkg == "" || srcPkg == tgtPkg {
			continue
		}

		edgeKey := srcPkg + "->" + tgtPkg + ":" + edge.Predicate
		if _, exists := edgeMap[edgeKey]; !exists {
			newEdge := edge
			newEdge.SourceID = srcPkg
			newEdge.TargetID = tgtPkg
			edgeMap[edgeKey] = &newEdge
		}
	}

	// Deterministic: sorted edge keys (C3-3).
	edgeKeys := make([]string, 0, len(edgeMap))
	for k := range edgeMap {
		edgeKeys = append(edgeKeys, k)
	}
	sort.Strings(edgeKeys)
	for _, k := range edgeKeys {
		edge := edgeMap[k]
		res.Edges = append(res.Edges, *edge)
	}

	return res
}

func getPackageIDForNode(id string, node *types.NativeNode) string {
	// Code-snippet node IDs (multiline variable initializers, error strings,
	// URL-encoded fragments) never denote a package path; reject them before
	// any grammar parsing (GAP-W-02).
	if strings.ContainsAny(id, " \t\r\n{}=%()<>[]&|!\"'`") {
		return ""
	}

	norm := ids.NormalizeLegacyID(id)
	if c, err := ids.ParseCanonicalID(norm); err == nil && c.Path != "" {
		dir := c.Path
		if idx := strings.LastIndex(dir, "/"); idx != -1 {
			dir = dir[:idx]
		}
		if dir == "" || dir == "." {
			return "root"
		}
		if !isValidPackageDir(dir) {
			return ""
		}
		return "pkg:" + dir
	}

	clean := strings.TrimPrefix(id, "file:")
	clean = strings.TrimPrefix(clean, "module:")
	if idx := strings.Index(clean, "::"); idx != -1 {
		clean = clean[:idx]
	}
	if idx := strings.LastIndex(clean, "/"); idx != -1 {
		clean = clean[:idx]
	}
	if clean == "" || clean == "." {
		return "root"
	}
	if !isValidPackageDir(clean) {
		return ""
	}
	return "pkg:" + clean
}

// isValidPackageDir reports whether dir is a plausible package path. AKG node
// IDs occasionally carry code-snippet names (multiline variable initializers,
// error strings, URL-encoded fragments); those must never become package
// boundaries (GAP-W-02). A valid package dir either contains a slash (nested
// package) or is a known root-level package name / Go file.
func isValidPackageDir(dir string) bool {
	if dir == "" {
		return false
	}
	if strings.ContainsAny(dir, " \t\r\n{}=%()<>[]&|!\"'`") {
		return false
	}
	if strings.Contains(dir, "/") {
		return true
	}
	switch dir {
	case "cmd", "internal", "tests", "pkg", "main", "main.go",
		"tools", "scripts", "vendor", "external", "build", "testdata",
		"docs", "assets", "config", "deploy", "migrations", "examples", "hack",
		"src", "lib", "app", "api", "web", "server", "client", "core", "bin", "data":
		return true
	}
	return strings.HasSuffix(dir, ".go")
}

func getPackageNameFromID(pkgID string) string {
	name := strings.TrimPrefix(pkgID, "pkg:")
	if name == "root" {
		return "Root Package"
	}
	return name
}

// governGlobalEntityBudget retains the highest-impact nodes according to degree centrality
// and structural importance (classes, interfaces, hotspots) to prevent diagram overflow.
func governGlobalEntityBudget(graph *types.NativeGraph, budget int) *types.NativeGraph {
	if len(graph.Nodes) <= budget {
		return graph
	}

	degrees := make(map[string]int)
	for _, edge := range graph.Edges {
		degrees[edge.SourceID]++
		degrees[edge.TargetID]++
	}

	type scoredNode struct {
		node  *types.NativeNode
		score int
	}

	var scored []scoredNode
	for id, node := range graph.Nodes {
		score := degrees[id]
		if node.Kind == ont.PredStruct || node.Kind == ont.PredClass || node.Kind == ont.PredInterface {
			score += 10
		}
		if node.Properties != nil && node.Properties["is_hotspot"] == "true" {
			score += 20
		}
		scored = append(scored, scoredNode{node: node, score: score})
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].node.ID < scored[j].node.ID
	})

	selectedIDs := make(map[string]bool)
	res := &types.NativeGraph{
		Nodes: make(map[string]*types.NativeNode),
		Edges: nil,
	}

	limit := budget
	if limit > len(scored) {
		limit = len(scored)
	}

	for i := 0; i < limit; i++ {
		n := scored[i].node
		selectedIDs[n.ID] = true
		res.Nodes[n.ID] = n
	}

	for _, edge := range graph.Edges {
		if selectedIDs[edge.SourceID] && selectedIDs[edge.TargetID] {
			res.Edges = append(res.Edges, edge)
		}
	}

	return res
}

// isTestNode reports whether a node belongs to test scaffolding (*_test.go or Test* functions).
func isTestNode(id string, node *types.NativeNode) bool {
	norm := ids.NormalizeLegacyID(id)
	if strings.Contains(norm, "_test.go") || strings.Contains(id, "_test.go") {
		return true
	}
	if node != nil {
		if strings.HasPrefix(node.Name, "Test") || strings.HasPrefix(node.Name, "Benchmark") || strings.HasPrefix(node.Name, "Example") {
			return true
		}
	}
	return false
}

// applyFolderSemanticZoom extracts all nodes inside the target folder, filters out
// test scaffolding for architectural diagrams (unless explicitly requested),
// and governs node complexity to ensure responsive diagram rendering.
func applyFolderSemanticZoom(graph *types.NativeGraph, diagType types.DiagramType, opts types.QueryOptions) *types.NativeGraph {
	if graph == nil || len(graph.Nodes) == 0 {
		return graph
	}

	filtered := graph
	if !opts.IncludeTests && opts.Scope != types.ScopeFile {
		hasNonTest := false
		for id, node := range graph.Nodes {
			if !isTestNode(id, node) {
				hasNonTest = true
				break
			}
		}

		if hasNonTest {
			filtered = &types.NativeGraph{
				Nodes: make(map[string]*types.NativeNode),
				Edges: nil,
			}
			for id, node := range graph.Nodes {
				if !isTestNode(id, node) {
					filtered.Nodes[id] = node
				}
			}
			for _, edge := range graph.Edges {
				if _, srcOk := filtered.Nodes[edge.SourceID]; srcOk {
					if _, tgtOk := filtered.Nodes[edge.TargetID]; tgtOk {
						filtered.Edges = append(filtered.Edges, edge)
					}
				}
			}
		}
	}

	maxBudget := opts.MaxNodes
	if maxBudget <= 0 {
		maxBudget = 80 // Safe visual budget for folder scope diagrams
	}

	if len(filtered.Nodes) > maxBudget {
		return governGlobalEntityBudget(filtered, maxBudget)
	}

	return filtered
}
