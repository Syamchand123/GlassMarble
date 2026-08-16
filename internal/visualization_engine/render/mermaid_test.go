package aggregate

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func nodeInOutput(t *testing.T, output, nodeID string) {
	t.Helper()
	sanitized := strings.ReplaceAll(nodeID, "::", "_")
	sanitized = strings.ReplaceAll(sanitized, ".", "_")
	sanitized = strings.ReplaceAll(sanitized, ":", "_")
	if !strings.Contains(output, nodeID) && !strings.Contains(output, sanitized) {
		t.Errorf("expected node %q (or sanitized %q) to appear in output", nodeID, sanitized)
	}
}

func TestRenderClassDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "a.go::Parent", Name: "Parent", Kind: "gm:TypeDecl"},
			{ID: "a.go::Child", Name: "Child", Kind: "gm:TypeDecl"},
		},
		Edges: []types.LayoutEdge{
			{SourceID: "a.go::Child", TargetID: "a.go::Parent", Predicate: "gm:inheritsFrom"},
		},
	}
	output := RenderDiagram(tree, types.UMLClass)
	if !strings.Contains(output, "classDiagram") {
		t.Error("expected output to contain 'classDiagram'")
	}
	if !strings.Contains(output, "Parent") {
		t.Error("expected output to contain class name 'Parent'")
	}
	if !strings.Contains(output, "Child") {
		t.Error("expected output to contain class name 'Child'")
	}
	if !strings.Contains(output, "inherits") {
		t.Error("expected output to contain inheritance relationship")
	}
	nodeInOutput(t, output, "a.go::Parent")
	nodeInOutput(t, output, "a.go::Child")
}

func TestRenderObjectDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "main.go::Obj", Name: "Obj", Kind: "gm:TypeDecl"},
		},
	}
	output := RenderDiagram(tree, types.UMLObject)
	if !strings.Contains(output, "classDiagram") {
		t.Error("expected output to contain 'classDiagram'")
	}
	nodeInOutput(t, output, "main.go::Obj")
}

func TestRenderC4ContextDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "user1", Name: "User", Kind: "gm:User"},
			{ID: "ext1", Name: "ExternalSys", Kind: "gm:ExternalSystem"},
		},
		Edges: []types.LayoutEdge{
			{SourceID: "user1", TargetID: "ext1", Predicate: "gm:calls"},
		},
	}
	output := RenderDiagram(tree, types.C4Context)
	if !strings.Contains(output, "C4Context") {
		t.Error("expected output to contain 'C4Context'")
	}
	nodeInOutput(t, output, "user1")
	nodeInOutput(t, output, "ext1")
	if !strings.Contains(output, "user1") && !strings.Contains(output, "ext1") {
		t.Error("expected nodes to appear in C4Context output")
	}
}

func TestRenderC4ContainerDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Children: []*types.LayoutTree{
			{
				BoundaryName: "App",
				Nodes: []*types.LayoutNode{
					{ID: "db1", Name: "Postgres", Kind: "gm:Database", PrimitiveType: "DATABASE"},
				},
			},
		},
	}
	output := RenderDiagram(tree, types.C4Container)
	if !strings.Contains(output, "C4Container") {
		t.Error("expected output to contain 'C4Container'")
	}
	nodeInOutput(t, output, "db1")
}

func TestRenderC4ComponentDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "comp1", Name: "UserService", Kind: "gm:TypeDecl"},
		},
	}
	output := RenderDiagram(tree, types.C4Component)
	if !strings.Contains(output, "C4Component") {
		t.Error("expected output to contain 'C4Component'")
	}
	if !strings.Contains(output, "title") {
		t.Error("expected output to contain title")
	}
	nodeInOutput(t, output, "comp1")
}

func TestRenderC4LandscapeDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
	}
	output := RenderDiagram(tree, types.C4Landscape)
	if !strings.Contains(output, "C4Context") {
		t.Error("expected output to contain 'C4Context'")
	}
}

func TestRenderC4DynamicDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "a", Name: "ServiceA", Kind: "gm:Executable"},
		},
		Edges: []types.LayoutEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
		},
	}
	output := RenderDiagram(tree, types.C4Dynamic)
	if !strings.Contains(output, "sequenceDiagram") {
		t.Error("expected output to contain 'sequenceDiagram'")
	}
	nodeInOutput(t, output, "a")
}

