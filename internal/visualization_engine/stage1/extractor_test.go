package stage1

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func TestExtractSubgraphClass(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "testdata", "minimal.ttl")
	sub, err := ExtractSubgraph(path, types.UMLClass, types.QueryOptions{
		EntryPointID: "main.go::Main",
	})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
	if len(sub.Nodes) == 0 {
		t.Error("expected at least one extracted node in UMLClass diagram")
	}
}

func TestExtractSubgraphCallGraph(t *testing.T) {
	path := filepath.Join("..", "testdata", "minimal.ttl")
	sub, err := ExtractSubgraph(path, types.CallGraph, types.QueryOptions{EntryPointID: "main.go::Main"})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphPackage(t *testing.T) {
	path := filepath.Join("..", "testdata", "all_kinds.ttl")
	sub, err := ExtractSubgraph(path, types.UMLPackage, types.QueryOptions{})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestApplyScopeGlobal(t *testing.T) {
	path := filepath.Join("..", "testdata", "scope_internal.ttl")
	sub, err := ExtractSubgraph(path, types.CallGraph, types.QueryOptions{EntryPointID: "internal/api/handler.go::HandleRequest"})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	before := len(sub.Nodes)
	ApplyScope(sub, types.QueryOptions{Scope: types.ScopeGlobal})
	if len(sub.Nodes) != before {
		t.Errorf("expected no filtering with ScopeGlobal, before=%d after=%d", before, len(sub.Nodes))
	}
}

func TestApplyScopeFolder(t *testing.T) {
	path := filepath.Join("..", "testdata", "scope_internal.ttl")
	sub, err := ExtractSubgraph(path, types.CallGraph, types.QueryOptions{EntryPointID: "internal/api/handler.go::HandleRequest"})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	ApplyScope(sub, types.QueryOptions{Scope: types.ScopeFolder, ScopePath: "internal"})
	for id, n := range sub.Nodes {
		if !strings.Contains(n.FileURI, "internal") && !strings.HasPrefix(id, "internal") {
			t.Errorf("node %q should be filtered out with ScopeFolder internal, FileURI=%q", id, n.FileURI)
		}
	}
}

func TestExtractSubgraphSequence(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.UMLSequence, types.QueryOptions{
		EntryPointID: "cmd/app/main.go::Main",
		MaxDepth:     5,
	})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphC4Context(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.C4Context, types.QueryOptions{})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphDataFlow(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.DataFlow, types.QueryOptions{})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphERDiagram(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.ERDiagram, types.QueryOptions{})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphC4Container(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.C4Container, types.QueryOptions{})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphDependency(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.DependencyGraph, types.QueryOptions{})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphChangeImpact(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.ChangeImpact, types.QueryOptions{
		ChangedFiles: []string{"internal/api/handler.go"},
	})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphLayered(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.LayeredArchitecture, types.QueryOptions{})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestApplyScopeFile(t *testing.T) {
	path := filepath.Join("..", "testdata", "scope_internal.ttl")
	sub, err := ExtractSubgraph(path, types.CallGraph, types.QueryOptions{EntryPointID: "internal/api/handler.go::HandleRequest"})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	ApplyScope(sub, types.QueryOptions{Scope: types.ScopeFile, ScopePath: "handler.go"})
	for id, n := range sub.Nodes {
		if !strings.HasSuffix(n.FileURI, "handler.go") && !strings.HasPrefix(id, "handler.go") {
			t.Errorf("node %q should be filtered out with ScopeFile handler.go, FileURI=%q", id, n.FileURI)
		}
	}
}

func TestApplyScopeFolderEmptyPath(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{"a": {ID: "a"}},
		Edges: nil,
	}
	ApplyScope(sub, types.QueryOptions{Scope: types.ScopeFolder, ScopePath: ""})
	if len(sub.Nodes) != 1 {
		t.Error("expected no filtering with empty ScopePath")
	}
}

func TestExtractSubgraphMindmap(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.Mindmap, types.QueryOptions{})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphInfrastructure(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.Infrastructure, types.QueryOptions{})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphUMLState(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.UMLState, types.QueryOptions{})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestGetExtractionConfig(t *testing.T) {
	cfg := GetExtractionConfig(types.UMLClass, types.QueryOptions{})
	if cfg.Name != "UMLClass" {
		t.Errorf("expected name UMLClass, got %s", cfg.Name)
	}
	if len(cfg.NodeKindFilter) == 0 {
		t.Error("expected non-empty NodeKindFilter")
	}
}

func TestGetExtractionConfigDefault(t *testing.T) {
	cfg := GetExtractionConfig(types.DiagramType("UNKNOWN"), types.QueryOptions{})
	if cfg.Name != "Default" {
		t.Errorf("expected name Default, got %s", cfg.Name)
	}
}

func TestMatchesKind(t *testing.T) {
	if !matchesKind("gm:TypeDecl", []string{"gm:TypeDecl"}) {
		t.Error("expected match")
	}
	if matchesKind("gm:Executable", []string{"gm:TypeDecl"}) {
		t.Error("expected no match")
	}
	if !matchesKind("gm:TypeDecl", nil) {
		t.Error("expected match with empty filter")
	}
}

func TestExtractSubgraphUMLActivity(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.UMLActivity, types.QueryOptions{EntryPointID: "cmd/app/main.go::Main"})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphUMLComposite(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.UMLComposite, types.QueryOptions{EntryPointID: "internal/models/user.go::User"})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphUMLProfile(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.UMLProfile, types.QueryOptions{})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphUMLUsecase(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.UMLUsecase, types.QueryOptions{})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphUMLCommunication(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.UMLCommunication, types.QueryOptions{EntryPointID: "cmd/app/main.go::Main"})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphUMLInteractionOverview(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.UMLInteractionOverview, types.QueryOptions{EntryPointID: "cmd/app/main.go::Main"})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphUMLTiming(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.UMLTiming, types.QueryOptions{})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphC4Code(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.C4Code, types.QueryOptions{EntryPointID: "internal/models/user.go::User"})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphC4Dynamic(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.C4Dynamic, types.QueryOptions{EntryPointID: "cmd/app/main.go::Main"})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphC4Deployment(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.C4Deployment, types.QueryOptions{})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestExtractSubgraphFlowchart(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.Flowchart, types.QueryOptions{EntryPointID: "cmd/app/main.go::Main"})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestEntryPointsAutoDiscover(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	sub, err := ExtractSubgraph(path, types.CallGraph, types.QueryOptions{EntryPointID: "cmd/app/main.go::Main"})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
}

func TestEntryPointsEmpty(t *testing.T) {
	path := filepath.Join("..", "testdata", "empty.ttl")
	sub, err := ExtractSubgraph(path, types.CallGraph, types.QueryOptions{})
	if err != nil {
		t.Fatalf("ExtractSubgraph failed: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
	if len(sub.Nodes) != 0 {
		t.Errorf("expected 0 nodes from empty file, got %d", len(sub.Nodes))
	}
}

func TestFilterNodesWithScopePrefix(t *testing.T) {
	nodes := map[string]*types.TTLNode{
		"internal/a.go::A": {ID: "internal/a.go::A", Name: "A", Kind: "gm:Executable"},
		"cmd/b.go::B":      {ID: "cmd/b.go::B", Name: "B", Kind: "gm:Executable"},
	}
	ids := filterNodes(nodes, types.QueryOptions{ScopePrefix: "internal"}, func(n *types.TTLNode) bool {
		return true
	})
	if len(ids) != 1 || ids[0] != "internal/a.go::A" {
		t.Errorf("expected 1 internal node, got %v", ids)
	}
}

func TestFilterNodesWithMaxNodes(t *testing.T) {
	nodes := map[string]*types.TTLNode{
		"a": {ID: "a", Kind: "gm:Executable"},
		"b": {ID: "b", Kind: "gm:Executable"},
		"c": {ID: "c", Kind: "gm:Executable"},
	}
	ids := filterNodes(nodes, types.QueryOptions{MaxNodes: 2}, func(n *types.TTLNode) bool {
		return true
	})
	if len(ids) > 2 {
		t.Errorf("expected max 2 nodes, got %d", len(ids))
	}
}

func TestReverseBFS(t *testing.T) {
	nodes := map[string]*types.TTLNode{
		"a": {ID: "a"},
		"b": {ID: "b"},
		"c": {ID: "c"},
	}
	edges := []types.TTLEdge{
		{SourceID: "a", TargetID: "c", Predicate: "gm:calls"},
		{SourceID: "b", TargetID: "c", Predicate: "gm:calls"},
	}
	sub := reverseBFS([]string{"c"}, nodes, edges, 3, func(e types.TTLEdge) bool { return true })
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
	// reverseBFS includes start nodes + all nodes reachable via inbound edges
	// c is the start, a and b point to c, so all 3 are included
	if len(sub.Nodes) != 3 {
		t.Errorf("expected 3 nodes (c + a + b), got %d", len(sub.Nodes))
	}
	if _, ok := sub.Nodes["c"]; !ok {
		t.Error("expected start node 'c' to be included")
	}
}

func TestBothPassSubgraph(t *testing.T) {
	nodes := map[string]*types.TTLNode{
		"a": {ID: "a", Kind: "gm:Executable"},
		"b": {ID: "b", Kind: "gm:TypeDecl"},
	}
	edges := []types.TTLEdge{
		{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
	}
	sub := bothPassSubgraph([]string{}, nodes, edges, 3,
		func(n *types.TTLNode) bool { return n.Kind == "gm:Executable" },
		func(e types.TTLEdge) bool { return true },
	)
	if sub == nil {
		t.Fatal("expected non-nil subgraph")
	}
	// Only node "a" (gm:Executable) should pass, so edge a->b should be filtered out since b not in nodes
	if len(sub.Nodes) != 1 {
		t.Errorf("expected 1 node (Executable), got %d", len(sub.Nodes))
	}
}

func TestApplyScopeFolderFilter(t *testing.T) {
	g := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"internal/a.go::A": {ID: "internal/a.go::A", FileURI: "internal/a.go"},
			"cmd/b.go::B":      {ID: "cmd/b.go::B", FileURI: "cmd/b.go"},
		},
	}
	ApplyScope(g, types.QueryOptions{Scope: types.ScopeFolder, ScopePath: "internal"})
	if _, ok := g.Nodes["cmd/b.go::B"]; ok {
		t.Error("cmd node should have been filtered out")
	}
	if _, ok := g.Nodes["internal/a.go::A"]; !ok {
		t.Error("internal node should remain")
	}
}

func TestPredicatesForGroup(t *testing.T) {
	groups := []types.PredicateGroup{
		types.GroupCallGraph,
		types.GroupTypeHierarchy,
		types.GroupComposition,
		types.GroupDataFlow,
		types.GroupControlFlow,
		types.GroupStructural,
		types.GroupMessaging,
		types.GroupInfrastructure,
		types.GroupSecurity,
		types.GroupBinding,
		types.GroupAny,
	}
	for _, g := range groups {
		preds := predicatesForGroup(g)
		if g == types.GroupAny {
			if preds != nil {
				t.Errorf("GroupAny should return nil, got %v", preds)
			}
		} else if len(preds) == 0 {
			t.Errorf("group %d should return non-empty predicates", g)
		}
	}
}

func TestFilterNodesMaxNodes(t *testing.T) {
	nodes := map[string]*types.TTLNode{
		"a": {ID: "a", Kind: "gm:TypeDecl"},
		"b": {ID: "b", Kind: "gm:TypeDecl"},
		"c": {ID: "c", Kind: "gm:TypeDecl"},
	}
	ids := filterNodes(nodes, types.QueryOptions{MaxNodes: 2}, func(n *types.TTLNode) bool {
		return true
	})
	if len(ids) > 2 {
		t.Errorf("expected at most 2 IDs with MaxNodes=2, got %d", len(ids))
	}
}

func TestExtractWithConfigMaxNodesZero(t *testing.T) {
	nodes := map[string]*types.TTLNode{
		"a": {ID: "a", Kind: "gm:TypeDecl"},
		"b": {ID: "b", Kind: "gm:TypeDecl"},
	}
	var edges []types.TTLEdge
	cfg := types.ExtractionConfig{
		Name: "Test", EntryStrategy: types.EntryStrategyAll,
		Direction: types.EdgeDirectionBoth,
	}
	opts := types.QueryOptions{MaxNodes: 0}
	sub := extractWithConfig(nodes, edges, cfg, opts)
	if len(sub.Nodes) != 2 {
		t.Errorf("expected 2 nodes when MaxNodes=0, got %d", len(sub.Nodes))
	}
}

func TestExtractWithConfigEntryPointOverride(t *testing.T) {
	nodes := map[string]*types.TTLNode{
		"a": {ID: "a", Kind: "gm:Executable"},
		"b": {ID: "b", Kind: "gm:Executable"},
	}
	edges := []types.TTLEdge{{SourceID: "a", TargetID: "b", Predicate: "gm:calls"}}
	cfg := types.ExtractionConfig{
		Name: "Test", EntryStrategy: types.EntryStrategyEntryPoint,
		Direction: types.EdgeDirectionForward, MaxDepth: 5,
	}
	opts := types.QueryOptions{EntryPointID: "a"}
	sub := extractWithConfig(nodes, edges, cfg, opts)
	if _, ok := sub.Nodes["a"]; !ok {
		t.Error("expected entry point 'a' to be included")
	}
	if _, ok := sub.Nodes["b"]; !ok {
		t.Error("expected 'b' to be reachable from entry point 'a'")
	}
}

func TestExtractWithConfigGroupAny(t *testing.T) {
	nodes := map[string]*types.TTLNode{
		"a": {ID: "a", Kind: "gm:Executable"},
		"b": {ID: "b", Kind: "gm:Executable"},
	}
	edges := []types.TTLEdge{
		{SourceID: "a", TargetID: "b", Predicate: "gm:customPredicate"},
	}
	cfg := types.ExtractionConfig{
		Name: "Test", EntryStrategy: types.EntryStrategyAll,
		PredicateGroup: []types.PredicateGroup{types.GroupAny},
		Direction: types.EdgeDirectionForward, MaxDepth: 5,
	}
	sub := extractWithConfig(nodes, edges, cfg, types.QueryOptions{})
	// GroupAny should allow all predicates, including custom ones
	if len(sub.Edges) != 1 {
		t.Errorf("expected 1 edge with GroupAny, got %d", len(sub.Edges))
	}
}

func TestExtractWithConfigChangedFiles(t *testing.T) {
	nodes := map[string]*types.TTLNode{
		"main.go::a": {ID: "main.go::a", Kind: "gm:Executable", FileURI: "main.go"},
		"util.go::b":  {ID: "util.go::b", Kind: "gm:Executable", FileURI: "util.go"},
	}
	edges := []types.TTLEdge{
		{SourceID: "main.go::a", TargetID: "util.go::b", Predicate: "gm:calls"},
	}
	cfg := types.ExtractionConfig{
		Name: "Test", EntryStrategy: types.EntryStrategyChangedFiles,
		Direction: types.EdgeDirectionReverse,
	}
	opts := types.QueryOptions{ChangedFiles: []string{"main.go"}}
	sub := extractWithConfig(nodes, edges, cfg, opts)
	if _, ok := sub.Nodes["main.go::a"]; !ok {
		t.Error("expected node from changed file 'main.go' to be included")
	}
}
