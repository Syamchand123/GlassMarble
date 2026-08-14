package arch_intelligence

import (
	"math"
	"reflect"
	"sort"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

func addTestNode(graph *akg.CodePropertyGraph, id, kind, name string) {
	graph.Nodes = graph.Nodes.Set(id, &link.ResolvedNode{ID: id, Kind: kind, Name: name})
}

func addTestEdge(graph *akg.CodePropertyGraph, src, tgt string, typ link.RelationshipType) {
	edges, _ := graph.OutboundEdges.Get(src)
	edges = append(edges, link.ResolvedEdge{SourceID: src, TargetID: tgt, Type: typ})
	graph.OutboundEdges = graph.OutboundEdges.Set(src, edges)

	inb, _ := graph.InboundEdges.Get(tgt)
	inb = append(inb, link.ResolvedEdge{SourceID: src, TargetID: tgt, Type: typ})
	graph.InboundEdges = graph.InboundEdges.Set(tgt, inb)
}

// addTestClique connects all unordered pairs of ids with CALLS edges (one
// direction each — buildLouvainGraph symmetrizes).
func addTestClique(graph *akg.CodePropertyGraph, ids ...string) {
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			addTestEdge(graph, ids[i], ids[j], link.EdgeCalls)
		}
	}
}

func TestSCCIterative_Cycle(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	for _, id := range []string{"a", "b", "c", "d"} {
		addTestNode(graph, id, "FUNCTION", id)
	}
	addTestEdge(graph, "a", "b", link.EdgeCalls)
	addTestEdge(graph, "b", "c", link.EdgeCalls)
	addTestEdge(graph, "c", "a", link.EdgeCalls)

	snap := NewGraphSnapshot(graph)
	first := SCCIterative(snap)
	second := SCCIterative(snap)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("SCCIterative is not deterministic:\n1: %v\n2: %v", first, second)
	}

	cyclicComps := 0
	singletons := 0
	var cycleMembers []string
	for _, comp := range first {
		if len(comp) > 1 {
			cyclicComps++
			cycleMembers = append(cycleMembers, comp...)
		} else {
			singletons++
		}
	}
	if cyclicComps != 1 {
		t.Fatalf("expected exactly 1 non-singleton SCC, got %d (%v)", cyclicComps, first)
	}
	sort.Strings(cycleMembers)
	if !reflect.DeepEqual(cycleMembers, []string{"a", "b", "c"}) {
		t.Fatalf("expected cycle SCC to contain a,b,c, got %v", cycleMembers)
	}
	if singletons != 1 {
		t.Fatalf("expected 1 singleton SCC (isolated d), got %d (%v)", singletons, first)
	}
}

func TestSCCIterative_NoCycle(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	for _, id := range []string{"a", "b", "c"} {
		addTestNode(graph, id, "FUNCTION", id)
	}
	addTestEdge(graph, "a", "b", link.EdgeCalls)
	addTestEdge(graph, "b", "c", link.EdgeCalls)

	snap := NewGraphSnapshot(graph)
	sccs := SCCIterative(snap)
	if len(sccs) != 3 {
		t.Fatalf("expected 3 SCCs, got %v", sccs)
	}
	for _, comp := range sccs {
		if len(comp) != 1 {
			t.Fatalf("expected all singleton SCCs, got %v", sccs)
		}
	}

	metrics := CalculateMetricsFromSnapshot(snap)
	if metrics.CycleCount != 0 {
		t.Fatalf("expected CycleCount 0, got %d", metrics.CycleCount)
	}
	if metrics.StronglyConnectedComponents != 3 {
		t.Fatalf("expected 3 SCCs in metrics, got %d", metrics.StronglyConnectedComponents)
	}
}

func TestSCCIterative_DanglingEdge(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	addTestNode(graph, "a", "FUNCTION", "a")
	// Target "missing" is not registered in Nodes — must be ignored, not panic.
	edges, _ := graph.OutboundEdges.Get("a")
	edges = append(edges, link.ResolvedEdge{SourceID: "a", TargetID: "missing", Type: link.EdgeCalls})
	graph.OutboundEdges = graph.OutboundEdges.Set("a", edges)

	sccs := SCCIterative(NewGraphSnapshot(graph))
	if len(sccs) != 1 || len(sccs[0]) != 1 || sccs[0][0] != "a" {
		t.Fatalf("expected dangling edge ignored and SCC of a alone, got %v", sccs)
	}
}

