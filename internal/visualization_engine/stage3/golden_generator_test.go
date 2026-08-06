package stage3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
	"github.com/stretchr/testify/require"
)

// TestGenerateGoldenFixtures renders all 31 diagram types to Mermaid and PlantUML formats
// and verifies/persists the 62 golden fixture files in testdata/golden/ (W4-06 / §8.0).
func TestGenerateGoldenFixtures(t *testing.T) {
	goldenDir := filepath.Join("..", "testdata", "golden")
	require.NoError(t, os.MkdirAll(goldenDir, 0755))

	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "node1", Name: "ServiceA", Kind: "STRUCT"},
			{ID: "node2", Name: "RepositoryB", Kind: "STRUCT"},
		},
		Edges: []types.LayoutEdge{
			{SourceID: "node1", TargetID: "node2", Predicate: "gm:calls"},
		},
		Summary: &types.GraphSummary{NodeCount: 2, EdgeCount: 1},
	}

	allTypes := []types.DiagramType{
		// 14 UML
		types.UMLClass, types.UMLObject, types.UMLComponent, types.UMLDeployment,
		types.UMLPackage, types.UMLComposite, types.UMLProfile, types.UMLUsecase,
		types.UMLActivity, types.UMLState, types.UMLSequence, types.UMLCommunication,
		types.UMLInteractionOverview, types.UMLTiming,
		// 7 C4
		types.C4Context, types.C4Container, types.C4Component, types.C4Code,
		types.C4Landscape, types.C4Dynamic, types.C4Deployment,
		// 10 Specialized
		types.ERDiagram, types.DataFlow, types.Mindmap, types.Flowchart,
		types.DependencyGraph, types.HotspotComplexity, types.CallGraph,
		types.LayeredArchitecture, types.ChangeImpact, types.Infrastructure,
	}

	formats := []string{"mermaid", "plantuml"}
	count := 0

	for _, dt := range allTypes {
		for _, fmtName := range formats {
			markup := RenderDiagramFormat(tree, dt, fmtName)
			require.NotEmpty(t, markup, "markup for %s (%s) must not be empty", dt, fmtName)

			ext := ".mmd"
			if fmtName == "plantuml" {
				ext = ".puml"
			}

			filename := strings.ToLower(string(dt)) + ext
			path := filepath.Join(goldenDir, filename)

			require.NoError(t, os.WriteFile(path, []byte(markup), 0644))
			count++
		}
	}

	require.Equal(t, 62, count, "should generate exactly 62 golden fixture files (31 types x 2 formats)")
}
