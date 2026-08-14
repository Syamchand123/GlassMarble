package akg

import (
	"fmt"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryByKind(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "STRUCT", Name: "Foo"})
	g.Nodes = g.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "Bar"})
	g.Nodes = g.Nodes.Set("c", &link.ResolvedNode{ID: "c", Kind: "STRUCT", Name: "Baz"})
	g.KindIndex = g.KindIndex.Set("STRUCT", map[string]bool{"a": true, "c": true})
	g.KindIndex = g.KindIndex.Set("FUNCTION", map[string]bool{"b": true})

	results := g.Query(link.QueryFilter{Kind: "STRUCT"})
	if len(results) != 2 {
		t.Errorf("expected 2 STRUCT nodes, got %d", len(results))
	}
}

func TestQueryByNameContains(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "CLASS", Name: "UserService"})
	g.Nodes = g.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "CLASS", Name: "OrderService"})
	g.Nodes = g.Nodes.Set("c", &link.ResolvedNode{ID: "c", Kind: "CLASS", Name: "UserController"})
	g.KindIndex = g.KindIndex.Set("CLASS", map[string]bool{"a": true, "b": true, "c": true})

	results := g.Query(link.QueryFilter{NameContains: "Service"})
	if len(results) != 2 {
		t.Errorf("expected 2 nodes with 'Service', got %d", len(results))
	}
}

func TestQueryByProperty(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "FILE", Name: "main.go", Properties: map[string]string{"lang": "go"}})
	g.Nodes = g.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "FILE", Name: "app.py", Properties: map[string]string{"lang": "python"}})
	g.KindIndex = g.KindIndex.Set("FILE", map[string]bool{"a": true, "b": true})

	results := g.Query(link.QueryFilter{Properties: map[string]string{"lang": "go"}})
	if len(results) != 1 || results[0].ID != "a" {
		t.Errorf("expected 1 node with lang=go, got %d", len(results))
	}
}

func TestQueryByPrimitive(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "save", Primitive: "DATABASE"})
	g.Nodes = g.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "fetch", Primitive: "NETWORK_IO"})
	g.KindIndex = g.KindIndex.Set("FUNCTION", map[string]bool{"a": true, "b": true})

	results := g.Query(link.QueryFilter{Primitive: "DATABASE"})
	if len(results) != 1 || results[0].ID != "a" {
		t.Errorf("expected 1 node with primitive=DATABASE, got %d", len(results))
	}
}

func TestQueryComposite(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "CLASS", Name: "UserService", Primitive: "DATABASE"})
	g.Nodes = g.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "CLASS", Name: "OrderService", Primitive: "NETWORK_IO"})
	g.KindIndex = g.KindIndex.Set("CLASS", map[string]bool{"a": true, "b": true})

	results := g.Query(link.QueryFilter{Kind: "CLASS", NameContains: "Service", Primitive: "DATABASE"})
	if len(results) != 1 || results[0].ID != "a" {
		t.Errorf("expected 1 node matching all filters, got %d", len(results))
	}
}

func TestQueryPagination(t *testing.T) {
	g := NewCodePropertyGraph("test")
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("n%d", i)
		g.Nodes = g.Nodes.Set(id, &link.ResolvedNode{ID: id, Kind: "NODE", Name: fmt.Sprintf("Node%d", i)})
	}

	results := g.Query(link.QueryFilter{Limit: 3, Offset: 5})
	if len(results) != 3 {
		t.Errorf("expected 3 results with limit=3 offset=5, got %d", len(results))
	}
}

func TestQueryEmptyFilter(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "A", Name: "A"})
	g.Nodes = g.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "B", Name: "B"})

	results := g.Query(link.QueryFilter{})
	if len(results) != 2 {
		t.Errorf("expected 2 results with empty filter, got %d", len(results))
	}
}

func TestGetNodesByPattern(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "caller"})
	g.Nodes = g.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "callee"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []link.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: link.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("b", []link.ResolvedEdge{{SourceID: "b", TargetID: "a", Type: link.EdgeComposes}})

	callers := g.GetNodesByPattern(link.EdgeCalls, "b")
	if len(callers) != 1 || callers[0] != "a" {
		t.Errorf("expected 1 caller of b, got %v", callers)
	}

	allCallers := g.GetNodesByPattern(link.EdgeCalls, "")
	if len(allCallers) != 1 || allCallers[0] != "a" {
		t.Errorf("expected 1 node with CALLS edge, got %v", allCallers)
	}
}

func TestMatchPattern(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "CLASS", Name: "UserService"})
	g.Nodes = g.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "STRUCT", Name: "OrderRepo", Properties: map[string]string{"lang": "go"}})
	g.Nodes = g.Nodes.Set("c", &link.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "main"})
	g.KindIndex = g.KindIndex.Set("CLASS", map[string]bool{"a": true})
	g.KindIndex = g.KindIndex.Set("STRUCT", map[string]bool{"b": true})
	g.KindIndex = g.KindIndex.Set("FUNCTION", map[string]bool{"c": true})

	results := g.Match("kind:CLASS")
	if len(results) != 1 || results[0].ID != "a" {
		t.Errorf("Match kind:CLASS expected 1 result, got %d", len(results))
	}

	results = g.Match("name:Service")
	if len(results) != 1 || results[0].ID != "a" {
		t.Errorf("Match name:Service expected 1 result, got %d", len(results))
	}

	results = g.Match("prop:lang=go")
	if len(results) != 1 || results[0].ID != "b" {
		t.Errorf("Match prop:lang=go expected 1 result, got %d", len(results))
	}
}

