package stage1

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

const lazyScopedFixture = `
@prefix gm: <http://glassmarble.org/schema#> .

<http://glassmarble.org/node/a.go::Alpha> a gm:Function ;
    gm:name "Alpha" ;
    gm:belongsToFile <http://glassmarble.org/file/a.go> ;
    gm:lineStart 1 ;
    gm:lineEnd 10 ;
    gm:isEntrypoint true ;
    .
<http://glassmarble.org/node/b.go::Beta> a gm:Function ;
    gm:name "Beta" ;
    gm:belongsToFile <http://glassmarble.org/file/b.go> ;
    gm:lineStart 1 ;
    gm:lineEnd 8 ;
    .
<http://glassmarble.org/node/a.go::Alpha> gm:calls <http://glassmarble.org/node/b.go::Beta> .
<< <http://glassmarble.org/node/a.go::Alpha> gm:calls <http://glassmarble.org/node/b.go::Beta> >> gm:lineNumber 3 .
<http://glassmarble.org/node/b.go::Beta> gm:calls <http://glassmarble.org/node/a.go::Alpha> .
<< <http://glassmarble.org/node/b.go::Beta> gm:calls <http://glassmarble.org/node/a.go::Alpha> >> gm:lineNumber 4 .
`

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.ttl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestParseTTLFileToNativeScopedEqualsFullPlusScope: the file-scoped lazy
// parse equals a full parse followed by ApplyScope(ScopeFile)
// (AUDIT Issue 4 Phase 4A-2).
func TestParseTTLFileToNativeScopedEqualsFullPlusScope(t *testing.T) {
	path := writeFixture(t, lazyScopedFixture)

	full, err := ParseTTLFileToNative(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := full.Clone()
	ApplyScope(expected, types.QueryOptions{Scope: types.ScopeFile, ScopePath: "a.go"})

	got, err := ParseTTLFileToNativeScoped(path, "a.go")
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

// TestParseTTLNodeByID: streaming single-node lookup with reified edge
// binding and last-block-wins semantics (AUDIT Issue 4 Phase 4A-2).
func TestParseTTLNodeByID(t *testing.T) {
	path := writeFixture(t, lazyScopedFixture)

	node, edges, err := ParseTTLNodeByID(path, "a.go::Alpha")
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
			t.Errorf("edge %s->%s missing line number binding", e.SourceID, e.TargetID)
		}
	}

	missing, _, err := ParseTTLNodeByID(path, "ghost")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Errorf("expected nil for missing node, got %+v", missing)
	}
}

// TestParseTTLNodeByIDLastBlockWins: an appended edit overrides the base
// block, exactly like a full restore.
func TestParseTTLNodeByIDLastBlockWins(t *testing.T) {
	path := writeFixture(t, `<http://glassmarble.org/node/n1> a gm:Function ;
    gm:name "old" ;
    .
<http://glassmarble.org/node/n1> a gm:Function ;
    gm:name "new" ;
    .
`)

	node, _, err := ParseTTLNodeByID(path, "n1")
	if err != nil {
		t.Fatal(err)
	}
	if node == nil || node.Name != "new" {
		t.Fatalf("expected last block to win (name=new), got %+v", node)
	}
}

// TestStreamTTLNodesStops: iteration stops cleanly when the callback returns
// false, and tombstone blocks never reach the callback.
func TestStreamTTLNodesStops(t *testing.T) {
	path := writeFixture(t, `<http://glassmarble.org/node/n1> a gm:Function ;
    gm:name "one" ;
    .
<http://glassmarble.org/node/gone> a gm:Deleted ;
    gm:status "DELETED" ;
    .
<http://glassmarble.org/node/n2> a gm:Function ;
    gm:name "two" ;
    .
`)

	count := 0
	err := StreamTTLNodes(path, func(n *types.TTLNode) bool {
		count++
		return count < 1
	})
	if err != nil && !StopStreaming(err) {
		t.Fatalf("early stop must be clean, got %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 node before stop, got %d", count)
	}

	if err := StreamTTLNodes(path, func(n *types.TTLNode) bool {
		if n.ID == "gone" {
			t.Fatal("tombstone block must not be streamed as a node")
		}
		return true
	}); err != nil {
		t.Fatal(err)
	}
}

// TestStreamTTLBlocksOrder: generic block streaming covers node, edge, and
// reified blocks in file order.
func TestStreamTTLBlocksOrder(t *testing.T) {
	path := writeFixture(t, lazyScopedFixture)

	var baseEdge, reified, nodeBlocks int
	err := StreamTTLBlocks(path, func(block string) error {
		trimmed := strings.TrimSuffix(strings.TrimSpace(block), ".")
		switch {
		case strings.HasPrefix(trimmed, "<<"):
			reified++
		case isBaseEdge(trimmed):
			baseEdge++
		default:
			nodeBlocks++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if nodeBlocks != 2 {
		t.Errorf("expected 2 node blocks, got %d", nodeBlocks)
	}
	if baseEdge != 2 {
		t.Errorf("expected 2 base edge triples, got %d", baseEdge)
	}
	if reified != 2 {
		t.Errorf("expected 2 reified property blocks, got %d", reified)
	}
}
