package akg_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage1"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// These tests exercise the GraphJSON store entry points (ParseGraphFile and
// friends) — the JSON equivalents of the legacy Turtle parsers in
// internal/visualization_engine/stage1. The fixtures live in testdata/.

func TestParseMinimal(t *testing.T) {
	path := filepath.Join("testdata", "minimal.json")
	nodes, edges, err := akg.ParseGraphFile(path)
	if err != nil {
		t.Fatalf("ParseGraphFile failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}
	if len(edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(edges))
	}
}

func TestParseEmpty(t *testing.T) {
	path := filepath.Join("testdata", "empty.json")
	nodes, edges, err := akg.ParseGraphFile(path)
	if err != nil {
		t.Fatalf("ParseGraphFile failed: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}

func TestParseAllPredicates(t *testing.T) {
	path := filepath.Join("testdata", "all_predicates.json")
	nodes, edges, err := akg.ParseGraphFile(path)
	if err != nil {
		t.Fatalf("ParseGraphFile failed: %v", err)
	}
	predCount := make(map[string]int)
	for _, e := range edges {
		predCount[e.Predicate]++
	}
	// File membership is structural in the JSON store (node file_spec), not
	// a gm:belongsToFile edge.
	expectedPreds := []string{"gm:calls", "gm:inheritsFrom", "gm:composes", "gm:dataFlowTo", "gm:controlFlowTo"}
	for _, p := range expectedPreds {
		if predCount[p] == 0 {
			t.Errorf("expected at least one edge with predicate %q", p)
		}
	}
	if len(nodes) == 0 {
		t.Error("expected at least one node")
	}
}

func TestParseAllKinds(t *testing.T) {
	path := filepath.Join("testdata", "all_kinds.json")
	nodes, edges, err := akg.ParseGraphFile(path)
	if err != nil {
		t.Fatalf("ParseGraphFile failed: %v", err)
	}
	kindCount := make(map[string]int)
	for _, n := range nodes {
		kindCount[n.Kind]++
	}
	// TYPE_DECL maps to gm:Struct (the class the Turtle serializer wrote for
	// type declarations); gm:TypeDecl stays a legacy fallback class.
	expectedKinds := []string{"gm:Struct", "gm:Function", "gm:Namespace", "gm:File", "gm:VirtualDatabase", "gm:ExternalAPI"}
	for _, k := range expectedKinds {
		if kindCount[k] == 0 {
			t.Errorf("expected at least one node with kind %q", k)
		}
	}
	t.Logf("parsed %d nodes, %d edges", len(nodes), len(edges))
}

func TestParseFullGraph(t *testing.T) {
	path := filepath.Join("testdata", "full_graph.json")
	nodes, edges, err := akg.ParseGraphFile(path)
	if err != nil {
		t.Fatalf("ParseGraphFile failed: %v", err)
	}
	if len(nodes) == 0 {
		t.Error("expected non-zero nodes from full_graph.json")
	}
	if len(edges) == 0 {
		t.Error("expected non-zero edges from full_graph.json")
	}
}

func TestParseGraphFileToNative(t *testing.T) {
	path := filepath.Join("testdata", "minimal.json")
	ng, err := akg.ParseGraphFileToNative(path)
	if err != nil {
		t.Fatalf("ParseGraphFileToNative failed: %v", err)
	}
	if ng == nil {
		t.Fatal("expected non-nil NativeGraph")
	}
	if len(ng.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(ng.Nodes))
	}
	if len(ng.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(ng.Edges))
	}
}

func TestParseEmptyFile(t *testing.T) {
	nodes, edges, err := akg.ParseGraphFile(filepath.Join("testdata", "empty.json"))
	if err != nil {
		t.Fatalf("ParseGraphFile failed: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}

func TestParseDeltaAppend(t *testing.T) {
	path := filepath.Join("testdata", "delta_append.json")
	nodes, edges, err := akg.ParseGraphFile(path)
	if err != nil {
		t.Fatalf("ParseGraphFile failed: %v", err)
	}
	// Deleted nodes are absent from the JSON store, so OldWorker must not be
	// present (the TTL tombstone concept does not exist in GraphJSON).
	if _, ok := nodes["cmd/app/worker.go::OldWorker"]; ok {
		t.Error("OldWorker with DELETED status should not be in nodes")
	}
	// Main and Connect should still be present
	if _, ok := nodes["cmd/app/main.go::Main"]; !ok {
		t.Error("expected Main to be present in delta_append.json")
	}
	if len(edges) > 0 {
		t.Logf("parsed %d edges from delta_append.json", len(edges))
	}
}

func TestParseMalformedState(t *testing.T) {
	nodes, edges, err := akg.ParseGraphFile(filepath.Join("testdata", "does_not_exist.json"))
	if err == nil {
		t.Error("expected error for non-existent file")
	}
	if nodes != nil {
		t.Error("expected nil nodes for error case")
	}
	if edges != nil {
		t.Error("expected nil edges for error case")
	}
}

const lazyScopedFixtureJSON = `{
  "schema_version": 3,
  "commit_hash": "fixture",
  "version": 0,
  "entrypoints": ["a.go::Alpha"],
  "nodes": [
    {
      "id": "a.go::Alpha",
      "kind": "FUNCTION",
      "name": "Alpha",
      "file_spec": { "path": "a.go", "line_start": 1, "line_end": 10 }
    },
    {
      "id": "b.go::Beta",
      "kind": "FUNCTION",
      "name": "Beta",
      "file_spec": { "path": "b.go", "line_start": 1, "line_end": 8 }
    }
  ],
  "edges": [
    {
      "source_id": "a.go::Alpha",
      "target_id": "b.go::Beta",
      "type": "CALLS",
      "line_number": 3
    },
    {
      "source_id": "b.go::Beta",
      "target_id": "a.go::Alpha",
      "type": "CALLS",
      "line_number": 4
    }
  ]
}
`

func writeJSONFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestParseGraphFileToNativeScopedEqualsFullPlusScope: the file-scoped lazy
// load equals a full load followed by stage1.ApplyScope(ScopeFile),
// mirroring the TTL lazy-parser guarantee (AUDIT Issue 4 Phase 4A-2).
func TestParseGraphFileToNativeScopedEqualsFullPlusScope(t *testing.T) {
	path := writeJSONFixture(t, lazyScopedFixtureJSON)

	full, err := akg.ParseGraphFileToNative(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := full.Clone()
	stage1.ApplyScope(expected, types.QueryOptions{Scope: types.ScopeFile, ScopePath: "a.go"})

	got, err := akg.ParseGraphFileToNativeScoped(path, "a.go")
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Nodes) != len(expected.Nodes) {
		t.Fatalf("scoped node count %d != full+scope %d", len(got.Nodes), len(expected.Nodes))
	}
	for id, n := range expected.Nodes {
		g, ok := got.Nodes[id]
		if !ok {
			t.Fatalf("scoped parse missing node %s", id)
		}
		if g.Kind != n.Kind || g.Name != n.Name || g.FileURI != n.FileURI || g.LineStart != n.LineStart || g.LineEnd != n.LineEnd || g.IsEntrypoint != n.IsEntrypoint {
			t.Errorf("node %s mismatch: got %+v want %+v", id, g, n)
		}
	}
	if len(got.Edges) != len(expected.Edges) {
		t.Fatalf("scoped edge count %d != full+scope %d", len(got.Edges), len(expected.Edges))
	}
	for _, e := range expected.Edges {
		found := false
		for _, ge := range got.Edges {
			if ge.SourceID == e.SourceID && ge.Predicate == e.Predicate && ge.TargetID == e.TargetID && ge.LineNumber == e.LineNumber {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("scoped parse missing edge %s %s %s (L%d)", e.SourceID, e.Predicate, e.TargetID, e.LineNumber)
		}
	}

	// The scoped graph must contain ONLY the scoped file's nodes.
	if _, ok := got.Nodes["b.go::Beta"]; ok {
		t.Error("scoped parse must not include nodes from other files")
	}
	// Cross-file edges are dropped entirely (one endpoint out of scope).
	if len(got.Edges) != 0 {
		t.Errorf("scoped parse must drop cross-file edges, got %d", len(got.Edges))
	}
}

// TestParseGraphNodeByID: lazy single-node lookup with entrypoint flags and
// incident-edge collection (AUDIT Issue 4 Phase 4A-2).
func TestParseGraphNodeByID(t *testing.T) {
	path := writeJSONFixture(t, lazyScopedFixtureJSON)

	node, edges, err := akg.ParseGraphNodeByID(path, "a.go::Alpha")
	if err != nil {
		t.Fatal(err)
	}
	if node == nil {
		t.Fatal("expected node a.go::Alpha")
	}
	if node.Kind != "gm:Function" || node.Name != "Alpha" || !node.IsEntrypoint {
		t.Errorf("node fields wrong: %+v", node)
	}
	if len(edges) != 2 {
		t.Fatalf("expected 2 incident edges, got %d", len(edges))
	}
	for _, e := range edges {
		if e.LineNumber == 0 {
			t.Errorf("edge %s->%s missing line number", e.SourceID, e.TargetID)
		}
	}

	missing, _, err := akg.ParseGraphNodeByID(path, "ghost")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Errorf("expected nil for missing node, got %+v", missing)
	}
}

// TestParseGraphNodeByIDLastBlockWins: an appended edit overrides the base
// entry, exactly like a full restore.
func TestParseGraphNodeByIDLastBlockWins(t *testing.T) {
	path := writeJSONFixture(t, `{
  "schema_version": 3,
  "commit_hash": "fixture",
  "version": 0,
  "nodes": [
    { "id": "n1", "kind": "FUNCTION", "name": "old", "file_spec": { "path": "a.go" } },
    { "id": "n1", "kind": "FUNCTION", "name": "new", "file_spec": { "path": "a.go" } }
  ],
  "edges": []
}
`)

	node, _, err := akg.ParseGraphNodeByID(path, "n1")
	if err != nil {
		t.Fatal(err)
	}
	if node == nil || node.Name != "new" {
		t.Fatalf("expected last entry to win (name=new), got %+v", node)
	}
}

// TestStreamGraphNodesStops: iteration stops cleanly when the callback
// returns false.
func TestStreamGraphNodesStops(t *testing.T) {
	path := writeJSONFixture(t, lazyScopedFixtureJSON)

	count := 0
	err := akg.StreamGraphNodes(path, func(n *types.NativeNode) bool {
		count++
		return count < 1
	})
	if err != nil {
		t.Fatalf("early stop must be clean, got %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 node before stop, got %d", count)
	}

	total := 0
	if err := akg.StreamGraphNodes(path, func(n *types.NativeNode) bool {
		total++
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("expected 2 nodes in stream, got %d", total)
	}
}

// TestStreamGraphJSONSectionsOrder: streaming covers the nodes and edges
// sections of the GraphJSON document.
func TestStreamGraphJSONSectionsOrder(t *testing.T) {
	path := writeJSONFixture(t, lazyScopedFixtureJSON)

	var nodeBlocks, baseEdge int
	if err := akg.StreamGraphNodes(path, func(n *types.NativeNode) bool {
		nodeBlocks++
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if err := akg.StreamGraphEdges(path, func(e *types.NativeEdge) bool {
		baseEdge++
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if nodeBlocks != 2 {
		t.Errorf("expected 2 node entries, got %d", nodeBlocks)
	}
	if baseEdge != 2 {
		t.Errorf("expected 2 edge entries, got %d", baseEdge)
	}
}
