package aggregate

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// richTree exercises every renderer with nodes whose sanitized names collide,
// nested boundaries, and every arrow style the renderers emit.
func richTree() *types.LayoutTree {
	return &types.LayoutTree{
		BoundaryName: "Root",
		Summary: &types.GraphSummary{
			NodeCount: 8, EdgeCount: 9, Density: 0.5, Diameter: 3,
			AvgPathLength: 1.5, ClusterCount: 2, LargestSCCSize: 2,
			GodObjectCount: 1, ConnectedComponents: 1,
		},
		Children: []*types.LayoutTree{
			{
				BoundaryName: "internal/api",
				Nodes: []*types.LayoutNode{
					{ID: "internal/api/handler.go::HandleRequest", Name: "HandleRequest", Kind: "gm:Executable"},
					{ID: "internal/api/model.go::UserService", Name: "UserService", Kind: "gm:Struct"},
					{ID: "internal/api/model.go::Handler", Name: "Handler", Kind: "gm:Class"},
					{ID: "internal/api/service.go::Client", Name: "Client", Kind: "gm:TypeDecl"},
					{ID: "internal/api/db.go::ConnPool", Name: "ConnPool", Kind: "gm:TypeDecl", PrimitiveType: "DATABASE"},
					{ID: "internal/api/cache.go::Session", Name: "Session", Kind: "gm:TypeDecl", PrimitiveType: "CACHE"},
				},
			},
			{
				BoundaryName: "cmd/app",
				Nodes: []*types.LayoutNode{
					{ID: "cmd/app/worker-service.go::Worker", Name: "Worker", Kind: "gm:Executable"},
					{ID: "cmd/app/worker_service.go::Worker", Name: "Worker", Kind: "gm:Function"},
					{ID: "cmd/app/main.go::Main", Name: "Main", Kind: "gm:Executable", IsEntrypoint: true},
				},
			},
		},
		Edges: []types.LayoutEdge{
			{SourceID: "cmd/app/main.go::Main", TargetID: "internal/api/handler.go::HandleRequest", Predicate: "gm:calls", LineNumber: 1},
			{SourceID: "internal/api/handler.go::HandleRequest", TargetID: "internal/api/model.go::UserService", Predicate: "gm:calls", LineNumber: 2},
			{SourceID: "internal/api/model.go::Handler", TargetID: "internal/api/model.go::UserService", Predicate: "gm:extends", LineNumber: 3},
			{SourceID: "internal/api/model.go::Handler", TargetID: "internal/api/service.go::Client", Predicate: "gm:hasMember", LineNumber: 4},
			{SourceID: "internal/api/service.go::Client", TargetID: "internal/api/db.go::ConnPool", Predicate: "gm:connectsTo", LineNumber: 5},
			{SourceID: "internal/api/service.go::Client", TargetID: "internal/api/cache.go::Session", Predicate: "gm:mutatesGlobal", LineNumber: 6},
			{SourceID: "cmd/app/worker-service.go::Worker", TargetID: "cmd/app/worker_service.go::Worker", Predicate: "gm:dispatchesEvent", LineNumber: 7},
			{SourceID: "internal/api/handler.go::HandleRequest", TargetID: "cmd/app/main.go::Main", Predicate: "gm:calls", LineNumber: 8, IsCycle: true},
			{SourceID: "internal/api/model.go::Handler", TargetID: "internal/api/cache.go::Session", Predicate: "gm:mixes", LineNumber: 10},
			{SourceID: "internal/api/model.go::Handler", TargetID: "internal/api/service.go::Client", Predicate: "gm:diInjects", LineNumber: 11},
		},
	}
}

var validMermaidHeaders = []string{
	"classDiagram", "flowchart", "graph", "sequenceDiagram",
	"stateDiagram-v2", "timeline", "erDiagram", "mindmap",
	"C4Context", "C4Container", "C4Component", "C4Deployment",
}

