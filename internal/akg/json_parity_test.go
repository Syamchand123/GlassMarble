package akg

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

// TestTTLJSONParity (Phase B parity gate) serializes the same graph through the
// legacy TTL path and the GraphJSON path, then asserts structural equality:
// node set (IDs, kinds, names, properties), edge set (projected to the
// TTL-equivalent deduped (source, predicate, target, line) triples since TTL
// collapses duplicate parallel edges and drops edge properties), entrypoints,
// folder zones, commit hash and version. JSON must be a strict superset of the
// TTL semantics — never a subset.
func TestTTLJSONParity(t *testing.T) {
	g := buildExportTestGraph()

	// Legacy TTL side: serialize + canonical parse (same code path loadFromDisk uses).
	var ttlBuf bytes.Buffer
	if err := SerializeToTurtle(g, &ttlBuf); err != nil {
		t.Fatalf("SerializeToTurtle failed: %v", err)
	}
	StatePath := filepath.Join(t.TempDir(), "akg_state.ttl")
	if err := os.WriteFile(StatePath, ttlBuf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	fromTTL, err := reconstructFromTTLFileEx(StatePath, false)
	if err != nil {
		t.Fatalf("TTL reconstruct failed: %v", err)
	}

	// JSON side.
	var jsonBuf bytes.Buffer
	if err := ExportGraphJSON(g, &jsonBuf); err != nil {
		t.Fatalf("ExportGraphJSON failed: %v", err)
	}
	fromJSON, err := ImportGraphJSON(&jsonBuf)
	if err != nil {
		t.Fatalf("ImportGraphJSON failed: %v", err)
	}

	// Metadata parity.
	if fromTTL.CommitHash != fromJSON.CommitHash || fromTTL.Version != fromJSON.Version {
		t.Errorf("metadata mismatch: TTL={%q v%d} JSON={%q v%d}", fromTTL.CommitHash, fromTTL.Version, fromJSON.CommitHash, fromJSON.Version)
	}

	// Node set parity (incl. properties).
	fromJSON.Nodes.Iterate(func(id string, jn *stage4.ResolvedNode) {
		tn, ok := fromTTL.Nodes.Get(id)
		if !ok {
			t.Errorf("JSON node %q missing from TTL load", id)
			return
		}
		if tn.Kind != jn.Kind || tn.Name != jn.Name {
			t.Errorf("node %q mismatch: TTL={kind %q name %q} JSON={kind %q name %q}", id, tn.Kind, tn.Name, jn.Kind, jn.Name)
		}
		if tn.FileSpec.Path != jn.FileSpec.Path || tn.FileSpec.LineStart != jn.FileSpec.LineStart || tn.FileSpec.LineEnd != jn.FileSpec.LineEnd {
			t.Errorf("node %q file_spec mismatch: TTL=%+v JSON=%+v", id, tn.FileSpec, jn.FileSpec)
		}
		for k, v := range jn.Properties {
			if tn.Properties[k] != v {
				t.Errorf("node %q property %q mismatch: TTL=%q JSON=%q", id, k, tn.Properties[k], v)
			}
		}
	})
	if fromTTL.Nodes.Len() != fromJSON.Nodes.Len() {
		t.Errorf("node count mismatch: TTL=%d JSON=%d", fromTTL.Nodes.Len(), fromJSON.Nodes.Len())
	}

	// Edge set parity on the TTL-canonical (source, type, target) projection:
	// the serializer collapses duplicate parallel edges to one triple keeping
	// the max line number, so the JSON edge set is compared in that same
	// canonical form. JSON must be a strict superset of the TTL semantics.
	ttlTriples := edgeTripleCanonicalSet(fromTTL)
	jsonTriples := edgeTripleCanonicalSet(fromJSON)
	for k, line := range jsonTriples {
		if ttlTriples[k] != line {
			t.Errorf("edge triple %q mismatch: TTL line=%d JSON line=%d", k, ttlTriples[k], line)
		}
	}
	if len(ttlTriples) != len(jsonTriples) {
		t.Errorf("edge triple set size mismatch: TTL=%d JSON=%d", len(ttlTriples), len(jsonTriples))
	}

	// Entrypoints + folder zones parity.
	for _, ep := range fromJSON.Entrypoints {
		found := false
		for _, tEp := range fromTTL.Entrypoints {
			if tEp == ep {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("JSON entrypoint %q missing from TTL load", ep)
		}
	}
	fromJSON.FolderZones.Iterate(func(id, zone string) {
		tZone, ok := fromTTL.FolderZones.Get(id)
		if !ok || tZone != zone {
			t.Errorf("folder zone %q mismatch: TTL=%q JSON=%q", id, tZone, zone)
		}
	})
}

// TestJSONStatePersistsAndReloads verifies the Phase C single-write: after a
// graph save, akg.json is the only state artifact (no TTL mirror) and it
// restores the identical graph.
func TestJSONStatePersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatal(err)
	}

	g := NewCodePropertyGraph("dual")
	g.Version = 9
	g.Nodes = g.Nodes.Set("p", &stage4.ResolvedNode{ID: "p", Kind: "STRUCT", Name: "P", FileSpec: stage4.LocationMeta{Path: "p.go"}})
	g.Nodes = g.Nodes.Set("q", &stage4.ResolvedNode{ID: "q", Kind: "STRUCT", Name: "Q", FileSpec: stage4.LocationMeta{Path: "q.go"}})
	addEdgeToGraph(g, "p", "q", stage4.EdgeComposes, 4)

	if err := tm.ReplaceGraph(g); err != nil {
		t.Fatalf("ReplaceGraph failed: %v", err)
	}
	tm.Close()

	jsonPath := filepath.Join(dir, "akg.json")
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("akg.json not written by commit: %v", err)
	}

	// The legacy TTL mirror is no longer produced since Phase C.
	if _, err := os.Stat(filepath.Join(dir, "akg_state.ttl")); !os.IsNotExist(err) {
		t.Fatal("akg_state.ttl must not be written since Phase C")
	}

	f, err := os.Open(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fromJSON, err := ImportGraphJSON(f)
	if err != nil {
		t.Fatalf("akg.json did not import cleanly: %v", err)
	}
	if fromJSON.Nodes.Len() != 2 {
		t.Errorf("JSON reload node count = %d, want 2", fromJSON.Nodes.Len())
	}
	edges := fromJSON.GetOutboundEdges("p")
	if len(edges) != 1 || edges[0].TargetID != "q" {
		t.Errorf("JSON reload edge mismatch: %+v", edges)
	}
}

// edgeTripleCanonicalSet projects edges into the TTL-canonical form: one entry
// per (source, type, target) keeping the max line number — the exact semantics
// of the TTL serializer's parallel-edge dedup.
func edgeTripleCanonicalSet(g *CodePropertyGraph) map[string]int {
	out := make(map[string]int)
	g.OutboundEdges.Iterate(func(_ string, edges []stage4.ResolvedEdge) {
		for _, e := range edges {
			key := e.SourceID + "|" + string(e.Type) + "|" + e.TargetID
			if e.LineNumber > out[key] {
				out[key] = e.LineNumber
			}
		}
	})
	return out
}
