package render

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func sanitizeForTest(name string) string {
	res := name
	res = strings.ReplaceAll(res, "::", "_")
	res = strings.ReplaceAll(res, ":", "_")
	res = strings.ReplaceAll(res, ".", "_")
	res = strings.ReplaceAll(res, "%20", "_")
	res = strings.ReplaceAll(res, "/", "_")
	res = strings.ReplaceAll(res, "\\", "_")
	res = strings.ReplaceAll(res, "-", "_")
	return res
}

func pumlNodeInOutput(t *testing.T, output, nodeID string) {
	t.Helper()
	sanitized := sanitizeForTest(nodeID)
	if !strings.Contains(output, nodeID) && !strings.Contains(output, sanitized) {
		t.Errorf("expected node %q (or sanitized %q) to appear in PlantUML output", nodeID, sanitized)
	}
}

func TestRenderPlantUMLClass(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "main.go::MyClass", Name: "MyClass", Kind: "gm:TypeDecl"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.UMLClass)
	if !strings.Contains(output, "@startuml") {
		t.Error("expected output to contain '@startuml'")
	}
	if !strings.Contains(output, "@enduml") {
		t.Error("expected output to contain '@enduml'")
	}
	pumlNodeInOutput(t, output, "main.go::MyClass")
}

func TestRenderPlantUMLGeneric(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "main.go::Main", Name: "Main", Kind: "gm:Executable"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.CallGraph)
	if !strings.Contains(output, "@startuml") {
		t.Error("expected output to contain '@startuml'")
	}
	if !strings.Contains(output, "@enduml") {
		t.Error("expected output to contain '@enduml'")
	}
	pumlNodeInOutput(t, output, "main.go::Main")
}

func TestRenderPlantUMLC4Context(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "user1", Name: "User", Kind: "gm:User"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.C4Context)
	if !strings.Contains(output, "!include <C4/C4_Context>") {
		t.Error("expected output to contain C4 include")
	}
	pumlNodeInOutput(t, output, "user1")
}

func TestRenderPlantUMLC4Container(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Children: []*types.LayoutTree{
			{
				BoundaryName: "App",
				Nodes: []*types.LayoutNode{
					{ID: "svc1", Name: "Service", Kind: "gm:Executable"},
				},
			},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.C4Container)
	if !strings.Contains(output, "!include <C4/C4_Container>") {
		t.Error("expected output to contain C4_Container include")
	}
	pumlNodeInOutput(t, output, "svc1")
}

func TestRenderPlantUMLC4Component(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "comp1", Name: "Component", Kind: "gm:TypeDecl"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.C4Component)
	if !strings.Contains(output, "!include <C4/C4_Component>") {
		t.Error("expected output to contain C4_Component include")
	}
	pumlNodeInOutput(t, output, "comp1")
}

func TestRenderPlantUMLC4Landscape(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Children: []*types.LayoutTree{
			{
				BoundaryName: "System1",
				Nodes: []*types.LayoutNode{
					{ID: "ns1", Name: "Namespace", Kind: "gm:Namespace"},
					{ID: "ext1", Name: "ExternalSys", Kind: "gm:ExternalSystem"},
				},
			},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.C4Landscape)
	if !strings.Contains(output, "!include <C4/C4_Context>") {
		t.Error("expected output to contain C4_Context include")
	}
	pumlNodeInOutput(t, output, "ext1")
}

func TestRenderPlantUMLC4Dynamic(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "a", Name: "ServiceA", Kind: "gm:Executable"},
		},
		Edges: []types.LayoutEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.C4Dynamic)
	if !strings.Contains(output, "title") {
		t.Error("expected output to contain title")
	}
	pumlNodeInOutput(t, output, "a")
}

func TestRenderPlantUMLComponentDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "comp1", Name: "DB", Kind: "gm:Database", PrimitiveType: "DATABASE"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.UMLComponent)
	if !strings.Contains(output, "database") {
		t.Error("expected output to contain 'database'")
	}
	pumlNodeInOutput(t, output, "comp1")
}