func TestRenderC4DeploymentDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Children: []*types.LayoutTree{
			{
				BoundaryName: "Production",
				Nodes: []*types.LayoutNode{
					{ID: "db", Name: "DB", Kind: "gm:Database", PrimitiveType: "DATABASE"},
					{ID: "ns1", Name: "ProdNS", Kind: "gm:Namespace"},
				},
			},
		},
	}
	output := RenderDiagram(tree, types.C4Deployment)
	if !strings.Contains(output, "C4Deployment") {
		t.Error("expected output to contain 'C4Deployment'")
	}
	nodeInOutput(t, output, "db")
	nodeInOutput(t, output, "ns1")
}

func TestRenderSequenceDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Edges: []types.LayoutEdge{
			{SourceID: "main.go::Main", TargetID: "utils.go::Helper", Predicate: "gm:calls", LineNumber: 1},
		},
	}
	output := RenderDiagram(tree, types.UMLSequence)
	if !strings.Contains(output, "sequenceDiagram") {
		t.Error("expected output to contain 'sequenceDiagram'")
	}
	if !strings.Contains(output, "main.go::Main") && !strings.Contains(output, "Main") {
		t.Error("expected participant Main in sequence diagram")
	}
}

func TestRenderActivityDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "step1", Name: "Step1", Kind: "gm:Executable"},
		},
	}
	output := RenderDiagram(tree, types.UMLActivity)
	if !strings.Contains(output, "flowchart") {
		t.Error("expected output to contain 'flowchart'")
	}
	nodeInOutput(t, output, "step1")
}

func TestRenderStateDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "state1", Name: "Idle", Kind: "gm:Executable"},
		},
	}
	output := RenderDiagram(tree, types.UMLState)
	if !strings.Contains(output, "stateDiagram") {
		t.Error("expected output to contain 'stateDiagram'")
	}
	nodeInOutput(t, output, "state1")
}

func TestRenderDataFlowDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "var1", Name: "x", Kind: "gm:Variable"},
		},
	}
	output := RenderDiagram(tree, types.DataFlow)
	if !strings.Contains(output, "flowchart") {
		t.Error("expected output to contain 'flowchart'")
	}
	nodeInOutput(t, output, "var1")
}

func TestRenderERDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "entity1", Name: "User", Kind: "gm:TypeDecl"},
		},
	}
	output := RenderDiagram(tree, types.ERDiagram)
	if !strings.Contains(output, "erDiagram") {
		t.Error("expected output to contain 'erDiagram'")
	}
	nodeInOutput(t, output, "entity1")
}

func TestRenderMindmapDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "mod1", Name: "Module1", Kind: "gm:Namespace"},
		},
	}
	output := RenderDiagram(tree, types.Mindmap)
	if !strings.Contains(output, "mindmap") {
		t.Error("expected output to contain 'mindmap'")
	}
	nodeInOutput(t, output, "mod1")
}

func TestRenderInfrastructureDiagram(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Children: []*types.LayoutTree{
			{
				BoundaryName: "DataLayer",
				Nodes: []*types.LayoutNode{
					{ID: "ns1", Name: "Internal", Kind: "gm:Namespace"},
					{ID: "db1", Name: "Redis", Kind: "gm:Database", PrimitiveType: "DATABASE"},
				},
			},
		},
	}
	output := RenderDiagram(tree, types.Infrastructure)
	if !strings.Contains(output, "C4Context") {
		t.Error("expected output to contain 'C4Context'")
	}
	nodeInOutput(t, output, "db1")
	nodeInOutput(t, output, "ns1")
}

func TestRenderDependencyGraph(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "mod1", Name: "ModuleA", Kind: "gm:Namespace"},
		},
	}
	output := RenderDiagram(tree, types.DependencyGraph)
	if !strings.Contains(output, "flowchart") {
		t.Error("expected output to contain 'flowchart'")
	}
	nodeInOutput(t, output, "mod1")
}

func TestRenderHotspotComplexity(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "hot1", Name: "HotFunc", Kind: "gm:Executable"},
		},
	}
	output := RenderDiagram(tree, types.HotspotComplexity)
	if !strings.Contains(output, "flowchart") {
		t.Error("expected output to contain 'flowchart'")
	}
	nodeInOutput(t, output, "hot1")
}

func TestRenderCallGraph(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "fn1", Name: "Func1", Kind: "gm:Executable"},
		},
		Edges: []types.LayoutEdge{
			{SourceID: "fn1", TargetID: "fn2", Predicate: "gm:calls"},
		},
	}
	output := RenderDiagram(tree, types.CallGraph)
	if !strings.Contains(output, "flowchart") {
		t.Error("expected output to contain 'flowchart'")
	}
	nodeInOutput(t, output, "fn1")
	if !strings.Contains(output, "fn1") && !strings.Contains(output, "fn2") {
		t.Error("expected call graph nodes to appear")
	}
}

