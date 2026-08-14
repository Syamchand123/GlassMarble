package link

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
	"github.com/stretchr/testify/assert"
)

func TestSpecializedRenderers(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "node1", Name: "ComponentA", Kind: "STRUCT"},
		},
		Summary: &types.GraphSummary{NodeCount: 1},
	}

	umlTypes := []types.DiagramType{
		types.UMLClass, types.UMLObject, types.UMLComponent, types.UMLDeployment,
		types.UMLPackage, types.UMLComposite, types.UMLProfile, types.UMLUsecase,
		types.UMLActivity, types.UMLState, types.UMLSequence, types.UMLCommunication,
		types.UMLInteractionOverview, types.UMLTiming,
	}

	for _, ut := range umlTypes {
		out := RenderUMLDiagram(tree, ut, "mermaid")
		assert.NotEmpty(t, out, "UML diagram %s should produce non-empty markup", ut)
	}

	c4Types := []types.DiagramType{
		types.C4Context, types.C4Container, types.C4Component, types.C4Code,
		types.C4Landscape, types.C4Dynamic, types.C4Deployment,
	}

	for _, ct := range c4Types {
		out := RenderC4Diagram(tree, ct, "mermaid")
		assert.NotEmpty(t, out, "C4 diagram %s should produce non-empty markup", ct)
	}

	specTypes := []types.DiagramType{
		types.ERDiagram, types.DataFlow, types.Mindmap, types.Flowchart,
		types.DependencyGraph, types.HotspotComplexity, types.CallGraph,
		types.LayeredArchitecture, types.ChangeImpact, types.Infrastructure,
	}

	for _, st := range specTypes {
		out := RenderSpecializedDiagram(tree, st, "mermaid")
		assert.NotEmpty(t, out, "Specialized diagram %s should produce non-empty markup", st)
	}
}
