package arch_linter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern  string
		path     string
		expected bool
	}{
		{"internal/domain/**", "internal/domain/user.go", true},
		{"internal/domain/**", "internal/domain/sub/entity.go", true},
		{"internal/domain/**", "internal/infrastructure/db.go", false},
		{"internal/**/db.go", "internal/infrastructure/db.go", true},
		{"*.go", "main.go", true},
		{"*.go", "internal/main.go", false},
		{"**/testdata/**", "internal/akg/testdata/minimal.json", true},
	}

	for _, tt := range tests {
		got := MatchGlob(tt.pattern, tt.path)
		if got != tt.expected {
			t.Errorf("MatchGlob(%q, %q) = %v; want %v", tt.pattern, tt.path, got, tt.expected)
		}
	}
}

func TestLintDenyImportsAndCycles(t *testing.T) {
	graph := akg.NewCodePropertyGraph("commit1")

	// Node 1: Domain entity
	graph.Nodes = graph.Nodes.Set("n1", &link.ResolvedNode{
		ID:   "n1",
		Name: "UserEntity",
		Kind: "STRUCT",
		FileSpec: link.LocationMeta{
			Path:      "internal/domain/user.go",
			LineStart: 10,
			LineEnd:   20,
		},
	})

	// Node 2: Database repository (infrastructure)
	graph.Nodes = graph.Nodes.Set("n2", &link.ResolvedNode{
		ID:   "n2",
		Name: "PostgresRepo",
		Kind: "STRUCT",
		FileSpec: link.LocationMeta{
			Path:      "internal/infrastructure/db.go",
			LineStart: 15,
			LineEnd:   30,
		},
	})

	// Node 3: Service
	graph.Nodes = graph.Nodes.Set("n3", &link.ResolvedNode{
		ID:   "n3",
		Name: "UserService",
		Kind: "STRUCT",
		FileSpec: link.LocationMeta{
			Path:      "internal/service/user_service.go",
			LineStart: 5,
			LineEnd:   25,
		},
	})

	// Add illegal edge: Domain -> Infrastructure
	graph.OutboundEdges = graph.OutboundEdges.Set("n1", []link.ResolvedEdge{
		{
			SourceID: "n1",
			TargetID: "n2",
			Type:     link.EdgeDependsOn,
		},
	})

	// Add legal edge: Service -> Domain
	graph.OutboundEdges = graph.OutboundEdges.Set("n3", []link.ResolvedEdge{
		{
			SourceID: "n3",
			TargetID: "n1",
			Type:     link.EdgeDependsOn,
		},
	})

	ruleset := &Ruleset{
		Version: "1",
		Rules: []Rule{
			{
				ID:          "domain-isolation",
				Name:        "Domain Isolation",
				Severity:    SeverityError,
				From:        "internal/domain/**",
				DenyImports: []string{"internal/infrastructure/**"},
			},
			{
				ID:            "no-cycles",
				Name:          "No Cycles",
				Severity:      SeverityError,
				PreventCycles: true,
				Scope:         "internal/**",
			},
		},
	}

	res, err := Lint(graph, ruleset)
	if err != nil {
		t.Fatalf("Lint returned error: %v", err)
	}

	if res.Passed {
		t.Error("expected Lint to fail due to domain-isolation violation, but passed")
	}

	if res.ErrorsCount != 1 {
		t.Errorf("expected 1 error violation, got %d", res.ErrorsCount)
	}

	if len(res.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(res.Violations))
	}

	v := res.Violations[0]
	if v.RuleID != "domain-isolation" {
		t.Errorf("expected violation rule ID 'domain-isolation', got %q", v.RuleID)
	}
	if v.SourcePath != "internal/domain/user.go" {
		t.Errorf("expected source path 'internal/domain/user.go', got %q", v.SourcePath)
	}
}

func TestScaffoldRules(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gmb-lint-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	target := filepath.Join(tempDir, "rules.yaml")
	created, err := ScaffoldRules(target)
	if err != nil {
		t.Fatalf("ScaffoldRules failed: %v", err)
	}

	if _, err := os.Stat(created); os.IsNotExist(err) {
		t.Errorf("file not created at %s", created)
	}

	rs, err := LoadRules(created)
	if err != nil {
		t.Fatalf("failed to load scaffolded rules: %v", err)
	}

	if len(rs.Rules) == 0 {
		t.Error("expected non-empty rules list in scaffolded rules")
	}
}
