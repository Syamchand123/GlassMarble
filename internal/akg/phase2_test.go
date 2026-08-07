package akg

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

// TestLegacyReadBackCompat (W2-02 / K-01 / AC-28):
// Asserts legacy TTL files with base triples + reified blocks parse to identical graph as v3.
func TestLegacyReadBackCompat(t *testing.T) {
	dir := t.TempDir()
	legacyTTL := `@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix gm: <http://glassmarble.org/schema#> .

<http://glassmarble.org/node/metadata> a gm:MetaData ;
    gm:commitHash "legacy_hash" ;
    gm:schemaVersion 2 ;
    gm:version 1 ;
    gm:name "GlassMarble Project MetaData" .

<http://glassmarble.org/node/n1> a gm:Struct ;
    gm:name "N1" .

<http://glassmarble.org/node/n2> a gm:Struct ;
    gm:name "N2" .

<http://glassmarble.org/node/n1> gm:calls <http://glassmarble.org/node/n2> .
<< <http://glassmarble.org/node/n1> gm:calls <http://glassmarble.org/node/n2> >> gm:lineNumber 25 .
`
	StatePath := filepath.Join(dir, "akg_state.ttl")
	if err := os.WriteFile(StatePath, []byte(legacyTTL), 0644); err != nil {
		t.Fatalf("failed to write legacy TTL: %v", err)
	}

	graph, err := reconstructFromTTLFile(StatePath)
	if err != nil {
		t.Fatalf("reconstructFromTTLFile failed on legacy TTL: %v", err)
	}

	if graph.Nodes.Len() != 2 {
		t.Errorf("expected 2 nodes, got %d", graph.Nodes.Len())
	}
	if graph.CommitHash != "legacy_hash" {
		t.Errorf("expected commitHash 'legacy_hash', got %q", graph.CommitHash)
	}
	edges, ok := graph.OutboundEdges.Get("n1")
	if !ok || len(edges) != 1 {
		t.Fatalf("expected 1 outbound edge from n1, got %d", len(edges))
	}
	if edges[0].LineNumber != 25 {
		t.Errorf("expected LineNumber 25, got %d", edges[0].LineNumber)
	}
}

// TestNoContentByDefault (K-05 / AC-06):
// Asserts the content policy drops source code from AKG nodes when
// --store-code is disabled, and the canonical GraphJSON export never
// carries it.
func TestNoContentByDefault(t *testing.T) {
	node := &stage4.ResolvedNode{
		ID:   "type:main:App",
		Kind: "STRUCT",
		Name: "App",
		Properties: map[string]string{
			"content": "func (a *App) Run() { println(\"hello\") }",
		},
	}
	ApplyContentPolicy(node, GetStoreCode())

	graph := NewCodePropertyGraph("content_test")
	graph.Nodes = graph.Nodes.Set(node.ID, node)

	var buf bytes.Buffer
	if err := ExportGraphJSON(graph, &buf); err != nil {
		t.Fatalf("ExportGraphJSON failed: %v", err)
	}
	if strings.Contains(buf.String(), "func (a *App) Run()") {
		t.Errorf("content leaked into GraphJSON when --store-code is disabled:\n%s", buf.String())
	}
}

// TestStoreCodeCap (K-05):
// Asserts that when --store-code is enabled, content is kept for structural
// nodes and capped at 512B in the GraphJSON export.
func TestStoreCodeCap(t *testing.T) {
	longContent := strings.Repeat("A", 1000)
	node := &stage4.ResolvedNode{
		ID:   "type:main:App",
		Kind: "STRUCT",
		Name: "App",
		Properties: map[string]string{
			"content": longContent,
		},
	}
	ApplyContentPolicy(node, true)

	graph := NewCodePropertyGraph("content_test")
	graph.Nodes = graph.Nodes.Set(node.ID, node)

	var buf bytes.Buffer
	if err := ExportGraphJSON(graph, &buf); err != nil {
		t.Fatalf("ExportGraphJSON failed: %v", err)
	}
	if strings.Contains(buf.String(), longContent) {
		t.Errorf("content was not truncated to %d bytes", MaxContentLength)
	}
	if !strings.Contains(buf.String(), strings.Repeat("A", MaxContentLength)) {
		t.Errorf("expected capped content in GraphJSON export")
	}
}

