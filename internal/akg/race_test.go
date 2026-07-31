package akg

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/stretchr/testify/assert"
)

func TestConcurrentReadWrite_NoRace(t *testing.T) {
	graph := NewCodePropertyGraph("test")

	// Add 100 nodes
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("node_%d", i)
		graph.Nodes = graph.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "FUNCTION", Name: id})
		existing, _ := graph.KindIndex.Get("FUNCTION")
		newSet := make(map[string]bool, len(existing)+1)
		for k, v := range existing {
			newSet[k] = v
		}
		newSet[id] = true
		graph.KindIndex = graph.KindIndex.Set("FUNCTION", newSet)
	}

	var wg sync.WaitGroup
	// 20 concurrent readers using Safe* methods
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				graph.SafeGetNode("node_0")
				graph.SafeGetOutboundEdges("node_0")
				graph.SafeGetNodesByKind("FUNCTION")
				graph.SafeDetectCycles()
				graph.SafeQuery(stage4.QueryFilter{Kind: "FUNCTION"})
			}
		}()
	}

	wg.Wait()
}

func TestSafeMethods_Basic(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "STRUCT", Name: "A"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "STRUCT", Name: "B"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.KindIndex = g.KindIndex.Set("STRUCT", map[string]bool{"a": true, "b": true})

	node, ok := g.SafeGetNode("a")
	assert.True(t, ok)
	assert.Equal(t, "A", node.Name)

	edges := g.SafeGetOutboundEdges("a")
	assert.Len(t, edges, 1)

	inbound := g.SafeGetInboundEdges("b")
	assert.Len(t, inbound, 1)

	nodes := g.SafeGetNodesByKind("STRUCT")
	assert.Len(t, nodes, 2)

	result := g.SafeDetectCycles()
	assert.Empty(t, result)

	aps := g.SafeFindArticulationPoints()
	assert.Empty(t, aps)

	pr := g.SafeCalculatePageRank(10, 0.85)
	assert.Len(t, pr, 2)

	bc := g.SafeCalculateBetweennessCentrality(false)
	assert.Len(t, bc, 2)

	path := g.SafeFindPath("a", "b", 10)
	assert.Equal(t, []string{"a", "b"}, path)

	pat := g.SafeGetNodesByPattern(stage4.EdgeCalls, "b")
	assert.Len(t, pat, 1)

	q := g.SafeQuery(stage4.QueryFilter{Kind: "STRUCT"})
	assert.Len(t, q, 2)
}
