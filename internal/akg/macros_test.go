package akg

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMacroRule_DeadCode(t *testing.T) {
	graph := NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("orphan", &stage4.ResolvedNode{
		ID: "orphan", Kind: "FUNCTION", Name: "deadFunc",
	})
	graph.Nodes = graph.Nodes.Set("used", &stage4.ResolvedNode{
		ID: "used", Kind: "FUNCTION", Name: "liveFunc",
	})
	graph.InboundEdges = graph.InboundEdges.Set("used", []stage4.ResolvedEdge{
		{SourceID: "caller", TargetID: "used", Type: stage4.EdgeCalls},
	})

	RunTopologicalMacroInference(graph)

	rules, _ := graph.MacroRules.Get("orphan")
	if len(rules) == 0 {
		t.Error("expected dead code rule for orphan node")
	}
}

func TestMacroRule_CircularDependency(t *testing.T) {
	graph := NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "MODULE", Name: "ModuleA"})
	graph.Nodes = graph.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "MODULE", Name: "ModuleB"})
	graph.Nodes = graph.Nodes.Set("c", &stage4.ResolvedNode{ID: "c", Kind: "MODULE", Name: "ModuleC"})
	graph.OutboundEdges = graph.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	graph.OutboundEdges = graph.OutboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "c", Type: stage4.EdgeCalls}})
	graph.OutboundEdges = graph.OutboundEdges.Set("c", []stage4.ResolvedEdge{{SourceID: "c", TargetID: "a", Type: stage4.EdgeCalls}})

	RunTopologicalMacroInference(graph)

	rules, _ := graph.MacroRules.Get("a")
	if len(rules) == 0 {
		t.Error("expected circular dependency rule for node in cycle")
	}
}

func TestMacroRule_ServiceLayer(t *testing.T) {
	graph := NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("svc", &stage4.ResolvedNode{
		ID: "svc", Kind: "CLASS", Name: "UserService",
	})

	RunTopologicalMacroInference(graph)

	found := false
	rules, _ := graph.MacroRules.Get("svc")
	for _, r := range rules {
		if contains(r, "Service") || contains(r, "Business Logic") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected service layer rule for UserService")
	}
}

func TestMacroRule_NetworkIO(t *testing.T) {
	graph := NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("api", &stage4.ResolvedNode{
		ID: "api", Kind: "CLASS", Name: "ApiClient", Primitive: "NETWORK_IO",
	})

	RunTopologicalMacroInference(graph)

	found := false
	rules, _ := graph.MacroRules.Get("api")
	for _, r := range rules {
		if contains(r, "Network") || contains(r, "Remote") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected network IO rule for ApiClient")
	}
}

func TestMacroRule_GodObject(t *testing.T) {
	graph := NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("god", &stage4.ResolvedNode{ID: "god", Kind: "STRUCT", Name: "GodObject"})
	for i := 0; i < 20; i++ {
		id := string(rune('a' + i))
		graph.Nodes = graph.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "STRUCT", Name: "Dep" + id})

		edges, _ := graph.OutboundEdges.Get("god")
		graph.OutboundEdges = graph.OutboundEdges.Set("god", append(edges,
			stage4.ResolvedEdge{SourceID: "god", TargetID: id, Type: stage4.EdgeCalls}))

		inEdges, _ := graph.InboundEdges.Get("god")
		graph.InboundEdges = graph.InboundEdges.Set("god", append(inEdges,
			stage4.ResolvedEdge{SourceID: id, TargetID: "god", Type: stage4.EdgeCalls}))
	}

	_ = graph.DetectGodObjects()
}

func TestDisabledRules_ExcludeRule(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("svc", &stage4.ResolvedNode{ID: "svc", Kind: "CLASS", Name: "UserService"})
	g.Nodes = g.Nodes.Set("ctrl", &stage4.ResolvedNode{ID: "ctrl", Kind: "CLASS", Name: "UserController"})
	g.KindIndex = g.KindIndex.Set("CLASS", map[string]bool{"svc": true, "ctrl": true})

	// Use DisabledRules to disable rule_06 (Service Layer)
	config := stage4.LinkerConfig{
		MacroInference: "all",
		DisabledRules:  []string{"rule_06"},
	}
	RunTopologicalMacroInference(g, config)

	// Rule_06 (Service Layer) should NOT fire for svc
	rulesSvc, ok := g.MacroRules.Get("svc")
	if ok {
		for _, r := range rulesSvc {
			assert.NotContains(t, r, "Business Logic Service", "Rule 06 should be disabled")
		}
	}

	// Rule_07 (Controller) should fire for ctrl since it's not disabled
	rulesCtrl, ok := g.MacroRules.Get("ctrl")
	if ok {
		found := false
		for _, r := range rulesCtrl {
			if strings.Contains(r, "Controller") {
				found = true
				break
			}
		}
		assert.True(t, found, "Rule 07 should still fire")
	}
}

