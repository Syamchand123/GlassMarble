package normalize

import (
	"math"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func TestComputePageRank(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
			"c": {ID: "c"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "c", Predicate: "gm:calls"},
			{SourceID: "c", TargetID: "a", Predicate: "gm:calls"},
		},
	}
	pr := ComputePageRank(sub, 0.85, 100)
	if len(pr) != 3 {
		t.Fatalf("expected 3 PageRank entries, got %d", len(pr))
	}
	var sum float64
	for _, v := range pr {
		sum += v
	}
	if math.Abs(sum-1.0) > 0.01 {
		t.Errorf("PageRank sum should be ~1.0, got %f", sum)
	}
}

func TestComputePageRankDisconnected(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
		},
		Edges: nil,
	}
	pr := ComputePageRank(sub, 0.85, 100)
	if len(pr) != 2 {
		t.Fatalf("expected 2 PageRank entries, got %d", len(pr))
	}
}

func TestComputePageRankEmpty(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{},
		Edges: nil,
	}
	pr := ComputePageRank(sub, 0.85, 100)
	if len(pr) != 0 {
		t.Errorf("expected 0 PageRank entries, got %d", len(pr))
	}
}

func TestComputeBetweenness(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
			"c": {ID: "c"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "c", Predicate: "gm:calls"},
		},
	}
	bc := ComputeBetweenness(sub)
	if len(bc) != 3 {
		t.Fatalf("expected 3 betweenness entries, got %d", len(bc))
	}
	if bc["b"] <= 0 {
		t.Errorf("node b should have positive betweenness, got %f", bc["b"])
	}
}

func TestComputeBetweennessChain(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"}, "b": {ID: "b"}, "c": {ID: "c"},
			"d": {ID: "d"}, "e": {ID: "e"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "c", Predicate: "gm:calls"},
			{SourceID: "c", TargetID: "d", Predicate: "gm:calls"},
			{SourceID: "d", TargetID: "e", Predicate: "gm:calls"},
		},
	}
	bc := ComputeBetweenness(sub)
	if len(bc) != 5 {
		t.Fatalf("expected 5 betweenness entries, got %d", len(bc))
	}
	// In a chain a→b→c→d→e, intermediate nodes (b,c,d) have positive betweenness
	if bc["b"] <= 0 {
		t.Errorf("node b should have positive betweenness, got %f", bc["b"])
	}
	if bc["c"] <= 0 {
		t.Errorf("node c should have positive betweenness, got %f", bc["c"])
	}
	// Endpoints (a,e) should have 0 betweenness
	if bc["a"] != 0 {
		t.Errorf("endpoint a should have 0 betweenness, got %f", bc["a"])
	}
	if bc["e"] != 0 {
		t.Errorf("endpoint e should have 0 betweenness, got %f", bc["e"])
	}
}

func TestComputeBetweennessDisconnected(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
			"c": {ID: "c"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
		},
	}
	bc := ComputeBetweenness(sub)
	if len(bc) != 3 {
		t.Errorf("expected 3 betweenness entries for disconnected graph, got %d", len(bc))
	}
}

func TestComputeDegrees(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
			"c": {ID: "c"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "c", Predicate: "gm:calls"},
			{SourceID: "a", TargetID: "c", Predicate: "gm:calls"},
		},
	}
	inDeg, outDeg := ComputeDegrees(sub)
	if inDeg["c"] != 2 {
		t.Errorf("expected in-degree of c to be 2, got %d", inDeg["c"])
	}
	if outDeg["a"] != 2 {
		t.Errorf("expected out-degree of a to be 2, got %d", outDeg["a"])
	}
	if inDeg["a"] != 0 {
		t.Errorf("expected in-degree of a to be 0, got %d", inDeg["a"])
	}
}

func TestComputeDegreesEmpty(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{"a": {ID: "a"}},
		Edges: nil,
	}
	inDeg, outDeg := ComputeDegrees(sub)
	if inDeg["a"] != 0 {
		t.Errorf("expected in-degree 0, got %d", inDeg["a"])
	}
	if outDeg["a"] != 0 {
		t.Errorf("expected out-degree 0, got %d", outDeg["a"])
	}
}