func TestGoldenMermaidSyntaxChecker(t *testing.T) {
	tree := richTree()
	for _, dt := range []types.DiagramType{
		types.UMLClass, types.UMLObject, types.UMLComponent, types.UMLDeployment,
		types.UMLPackage, types.UMLComposite, types.UMLProfile, types.UMLUsecase,
		types.UMLActivity, types.UMLState, types.UMLSequence, types.UMLCommunication,
		types.UMLInteractionOverview, types.UMLTiming, types.C4Context, types.C4Container,
		types.C4Component, types.C4Code, types.C4Landscape, types.C4Dynamic,
		types.C4Deployment, types.ERDiagram, types.DataFlow, types.Mindmap,
		types.Flowchart, types.DependencyGraph, types.HotspotComplexity, types.CallGraph,
		types.LayeredArchitecture, types.ChangeImpact, types.Infrastructure,
	} {
		name := string(dt)
		output := RenderDiagram(tree, dt)
		if strings.TrimSpace(output) == "" {
			t.Errorf("%s: renderer produced empty output", name)
			continue
		}
		firstLine := strings.TrimSpace(strings.SplitN(output, "\n", 2)[0])
		headerOK := false
		for _, h := range validMermaidHeaders {
			if strings.HasPrefix(firstLine, h) {
				headerOK = true
				break
			}
		}
		if !headerOK {
			t.Errorf("%s: unrecognized mermaid header %q", name, firstLine)
		}
		for i, raw := range strings.Split(output, "\n") {
			line := strings.TrimSpace(raw)
			if line == "" {
				continue
			}
			if strings.Count(line, "\"")%2 != 0 {
				t.Errorf("%s: line %d has unbalanced quotes: %q", name, i+1, line)
			}
			if strings.Contains(line, "|") {
				arrowMarkers := []string{"-.->|", "==>|", "-->|", "x--x|", "===", "=--"}
				for _, m := range arrowMarkers {
					if strings.Contains(line, m) && strings.HasSuffix(line, "|") {
						t.Errorf("%s: line %d has unterminated arrow text marker: %q", name, i+1, line)
					}
				}
			}
			if dt == types.Mindmap && strings.HasPrefix(line, "%") {
				t.Errorf("mindmap line %d: comments are not valid in mindmap: %q", i+1, line)
			}
		}
	}
}

func TestGoldenClassDiagramRelations(t *testing.T) {
	output := RenderDiagram(richTree(), types.UMLClass)
	for _, want := range []string{"classDiagram", ": extends", ": mixes", ": has", "UserService", "Handler"} {
		if !strings.Contains(output, want) {
			t.Errorf("class diagram missing %q", want)
		}
	}
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, marker := range []string{"--|>", "..|>", "--*", "--o"} {
			if strings.Contains(trimmed, marker) && !strings.Contains(trimmed, ": ") {
				t.Errorf("relation line missing label: %q", line)
			}
		}
	}
}

func TestGoldenDataFlowArrow(t *testing.T) {
	output := RenderDiagram(richTree(), types.DataFlow)
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "mutatesGlobal") {
			if !strings.Contains(line, "==>|") || strings.HasSuffix(strings.TrimSpace(line), "|") {
				t.Errorf("taint/mutation edge must use closed ==>|label| arrow: %q", line)
			}
		}
	}
}

func TestGoldenAliasCollisionResolution(t *testing.T) {
	// "cmd/app/worker-service.go::Worker" and "cmd/app/worker_service.go::Worker"
	// sanitize to the same base; the registry must disambiguate them.
	output := RenderDiagram(richTree(), types.CallGraph)
	base := sanitizeName("cmd/app/worker-service.go::Worker")
	suffixed := base + "_1"
	if !strings.Contains(output, base) {
		t.Errorf("expected first colliding node with base alias %q:\n%s", base, output)
	}
	if !strings.Contains(output, suffixed) {
		t.Errorf("expected numeric suffix on second colliding alias (base %q):\n%s", base, output)
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Worker") && strings.Contains(line, "-->") &&
			!strings.Contains(line, base) && !strings.Contains(line, suffixed) {
			t.Errorf("edge must reference a registered alias, got %q", line)
		}
	}
}