func TestMacroCache_DifferentNodes(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("svc_a", &stage4.ResolvedNode{ID: "svc_a", Kind: "CLASS", Name: "PaymentService"})
	g.Nodes = g.Nodes.Set("svc_b", &stage4.ResolvedNode{ID: "svc_b", Kind: "CLASS", Name: "OrderService"})
	g.KindIndex = g.KindIndex.Set("CLASS", map[string]bool{"svc_a": true, "svc_b": true})

	RunTopologicalMacroInference(g)

	// Both nodes should have rules but cache keys must differ
	rulesA, okA := g.MacroRules.Get("svc_a")
	require.True(t, okA)
	rulesB, okB := g.MacroRules.Get("svc_b")
	require.True(t, okB)

	// Different names should produce different cache keys
	// We can verify by checking the macroCache entries
	assert.NotEqual(t, rulesA, rulesB, "Different nodes should have different rules")
}

func TestRuleRegistry_AllRules(t *testing.T) {
	// Verify the registry contains all expected rules
	assert.GreaterOrEqual(t, len(RuleRegistry), 28, "Should have at least 28 rules")

	// Verify each rule has an ID, Name, Tier, and both functions
	for _, rule := range RuleRegistry {
		assert.NotEmpty(t, rule.ID, "Rule ID must not be empty")
		assert.NotEmpty(t, rule.Name, "Rule Name must not be empty")
		assert.NotEmpty(t, rule.Tier, "Rule Tier must not be empty")
		assert.NotNil(t, rule.Enabled, "Rule Enabled must not be nil")
		assert.NotNil(t, rule.Apply, "Rule Apply must not be nil")
	}

	// Verify all rules have unique IDs
	idSet := make(map[string]bool)
	for _, rule := range RuleRegistry {
		assert.False(t, idSet[rule.ID], "Duplicate rule ID: %s", rule.ID)
		idSet[rule.ID] = true
	}
}

func TestMacroRule_ArticulationPoint(t *testing.T) {
	graph := NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("bridge", &stage4.ResolvedNode{ID: "bridge", Kind: "MODULE", Name: "BridgeModule"})
	graph.Nodes = graph.Nodes.Set("left", &stage4.ResolvedNode{ID: "left", Kind: "MODULE", Name: "LeftMod"})
	graph.Nodes = graph.Nodes.Set("right", &stage4.ResolvedNode{ID: "right", Kind: "MODULE", Name: "RightMod"})
	graph.OutboundEdges = graph.OutboundEdges.Set("bridge", []stage4.ResolvedEdge{
		{SourceID: "bridge", TargetID: "left", Type: stage4.EdgeCalls},
		{SourceID: "bridge", TargetID: "right", Type: stage4.EdgeCalls},
	})
	graph.InboundEdges = graph.InboundEdges.Set("left", []stage4.ResolvedEdge{{SourceID: "bridge", TargetID: "left", Type: stage4.EdgeCalls}})
	graph.InboundEdges = graph.InboundEdges.Set("right", []stage4.ResolvedEdge{{SourceID: "bridge", TargetID: "right", Type: stage4.EdgeCalls}})
	graph.OutboundEdges = graph.OutboundEdges.Set("left", []stage4.ResolvedEdge{})

	_ = graph.FindArticulationPoints()
}

