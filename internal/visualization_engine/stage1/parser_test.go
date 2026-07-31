package stage1

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func TestParseMinimal(t *testing.T) {
	path := filepath.Join("..", "testdata", "minimal.ttl")
	nodes, edges, err := ParseTTLFile(path)
	if err != nil {
		t.Fatalf("ParseTTLFile failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}
	if len(edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(edges))
	}
}

func TestParseEmpty(t *testing.T) {
	path := filepath.Join("..", "testdata", "empty.ttl")
	nodes, edges, err := ParseTTLFile(path)
	if err != nil {
		t.Fatalf("ParseTTLFile failed: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}

func TestParseAllPredicates(t *testing.T) {
	path := filepath.Join("..", "testdata", "all_predicates.ttl")
	nodes, edges, err := ParseTTLFile(path)
	if err != nil {
		t.Fatalf("ParseTTLFile failed: %v", err)
	}
	predCount := make(map[string]int)
	for _, e := range edges {
		predCount[e.Predicate]++
	}
	expectedPreds := []string{"gm:calls", "gm:inheritsFrom", "gm:composes", "gm:dataFlowTo", "gm:controlFlowTo", "gm:belongsToFile"}
	for _, p := range expectedPreds {
		if predCount[p] == 0 {
			t.Errorf("expected at least one edge with predicate %q", p)
		}
	}
	if len(nodes) == 0 {
		t.Error("expected at least one node")
	}
}

func TestParseAllKinds(t *testing.T) {
	path := filepath.Join("..", "testdata", "all_kinds.ttl")
	nodes, edges, err := ParseTTLFile(path)
	if err != nil {
		t.Fatalf("ParseTTLFile failed: %v", err)
	}
	kindCount := make(map[string]int)
	for _, n := range nodes {
		kindCount[n.Kind]++
	}
	expectedKinds := []string{"gm:TypeDecl", "gm:Executable", "gm:Namespace", "gm:File", "gm:Database", "gm:ExternalSystem"}
	for _, k := range expectedKinds {
		if kindCount[k] == 0 {
			t.Errorf("expected at least one node with kind %q", k)
		}
	}
	t.Logf("parsed %d nodes, %d edges", len(nodes), len(edges))
}

func TestParseNodeBlockBasic(t *testing.T) {
	nodes := make(map[string]*types.TTLNode)
	block := `<http://glassmarble.org/node/main.go::Main> a gm:Executable ;
    gm:name "Main" ;
    gm:lineStart 1 ;
    gm:lineEnd 10 ;
    .`
	parseNodeBlock(block, nodes)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n, ok := nodes["main.go::Main"]
	if !ok {
		t.Fatal("expected node key 'main.go::Main'")
	}
	if n.Kind != "gm:Executable" {
		t.Errorf("expected kind gm:Executable, got %s", n.Kind)
	}
	if n.Name != "Main" {
		t.Errorf("expected name 'Main', got '%s'", n.Name)
	}
	if n.LineStart != 1 {
		t.Errorf("expected LineStart 1, got %d", n.LineStart)
	}
	if n.LineEnd != 10 {
		t.Errorf("expected LineEnd 10, got %d", n.LineEnd)
	}
}

func TestParseNodeWithDeletedStatus(t *testing.T) {
	nodes := make(map[string]*types.TTLNode)
	block := `<http://glassmarble.org/node/old.go::OldFunc> a gm:Executable ;
    gm:name "OldFunc" ;
    gm:status "DELETED" ;
    .`
	parseNodeBlock(block, nodes)
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes for deleted entry, got %d", len(nodes))
	}
}

func TestParseNodeWithBelongsToFile(t *testing.T) {
	nodes := make(map[string]*types.TTLNode)
	block := `<http://glassmarble.org/node/main.go::Main> a gm:Executable ;
    gm:name "Main" ;
    gm:belongsToFile <http://glassmarble.org/file/main.go> ;
    .`
	parseNodeBlock(block, nodes)
	n, ok := nodes["main.go::Main"]
	if !ok {
		t.Fatal("expected node key 'main.go::Main'")
	}
	if n.FileURI != "file:main.go" {
		t.Errorf("expected FileURI 'file:main.go', got '%s'", n.FileURI)
	}
}

func TestParseNodeWithEntryPoint(t *testing.T) {
	nodes := make(map[string]*types.TTLNode)
	block := `<http://glassmarble.org/node/main.go::Main> a gm:Executable ;
    gm:name "Main" ;
    gm:isEntrypoint true ;
    .`
	parseNodeBlock(block, nodes)
	n, ok := nodes["main.go::Main"]
	if !ok {
		t.Fatal("expected node key 'main.go::Main'")
	}
	if !n.IsEntrypoint {
		t.Error("expected IsEntrypoint to be true")
	}
}

func TestParseURIStandard(t *testing.T) {
	uri := "<http://glassmarble.org/node/main.go::Main>"
	result := types.ParseNodeURI(uri)
	if result != "main.go::Main" {
		t.Errorf("expected 'main.go::Main', got '%s'", result)
	}
}

func TestParseURIFile(t *testing.T) {
	uri := "<http://glassmarble.org/file/main.go>"
	result := types.ParseNodeURI(uri)
	if result != "file:main.go" {
		t.Errorf("expected 'file:main.go', got '%s'", result)
	}
}

func TestParseURINamespace(t *testing.T) {
	uri := "<http://glassmarble.org/namespace/internal>"
	result := types.ParseNodeURI(uri)
	if result != "module:internal" {
		t.Errorf("expected 'module:internal', got '%s'", result)
	}
}

func TestParseLiteral(t *testing.T) {
	val := "\"Hello World\""
	result := parseLiteral(val)
	if result != "Hello World" {
		t.Errorf("expected 'Hello World', got '%s'", result)
	}
}

func TestParseLiteralWithEscapedQuotes(t *testing.T) {
	val := "\"Hello \\\"World\\\"\""
	result := parseLiteral(val)
	if result != "Hello \"World\"" {
		t.Errorf("expected 'Hello \"World\"', got '%s'", result)
	}
}

func TestParseLiteralWithNewline(t *testing.T) {
	val := "\"Line1\\nLine2\""
	result := parseLiteral(val)
	if !strings.Contains(result, "\n") {
		t.Errorf("expected newline in result, got '%s'", result)
	}
}

func TestParseBaseEdge(t *testing.T) {
	edgeMap := make(map[string]*types.TTLEdge)
	block := `<http://glassmarble.org/node/a.go::A> gm:calls <http://glassmarble.org/node/b.go::B> .`
	parseBaseEdge(block, edgeMap)
	if len(edgeMap) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edgeMap))
	}
}

