package akg

import (
	"fmt"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// benchDetachGraph builds a graph the size of a real repository, with the
// property counts nodes actually carry after analysis.
func benchDetachGraph(n int) *CodePropertyGraph {
	g := NewCodePropertyGraph("bench")
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("pkg%d/file%d.go::sym%d", i%40, i%400, i)
		g.Nodes = g.Nodes.Set(id, &link.ResolvedNode{
			ID:   id,
			Kind: "FUNCTION",
			Name: fmt.Sprintf("sym%d", i),
			Properties: map[string]string{
				"content":     "func sym() {}",
				"visibility":  "public",
				"gm:pagerank": "0.0001",
			},
			PrimitiveScores: map[string]float64{"DISK_IO": 0.2, "NETWORK": 0.1},
		})
	}
	return g
}

func benchDetach(b *testing.B, n int) {
	g := benchDetachGraph(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// A fresh graph per iteration sharing the same node map: detaching
		// twice over one graph would measure the second, cheaper pass over
		// already-private maps. CodePropertyGraph carries a mutex, so the
		// struct is rebuilt rather than copied.
		clone := NewCodePropertyGraph("bench")
		clone.Nodes = g.Nodes
		clone.detachNodesForWrite()
	}
}

func BenchmarkDetachNodesForWrite5k(b *testing.B)  { benchDetach(b, 5000) }
func BenchmarkDetachNodesForWrite15k(b *testing.B) { benchDetach(b, 15000) }
