package akg

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

func TestShouldApplyRule(t *testing.T) {
	tests := []struct {
		tier     string
		mode     string
		expected bool
	}{
		{RuleTierHeuristic, "disabled", false},
		{RuleTierStructural, "disabled", false},
		{RuleTierArchitectural, "disabled", false},

		{RuleTierHeuristic, "structural", false},
		{RuleTierStructural, "structural", true},
		{RuleTierArchitectural, "structural", true},

		{RuleTierHeuristic, "all", true},
		{RuleTierStructural, "all", true},
		{RuleTierArchitectural, "all", true},
	}

	for _, tt := range tests {
		got := shouldApplyRule(tt.tier, tt.mode)
		if got != tt.expected {
			t.Errorf("shouldApplyRule(%q, %q) = %v, want %v", tt.tier, tt.mode, got, tt.expected)
		}
	}
}

func TestMacroInferenceDisabled(t *testing.T) {
	graph := NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("test::main", &link.ResolvedNode{
		ID:   "test::main",
		Kind: "FUNCTION",
		Name: "main",
	})
	graph.Nodes = graph.Nodes.Set("test::Service", &link.ResolvedNode{
		ID:   "test::Service",
		Kind: "CLASS",
		Name: "UserService",
	})

	// Disabled mode should not add any macro rules
	cfg := link.LinkerConfig{MacroInference: "disabled"}
	RunTopologicalMacroInference(graph, cfg)

	graph.MacroRules.Iterate(func(id string, rules []string) {
		if len(rules) > 0 {
			t.Errorf("disabled mode: node %q has macro rules: %v", id, rules)
		}
	})
}

func TestMacroInferenceStructural(t *testing.T) {
	graph := NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("test::UserService", &link.ResolvedNode{
		ID:   "test::UserService",
		Kind: "CLASS",
		Name: "UserService",
	})
	graph.Nodes = graph.Nodes.Set("test::Controller", &link.ResolvedNode{
		ID:   "test::Controller",
		Kind: "CLASS",
		Name: "UserController",
	})
	graph.Nodes = graph.Nodes.Set("test::main", &link.ResolvedNode{
		ID:   "test::main",
		Kind: "FUNCTION",
		Name: "main",
		Properties: map[string]string{
			"content": "func main()",
		},
	})

	// Add some structural edges to trigger structural rules
	graph.OutboundEdges = graph.OutboundEdges.Set("test::main", []link.ResolvedEdge{
		{SourceID: "test::main", TargetID: "test::Controller", Type: link.EdgeCalls, LineNumber: 1},
	})

	cfg := link.LinkerConfig{MacroInference: "structural"}
	RunTopologicalMacroInference(graph, cfg)

	// In structural mode, no heuristic rules should fire
	graph.MacroRules.Iterate(func(id string, rules []string) {
		for _, r := range rules {
			t.Logf("node %q rule: %s", id, r)
			// UserService would match Rule 6 (service) but it's heuristic
			// UserController would match Rule 7 (controller) but it's heuristic
		}
	})

	// Pure heuristic rules should NOT fire in structural mode
	graph.MacroRules.Iterate(func(_ string, rules []string) {
		for _, r := range rules {
			if contains(r, "[heuristic]") {
				t.Errorf("structural mode: heuristic rule fired: %s", r)
			}
		}
	})
}

func TestMacroInferenceAll(t *testing.T) {
	graph := NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("test::Controller", &link.ResolvedNode{
		ID:   "test::Controller",
		Kind: "CLASS",
		Name: "UserController",
	})

	cfg := link.LinkerConfig{MacroInference: "all"}
	RunTopologicalMacroInference(graph, cfg)

	foundHeuristic := false
	graph.MacroRules.Iterate(func(_ string, rules []string) {
		for _, r := range rules {
			t.Logf("all mode rule: %s", r)
			if contains(r, "[heuristic]") {
				foundHeuristic = true
			}
		}
	})

	// In "all" mode, at least some heuristic rules should fire (e.g., "Controller" matches Rule 7)
	if !foundHeuristic {
		t.Error("all mode: expected at least one heuristic rule to fire for 'Controller'")
	}
}

func TestMacroInferenceDefault(t *testing.T) {
	// Calling with no config should behave like "all"
	graph := NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("test::Controller", &link.ResolvedNode{
		ID:   "test::Controller",
		Kind: "CLASS",
		Name: "UserController",
	})

	RunTopologicalMacroInference(graph)

	found := false
	graph.MacroRules.Iterate(func(_ string, rules []string) {
		if len(rules) > 0 {
			found = true
		}
	})
	if !found {
		t.Error("default mode (no config): expected some rules to fire")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsString(s, substr)
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
