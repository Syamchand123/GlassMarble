package akg

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// TestRunIncrementalMacroInference_ChangedSubgraphOnly verifies that only the
// changed node (plus inbound dependents) is re-inferred, while an unrelated
// node keeps its previously inferred rules untouched.
func TestRunIncrementalMacroInference_ChangedSubgraphOnly(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("svc", &link.ResolvedNode{ID: "svc", Kind: "CLASS", Name: "MyService", FileSpec: link.LocationMeta{Path: "svc.go"}})
	g.Nodes = g.Nodes.Set("repo", &link.ResolvedNode{ID: "repo", Kind: "CLASS", Name: "UserRepository", FileSpec: link.LocationMeta{Path: "repo.go"}})
	g.Nodes = g.Nodes.Set("other", &link.ResolvedNode{ID: "other", Kind: "CLASS", Name: "Widget", FileSpec: link.LocationMeta{Path: "other.go"}})

	// Full inference first establishes rules for all nodes.
	RunTopologicalMacroInference(g)
	if _, ok := g.MacroRules.Get("repo"); !ok {
		t.Fatal("full inference should have produced rules for repo")
	}
	_, otherHas := g.MacroRules.Get("other")

	// Now a delta touches only repo.go. other.go must be untouched (still
	// absent from MacroRules if it had none before).
	RunIncrementalMacroInference(g, []string{"repo.go"}, []string{"repo"})

	if _, ok := g.MacroRules.Get("repo"); !ok {
		t.Error("repo rules missing after incremental inference")
	}
	_, otherAfter := g.MacroRules.Get("other")
	if otherHas != otherAfter {
		t.Errorf("unrelated node 'other' rules changed by incremental inference (before=%v after=%v)", otherHas, otherAfter)
	}
}

// TestRunIncrementalMacroInference_SeedsFromFilesWithoutPaths verifies that
// delta nodes whose FileSpec.Path is empty are still re-inferred when they are
// passed explicitly as changed node IDs.
func TestRunIncrementalMacroInference_SeedsFromFilesWithoutPaths(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("svc", &link.ResolvedNode{ID: "svc", Kind: "CLASS", Name: "MyService"})

	RunIncrementalMacroInference(g, []string{"test.go"}, []string{"svc"})

	rules, ok := g.MacroRules.Get("svc")
	if !ok {
		t.Fatal("expected MacroRules entry for svc")
	}
	hasServiceRule := false
	for _, r := range rules {
		if contains(r, "Service") || contains(r, "Business Logic") {
			hasServiceRule = true
			break
		}
	}
	if !hasServiceRule {
		t.Errorf("expected Service Layer rule for MyService, got %v", rules)
	}
}

// TestRunIncrementalMacroInference_InboundDependentsReinferred verifies that a
// caller reaching a changed node is also re-inferred (its rule inputs depend on
// the DFS walk through the callee).
func TestRunIncrementalMacroInference_InboundDependentsReinferred(t *testing.T) {
	g := NewCodePropertyGraph("test")
	// dbStore has DATABASE primitive; cacheStore is reached by svc and carries
	// "cache" in its name so rule_21 (Cache-Aside) can fire.
	g.Nodes = g.Nodes.Set("dbStore", &link.ResolvedNode{ID: "dbStore", Kind: "CLASS", Name: "Store", Primitive: "DATABASE", FileSpec: link.LocationMeta{Path: "db.go"}})
	g.Nodes = g.Nodes.Set("cache", &link.ResolvedNode{ID: "cache", Kind: "CLASS", Name: "CacheClient", FileSpec: link.LocationMeta{Path: "cache.go"}})
	g.Nodes = g.Nodes.Set("svc", &link.ResolvedNode{ID: "svc", Kind: "CLASS", Name: "MyService", FileSpec: link.LocationMeta{Path: "svc.go"}})
	addEdgeToGraph(g, "svc", "cache", link.EdgeCalls, 1)
	addEdgeToGraph(g, "cache", "dbStore", link.EdgeCalls, 2)

	// Change db.go; dbStore is reached through cache, so cache and svc are
	// inbound dependents within bounded depth and must be re-inferred.
	RunIncrementalMacroInference(g, []string{"db.go"}, []string{"dbStore"})

	if _, ok := g.MacroRules.Get("dbStore"); !ok {
		t.Error("changed node dbStore not re-inferred")
	}
	if _, ok := g.MacroRules.Get("cache"); !ok {
		t.Error("inbound dependent cache not re-inferred")
	}
	if _, ok := g.MacroRules.Get("svc"); !ok {
		t.Error("inbound dependent svc not re-inferred")
	}
}

// TestRunIncrementalMacroInference_DisabledModeIsNoop verifies that the
// "disabled" macro mode skips all work, mirroring the full reasoner.
func TestRunIncrementalMacroInference_DisabledModeIsNoop(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("svc", &link.ResolvedNode{ID: "svc", Kind: "CLASS", Name: "MyService", FileSpec: link.LocationMeta{Path: "svc.go"}})

	RunIncrementalMacroInference(g, []string{"svc.go"}, []string{"svc"}, link.LinkerConfig{MacroInference: "disabled"})

	if _, ok := g.MacroRules.Get("svc"); ok {
		t.Error("disabled mode should not infer rules")
	}
}

// TestRunIncrementalMacroInference_NilGraph is a safety no-op.
func TestRunIncrementalMacroInference_NilGraph(t *testing.T) {
	RunIncrementalMacroInference(nil, []string{"a.go"}, nil)
	// No panic is the assertion.
}