func TestPageRankSnapshot_Normalized(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	for _, id := range []string{"a", "b", "c"} {
		addTestNode(graph, id, "FUNCTION", id)
	}
	addTestEdge(graph, "a", "b", link.EdgeCalls) // c is dangling

	snap := NewGraphSnapshot(graph)
	ranks := PageRankSnapshot(snap, 20, 0.85)
	if len(ranks) != 3 {
		t.Fatalf("expected 3 ranks, got %v", ranks)
	}
	var total float64
	for _, id := range snap.NodeIDs {
		total += ranks[id]
	}
	if math.Abs(total-1.0) > 1e-9 {
		t.Fatalf("ranks must sum to 1.0, got %v (sum=%v)", ranks, total)
	}
	for _, id := range snap.NodeIDs {
		if ranks[id] <= 0 {
			t.Fatalf("expected positive rank for %s, got %v", id, ranks[id])
		}
	}

	// Single-node graph returns the singleton with rank 1.0.
	solo := akg.NewCodePropertyGraph("test")
	addTestNode(solo, "n1", "FUNCTION", "n1")
	soloRanks := PageRankSnapshot(NewGraphSnapshot(solo), 10, 0.85)
	if len(soloRanks) != 1 || math.Abs(soloRanks["n1"]-1.0) > 1e-9 {
		t.Fatalf("expected single-node rank 1.0, got %v", soloRanks)
	}
}

func TestPageRankSnapshot_Dangling(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	for _, id := range []string{"a", "b", "c"} {
		addTestNode(graph, id, "FUNCTION", id)
	}
	addTestEdge(graph, "a", "b", link.EdgeCalls)
	addTestEdge(graph, "b", "c", link.EdgeCalls) // c is the dangling sink

	ranks := PageRankSnapshot(NewGraphSnapshot(graph), 20, 0.85)
	// Chain a->b->c: every node feeds the next and the dangling sink c keeps
	// the mass it receives (plus its dangling share), so the correct ordering
	// is rank(c) > rank(b) > rank(a) — strictly. NOTE: the draft brief asked
	// for rank(b) > rank(c), but a sink node collects rank, it does not lose
	// it; the implementation matches standard PageRank here.
	if !(ranks["c"] > ranks["b"] && ranks["b"] > ranks["a"]) {
		t.Fatalf("expected rank(c) > rank(b) > rank(a), got %v", ranks)
	}
}

func TestLCOM4Snapshot(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("C", &link.ResolvedNode{ID: "C", Kind: "CLASS", Name: "C"})
	for _, f := range []string{"f1", "f2", "f3"} {
		graph.Nodes = graph.Nodes.Set(f, &link.ResolvedNode{ID: f, Kind: "FIELD", Name: f})
	}
	for _, m := range []string{"m1", "m2", "m3"} {
		graph.Nodes = graph.Nodes.Set(m, &link.ResolvedNode{ID: m, Kind: "FUNCTION", Name: m})
	}
	for _, f := range []string{"f1", "f2", "f3"} {
		addTestEdge(graph, "C", f, link.EdgeHasField)
	}
	for _, m := range []string{"m1", "m2", "m3"} {
		addTestEdge(graph, "C", m, link.EdgeHasReceiver)
	}
	// m1 and m2 both access f1; m3 accesses no field.
	addTestEdge(graph, "m1", "f1", link.EdgeReferences)
	addTestEdge(graph, "m2", "f1", link.EdgeReferences)

	snap := NewGraphSnapshot(graph)
	if got := LCOM4Snapshot(snap.Nodes["C"], snap); got != 2 {
		t.Fatalf("expected LCOM4=2 ({m1,m2} via f1; m3 isolated), got %v", got)
	}

	// Class with fields but no methods -> 0.
	empty := akg.NewCodePropertyGraph("test")
	empty.Nodes = empty.Nodes.Set("C2", &link.ResolvedNode{ID: "C2", Kind: "CLASS", Name: "C2"})
	empty.Nodes = empty.Nodes.Set("fx", &link.ResolvedNode{ID: "fx", Kind: "FIELD", Name: "fx"})
	addTestEdge(empty, "C2", "fx", link.EdgeHasField)
	emptySnap := NewGraphSnapshot(empty)
	if got := LCOM4Snapshot(emptySnap.Nodes["C2"], emptySnap); got != 0 {
		t.Fatalf("expected LCOM4=0 for class without methods, got %v", got)
	}
}

