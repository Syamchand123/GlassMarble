package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/extract"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/layout"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
	"github.com/stretchr/testify/require"
)

// buildTreeFromTTLFixture parses a real TTL fixture and runs the full
// extract → metric → layout chain so the golden fixtures exercise the real
// pipeline on real data instead of a hand-inlined stub tree (GAP-M-07).
func buildTreeFromTTLFixture(t *testing.T, relTTL string, dt types.DiagramType, opts types.QueryOptions) *types.LayoutTree {
	t.Helper()
	native, err := extract.ParseTTLFileToNative(relTTL)
	require.NoError(t, err, "ParseTTLFileToNative(%s)", relTTL)
	require.NotEmpty(t, native.Nodes, "fixture %s must contain nodes", relTTL)

	cfg := extract.GetExtractionConfig(dt, opts)
	sub, _, err := extract.ExtractFromSubgraph(native, cfg, opts)
	require.NoError(t, err, "ExtractFromSubgraph(%s)", dt)

	metrics := layout.ComputeAllMetrics(sub)
	require.NotNil(t, metrics, "ComputeAllMetrics(%s)", dt)

	tree := layout.BuildLayoutTreeEx(sub, metrics, metrics.Communities, opts, dt)
	require.NotNil(t, tree, "BuildLayoutTreeEx(%s)", dt)
	return tree
}

// TestGenerateGoldenFixtures renders all 31 diagram types to Mermaid,
// PlantUML and DOT formats from a real TTL fixture and persists the 93
// golden fixture files in testdata/golden/ (W4-06 / §8.0, GAP-H-03,
// GAP-M-07).
func TestGenerateGoldenFixtures(t *testing.T) {
	goldenDir := filepath.Join("..", "testdata", "golden")
	require.NoError(t, os.MkdirAll(goldenDir, 0755))

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
	require.Len(t, allTypes, 31, "DiagramType enumeration must cover all 31 types")

	formats := []struct {
		name string
		ext  string
	}{
		{"mermaid", ".mmd"},
		{"plantuml", ".puml"},
		{"dot", ".dot"},
	}
	count := 0
	firstTypeNodes := -1

	for _, dt := range allTypes {
		// Flat all-nodes extraction (no entry point): every type renders
		// the real fixture graph, whatever its entry strategy.
		opts := types.QueryOptions{}
		tree := buildTreeFromTTLFixture(t, filepath.Join("..", "testdata", "full_graph.ttl"), dt, opts)
		if firstTypeNodes < 0 {
			firstTypeNodes = collectGoldenNodeCount(tree)
		}

		for _, fmt := range formats {
			markup := RenderDiagramFormat(tree, dt, fmt.name)
			require.NotEmpty(t, markup, "markup for %s (%s) must not be empty", dt, fmt.name)

			filename := strings.ToLower(string(dt)) + fmt.ext
			path := filepath.Join(goldenDir, filename)
			require.NoError(t, os.WriteFile(path, []byte(markup), 0644))
			count++
		}
	}

	// The fixture tree must be a real graph, not an empty stub (GAP-M-07).
	require.Greater(t, firstTypeNodes, 0, "real TTL fixture must yield nodes for the UML class diagram")
	require.Equal(t, 93, count, "should generate exactly 93 golden fixture files (31 types x 3 formats)")
}

// collectGoldenNodeCount counts every node across the tree hierarchy,
// including nodes grouped into boundary children.
func collectGoldenNodeCount(t *types.LayoutTree) int {
	if t == nil {
		return 0
	}
	n := len(t.Nodes)
	for _, c := range t.Children {
		n += collectGoldenNodeCount(c)
	}
	return n
}