func TestIsBaseEdge(t *testing.T) {
	if !isBaseEdge("<src> pred <tgt> .") {
		t.Error("expected true for simple triple with plain predicate")
	}
	if !isBaseEdge("<a> <b> <c> .") {
		t.Error("expected true for triple with URI predicate (3 tokens)")
	}
	if isBaseEdge("<a> a gm:Type ; gm:name \"X\" .") {
		t.Error("expected false for node block with semicolons")
	}
	if isBaseEdge("<< <a> <b> <c> >> gm:lineNumber 1 .") {
		t.Error("expected false for reified edge property block")
	}
}

func TestBindEdgeProperty(t *testing.T) {
	edgeMap := make(map[string]*types.TTLEdge)
	edgeKey := "a.go::A|gm:calls|b.go::B"
	edgeMap[edgeKey] = &types.TTLEdge{
		SourceID: "a.go::A", Predicate: "gm:calls", TargetID: "b.go::B",
	}
	block := `<< <http://glassmarble.org/node/a.go::A> gm:calls <http://glassmarble.org/node/b.go::B> >> gm:lineNumber 42 .`
	bindEdgeProperty(block, edgeMap)
	if edgeMap[edgeKey].LineNumber != 42 {
		t.Errorf("expected lineNumber 42, got %d", edgeMap[edgeKey].LineNumber)
	}
}

func TestBindEdgePropertyFusedBrackets(t *testing.T) {
	edgeMap := make(map[string]*types.TTLEdge)
	edgeKey := "a.go::A|gm:calls|b.go::B"
	edgeMap[edgeKey] = &types.TTLEdge{
		SourceID: "a.go::A", Predicate: "gm:calls", TargetID: "b.go::B",
	}
	block := `<<<http://glassmarble.org/node/a.go::A> gm:calls <http://glassmarble.org/node/b.go::B>>> gm:lineNumber 42 .`
	bindEdgeProperty(block, edgeMap)
	if edgeMap[edgeKey].LineNumber != 42 {
		t.Errorf("expected lineNumber 42, got %d", edgeMap[edgeKey].LineNumber)
	}
}

func TestParseFullGraph(t *testing.T) {
	path := filepath.Join("..", "testdata", "full_graph.ttl")
	nodes, edges, err := ParseTTLFile(path)
	if err != nil {
		t.Fatalf("ParseTTLFile failed: %v", err)
	}
	if len(nodes) == 0 {
		t.Error("expected non-zero nodes from full_graph.ttl")
	}
	if len(edges) == 0 {
		t.Error("expected non-zero edges from full_graph.ttl")
	}
}

func TestParseTTLFileToNative(t *testing.T) {
	path := filepath.Join("..", "testdata", "minimal.ttl")
	ng, err := ParseTTLFileToNative(path)
	if err != nil {
		t.Fatalf("ParseTTLFileToNative failed: %v", err)
	}
	if ng == nil {
		t.Fatal("expected non-nil NativeGraph")
	}
	if len(ng.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(ng.Nodes))
	}
	if len(ng.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(ng.Edges))
	}
}