func TestRenderLayeredArchitecture(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "handler1", Name: "Handler", Kind: "gm:Executable", PrimitiveType: "HTTP"},
		},
	}
	output := RenderDiagram(tree, types.LayeredArchitecture)
	if !strings.Contains(output, "flowchart") {
		t.Error("expected output to contain 'flowchart'")
	}
	nodeInOutput(t, output, "handler1")
}

func TestRenderFlowchart(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "step1", Name: "Step1", Kind: "gm:Executable"},
		},
	}
	output := RenderDiagram(tree, types.Flowchart)
	if !strings.Contains(output, "flowchart") {
		t.Error("expected output to contain 'flowchart'")
	}
	nodeInOutput(t, output, "step1")
}

func TestRenderFlowchartFallback(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "fb1", Name: "FallbackNode", Kind: "gm:Executable"},
		},
		Summary: &types.GraphSummary{NodeCount: 1, EdgeCount: 0},
	}
	output := RenderDiagram(tree, types.DiagramType("UNKNOWN"))
	if !strings.Contains(output, "flowchart") {
		t.Error("expected fallback output to contain 'flowchart'")
	}
	nodeInOutput(t, output, "fb1")
	if !strings.Contains(output, "Graph Summary:") {
		t.Error("expected summary footer in fallback output")
	}
	t.Log("output contains summary footer for fallback")
}

func TestRenderChangeImpact(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "impact1", Name: "Affected", Kind: "gm:Executable"},
		},
	}
	output := RenderDiagram(tree, types.ChangeImpact)
	if !strings.Contains(output, "flowchart") {
		t.Error("expected output to contain 'flowchart'")
	}
	nodeInOutput(t, output, "impact1")
}

func TestRenderEdgeStyles(t *testing.T) {
	var sb strings.Builder
	edge := types.LayoutEdge{SourceID: "a", TargetID: "b", Predicate: "gm:calls"}
	renderEdgeStyles(edge, "src_a", "tgt_b", &sb)
	output := sb.String()
	if !strings.Contains(output, "src_a") {
		t.Error("expected source ID in default edge output")
	}
	if !strings.Contains(output, "tgt_b") {
		t.Error("expected target ID in default edge output")
	}
}

func TestRenderEdgeStyleCycle(t *testing.T) {
	var sb strings.Builder
	edge := types.LayoutEdge{SourceID: "a", TargetID: "b", Predicate: "gm:calls", IsCycle: true}
	renderEdgeStyles(edge, "src_a", "tgt_b", &sb)
	output := sb.String()
	if !strings.Contains(output, "«CYCLE»") {
		t.Error("expected «CYCLE» marker for cycle edges")
	}
}

func TestRenderEdgeStyleVulnerable(t *testing.T) {
	var sb strings.Builder
	edge := types.LayoutEdge{SourceID: "a", TargetID: "b", Predicate: "gm:vulnerableTaint"}
	renderEdgeStyles(edge, "src_a", "tgt_b", &sb)
	output := sb.String()
	if !strings.Contains(output, "==>") {
		t.Error("expected thick arrow for vulnerable taint")
	}
}

func TestRenderEdgeStyleAlias(t *testing.T) {
	var sb strings.Builder
	edge := types.LayoutEdge{SourceID: "a", TargetID: "b", Predicate: "gm:pointsTo"}
	renderEdgeStyles(edge, "src_a", "tgt_b", &sb)
	output := sb.String()
	if !strings.Contains(output, "alias") {
		t.Error("expected alias label for pointsTo")
	}
}

func TestRenderEdgeStyleFFI(t *testing.T) {
	var sb strings.Builder
	edge := types.LayoutEdge{SourceID: "a", TargetID: "b", Predicate: "gm:ffiCall"}
	renderEdgeStyles(edge, "src_a", "tgt_b", &sb)
	output := sb.String()
	if !strings.Contains(output, "FFI") {
		t.Error("expected FFI label for ffiCall")
	}
}

func TestRenderEdgeStyleInjects(t *testing.T) {
	var sb strings.Builder
	edge := types.LayoutEdge{SourceID: "a", TargetID: "b", Predicate: "gm:diInjects"}
	renderEdgeStyles(edge, "src_a", "tgt_b", &sb)
	output := sb.String()
	if !strings.Contains(output, "injects") {
		t.Error("expected injects label for diInjects")
	}
}
