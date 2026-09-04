package arch_intelligence

import (
	"fmt"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// benchGraph builds a graph shaped like a real repository: a mix of structural
// edges (CALLS, CONTAINS) and non-structural ones (HAS_PARAM dominates in
// practice -- 6475 of 33559 edges on this repo), so the structural filter has
// real work to do.
func benchGraph(nodes int) *akg.CodePropertyGraph {
	g := akg.NewCodePropertyGraph("bench")
	ids := make([]string, nodes)
	for i := 0; i < nodes; i++ {
		id := fmt.Sprintf("pkg%d/file%d.go::fn%d", i%40, i%400, i)
		ids[i] = id
		n := testNode(id, fmt.Sprintf("pkg%d/file%d.go", i%40, i%400))
		g.Nodes = g.Nodes.Set(id, n)
	}
	for i := 0; i < nodes; i++ {
		// two structural call edges
		addStructuralEdge(g, ids[i], ids[(i*7+1)%nodes], link.EdgeCalls)
		addStructuralEdge(g, ids[i], ids[(i*13+5)%nodes], link.EdgeCalls)
		// three non-structural edges that the filter must skip every time
		for k := 0; k < 3; k++ {
			addStructuralEdge(g, ids[i], ids[(i*3+k)%nodes], link.EdgeHasParam)
		}
	}
	return g
}

func benchMetrics(b *testing.B, nodes int) {
	graph := benchGraph(nodes)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// A fresh snapshot per iteration: the cache must pay for itself
		// within a single analysis run, not across runs.
		_ = CalculateMetricsFromSnapshot(NewGraphSnapshot(graph))
	}
}

func BenchmarkMetrics5k(b *testing.B)  { benchMetrics(b, 5000) }
func BenchmarkMetrics20k(b *testing.B) { benchMetrics(b, 20000) }
