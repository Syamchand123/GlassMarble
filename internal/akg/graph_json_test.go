package akg

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// buildExportTestGraph builds a small graph exercising nodes, edges,
// entrypoints, folder zones and metadata.
func buildExportTestGraph() *CodePropertyGraph {
	g := NewCodePropertyGraph("abcdef")
	g.Version = 42
	g.SchemaVersion = CurrentSchemaVersion
	g.Entrypoints = []string{"a::main"}
	g.Nodes = g.Nodes.Set("a::main", &link.ResolvedNode{
		ID:         "a::main",
		Kind:       "FUNCTION",
		Name:       "main",
		Primitive:  "COMPUTE",
		FileSpec:   link.LocationMeta{Path: "cmd/app/main.go", LineStart: 5, LineEnd: 9},
		Properties: map[string]string{"metric": "fan_in=3"},
	})
	g.Nodes = g.Nodes.Set("b::svc", &link.ResolvedNode{
		ID:       "b::svc",
		Kind:     "MODULE",
		Name:     "svc",
		FileSpec: link.LocationMeta{Path: "internal/svc/svc.go", LineStart: 1},
	})
	addEdgeToGraph(g, "a::main", "b::svc", link.EdgeCalls, 7)
	addEdgeToGraph(g, "a::main", "b::svc", link.EdgeCalls, 8) // parallel edge with distinct line
	// Edge property facts (gm:provenance / gm:embedding, W1-11/W1-14).
	g.OutboundEdges = g.OutboundEdges.Set("a::main", []link.ResolvedEdge{
		{SourceID: "a::main", TargetID: "b::svc", Type: link.EdgeCalls, LineNumber: 7,
			Properties: map[string]string{"provenance": "import-resolved", "embedding": "0.91"}},
		{SourceID: "a::main", TargetID: "b::svc", Type: link.EdgeCalls, LineNumber: 8},
	})
	g.InboundEdges = g.InboundEdges.Set("b::svc", []link.ResolvedEdge{
		{SourceID: "a::main", TargetID: "b::svc", Type: link.EdgeCalls, LineNumber: 7,
			Properties: map[string]string{"provenance": "import-resolved", "embedding": "0.91"}},
		{SourceID: "a::main", TargetID: "b::svc", Type: link.EdgeCalls, LineNumber: 8},
	})
	g.FolderZones = g.FolderZones.Set("b::svc", "SERVICE_ZONE")
	return g
}

func addEdgeToGraph(g *CodePropertyGraph, src, tgt string, typ link.RelationshipType, line int) {
	edge := link.ResolvedEdge{SourceID: src, TargetID: tgt, Type: typ, LineNumber: line}
	out, _ := g.OutboundEdges.Get(src)
	g.OutboundEdges = g.OutboundEdges.Set(src, append(out, edge))
	in, _ := g.InboundEdges.Get(tgt)
	g.InboundEdges = g.InboundEdges.Set(tgt, append(in, edge))
}

// TestGraphJSONRoundTrip verifies export->import reproduces the graph exactly.
func TestGraphJSONRoundTrip(t *testing.T) {
	g := buildExportTestGraph()

	var buf bytes.Buffer
	if err := ExportGraphJSON(g, &buf); err != nil {
		t.Fatalf("export failed: %v", err)
	}

	restored, err := ImportGraphJSON(&buf)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}

	if restored.CommitHash != "abcdef" {
		t.Errorf("CommitHash = %q, want abcdef", restored.CommitHash)
	}
	if restored.Version != 42 {
		t.Errorf("Version = %d, want 42", restored.Version)
	}
	if restored.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", restored.SchemaVersion, CurrentSchemaVersion)
	}

	if restored.Nodes.Len() != 2 {
		t.Fatalf("node count = %d, want 2", restored.Nodes.Len())
	}
	main, ok := restored.Nodes.Get("a::main")
	if !ok {
		t.Fatal("a::main missing after import")
	}
	if main.Primitive != "COMPUTE" || main.FileSpec.Path != "cmd/app/main.go" {
		t.Errorf("main node lost detail: %+v", main)
	}
	if main.Properties["metric"] != "fan_in=3" {
		t.Errorf("main properties lost: %v", main.Properties)
	}

	// Parallel edges must survive (GraphJSON is lossless).
	edges := restored.GetOutboundEdges("a::main")
	if len(edges) != 2 {
		t.Fatalf("parallel edges lost: got %d, want 2", len(edges))
	}

	// Edge properties must survive the round trip (GraphEdgeJSON.Properties).
	var withProps *link.ResolvedEdge
	for i := range edges {
		if len(edges[i].Properties) > 0 {
			withProps = &edges[i]
			break
		}
	}
	if withProps == nil {
		t.Fatal("edge properties lost after import")
	}
	if withProps.Properties["provenance"] != "import-resolved" || withProps.Properties["embedding"] != "0.91" {
		t.Errorf("edge properties mismatch: %v", withProps.Properties)
	}

	zone, _ := restored.FolderZones.Get("b::svc")
	if zone != "SERVICE_ZONE" {
		t.Errorf("FolderZones lost: got %q", zone)
	}
}

