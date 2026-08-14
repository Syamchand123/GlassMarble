package akg

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// TestGraphJSONRoundTripFidelity (Phase B parity gate, JSON-only since Phase D)
// round-trips the same graph through ExportGraphJSON → ImportGraphJSON and
// asserts structural equality: node set (IDs, kinds, names, properties,
// file specs), edge set (source/type/target/line, parallel edges preserved),
// entrypoints, folder zones, commit hash and version. The canonical
// GraphJSON store must be lossless.
func TestGraphJSONRoundTripFidelity(t *testing.T) {
	g := buildExportTestGraph()

	var jsonBuf bytes.Buffer
	if err := ExportGraphJSON(g, &jsonBuf); err != nil {
		t.Fatalf("ExportGraphJSON failed: %v", err)
	}
	fromJSON, err := ImportGraphJSON(&jsonBuf)
	if err != nil {
		t.Fatalf("ImportGraphJSON failed: %v", err)
	}

	// Metadata parity.
	if g.CommitHash != fromJSON.CommitHash || g.Version != fromJSON.Version {
		t.Errorf("metadata mismatch: source={%q v%d} JSON={%q v%d}", g.CommitHash, g.Version, fromJSON.CommitHash, fromJSON.Version)
	}

	// Node set parity (incl. properties + file specs).
	fromJSON.Nodes.Iterate(func(id string, jn *link.ResolvedNode) {
		src, ok := g.Nodes.Get(id)
		if !ok {
			t.Errorf("JSON node %q not in source graph", id)
			return
		}
		if src.Kind != jn.Kind || src.Name != jn.Name {
			t.Errorf("node %q mismatch: source={kind %q name %q} JSON={kind %q name %q}", id, src.Kind, src.Name, jn.Kind, jn.Name)
		}
		if src.FileSpec.Path != jn.FileSpec.Path || src.FileSpec.LineStart != jn.FileSpec.LineStart || src.FileSpec.LineEnd != jn.FileSpec.LineEnd {
			t.Errorf("node %q file_spec mismatch: source=%+v JSON=%+v", id, src.FileSpec, jn.FileSpec)
		}
		for k, v := range jn.Properties {
			if src.Properties[k] != v {
				t.Errorf("node %q property %q mismatch: source=%q JSON=%q", id, k, src.Properties[k], v)
			}
		}
	})
	if g.Nodes.Len() != fromJSON.Nodes.Len() {
		t.Errorf("node count mismatch: source=%d JSON=%d", g.Nodes.Len(), fromJSON.Nodes.Len())
	}

	// Edge set parity on the (source, type, target, line) projection.
	srcTriples := edgeTripleCanonicalSet(g)
	jsonTriples := edgeTripleCanonicalSet(fromJSON)
	for k, line := range jsonTriples {
		if srcTriples[k] != line {
			t.Errorf("edge triple %q mismatch: source line=%d JSON line=%d", k, srcTriples[k], line)
		}
	}
	if len(srcTriples) != len(jsonTriples) {
		t.Errorf("edge triple set size mismatch: source=%d JSON=%d", len(srcTriples), len(jsonTriples))
	}

	// Entrypoints + folder zones parity.
	for _, ep := range g.Entrypoints {
		found := false
		for _, jEp := range fromJSON.Entrypoints {
			if jEp == ep {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("source entrypoint %q missing from JSON load", ep)
		}
	}
	g.FolderZones.Iterate(func(id, zone string) {
		jZone, ok := fromJSON.FolderZones.Get(id)
		if !ok || jZone != zone {
			t.Errorf("folder zone %q mismatch: source=%q JSON=%q", id, zone, jZone)
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
	g.Nodes = g.Nodes.Set("p", &link.ResolvedNode{ID: "p", Kind: "STRUCT", Name: "P", FileSpec: link.LocationMeta{Path: "p.go"}})
	g.Nodes = g.Nodes.Set("q", &link.ResolvedNode{ID: "q", Kind: "STRUCT", Name: "Q", FileSpec: link.LocationMeta{Path: "q.go"}})
	addEdgeToGraph(g, "p", "q", link.EdgeComposes, 4)

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
	g.OutboundEdges.Iterate(func(_ string, edges []link.ResolvedEdge) {
		for _, e := range edges {
			key := e.SourceID + "|" + string(e.Type) + "|" + e.TargetID
			if e.LineNumber > out[key] {
				out[key] = e.LineNumber
			}
		}
	})
	return out
}