func TestMacroRule_HighBlastRadius(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("target", &stage4.ResolvedNode{ID: "target", Kind: "CLASS", Name: "CoreClass"})
	for i := 0; i < 55; i++ {
		sid := fmt.Sprintf("src_%d", i)
		g.Nodes = g.Nodes.Set(sid, &stage4.ResolvedNode{ID: sid, Kind: "FILE", Name: sid})
		g.OutboundEdges = g.OutboundEdges.Set(sid, []stage4.ResolvedEdge{{SourceID: sid, TargetID: "target", Type: stage4.EdgeCalls}})
		existing, _ := g.InboundEdges.Get("target")
		g.InboundEdges = g.InboundEdges.Set("target", append(existing, stage4.ResolvedEdge{SourceID: sid, TargetID: "target", Type: stage4.EdgeCalls}))
	}
	g.Entrypoints = []string{"src_0"}
	RunTopologicalMacroInference(g)
	rules, ok := g.MacroRules.Get("target")
	require.True(t, ok)
	found := false
	for _, r := range rules {
		if strings.Contains(r, "Blast Radius") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should detect high blast radius")
}

func TestMacroRule_HighPageRank(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("hub", &stage4.ResolvedNode{ID: "hub", Kind: "CLASS", Name: "CoreHub"})
	for i := 0; i < 45; i++ {
		sid := fmt.Sprintf("src_%d", i)
		g.Nodes = g.Nodes.Set(sid, &stage4.ResolvedNode{ID: sid, Kind: "CLASS", Name: sid})
		g.OutboundEdges = g.OutboundEdges.Set(sid, []stage4.ResolvedEdge{{SourceID: sid, TargetID: "hub", Type: stage4.EdgeCalls}})
		existing, _ := g.InboundEdges.Get("hub")
		g.InboundEdges = g.InboundEdges.Set("hub", append(existing, stage4.ResolvedEdge{SourceID: sid, TargetID: "hub", Type: stage4.EdgeCalls}))
	}
	g.Entrypoints = []string{"src_0"}
	RunTopologicalMacroInference(g)
	rules, ok := g.MacroRules.Get("hub")
	require.True(t, ok)
	found := false
	for _, r := range rules {
		if strings.Contains(r, "Core Component") || strings.Contains(r, "PageRank") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should detect high-PageRank core component")
}

func TestMacroRule_IsolatedIsland(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "ConnectedA"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "ConnectedB"})
	g.Nodes = g.Nodes.Set("c", &stage4.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "IslandC"})
	g.Nodes = g.Nodes.Set("d", &stage4.ResolvedNode{ID: "d", Kind: "FUNCTION", Name: "IslandD"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("c", []stage4.ResolvedEdge{{SourceID: "c", TargetID: "d", Type: stage4.EdgeCalls}})
	g.Entrypoints = []string{"a"}
	RunTopologicalMacroInference(g)
	for _, id := range []string{"c", "d"} {
		rules, ok := g.MacroRules.Get(id)
		require.True(t, ok, "expected rules for island node %s", id)
		found := false
		for _, r := range rules {
			if strings.Contains(r, "Isolated") || strings.Contains(r, "Dead Sub-system") {
				found = true
				break
			}
		}
		assert.True(t, found, "node %s should have Isolated rule", id)
	}
}

func TestMacroRule_ArchitecturalBridge(t *testing.T) {
	g := NewCodePropertyGraph("test")
	n := 25
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("n%d", i)
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "CLASS", Name: id})
	}
	for i := 0; i < n-1; i++ {
		src := fmt.Sprintf("n%d", i)
		dst := fmt.Sprintf("n%d", i+1)
		existingSrc, _ := g.OutboundEdges.Get(src)
		g.OutboundEdges = g.OutboundEdges.Set(src, append(existingSrc,
			stage4.ResolvedEdge{SourceID: src, TargetID: dst, Type: stage4.EdgeCalls}))
		existingDst, _ := g.OutboundEdges.Get(dst)
		g.OutboundEdges = g.OutboundEdges.Set(dst, append(existingDst,
			stage4.ResolvedEdge{SourceID: dst, TargetID: src, Type: stage4.EdgeCalls}))
	}
	g.Entrypoints = []string{"n0"}
	RunTopologicalMacroInference(g)
	for _, idx := range []int{12, 13} {
		id := fmt.Sprintf("n%d", idx)
		rules, ok := g.MacroRules.Get(id)
		require.True(t, ok, "expected rules for bridge node %s", id)
		found := false
		for _, r := range rules {
			if strings.Contains(r, "Communication Hub") || strings.Contains(r, "Betweenness") {
				found = true
				break
			}
		}
		assert.True(t, found, "node %s should have Communication Hub rule", id)
	}
}

func TestMacroRule_LowCohesion(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("pkg", &stage4.ResolvedNode{ID: "pkg", Kind: "PACKAGE", Name: "LowCohesionPkg"})
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("comp_%d", i)
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "CLASS", Name: id})
		g.OutboundEdges = g.OutboundEdges.Set(id, []stage4.ResolvedEdge{{SourceID: id, TargetID: "pkg", Type: stage4.EdgeBelongsTo}})
		existing, _ := g.InboundEdges.Get("pkg")
		g.InboundEdges = g.InboundEdges.Set("pkg", append(existing, stage4.ResolvedEdge{SourceID: id, TargetID: "pkg", Type: stage4.EdgeBelongsTo}))
	}
	// Add 1 internal edge so cohesion = 1/3 = 0.33 (< 1.0 and > 0)
	existing, _ := g.OutboundEdges.Get("comp_0")
	g.OutboundEdges = g.OutboundEdges.Set("comp_0", append(existing,
		stage4.ResolvedEdge{SourceID: "comp_0", TargetID: "comp_1", Type: stage4.EdgeCalls}))
	RunTopologicalMacroInference(g)
	rules, ok := g.MacroRules.Get("pkg")
	require.True(t, ok, "expected rules for low-cohesion package")
	found := false
	for _, r := range rules {
		if strings.Contains(r, "Low Cohesion") {
			found = true
			break
		}
	}
	assert.True(t, found, "package should have Low Cohesion rule")
}

func TestMacroCacheCap(t *testing.T) {
	cache := NewCowMap[string, []string]()
	for i := 0; i < maxMacroCacheEntries+1; i++ {
		cache = cache.Set(fmt.Sprintf("key-%d", i), []string{"rule"})
	}
	if cache.Len() <= maxMacroCacheEntries {
		t.Fatalf("precondition: expected >%d entries, got %d", maxMacroCacheEntries, cache.Len())
	}
	capped := capMacroCache(cache)
	if capped.Len() != 0 {
		t.Errorf("capMacroCache must reset the cache once the bound is exceeded, got %d entries", capped.Len())
	}
	// Below the bound the cache is untouched.
	cache = NewCowMap[string, []string]()
	cache = cache.Set("k", []string{"rule"})
	if capMacroCache(cache).Len() != 1 {
		t.Error("capMacroCache must not touch a cache within the bound")
	}
}