// TestGraphJSONDeterministic verifies repeated exports are byte-identical.
func TestGraphJSONDeterministic(t *testing.T) {
	g := buildExportTestGraph()

	var b1, b2 bytes.Buffer
	if err := ExportGraphJSON(g, &b1); err != nil {
		t.Fatal(err)
	}
	if err := ExportGraphJSON(g, &b2); err != nil {
		t.Fatal(err)
	}
	if b1.String() != b2.String() {
		t.Error("export output is not deterministic")
	}
}

// TestGraphJSONCorruptRejected verifies a malformed document errors cleanly.
func TestGraphJSONCorruptRejected(t *testing.T) {
	_, err := ImportGraphJSON(strings.NewReader("{ not valid json"))
	if err == nil {
		t.Fatal("expected error for corrupt JSON")
	}
}

// TestGraphJSONEdgePropsNotDeduped verifies parallel edges that differ only in
// properties are exported and imported without collapsing, while true
// duplicates (identical incl. properties) are still deduplicated.
func TestGraphJSONEdgePropsNotDeduped(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &link.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []link.ResolvedEdge{
		{SourceID: "a", TargetID: "b", Type: link.EdgeCalls, LineNumber: 1},
		{SourceID: "a", TargetID: "b", Type: link.EdgeCalls, LineNumber: 1, Properties: map[string]string{"embedding": "0.5"}},
		{SourceID: "a", TargetID: "b", Type: link.EdgeCalls, LineNumber: 1, Properties: map[string]string{"embedding": "0.5"}},
	})
	g.InboundEdges = g.InboundEdges.Set("b", []link.ResolvedEdge{
		{SourceID: "a", TargetID: "b", Type: link.EdgeCalls, LineNumber: 1},
		{SourceID: "a", TargetID: "b", Type: link.EdgeCalls, LineNumber: 1, Properties: map[string]string{"embedding": "0.5"}},
		{SourceID: "a", TargetID: "b", Type: link.EdgeCalls, LineNumber: 1, Properties: map[string]string{"embedding": "0.5"}},
	})

	var buf bytes.Buffer
	if err := ExportGraphJSON(g, &buf); err != nil {
		t.Fatal(err)
	}
	restored, err := ImportGraphJSON(&buf)
	if err != nil {
		t.Fatal(err)
	}
	edges := restored.GetOutboundEdges("a")
	if len(edges) != 2 {
		t.Fatalf("edge count = %d, want 2 (plain + property-carrying)", len(edges))
	}
	propCount := 0
	for _, e := range edges {
		if len(e.Properties) > 0 {
			propCount++
			if e.Properties["embedding"] != "0.5" {
				t.Errorf("property value lost: %v", e.Properties)
			}
		}
	}
	if propCount != 1 {
		t.Errorf("property-carrying edge count = %d, want 1", propCount)
	}
}

// TestGraphJSONRejectsDanglingViaReplaceGraph verifies ReplaceGraph refuses
// graphs whose edges reference missing nodes.
func TestGraphJSONRejectsDanglingViaReplaceGraph(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &link.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	// Edge to nonexistent node b.
	g.OutboundEdges = g.OutboundEdges.Set("a", []link.ResolvedEdge{
		{SourceID: "a", TargetID: "missing", Type: link.EdgeCalls, LineNumber: 1},
	})
	g.InboundEdges = g.InboundEdges.Set("missing", []link.ResolvedEdge{
		{SourceID: "a", TargetID: "missing", Type: link.EdgeCalls, LineNumber: 1},
	})

	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer tm.Close()

	if err := tm.ReplaceGraph(g); err == nil {
		t.Fatal("expected ReplaceGraph to reject dangling edges")
	} else if !strings.Contains(err.Error(), "dangling") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestReplaceGraphPersistsAndReloads verifies a successful import survives a
// full reload from disk.
func TestReplaceGraphPersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatal(err)
	}

	g := NewCodePropertyGraph("imported")
	g.Version = 5
	g.Nodes = g.Nodes.Set("x", &link.ResolvedNode{ID: "x", Kind: "STRUCT", Name: "X", FileSpec: link.LocationMeta{Path: "x.go", LineStart: 3}})
	g.Nodes = g.Nodes.Set("y", &link.ResolvedNode{ID: "y", Kind: "STRUCT", Name: "Y", FileSpec: link.LocationMeta{Path: "y.go"}})
	addEdgeToGraph(g, "x", "y", link.EdgeComposes, 4)

	if err := tm.ReplaceGraph(g); err != nil {
		t.Fatalf("ReplaceGraph failed: %v", err)
	}
	tm.Close()

	tm2, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	defer tm2.Close()

	snap := tm2.GetActiveSnapshot()
	if snap.Nodes.Len() != 2 {
		t.Errorf("reloaded node count = %d, want 2", snap.Nodes.Len())
	}
	edges := snap.GetOutboundEdges("x")
	if len(edges) != 1 || edges[0].TargetID != "y" {
		t.Errorf("reloaded edge mismatch: %+v", edges)
	}
	if snap.Verified != true {
		t.Errorf("expected reloaded graph to be verified, got %q", snap.VerificationMsg)
	}
}
