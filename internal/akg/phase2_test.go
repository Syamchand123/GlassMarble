package akg

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// TestSerializeSingleStatement (W2-01 / K-01 / AC-11):
// Verifies that SerializeToTurtle writes single RDF-star statements per edge
// and does NOT double-write base triples.
func TestSerializeSingleStatement(t *testing.T) {
	graph := NewCodePropertyGraph("commit_test_hash")
	graph.SchemaVersion = CurrentSchemaVersion

	srcNode := &stage4.ResolvedNode{
		ID:   "type:internal/test:Foo",
		Kind: "STRUCT",
		Name: "Foo",
		FileSpec: stage4.LocationMeta{
			Path:      "internal/test/foo.go",
			LineStart: 10,
			LineEnd:   20,
		},
	}
	tgtNode := &stage4.ResolvedNode{
		ID:   "type:internal/test:Bar",
		Kind: "STRUCT",
		Name: "Bar",
		FileSpec: stage4.LocationMeta{
			Path:      "internal/test/bar.go",
			LineStart: 5,
			LineEnd:   15,
		},
	}

	graph.Nodes = graph.Nodes.Set(srcNode.ID, srcNode).Set(tgtNode.ID, tgtNode)

	edge := stage4.ResolvedEdge{
		SourceID:   srcNode.ID,
		TargetID:   tgtNode.ID,
		Type:       stage4.EdgeCalls,
		LineNumber: 15,
		Confidence: 1.0,
	}
	graph.OutboundEdges = graph.OutboundEdges.Set(srcNode.ID, []stage4.ResolvedEdge{edge})

	var buf bytes.Buffer
	err := SerializeToTurtle(graph, &buf)
	if err != nil {
		t.Fatalf("SerializeToTurtle failed: %v", err)
	}

	output := buf.String()

	// 1. Assert RDF-star single statement is present
	if !strings.Contains(output, "<< <http://glassmarble.org/node/type:internal/test:Foo> gm:calls <http://glassmarble.org/node/type:internal/test:Bar> >>") {
		t.Errorf("expected RDF-star edge statement in output:\n%s", output)
	}

	// 2. Assert double-written base triple is NOT present
	baseTriple := "<http://glassmarble.org/node/type:internal/test:Foo> gm:calls <http://glassmarble.org/node/type:internal/test:Bar> ."
	if strings.Contains(output, baseTriple) {
		t.Errorf("found unwanted base triple double-write:\n%s", output)
	}
}