func TestRenderPlantUMLObject(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "obj1", Name: "Obj", Kind: "gm:TypeDecl"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.UMLObject)
	if !strings.Contains(output, "@startuml") {
		t.Error("expected output to contain '@startuml'")
	}
	pumlNodeInOutput(t, output, "obj1")
}

func TestRenderPlantUMLProfile(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "prof1", Name: "ProfileType", Kind: "gm:TypeDecl"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.UMLProfile)
	if !strings.Contains(output, "@startuml") {
		t.Error("expected output to contain '@startuml'")
	}
	pumlNodeInOutput(t, output, "prof1")
}

func TestRenderPlantUMLDeployment(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Children: []*types.LayoutTree{
			{
				BoundaryName: "Node",
				Nodes: []*types.LayoutNode{
					{ID: "dep1", Name: "Deployment", Kind: "gm:Executable"},
				},
			},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.UMLDeployment)
	if !strings.Contains(output, "package") {
		t.Error("expected output to contain 'package'")
	}
	pumlNodeInOutput(t, output, "dep1")
}

func TestRenderPlantUMLPackage(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "pkg1", Name: "Pkg1", Kind: "gm:Namespace"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.UMLPackage)
	if !strings.Contains(output, "@startuml") {
		t.Error("expected output to contain '@startuml'")
	}
	pumlNodeInOutput(t, output, "pkg1")
}

func TestRenderPlantUMLComposite(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "comp1", Name: "Comp1", Kind: "gm:TypeDecl"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.UMLComposite)
	if !strings.Contains(output, "@startuml") {
		t.Error("expected output to contain '@startuml'")
	}
	pumlNodeInOutput(t, output, "comp1")
}

func TestRenderPlantUMLSequence(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Edges: []types.LayoutEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.UMLSequence)
	if !strings.Contains(output, "@startuml") {
		t.Error("expected output to contain '@startuml'")
	}
	if !strings.Contains(output, "a") && !strings.Contains(output, "b") {
		t.Error("expected participants a and b in sequence diagram")
	}
}

func TestRenderPlantUMLState(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "s1", Name: "State1", Kind: "gm:Executable"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.UMLState)
	if !strings.Contains(output, "@startuml") {
		t.Error("expected output to contain '@startuml'")
	}
	pumlNodeInOutput(t, output, "s1")
}

func TestRenderPlantUMLActivity(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "act1", Name: "Action1", Kind: "gm:Executable"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.UMLActivity)
	if !strings.Contains(output, "@startuml") {
		t.Error("expected output to contain '@startuml'")
	}
	pumlNodeInOutput(t, output, "act1")
}

func TestRenderPlantUMLInfrastructure(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "db1", Name: "DB", Kind: "gm:Database", PrimitiveType: "DATABASE"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.Infrastructure)
	if !strings.Contains(output, "@startuml") {
		t.Error("expected output to contain '@startuml'")
	}
	pumlNodeInOutput(t, output, "db1")
}

func TestRenderPlantUMLUsecase(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "uc1", Name: "Login", Kind: "gm:Executable"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.UMLUsecase)
	if !strings.Contains(output, "@startuml") {
		t.Error("expected output to contain '@startuml'")
	}
	if !strings.Contains(output, "@enduml") {
		t.Error("expected output to contain '@enduml'")
	}
	pumlNodeInOutput(t, output, "uc1")
}

func TestRenderPlantUMLCommunication(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "comm1", Name: "Sender", Kind: "gm:Executable"},
		},
		Edges: []types.LayoutEdge{
			{SourceID: "comm1", TargetID: "comm2", Predicate: "gm:calls"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.UMLCommunication)
	if !strings.Contains(output, "@startuml") {
		t.Error("expected output to contain '@startuml'")
	}
	if !strings.Contains(output, "@enduml") {
		t.Error("expected output to contain '@enduml'")
	}
	pumlNodeInOutput(t, output, "comm1")
}