func TestDeadCodeNodesSnapshot(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	addTestNode(graph, "main", "FUNCTION", "main")
	addTestNode(graph, "a", "FUNCTION", "a")
	addTestNode(graph, "b", "FUNCTION", "b")
	addTestNode(graph, "c", "FUNCTION", "c")
	addTestNode(graph, "Exported", "FUNCTION", "Exported")
	graph.Nodes = graph.Nodes.Set("testFn", &link.ResolvedNode{
		ID: "testFn", Kind: "FUNCTION", Name: "testFn",
		FileSpec: link.LocationMeta{Path: "pkg/x_test.go"},
	})
	addTestEdge(graph, "main", "a", link.EdgeCalls)
	addTestEdge(graph, "a", "b", link.EdgeCalls)
	graph.Entrypoints = []string{"main"}

	dead := DeadCodeNodesSnapshot(NewGraphSnapshot(graph))
	if len(dead) != 1 || dead[0] != "c" {
		t.Fatalf("expected exactly [c] as dead (c unreachable, Exported is API surface, testFn is test code), got %v", dead)
	}

	// No entrypoints -> empty result (no evidence of deadness).
	noEP := akg.NewCodePropertyGraph("test")
	for _, id := range []string{"main", "c"} {
		addTestNode(noEP, id, "FUNCTION", id)
	}
	if dead := DeadCodeNodesSnapshot(NewGraphSnapshot(noEP)); len(dead) != 0 {
		t.Fatalf("expected empty dead set with no entrypoints, got %v", dead)
	}
}

func TestCyclomaticComplexitySnapshot(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("f", &link.ResolvedNode{ID: "f", Kind: "FUNCTION", Name: "f"})
	graph.Nodes = graph.Nodes.Set("s", &link.ResolvedNode{ID: "s", Kind: "STRUCT", Name: "s"})
	for _, id := range []string{"b1", "b2", "b3"} {
		graph.Nodes = graph.Nodes.Set(id, &link.ResolvedNode{ID: id, Kind: "CFG_BRANCH", Name: id})
	}
	addTestEdge(graph, "f", "b1", link.EdgeConditionalBranch)
	addTestEdge(graph, "f", "b2", link.EdgeLoopBranch)
	addTestEdge(graph, "s", "b3", link.EdgeConditionalBranch) // non-function must be ignored

	cc := CyclomaticComplexitySnapshot(NewGraphSnapshot(graph))
	if len(cc) != 1 {
		t.Fatalf("expected complexity for exactly the function node, got %v", cc)
	}
	if cc["f"] != 3 {
		t.Fatalf("expected complexity 3 (1 + conditional + loop), got %d", cc["f"])
	}
}

func TestLouvainCommunityDetectionSnapshot_Deterministic(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	cliqueA := []string{"a1", "a2", "a3", "a4"}
	cliqueB := []string{"b1", "b2", "b3", "b4"}
	all := append(append([]string{}, cliqueA...), cliqueB...)
	for _, id := range all {
		addTestNode(graph, id, "FUNCTION", id)
	}
	addTestClique(graph, cliqueA...)
	addTestClique(graph, cliqueB...)
	addTestEdge(graph, "a1", "b1", link.EdgeCalls) // single bridge edge

	snap := NewGraphSnapshot(graph)
	c1 := LouvainCommunityDetectionSnapshot(snap, 4, 10)
	c2 := LouvainCommunityDetectionSnapshot(snap, 4, 10)
	if !reflect.DeepEqual(c1, c2) {
		t.Fatalf("Louvain is not deterministic:\n1: %v\n2: %v", c1, c2)
	}

	comm := func(id string) string { return c1[id] }
	for _, id := range cliqueA {
		if comm(id) != comm("a1") {
			t.Fatalf("clique A nodes must share one community, got %v", c1)
		}
	}
	for _, id := range cliqueB {
		if comm(id) != comm("b1") {
			t.Fatalf("clique B nodes must share one community, got %v", c1)
		}
	}
	if comm("a1") == comm("b1") {
		t.Fatalf("the two cliques must be distinct communities, got %v", c1)
	}
	seen := make(map[string]bool)
	for _, c := range c1 {
		seen[c] = true
	}
	if len(seen) != 2 {
		t.Fatalf("expected exactly 2 communities, got %v", c1)
	}
}

func TestLouvainCommunityDetectionSnapshot_Empty(t *testing.T) {
	empty := NewGraphSnapshot(akg.NewCodePropertyGraph("test"))
	if got := LouvainCommunityDetectionSnapshot(empty, 4, 10); len(got) != 0 {
		t.Fatalf("expected empty community map for empty graph, got %v", got)
	}

	single := akg.NewCodePropertyGraph("test")
	addTestNode(single, "n1", "FUNCTION", "n1")
	got := LouvainCommunityDetectionSnapshot(NewGraphSnapshot(single), 4, 10)
	if len(got) != 1 || got["n1"] != "0" {
		t.Fatalf("expected {\"n1\":\"0\"}, got %v", got)
	}
}

func TestLouvainCommunityDetection_SingleNode(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	addTestNode(graph, "n1", "FUNCTION", "n1")
	got := LouvainCommunityDetection(graph)
	if len(got) != 1 || got["n1"] != "0" {
		t.Fatalf("expected {\"n1\":\"0\"}, got %v", got)
	}
}
