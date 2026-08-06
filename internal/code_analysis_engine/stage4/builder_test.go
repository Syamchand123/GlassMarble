package stage4

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// TestPropertiesIsolation (AUDIT A-10): ResolvedNode must deep-copy the GAST
// Properties map so downstream stage4 annotation never leaks back into the
// shared GAST tree (which stage2/serializers also hold).
func TestPropertiesIsolation(t *testing.T) {
	gastProps := map[string]string{
		"fully_qualified_name": "sample.OrderService",
		"type_params":          "T any",
	}

	gast := &stage2.GASTNode{
		Type:       stage2.GASTTypeDeclaration,
		Name:       "OrderService",
		Kind:       "struct",
		StartLine:  1,
		EndLine:    10,
		Properties: gastProps,
		Children: []*stage2.GASTNode{
			{
				Type:      stage2.GASTFunction,
				Name:      "OrderService.Save",
				Kind:      "method",
				StartLine: 4,
				EndLine:   9,
				Properties: map[string]string{
					"fully_qualified_name": "sample.OrderService.Save",
				},
			},
		},
	}

	fileNode := &stage3.FileBoundaryNode{
		FileName:     "service.go",
		RelativePath: "src/sample/service.go",
		Language:     "go",
		GASTRoot:     gast,
	}
	root := stage3.NewDirectoryNode("", "")
	root.Files["service.go"] = fileNode
	s3out := &stage3.Stage3Output{CommitHash: "abc123", RootNode: root}

	output := BuildInitialNodes(s3out, nil)
	if len(output.GraphNodes) < 2 {
		t.Fatalf("expected type + method nodes, got %d", len(output.GraphNodes))
	}

	// Mutate resolved-node Properties as downstream stages do.
	typeNode := output.GraphNodes["src/sample/service.go::OrderService"]
	if typeNode == nil {
		t.Fatalf("type node not registered")
	}
	typeNode.Properties["taint"] = "user_input"
	typeNode.Properties["fully_qualified_name"] = "mutated"

	methodNode := output.GraphNodes["src/sample/service.go::OrderService::Save"]
	if methodNode == nil {
		t.Fatalf("method node not registered")
	}
	methodNode.Properties["taint"] = "secret"

	// GAST maps must be untouched.
	if gastProps["taint"] != "" || gastProps["fully_qualified_name"] != "sample.OrderService" {
		t.Errorf("type GAST Properties mutated via ResolvedNode: %v", gastProps)
	}
	methodProps := gast.Children[0].Properties
	if methodProps["taint"] != "" || methodProps["fully_qualified_name"] != "sample.OrderService.Save" {
		t.Errorf("method GAST Properties mutated via ResolvedNode: %v", methodProps)
	}
}