func TestParseEmptyFile(t *testing.T) {
	nodes, edges, err := ParseTTLFile(filepath.Join("..", "testdata", "empty.ttl"))
	if err != nil {
		t.Fatalf("ParseTTLFile failed: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}

func TestPredicatesForGroupCallGraph(t *testing.T) {
	preds := predicatesForGroup(types.GroupCallGraph)
	if len(preds) == 0 {
		t.Error("expected non-empty predicate list for GroupCallGraph")
	}
	hasCalls := false
	for _, p := range preds {
		if p == "gm:calls" {
			hasCalls = true
			break
		}
	}
	if !hasCalls {
		t.Error("expected gm:calls in GroupCallGraph predicates")
	}
}

func TestPredicatesForGroupAny(t *testing.T) {
	preds := predicatesForGroup(types.GroupAny)
	if preds != nil {
		t.Errorf("expected nil for GroupAny, got %v", preds)
	}
}

func TestPredicatesForGroupTypeHierarchy(t *testing.T) {
	preds := predicatesForGroup(types.GroupTypeHierarchy)
	hasInherits := false
	for _, p := range preds {
		if p == "gm:inheritsFrom" {
			hasInherits = true
			break
		}
	}
	if !hasInherits {
		t.Error("expected gm:inheritsFrom in GroupTypeHierarchy")
	}
}

func TestPredicatesForGroupDataFlow(t *testing.T) {
	preds := predicatesForGroup(types.GroupDataFlow)
	hasDataFlow := false
	for _, p := range preds {
		if p == "gm:dataFlowTo" {
			hasDataFlow = true
			break
		}
	}
	if !hasDataFlow {
		t.Error("expected gm:dataFlowTo in GroupDataFlow")
	}
}

func TestPredicatesForGroupControlFlow(t *testing.T) {
	preds := predicatesForGroup(types.GroupControlFlow)
	hasCFlow := false
	for _, p := range preds {
		if p == "gm:controlFlowTo" {
			hasCFlow = true
			break
		}
	}
	if !hasCFlow {
		t.Error("expected gm:controlFlowTo in GroupControlFlow")
	}
}

func TestPredicatesForGroupStructural(t *testing.T) {
	preds := predicatesForGroup(types.GroupStructural)
	hasImports := false
	for _, p := range preds {
		if p == "gm:imports" {
			hasImports = true
			break
		}
	}
	if !hasImports {
		t.Error("expected gm:imports in GroupStructural")
	}
}

func TestPredicatesForGroupMessaging(t *testing.T) {
	preds := predicatesForGroup(types.GroupMessaging)
	hasMessage := false
	for _, p := range preds {
		if p == "gm:sendsMessage" {
			hasMessage = true
			break
		}
	}
	if !hasMessage {
		t.Error("expected gm:sendsMessage in GroupMessaging")
	}
}

func TestParseDeltaAppend(t *testing.T) {
	path := filepath.Join("..", "testdata", "delta_append.ttl")
	nodes, edges, err := ParseTTLFile(path)
	if err != nil {
		t.Fatalf("ParseTTLFile failed: %v", err)
	}
	// OldWorker has gm:status "DELETED" and should be filtered out
	if _, ok := nodes["cmd/app/worker.go::OldWorker"]; ok {
		t.Error("OldWorker with DELETED status should not be in nodes")
	}
	// Main and Connect should still be present
	if _, ok := nodes["cmd/app/main.go::Main"]; !ok {
		t.Error("expected Main to be present in delta_append.ttl")
	}
	if len(edges) > 0 {
		t.Logf("parsed %d edges from delta_append.ttl", len(edges))
	}
}

func TestParseNodeBlockSemicolonOnly(t *testing.T) {
	nodes := make(map[string]*types.TTLNode)
	// Empty attributes with just semicolons should work
	block := `<http://glassmarble.org/node/empty.go::Empty> a gm:TypeDecl ;
    .
    `
	parseNodeBlock(block, nodes)
	if len(nodes) != 1 {
		t.Errorf("expected 1 node with empty attributes, got %d", len(nodes))
	}
}

func TestParseMalformedTTL(t *testing.T) {
	nodes, edges, err := ParseTTLFile(filepath.Join("..", "testdata", "does_not_exist.ttl"))
	if err == nil {
		t.Error("expected error for non-existent file")
	}
	if nodes != nil {
		t.Error("expected nil nodes for error case")
	}
	if edges != nil {
		t.Error("expected nil edges for error case")
	}
}

func TestParseNodeBlockHandlesMetadataTag(t *testing.T) {
	nodes := make(map[string]*types.TTLNode)
	block := `metadata a gm:Metadata ;
    gm:name "test" ;
    .`
	parseNodeBlock(block, nodes)
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes for metadata tag, got %d", len(nodes))
	}
}