func TestRenderPlantUMLInteractionOverview(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "io1", Name: "Interaction1", Kind: "gm:Executable"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.UMLInteractionOverview)
	if !strings.Contains(output, "@startuml") {
		t.Error("expected output to contain '@startuml'")
	}
	if !strings.Contains(output, "@enduml") {
		t.Error("expected output to contain '@enduml'")
	}
	pumlNodeInOutput(t, output, "io1")
}

func TestRenderPlantUMLTiming(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "t1", Name: "Signal", Kind: "gm:Executable"},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.UMLTiming)
	if !strings.Contains(output, "@startuml") {
		t.Error("expected output to contain '@startuml'")
	}
	if !strings.Contains(output, "@enduml") {
		t.Error("expected output to contain '@enduml'")
	}
	pumlNodeInOutput(t, output, "t1")
}

// TestRenderPlantUMLC4Deployment — see above.

// TestRenderPlantUMLAllDiagramTypes (GAP-L-03): every one of the 31
// DiagramType values must render well-formed PlantUML (open/close tags
// present) with no Mermaid-style fallback leaking through.
func TestRenderPlantUMLAllDiagramTypes(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "main.go::A", Name: "A", Kind: "gm:TypeDecl"},
			{ID: "main.go::B", Name: "B", Kind: "gm:Executable"},
		},
		Edges: []types.LayoutEdge{
			{SourceID: "main.go::A", TargetID: "main.go::B", Predicate: "gm:extends"},
		},
	}

	all := []types.DiagramType{
		types.UMLClass, types.UMLObject, types.UMLProfile, types.C4Code,
		types.UMLComponent, types.UMLComposite, types.UMLPackage, types.UMLDeployment,
		types.C4Context, types.C4Container, types.C4Component, types.C4Landscape,
		types.C4Dynamic, types.C4Deployment,
		types.UMLUsecase, types.UMLActivity, types.UMLState, types.UMLSequence,
		types.UMLCommunication, types.UMLInteractionOverview, types.UMLTiming,
		types.ERDiagram, types.DataFlow, types.Mindmap, types.Flowchart,
		types.DependencyGraph, types.HotspotComplexity, types.CallGraph,
		types.LayeredArchitecture, types.ChangeImpact, types.Infrastructure,
	}
	if len(all) != 31 {
		t.Fatalf("DiagramType count drifted: got %d, want 31", len(all))
	}

	for _, dt := range all {
		out := RenderPlantUMLDiagram(tree, dt)
		if !strings.Contains(out, "@startuml") {
			t.Errorf("%s: missing @startuml\n%s", dt, out)
		}
		if !strings.Contains(out, "@enduml") {
			t.Errorf("%s: missing @enduml\n%s", dt, out)
		}
		if !strings.Contains(out, "main.go") && !strings.Contains(out, sanitizeForTest("main.go::A")) {
			t.Errorf("%s: no node content rendered\n%s", dt, out)
		}
	}
}

func TestRenderPlantUMLC4Deployment(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Children: []*types.LayoutTree{
			{
				BoundaryName: "Production",
				Nodes: []*types.LayoutNode{
					{ID: "ns1", Name: "ProdNS", Kind: "gm:Namespace"},
					{ID: "db1", Name: "DB", Kind: "gm:Database", PrimitiveType: "DATABASE"},
				},
			},
		},
	}
	output := RenderPlantUMLDiagram(tree, types.C4Deployment)
	if !strings.Contains(output, "!include <C4/C4_Deployment>") {
		t.Error("expected output to contain C4_Deployment include")
	}
	pumlNodeInOutput(t, output, "db1")
	pumlNodeInOutput(t, output, "ns1")
}