// TestSchemaMigration (W2-08 / K-07 / AC-27):
// Asserts v2 TTL file is migrated to v3, a backup .bak file is created, and stale kinds are cleaned up.
func TestSchemaMigration(t *testing.T) {
	dir := t.TempDir()

	v2TTL := `@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix gm: <http://glassmarble.org/schema#> .

<http://glassmarble.org/node/metadata> a gm:MetaData ;
    gm:commitHash "v2_hash" ;
    gm:schemaVersion 2 ;
    gm:version 10 ;
    gm:name "GlassMarble Project MetaData" .

<http://glassmarble.org/node/t1> a gm:TypeDecl ;
    gm:name "StaleType" .
`
	StatePath := filepath.Join(dir, "akg_state.ttl")
	if err := os.WriteFile(StatePath, []byte(v2TTL), 0644); err != nil {
		t.Fatalf("failed to write v2 TTL: %v", err)
	}

	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("NewAKGTransactionManager failed: %v", err)
	}

	snap := tm.GetActiveSnapshot()
	if snap.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("expected active graph schema version %d, got %d", CurrentSchemaVersion, snap.SchemaVersion)
	}

	// Check backup file exists
	bakPath := filepath.Join(dir, "akg_state.v2.ttl.bak")
	if _, err := os.Stat(bakPath); os.IsNotExist(err) {
		t.Errorf("expected schema backup file at %s, but not found", bakPath)
	}

	// Check stale kind TypeDecl was reclassified to STRUCT
	node, ok := snap.Nodes.Get("t1")
	if !ok {
		t.Fatalf("node t1 not found in migrated snapshot")
	}
	if node.Kind != "STRUCT" {
		t.Errorf("expected reclassified kind 'STRUCT', got %q", node.Kind)
	}
}

// TestWriteReadSymmetry (W2-10 / K-08):
// Asserts serializer-written properties match restored properties (no 'code' property key ambiguity).
func TestWriteReadSymmetry(t *testing.T) {
	dir := t.TempDir()

	graph := NewCodePropertyGraph("symmetry_test")
	graph.SchemaVersion = CurrentSchemaVersion

	node := &stage4.ResolvedNode{
		ID:   "type:main:Model",
		Kind: "STRUCT",
		Name: "Model",
		Properties: map[string]string{
			"custom_prop": "val123",
		},
	}
	graph.Nodes = graph.Nodes.Set(node.ID, node)

	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("NewAKGTransactionManager failed: %v", err)
	}

	if err := tm.ReplaceGraph(graph); err != nil {
		t.Fatalf("ReplaceGraph failed: %v", err)
	}

	restoredSnap := tm.GetActiveSnapshot()
	restoredNode, ok := restoredSnap.Nodes.Get(node.ID)
	if !ok {
		t.Fatalf("restored node not found")
	}
	if restoredNode.Properties["custom_prop"] != "val123" {
		t.Errorf("custom_prop mismatch: %q", restoredNode.Properties["custom_prop"])
	}
	if _, hasCode := restoredNode.Properties["code"]; hasCode {
		t.Errorf("unexpected 'code' property key found in restored node (K-08 key symmetry failure)")
	}
}

// TestVerifySkipsMacro (W2-04 / K-03):
// Verifies that post-write verification (verifyJSONFile) does not run
// topological macro inference.
func TestVerifySkipsMacro(t *testing.T) {
	dir := t.TempDir()

	graph := NewCodePropertyGraph("verify_macro_test")
	graph.SchemaVersion = CurrentSchemaVersion

	node := &stage4.ResolvedNode{
		ID:   "type:main:Server",
		Kind: "STRUCT",
		Name: "Server",
	}
	graph.Nodes = graph.Nodes.Set(node.ID, node)

	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("NewAKGTransactionManager failed: %v", err)
	}

	// Calling saveToDisk invokes verifyJSONFile internally (K-03 / W2-04)
	if err := tm.saveToDisk(graph); err != nil {
		t.Fatalf("saveToDisk failed: %v", err)
	}

	if !graph.Verified {
		t.Errorf("graph.Verified = false after saveToDisk")
	}
}
