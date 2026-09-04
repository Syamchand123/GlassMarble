package akg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// buildBenchGraph creates a graph of roughly the shape a mid-sized repository
// produces: n nodes across 12 kinds, each with a few properties, and ~2 edges
// per node.
func buildBenchGraph(n int) *CodePropertyGraph {
	g := NewCodePropertyGraph("benchcommit")
	kinds := []string{"STRUCT", "INTERFACE", "FUNCTION", "METHOD", "CLASS", "MODULE",
		"PACKAGE", "FILE", "FIELD", "ENUM", "TYPE_ALIAS", "SERVICE"}
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("pkg/mod%03d/file%04d.go::Symbol%06d", i%40, i%900, i)
		ids[i] = id
		g.Nodes = g.Nodes.Set(id, &link.ResolvedNode{
			ID:   id,
			Name: fmt.Sprintf("Symbol%06d", i),
			Kind: kinds[i%len(kinds)],
			FileSpec: link.LocationMeta{
				Path:      fmt.Sprintf("pkg/mod%03d/file%04d.go", i%40, i%900),
				LineStart: i % 500,
				LineEnd:   i%500 + 8,
			},
			Properties: map[string]string{
				"gm:pagerank":   fmt.Sprintf("%0.6f", float64(i%1000)/1000),
				"gm:visibility": "public",
				"gm:language":   "go",
			},
		})
	}
	for i := 0; i < n; i++ {
		src := ids[i]
		edges := []link.ResolvedEdge{
			{SourceID: src, TargetID: ids[(i*7+1)%n], Type: link.EdgeCalls, LineNumber: i % 400},
			{SourceID: src, TargetID: ids[(i*13+5)%n], Type: link.EdgeDependsOn, LineNumber: i % 300},
		}
		g.OutboundEdges = g.OutboundEdges.Set(src, edges)
	}
	return g
}

// legacyWriteAndVerify reproduces the previous commit path exactly: serialize
// to the staged file, then re-read it, unmarshal into a second document,
// re-serialize the graph into a third, byte-compare, and scan for dangling
// edges over the parsed document.
func legacyWriteAndVerify(b *testing.B, dir string, g *CodePropertyGraph) {
	b.Helper()
	path := filepath.Join(dir, "legacy.json")

	f, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	if err := ExportGraphJSON(g, f); err != nil {
		b.Fatal(err)
	}
	f.Sync()
	f.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	var doc GraphJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		b.Fatal(err)
	}
	var buf bytes.Buffer
	if err := ExportGraphJSON(g, &buf); err != nil {
		b.Fatal(err)
	}
	if !bytes.Equal(data, buf.Bytes()) {
		b.Fatal("legacy parity check failed")
	}
	nodeSet := make(map[string]struct{}, len(doc.Nodes))
	for _, n := range doc.Nodes {
		nodeSet[n.ID] = struct{}{}
	}
	for _, e := range doc.Edges {
		if _, ok := nodeSet[e.SourceID]; !ok {
			b.Fatal("dangling")
		}
		if _, ok := nodeSet[e.TargetID]; !ok {
			b.Fatal("dangling")
		}
	}
}

// currentWriteAndVerify is the shipped path: one verified serialization pass
// plus a streaming digest read-back.
func currentWriteAndVerify(b *testing.B, dir string, g *CodePropertyGraph) {
	b.Helper()
	tm := &AKGTransactionManager{storageDir: dir}
	if err := tm.writeJSONState(g); err != nil {
		b.Fatal(err)
	}
}

func benchCommit(b *testing.B, nodes int, fn func(*testing.B, string, *CodePropertyGraph)) {
	g := buildBenchGraph(nodes)
	dir := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fn(b, dir, g)
	}
}

func BenchmarkCommitVerify_Legacy_5k(b *testing.B)  { benchCommit(b, 5000, legacyWriteAndVerify) }
func BenchmarkCommitVerify_Current_5k(b *testing.B) { benchCommit(b, 5000, currentWriteAndVerify) }

func BenchmarkCommitVerify_Legacy_20k(b *testing.B)  { benchCommit(b, 20000, legacyWriteAndVerify) }
func BenchmarkCommitVerify_Current_20k(b *testing.B) { benchCommit(b, 20000, currentWriteAndVerify) }
