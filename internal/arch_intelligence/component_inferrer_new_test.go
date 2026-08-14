package arch_intelligence

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/config"
)

var testClock = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

// testNode is a helper that creates a node with a file path.
func testNode(id, path string) *link.ResolvedNode {
	return &link.ResolvedNode{
		ID:   id,
		Name: id,
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path: path,
		},
	}
}

// addNodeWithPath inserts a node and returns the graph.
func addNodeWithPath(graph *akg.CodePropertyGraph, id, path string) *akg.CodePropertyGraph {
	graph.Nodes = graph.Nodes.Set(id, testNode(id, path))
	return graph
}

// addStructuralEdge registers an outbound+inbound structural edge.
func addStructuralEdge(graph *akg.CodePropertyGraph, src, tgt string, typ link.RelationshipType) *akg.CodePropertyGraph {
	edge := link.ResolvedEdge{SourceID: src, TargetID: tgt, Type: typ}
	edges, _ := graph.OutboundEdges.Get(src)
	edges = append(edges, edge)
	graph.OutboundEdges = graph.OutboundEdges.Set(src, edges)
	inb, _ := graph.InboundEdges.Get(tgt)
	inb = append(inb, edge)
	graph.InboundEdges = graph.InboundEdges.Set(tgt, inb)
	return graph
}

func TestInferComponentsFromSnapshot_StableIDs(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	for i := 0; i < 4; i++ {
		addNodeWithPath(graph, "api"+string(rune('a'+i)), "internal/api/file.go")
	}
	addNodeWithPath(graph, "db1", "internal/db/db.go")
	addNodeWithPath(graph, "db2", "internal/db/db2.go")

	components := InferComponentsFromSnapshot(NewGraphSnapshot(graph), nil, testClock)
	if len(components) != 2 {
		t.Fatalf("expected 2 components, got %d: %+v", len(components), components)
	}
	ids := map[string]bool{}
	for _, c := range components {
		ids[c.ID] = true
		for _, dir := range c.Directories {
			if dir != "internal/api" && dir != "internal/db" {
				t.Errorf("unexpected directory %q", dir)
			}
		}
	}
	if !ids["comp_internal_api"] || !ids["comp_internal_db"] {
		t.Errorf("expected comp_internal_api and comp_internal_db, got %v", ids)
	}
}

func TestInferComponentsFromSnapshot_MergeSmall(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	// Single-file subdirectory should merge into its parent.
	addNodeWithPath(graph, "h1", "internal/api/handler/h.go")
	for i := 0; i < 3; i++ {
		addNodeWithPath(graph, "a"+string(rune('a'+i)), "internal/api/file.go")
	}
	components := InferComponentsFromSnapshot(NewGraphSnapshot(graph), nil, testClock)
	if len(components) != 1 {
		t.Fatalf("expected 1 merged component, got %d", len(components))
	}
	if components[0].ID != "comp_internal_api" {
		t.Errorf("expected comp_internal_api, got %q", components[0].ID)
	}
	found := false
	for _, id := range components[0].NodeIDs {
		if id == "h1" {
			found = true
		}
	}
	if !found {
		t.Error("handler node was not merged into the parent component")
	}
}

func TestInferComponentsFromSnapshot_Excluded(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	addNodeWithPath(graph, "v1", "internal/vendor/lib.go")
	addNodeWithPath(graph, "v2", "internal/vendor/lib2.go")
	addNodeWithPath(graph, "app1", "internal/app/app.go")
	addNodeWithPath(graph, "app2", "internal/app/app2.go")

	cfg := &config.IntelligenceConfig{ArchExcludedDirs: []string{"internal/vendor"}}
	components := InferComponentsFromSnapshot(NewGraphSnapshot(graph), cfg, testClock)
	if len(components) != 1 {
		t.Fatalf("expected 1 component (vendor excluded), got %d", len(components))
	}
	if components[0].ID != "comp_internal_app" {
		t.Errorf("expected comp_internal_app, got %q", components[0].ID)
	}
}

func TestInferComponentsFromSnapshot_Dependencies(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	addNodeWithPath(graph, "a1", "internal/a/x.go")
	addNodeWithPath(graph, "a2", "internal/a/y.go")
	addNodeWithPath(graph, "b1", "internal/b/x.go")
	addNodeWithPath(graph, "b2", "internal/b/y.go")
	addStructuralEdge(graph, "a1", "b1", link.EdgeCalls)

	components := InferComponentsFromSnapshot(NewGraphSnapshot(graph), nil, testClock)
	byID := map[string]archmodel.DetectedComponent{}
	for _, c := range components {
		byID[c.ID] = c
	}
	compA, okA := byID["comp_internal_a"]
	compB, okB := byID["comp_internal_b"]
	if !okA || !okB {
		t.Fatalf("expected comp_internal_a and comp_internal_b, got %v", byID)
	}
	if len(compA.Dependencies) != 1 || compA.Dependencies[0] != "comp_internal_b" {
		t.Errorf("comp_a should depend on comp_b, got %v", compA.Dependencies)
	}
	if len(compB.Dependencies) != 0 {
		t.Errorf("comp_b should have no dependencies, got %v", compB.Dependencies)
	}
}

func TestInferComponents_Compat(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	addNodeWithPath(graph, "n1", "internal/service/foo.go")
	components := InferComponents(graph)
	if len(components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(components))
	}
}
