package akg

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestConcurrentDeltaTransactions_NoRace hammers the transaction manager with
// concurrent delta commits and verifies every committed node is visible in the
// final snapshot with no data races (run under -race).
func TestConcurrentDeltaTransactions_NoRace(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)
	defer tm.Close()

	const workers = 8
	const commitsPerWorker = 25

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for c := 0; c < commitsPerWorker; c++ {
				id := fmt.Sprintf("w%d_c%d", worker, c)
				payload := stage4.NewStage4Output("hash")
				payload.GraphNodes[id] = &stage4.ResolvedNode{ID: id, Kind: "FUNCTION", Name: id}
				if err := tm.ExecuteDeltaTransaction(payload, []string{"f.go"}); err != nil {
					t.Errorf("concurrent delta failed: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	snapshot := tm.GetActiveSnapshot()
	for w := 0; w < workers; w++ {
		for c := 0; c < commitsPerWorker; c++ {
			id := fmt.Sprintf("w%d_c%d", w, c)
			node, ok := snapshot.GetNode(id)
			if !ok || node.Name != id {
				t.Errorf("expected committed node %s in final snapshot (ok=%v)", id, ok)
			}
		}
	}
}

// TestConcurrentReadersWithReasonerCommits_NoRace exercises the exact scenario
// that used to race: the reasoner (RunTopologicalMacroInference) writes derived
// metrics into node.Properties during a delta commit while concurrent readers
// (SafeQuery / akgbridge pattern) read the active snapshot. Before the
// detachNodesForWrite fix, unchanged nodes were SHARED pointers between the
// shadow and the active graph, so the reasoner mutated maps concurrently
// readers were iterating. Run under -race.
func TestConcurrentReadersWithReasonerCommits_NoRace(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)
	defer tm.Close()

	// Seed a base graph so the shadow shares unchanged nodes with the active
	// snapshot (the pre-fix race condition).
	base := stage4.NewStage4Output("base")
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("svc_%d", i)
		base.GraphNodes[id] = &stage4.ResolvedNode{ID: id, Kind: "CLASS", Name: "Service" + fmt.Sprint(i)}
		base.OutboundEdges[id] = []stage4.ResolvedEdge{{SourceID: id, TargetID: "svc_0", Type: stage4.EdgeCalls, LineNumber: i}}
	}
	require.NoError(t, tm.ExecuteDeltaTransaction(base, []string{"base.go"}))

	var wg sync.WaitGroup
	var start chan struct{}
	start = make(chan struct{})
	close(start)

	// Concurrent readers hammering the active snapshot (reads Properties).
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 500; j++ {
				snap := tm.GetActiveSnapshot()
				snap.SafeQuery(stage4.QueryFilter{Kind: "CLASS"})
				snap.SafeQuery(stage4.QueryFilter{PropertyRegex: map[string]string{"pagerank": ".+"}})
				if n, ok := snap.SafeGetNode("svc_0"); ok {
					_ = n.Properties["macro_rules"]
				}
				snap.SafeCalculatePageRank(10, 0.85)
				snap.SafeCalculateBetweennessCentrality(false)
			}
		}()
	}

	// Concurrent delta commits that trigger reasoner property writes.
	for c := 0; c < 20; c++ {
		wg.Add(1)
		go func(commit int) {
			defer wg.Done()
			<-start
			payload := stage4.NewStage4Output("hash")
			id := fmt.Sprintf("new_svc_%d", commit)
			payload.GraphNodes[id] = &stage4.ResolvedNode{ID: id, Kind: "CLASS", Name: "NewService" + fmt.Sprint(commit)}
			payload.OutboundEdges[id] = []stage4.ResolvedEdge{{SourceID: id, TargetID: "svc_0", Type: stage4.EdgeCalls, LineNumber: commit}}
			if err := tm.ExecuteDeltaTransaction(payload, []string{"new.go"}); err != nil {
				t.Errorf("concurrent delta failed: %v", err)
			}
		}(c)
	}

	wg.Wait()
}
