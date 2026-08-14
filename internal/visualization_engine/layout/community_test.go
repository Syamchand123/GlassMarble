package normalize

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func TestDetectCommunities(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
			"c": {ID: "c"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "b", TargetID: "a", Predicate: "gm:calls"},
			{SourceID: "c", TargetID: "a", Predicate: "gm:calls"},
		},
	}
	communities := DetectCommunities(sub)
	if len(communities) != 3 {
		t.Errorf("expected 3 community entries, got %d", len(communities))
	}
	for id, comm := range communities {
		if comm == "" {
			t.Errorf("node %s has empty community", id)
		}
	}
}

func TestComputeWeightedModularitySimple(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
		},
	}
	communities := ComputeWeightedModularity(sub)
	if len(communities) != 2 {
		t.Errorf("expected 2 community entries, got %d", len(communities))
	}
}

func TestComputeWeightedModularityDisconnected(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
			"c": {ID: "c"},
			"d": {ID: "d"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
			{SourceID: "c", TargetID: "d", Predicate: "gm:calls"},
		},
	}
	communities := ComputeWeightedModularity(sub)
	if len(communities) != 4 {
		t.Errorf("expected 4 community entries, got %d", len(communities))
	}
}

func TestComputeWeightedModularityEdgeWeights(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
			"c": {ID: "c"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:belongsToFile"},
			{SourceID: "b", TargetID: "c", Predicate: "gm:controlFlowTo"},
		},
	}
	communities := ComputeWeightedModularity(sub)
	if len(communities) != 3 {
		t.Errorf("expected 3 community entries, got %d", len(communities))
	}
	_ = communities
}

func TestBuildWeightedAdjacency(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
		},
	}
	adj := buildWeightedAdjacency(sub)
	if len(adj) != 2 {
		t.Errorf("expected 2 adjacency entries, got %d", len(adj))
	}
	if len(adj["a"]) != 1 || adj["a"][0].id != "b" {
		t.Errorf("expected a->b edge, got %v", adj["a"])
	}
	if adj["a"][0].weight != 2.0 {
		t.Errorf("expected weight 2.0 for gm:calls, got %f", adj["a"][0].weight)
	}
}

func TestCommunityEdgeWeight(t *testing.T) {
	tests := []struct {
		pred     string
		expected float64
	}{
		{"gm:belongsToFile", 3.0},
		{"gm:calls", 2.0},
		{"gm:dataFlowTo", 1.0},
		{"gm:controlFlowTo", 0.5},
		{"gm:unknown", 1.0},
	}
	for _, tt := range tests {
		got := communityEdgeWeight(tt.pred)
		if got != tt.expected {
			t.Errorf("communityEdgeWeight(%q) = %f, want %f", tt.pred, got, tt.expected)
		}
	}
}

func TestComputeModularity(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
		},
	}
	adj := buildWeightedAdjacency(sub)
	communities := ComputeWeightedModularity(sub)
	mod := ComputeModularity(communities, adj)
	if mod == 0 {
		t.Log("modularity score computed (may be zero for small graph)")
	}
}

func TestModularityGain(t *testing.T) {
	neighbors := map[string]float64{"comm1": 2.0, "comm2": 1.0}
	best, gain := modularityGain("comm1", neighbors, 10.0, 3.0)
	if best == "" {
		t.Error("expected non-empty best community")
	}
	_ = gain
}