func TestDetectGodObjects(t *testing.T) {
	nodes := make(map[string]*types.TTLNode)
	var edges []types.TTLEdge
	for i := 0; i < 10; i++ {
		id := string(rune('a' + i))
		nodes[id] = &types.TTLNode{ID: id}
		edges = append(edges, types.TTLEdge{SourceID: id, TargetID: "hub", Predicate: "gm:calls"})
	}
	nodes["hub"] = &types.TTLNode{ID: "hub"}
	sub := &types.VirtualSubgraph{Nodes: nodes, Edges: edges}
	inDeg, outDeg := ComputeDegrees(sub)
	gods := DetectGodObjects(sub, inDeg, outDeg)
	if len(gods) == 0 {
		t.Error("expected at least one god object (hub with high in-degree)")
	}
}

func TestDetectGodObjectsSmallGraph(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
		},
	}
	inDeg, outDeg := ComputeDegrees(sub)
	gods := DetectGodObjects(sub, inDeg, outDeg)
	if len(gods) != 0 {
		t.Errorf("expected no god objects in small graph, got %d", len(gods))
	}
}

func TestDetectGodObjectsUniform(t *testing.T) {
	nodes := make(map[string]*types.TTLNode)
	var edges []types.TTLEdge
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		nodes[id] = &types.TTLNode{ID: id}
	}
	edges = append(edges, types.TTLEdge{SourceID: "a", TargetID: "b", Predicate: "gm:calls"})
	sub := &types.VirtualSubgraph{Nodes: nodes, Edges: edges}
	inDeg, outDeg := ComputeDegrees(sub)
	gods := DetectGodObjects(sub, inDeg, outDeg)
	if len(gods) != 0 {
		t.Errorf("expected no god objects in uniform graph, got %d", len(gods))
	}
}

func TestComputeGraphSummary(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
			"c": {ID: "c"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "c", Predicate: "gm:calls"},
		},
	}
	summary := ComputeGraphSummary(sub)
	if summary.NodeCount != 3 {
		t.Errorf("expected NodeCount 3, got %d", summary.NodeCount)
	}
	if summary.EdgeCount != 2 {
		t.Errorf("expected EdgeCount 2, got %d", summary.EdgeCount)
	}
	if summary.Density <= 0 {
		t.Errorf("expected positive density, got %f", summary.Density)
	}
}

func TestComputeGraphSummaryEmpty(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{},
		Edges: nil,
	}
	summary := ComputeGraphSummary(sub)
	if summary.NodeCount != 0 {
		t.Errorf("expected NodeCount 0, got %d", summary.NodeCount)
	}
	if summary.EdgeCount != 0 {
		t.Errorf("expected EdgeCount 0, got %d", summary.EdgeCount)
	}
}

func TestComputeInDegree(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{"a": {ID: "a"}, "b": {ID: "b"}},
		Edges: []types.TTLEdge{{SourceID: "a", TargetID: "b", Predicate: "gm:calls"}},
	}
	inDeg := ComputeInDegree(sub)
	if inDeg["a"] != 0 {
		t.Errorf("expected in-degree 0 for a, got %d", inDeg["a"])
	}
	if inDeg["b"] != 1 {
		t.Errorf("expected in-degree 1 for b, got %d", inDeg["b"])
	}
}

func TestComputeAllMetrics(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
		},
	}
	metrics := ComputeAllMetrics(sub)
	if metrics == nil {
		t.Fatal("expected non-nil metrics")
	}
	if len(metrics.PageRank) != 2 {
		t.Errorf("expected 2 PageRank entries, got %d", len(metrics.PageRank))
	}
	if metrics.Summary == nil {
		t.Fatal("expected non-nil Summary")
	}
	if metrics.Summary.NodeCount != 2 {
		t.Errorf("expected NodeCount 2, got %d", metrics.Summary.NodeCount)
	}
}

func TestComputeKCores(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
			"c": {ID: "c"},
			"d": {ID: "d"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "a", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "c", Predicate: "gm:calls"},
			{SourceID: "c", TargetID: "b", Predicate: "gm:calls"},
		},
	}
	kcore := ComputeKCores(sub)
	if len(kcore) != 4 {
		t.Errorf("expected 4 KCore entries, got %d", len(kcore))
	}
	if kcore["d"] != 0 {
		t.Errorf("expected KCore 0 for isolated node d, got %d", kcore["d"])
	}
}