func TestMacroCacheHit(t *testing.T) {
	graph := NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("test::Service", &link.ResolvedNode{
		ID: "test::Service", Kind: "CLASS", Name: "UserService",
	})
	graph.KindIndex = graph.KindIndex.Set("CLASS", map[string]bool{"test::Service": true})

	graph.macroCache = NewCowMap[string, []string]()

	cfg := link.LinkerConfig{MacroInference: "all"}
	RunTopologicalMacroInference(graph, cfg)

	RunTopologicalMacroInference(graph, cfg)

	rules, _ := graph.MacroRules.Get("test::Service")
	if len(rules) == 0 {
		t.Error("expected macro rules on second run")
	}
}

func TestMacroCache_Invalidation(t *testing.T) {
	graph := NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("svc", &link.ResolvedNode{ID: "svc", Kind: "CLASS", Name: "MyService"})
	graph.KindIndex = graph.KindIndex.Set("CLASS", map[string]bool{"svc": true})
	graph.macroCache = NewCowMap[string, []string]()

	cfg := link.LinkerConfig{MacroInference: "all"}
	RunTopologicalMacroInference(graph, cfg)
	rules, _ := graph.MacroRules.Get("svc")
	initialLen := len(rules)

	graph.macroCache = nil

	RunTopologicalMacroInference(graph, cfg)
	rules, _ = graph.MacroRules.Get("svc")
	if len(rules) == 0 {
		t.Error("expected rules after cache invalidation and re-inference")
	}
	_ = initialLen
}

func TestDisabledRules_AllowOthers(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("svc", &link.ResolvedNode{ID: "svc", Kind: "CLASS", Name: "UserService"})
	g.KindIndex = g.KindIndex.Set("CLASS", map[string]bool{"svc": true})

	config := link.LinkerConfig{
		MacroInference: "all",
		DisabledRules:  []string{"rule_06"},
	}
	RunTopologicalMacroInference(g, config)

	rules, ok := g.MacroRules.Get("svc")
	require.True(t, ok)

	// rule_06 (Business Logic Service) should be excluded
	for _, r := range rules {
		assert.NotContains(t, r, "Business Logic Service", "rule_06 should be disabled")
	}
	t.Logf("UserService rules (with rule_06 disabled): %v", rules)
}

func TestMatchPattern_Primitive(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "save", Primitive: "DATABASE"})
	g.Nodes = g.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "fetch", Primitive: "NETWORK_IO"})
	g.Nodes = g.Nodes.Set("c", &link.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "compute", Primitive: "COMPUTE"})
	g.KindIndex = g.KindIndex.Set("FUNCTION", map[string]bool{"a": true, "b": true, "c": true})

	results := g.Match("primitive:DATABASE")
	require.Len(t, results, 1)
	assert.Equal(t, "a", results[0].ID)
}

func TestQueryByNameRegex(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "CLASS", Name: "FooService"})
	g.Nodes = g.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "CLASS", Name: "BarService"})
	g.Nodes = g.Nodes.Set("c", &link.ResolvedNode{ID: "c", Kind: "CLASS", Name: "FooHandler"})
	g.KindIndex = g.KindIndex.Set("CLASS", map[string]bool{"a": true, "b": true, "c": true})

	results := g.Query(link.QueryFilter{NameRegex: ".*Service"})
	require.Len(t, results, 2)
	for _, n := range results {
		assert.Contains(t, n.Name, "Service")
	}
}

func TestQueryByPropertyRegex(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "FILE", Name: "main.go", Properties: map[string]string{"version": "v1.0"}})
	g.Nodes = g.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "FILE", Name: "app.py", Properties: map[string]string{"version": "v2.0"}})
	g.Nodes = g.Nodes.Set("c", &link.ResolvedNode{ID: "c", Kind: "FILE", Name: "util.go", Properties: map[string]string{"version": "v1.5"}})
	g.KindIndex = g.KindIndex.Set("FILE", map[string]bool{"a": true, "b": true, "c": true})

	results := g.Query(link.QueryFilter{PropertyRegex: map[string]string{"version": "v1\\..*"}})
	require.Len(t, results, 2)
	for _, n := range results {
		assert.Contains(t, n.Properties["version"], "v1.")
	}
}

func TestQueryByMinEdges(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "zero"})
	g.Nodes = g.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "one"})
	g.Nodes = g.Nodes.Set("c", &link.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "two"})
	g.OutboundEdges = g.OutboundEdges.Set("b", []link.ResolvedEdge{{SourceID: "b", TargetID: "a", Type: link.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("c", []link.ResolvedEdge{
		{SourceID: "c", TargetID: "a", Type: link.EdgeCalls},
		{SourceID: "c", TargetID: "b", Type: link.EdgeCalls},
	})

	results := g.Query(link.QueryFilter{MinEdges: 1})
	require.Len(t, results, 2)
	for _, n := range results {
		assert.NotEqual(t, "a", n.ID, "node with 0 edges should be excluded")
	}
}

func TestQueryByMaxEdges(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "zero"})
	g.Nodes = g.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "one"})
	g.Nodes = g.Nodes.Set("c", &link.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "two"})
	g.OutboundEdges = g.OutboundEdges.Set("b", []link.ResolvedEdge{{SourceID: "b", TargetID: "a", Type: link.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("c", []link.ResolvedEdge{
		{SourceID: "c", TargetID: "a", Type: link.EdgeCalls},
		{SourceID: "c", TargetID: "b", Type: link.EdgeCalls},
	})

	results := g.Query(link.QueryFilter{MaxEdges: 1})
	require.Len(t, results, 2)
	for _, n := range results {
		assert.NotEqual(t, "c", n.ID, "node with 2 edges should be excluded")
	}
}
