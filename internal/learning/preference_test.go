package learning

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

func TestLearnConventions(t *testing.T) {
	graph := &akg.CodePropertyGraph{
		Nodes:         akg.NewCowMap[string, *stage4.ResolvedNode](),
		FileNodeIndex: akg.NewCowMap[string, map[string]bool](),
	}

	// Setup nodes
	graph.Nodes = graph.Nodes.Set("1", &stage4.ResolvedNode{Kind: "STRUCT", Name: "AuthService"})
	graph.Nodes = graph.Nodes.Set("2", &stage4.ResolvedNode{Kind: "STRUCT", Name: "UserService"})
	graph.Nodes = graph.Nodes.Set("3", &stage4.ResolvedNode{Kind: "STRUCT", Name: "PaymentHandler"})

	// Setup files
	graph.FileNodeIndex = graph.FileNodeIndex.Set("internal/domain/auth_test.go", nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set("internal/api/routes_test.go", nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set("docs/adr/001-init.md", nil)

	conv := LearnConventions(graph, nil)

	if conv.ServiceNamingPattern != "*Service" {
		t.Errorf("Expected '*Service', got '%s'", conv.ServiceNamingPattern)
	}

	if conv.TestFilePattern != "*_test.go" {
		t.Errorf("Expected '*_test.go', got '%s'", conv.TestFilePattern)
	}

	if conv.ADRDirectory != "docs/adr" {
		t.Errorf("Expected 'docs/adr', got '%s'", conv.ADRDirectory)
	}

	hasDomain := false
	hasAPI := false
	for _, l := range conv.LayerDirectories {
		if l == "domain" {
			hasDomain = true
		}
		if l == "api" {
			hasAPI = true
		}
	}

	if !hasDomain || !hasAPI {
		t.Errorf("Missing expected layer directories in %v", conv.LayerDirectories)
	}
}