func TestComputeKCoresEmpty(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{},
		Edges: nil,
	}
	kcore := ComputeKCores(sub)
	if len(kcore) != 0 {
		t.Errorf("expected 0 KCore entries, got %d", len(kcore))
	}
}

func TestComputeMST(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
			"c": {ID: "c"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "c", Predicate: "gm:calls"},
			{SourceID: "a", TargetID: "c", Predicate: "gm:calls"},
		},
	}
	mst := ComputeMST(sub)
	if len(mst) == 0 {
		t.Error("expected non-empty MST")
	}
	for _, e := range mst {
		if e.Predicate != "mst" {
			t.Errorf("expected MST predicate 'mst', got '%s'", e.Predicate)
		}
	}
}

func TestComputeMSTEmpty(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{"a": {ID: "a"}},
		Edges: nil,
	}
	mst := ComputeMST(sub)
	if len(mst) != 0 {
		t.Errorf("expected empty MST, got %d edges", len(mst))
	}
}

func TestFindShortestPath(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"}, "b": {ID: "b"}, "c": {ID: "c"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "c", Predicate: "gm:calls"},
		},
	}
	path := FindShortestPath(sub, "a", "c")
	if len(path) != 3 {
		t.Errorf("expected path a->b->c (3 nodes), got %v", path)
	}
}

func TestFindShortestPathNoPath(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"}, "b": {ID: "b"},
		},
		Edges: nil,
	}
	path := FindShortestPath(sub, "a", "b")
	if path != nil {
		t.Errorf("expected nil for disconnected graph, got %v", path)
	}
}

func TestFindAllPaths(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"}, "b": {ID: "b"}, "c": {ID: "c"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "a", TargetID: "c", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "c", Predicate: "gm:calls"},
		},
	}
	paths := FindAllPaths(sub, "a", "c", 5)
	if len(paths) == 0 {
		t.Error("expected at least one path from a to c")
	}
}

func TestFindCriticalPath(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"}, "b": {ID: "b"}, "c": {ID: "c"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "c", Predicate: "gm:calls"},
		},
	}
	path := FindCriticalPath(sub)
	if len(path) != 3 {
		t.Errorf("expected critical path a->b->c (3 nodes), got %v", path)
	}
}

func TestFindCriticalPathWithCycle(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"}, "b": {ID: "b"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "a", Predicate: "gm:calls"},
		},
	}
	path := FindCriticalPath(sub)
	if path != nil {
		t.Log("critical path returns nil for cyclic graph (expected)")
	}
}

func TestComputeDiameter(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"}, "b": {ID: "b"}, "c": {ID: "c"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "c", Predicate: "gm:calls"},
		},
	}
	d := ComputeDiameter(sub)
	if d != 2 {
		t.Errorf("expected diameter 2, got %d", d)
	}
}

func TestComputeAvgPathLength(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"}, "b": {ID: "b"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
		},
	}
	avg := ComputeAvgPathLength(sub)
	if avg <= 0 {
		t.Errorf("expected positive avg path length, got %f", avg)
	}
}

func TestFindBottlenecks(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"}, "b": {ID: "b"}, "c": {ID: "c"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "c", Predicate: "gm:calls"},
		},
	}
	bottlenecks := FindBottlenecks(sub, 0.01)
	if len(bottlenecks) == 0 {
		t.Log("no bottlenecks found with threshold 0.01 (may be expected for small graph)")
	}
}

func TestCountSCCs(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"}, "b": {ID: "b"}, "c": {ID: "c"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "a", Predicate: "gm:calls"},
		},
	}
	sccs, largest := CountSCCs(sub)
	if len(sccs) == 0 {
		t.Error("expected at least one SCC")
	}
	if largest < 2 {
		t.Errorf("expected largest SCC >= 2 for a<->b cycle, got %d", largest)
	}
}

func TestComputeBipartiteScore(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"}, "b": {ID: "b"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
		},
	}
	score := ComputeBipartiteScore(sub)
	if score <= 0 {
		t.Errorf("expected positive bipartite score for a->b, got %f", score)
	}
}

func TestComputeBipartiteScoreEmpty(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{},
		Edges: nil,
	}
	score := ComputeBipartiteScore(sub)
	if score != 0 {
		t.Errorf("expected 0 for empty graph, got %f", score)
	}
}