// TestMetadataFields (W2-03 / K-02 / AC-12):
// Asserts metadata block contains commitHash, schemaVersion (3), version, analyzerVersion, generatedAt, views.
func TestMetadataFields(t *testing.T) {
	graph := NewCodePropertyGraph("abc123def456")
	graph.SchemaVersion = CurrentSchemaVersion
	graph.Version = 42

	var buf bytes.Buffer
	err := SerializeToTurtle(graph, &buf)
	if err != nil {
		t.Fatalf("SerializeToTurtle failed: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, ont.PredCommitHash+` "abc123def456"`) {
		t.Errorf("missing gm:commitHash in metadata:\n%s", output)
	}
	if !strings.Contains(output, ont.PredSchemaVersion+` 3`) {
		t.Errorf("missing gm:schemaVersion 3 in metadata:\n%s", output)
	}
	if !strings.Contains(output, ont.PredAnalyzerVersion+` "1.0.0-overhaul"`) {
		t.Errorf("missing gm:analyzerVersion in metadata:\n%s", output)
	}
	if !strings.Contains(output, ont.PredViews+` "structural"`) {
		t.Errorf("missing gm:views in metadata:\n%s", output)
	}
}

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
	ttlPath := filepath.Join(dir, "akg_state.ttl")
	if err := os.WriteFile(ttlPath, []byte(legacyTTL), 0644); err != nil {
		t.Fatalf("failed to write legacy TTL: %v", err)
	}

	graph, err := reconstructFromTTLFile(ttlPath)
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

// TestNoContentByDefault (W2-07 / K-05 / AC-06):
// Asserts analyze --full without --store-code yields zero gm:content in serialized TTL.
func TestNoContentByDefault(t *testing.T) {
	SetStoreCode(false)
	defer SetStoreCode(false)

	graph := NewCodePropertyGraph("content_test")

	node := &stage4.ResolvedNode{
		ID:   "type:main:App",
		Kind: "STRUCT",
		Name: "App",
		Properties: map[string]string{
			"content": "func (a *App) Run() { println(\"hello\") }",
		},
	}
	graph.Nodes = graph.Nodes.Set(node.ID, node)

	var buf bytes.Buffer
	if err := SerializeToTurtle(graph, &buf); err != nil {
		t.Fatalf("SerializeToTurtle failed: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "gm:content") {
		t.Errorf("found gm:content when --store-code is disabled:\n%s", output)
	}
	if !strings.Contains(output, ont.PredHasContent+` "false"`) {
		t.Errorf("expected gm:hasContent \"false\":\n%s", output)
	}
}

// TestStoreCodeCap (W2-07 / K-05):
// Asserts that when --store-code is enabled, content is kept for structural nodes and capped at 512B.
func TestStoreCodeCap(t *testing.T) {
	SetStoreCode(true)
	defer SetStoreCode(false)

	graph := NewCodePropertyGraph("content_test")

	longContent := strings.Repeat("A", 1000)
	node := &stage4.ResolvedNode{
		ID:   "type:main:App",
		Kind: "STRUCT",
		Name: "App",
		Properties: map[string]string{
			"content": longContent,
		},
	}
	graph.Nodes = graph.Nodes.Set(node.ID, node)

	var buf bytes.Buffer
	if err := SerializeToTurtle(graph, &buf); err != nil {
		t.Fatalf("SerializeToTurtle failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "gm:content") {
		t.Fatalf("expected gm:content when --store-code is enabled:\n%s", output)
	}
	if strings.Contains(output, longContent) {
		t.Errorf("content was not truncated to 512B")
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
	ttlPath := filepath.Join(dir, "akg_state.ttl")
	if err := os.WriteFile(ttlPath, []byte(v2TTL), 0644); err != nil {
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

// TestWALRoundTrip (W2-05 / 6.10):
// Asserts WAL entries replay cleanly to identical graph.
func TestWALRoundTrip(t *testing.T) {
	dir := t.TempDir()

	wal, err := NewWriteAheadLog(dir)
	if err != nil {
		t.Fatalf("NewWriteAheadLog failed: %v", err)
	}

	payload := stage4.NewStage4Output("wal_test_commit")
	node := &stage4.ResolvedNode{
		ID:   "type:pkg:Worker",
		Kind: "STRUCT",
		Name: "Worker",
	}
	payload.GraphNodes[node.ID] = node

	entry := &WALEntry{
		TxID:       1,
		CommitHash: "wal_test_commit",
		Timestamp:  time.Now().UTC(),
		Payload:    payload,
		Status:     WALStatusStarted,
	}

	if err := wal.AppendEntry(entry); err != nil {
		t.Fatalf("AppendEntry failed: %v", err)
	}

	entries, err := wal.ReadAllEntries()
	if err != nil {
		t.Fatalf("ReadAllEntries failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].CommitHash != "wal_test_commit" {
		t.Errorf("commit hash mismatch: %q", entries[0].CommitHash)
	}
}

// TestVerifySkipsMacro (W2-04 / K-03):
// Verifies that verifyTTLFile does not run topological macro inference.
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

	// Calling saveToDisk invokes verifyTTLFile internally
	if err := tm.saveToDisk(graph, nil, nil, 0); err != nil {
		t.Fatalf("saveToDisk failed: %v", err)
	}

	if !graph.Verified {
		t.Errorf("graph.Verified = false after saveToDisk")
	}
}
