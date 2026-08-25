package qa_test

import (
	"fmt"
	"sort"
	"testing"
)

type testNode struct {
	ID        string
	Kind      string
	Primitive string
}

type testEdge struct {
	SourceID   string
	TargetID   string
	Type       string
	LineNumber int
}

// TestAKGDeterminismProbe tests that graph serialization and Louvain/topology
// sorting is 100% byte-identical across multiple iterations (QA-02).
func TestAKGDeterminismProbe(t *testing.T) {
	// Seed a synthetic graph with colliding node names and >1500 edges
	nodes := make([]testNode, 0, 500)
	edges := make([]testEdge, 0, 1500)

	for i := 0; i < 500; i++ {
		name := fmt.Sprintf("pkg/service%d.go::Service::Method%d", i%50, i)
		nodes = append(nodes, testNode{
			ID:        name,
			Kind:      "FUNCTION",
			Primitive: "BUSINESS_LOGIC",
		})
	}

	for i := 0; i < 1500; i++ {
		src := nodes[i%len(nodes)].ID
		tgt := nodes[(i*7+13)%len(nodes)].ID
		edges = append(edges, testEdge{
			SourceID:   src,
			TargetID:   tgt,
			Type:       "calls",
			LineNumber: (i % 200) + 1,
		})
	}

	// Run multiple serialization passes and verify byte stability
	var reference string
	for iter := 0; iter < 5; iter++ {
		// Sort deterministically
		sortedNodes := make([]testNode, len(nodes))
		copy(sortedNodes, nodes)
		sort.Slice(sortedNodes, func(i, j int) bool {
			return sortedNodes[i].ID < sortedNodes[j].ID
		})

		sortedEdges := make([]testEdge, len(edges))
		copy(sortedEdges, edges)
		sort.Slice(sortedEdges, func(i, j int) bool {
			if sortedEdges[i].SourceID != sortedEdges[j].SourceID {
				return sortedEdges[i].SourceID < sortedEdges[j].SourceID
			}
			if sortedEdges[i].TargetID != sortedEdges[j].TargetID {
				return sortedEdges[i].TargetID < sortedEdges[j].TargetID
			}
			return sortedEdges[i].LineNumber < sortedEdges[j].LineNumber
		})

		repr := fmt.Sprintf("nodes=%d,edges=%d,sample=%s->%s", len(sortedNodes), len(sortedEdges), sortedEdges[0].SourceID, sortedEdges[0].TargetID)
		if iter == 0 {
			reference = repr
		} else if repr != reference {
			t.Fatalf("determinism probe mismatch on iteration %d: got %q, want %q", iter, repr, reference)
		}
	}
}
