package visualization_engine

import (
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// Renderer defines the contract for rendering a LayoutTree into a target markup format (W3-10 / §7.5).
type Renderer interface {
	Render(tree *types.LayoutTree, t types.DiagramType) (string, error)
	Format() string // "mermaid" | "plantuml" | "dot"
	Supported() []types.DiagramType
}

// MermaidRenderer renders layout trees to W3C Mermaid JS syntax.
type MermaidRenderer struct{}

func (r *MermaidRenderer) Render(tree *types.LayoutTree, t types.DiagramType) (string, error) {
	if tree == nil {
		return "", fmt.Errorf("cannot render nil LayoutTree")
	}
	return stage3.RenderDiagramFormat(tree, t, "mermaid"), nil
}

func (r *MermaidRenderer) Format() string { return "mermaid" }

func (r *MermaidRenderer) Supported() []types.DiagramType {
	return []types.DiagramType{
		types.UMLClass, types.UMLObject, types.UMLComponent, types.UMLDeployment,
		types.UMLPackage, types.UMLComposite, types.UMLProfile, types.UMLUsecase,
		types.UMLActivity, types.UMLState, types.UMLSequence, types.UMLCommunication,
		types.UMLInteractionOverview, types.UMLTiming,
		types.C4Context, types.C4Container, types.C4Component, types.C4Code,
		types.C4Landscape, types.C4Dynamic, types.C4Deployment,
		types.ERDiagram, types.DataFlow, types.Mindmap, types.Flowchart,
		types.DependencyGraph, types.HotspotComplexity, types.CallGraph,
		types.LayeredArchitecture, types.ChangeImpact, types.Infrastructure,
	}
}

// PlantUMLRenderer renders layout trees to PlantUML syntax.
type PlantUMLRenderer struct{}

func (r *PlantUMLRenderer) Render(tree *types.LayoutTree, t types.DiagramType) (string, error) {
	if tree == nil {
		return "", fmt.Errorf("cannot render nil LayoutTree")
	}
	return stage3.RenderDiagramFormat(tree, t, "plantuml"), nil
}

func (r *PlantUMLRenderer) Format() string { return "plantuml" }

func (r *PlantUMLRenderer) Supported() []types.DiagramType {
	return []types.DiagramType{
		types.UMLClass, types.UMLObject, types.UMLComponent, types.UMLDeployment,
		types.UMLPackage, types.UMLComposite, types.UMLProfile, types.UMLUsecase,
		types.UMLActivity, types.UMLState, types.UMLSequence, types.UMLCommunication,
		types.UMLInteractionOverview, types.UMLTiming,
		types.C4Context, types.C4Container, types.C4Component, types.C4Code,
		types.C4Landscape, types.C4Dynamic, types.C4Deployment,
		types.ERDiagram, types.DataFlow, types.Mindmap, types.Flowchart,
		types.DependencyGraph, types.HotspotComplexity, types.CallGraph,
		types.LayeredArchitecture, types.ChangeImpact, types.Infrastructure,
	}
}

// DOTRenderer renders layout trees to Graphviz DOT syntax.
type DOTRenderer struct{}

func (r *DOTRenderer) Render(tree *types.LayoutTree, t types.DiagramType) (string, error) {
	if tree == nil {
		return "", fmt.Errorf("cannot render nil LayoutTree")
	}
	return stage3.RenderDiagramFormat(tree, t, "dot"), nil
}

func (r *DOTRenderer) Format() string { return "dot" }

func (r *DOTRenderer) Supported() []types.DiagramType {
	return []types.DiagramType{
		types.UMLClass, types.UMLObject, types.UMLComponent, types.UMLDeployment,
		types.UMLPackage, types.UMLComposite, types.UMLProfile, types.UMLUsecase,
		types.UMLActivity, types.UMLState, types.UMLSequence, types.UMLCommunication,
		types.UMLInteractionOverview, types.UMLTiming,
		types.C4Context, types.C4Container, types.C4Component, types.C4Code,
		types.C4Landscape, types.C4Dynamic, types.C4Deployment,
		types.ERDiagram, types.DataFlow, types.Mindmap, types.Flowchart,
		types.DependencyGraph, types.HotspotComplexity, types.CallGraph,
		types.LayeredArchitecture, types.ChangeImpact, types.Infrastructure,
	}
}
