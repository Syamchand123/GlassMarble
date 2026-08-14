package normalize

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func TestNewMetrics(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a": {ID: "a"},
			"b": {ID: "b"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
		},
	}
	metrics := NewMetrics(sub, types.QueryOptions{})
	if metrics == nil {
		t.Fatal("expected non-nil metrics")
	}
	if len(metrics.PageRank) != 2 {
		t.Errorf("expected 2 PageRank entries, got %d", len(metrics.PageRank))
	}
}

func TestNewMetricsEmpty(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{},
		Edges: nil,
	}
	metrics := NewMetrics(sub, types.QueryOptions{})
	if metrics == nil {
		t.Fatal("expected non-nil metrics")
	}
}

func TestBuildFromSubgraph(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a.go::A": {ID: "a.go::A", Name: "A", Kind: "gm:Executable"},
			"b.go::B": {ID: "b.go::B", Name: "B", Kind: "gm:Executable"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a.go::A", TargetID: "b.go::B", Predicate: "gm:calls"},
		},
	}
	tree := BuildFromSubgraph(sub, types.QueryOptions{Scope: types.ScopeGlobal}, types.UMLClass)
	if tree == nil {
		t.Fatal("expected non-nil LayoutTree")
	}
	if tree.Summary == nil {
		t.Fatal("expected non-nil Summary")
	}
	if tree.Summary.NodeCount != 2 {
		t.Errorf("expected NodeCount 2, got %d", tree.Summary.NodeCount)
	}
}

func TestBuildFromSubgraphEmpty(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{},
		Edges: nil,
	}
	tree := BuildFromSubgraph(sub, types.QueryOptions{}, types.UMLClass)
	if tree == nil {
		t.Fatal("expected non-nil LayoutTree for empty subgraph")
	}
	if tree.BoundaryName != "Root" {
		t.Errorf("expected Root boundary, got %s", tree.BoundaryName)
	}
}

func TestMetricsToString(t *testing.T) {
	s := MetricsToString(nil)
	if s != "no metrics" {
		t.Errorf("expected 'no metrics', got '%s'", s)
	}
}

func TestMetricsToStringWithSummary(t *testing.T) {
	metrics := &DiagramMetrics{
		Summary: &types.GraphSummary{
			NodeCount: 5, EdgeCount: 10, Density: 0.5,
			Diameter: 3, AvgPathLength: 1.5, ClusterCount: 2,
			GodObjectCount: 0,
		},
	}
	s := MetricsToString(metrics)
	if s == "no metrics" {
		t.Error("expected formatted string, got 'no metrics'")
	}
}

func TestMetricsToStringWithoutSummary(t *testing.T) {
	metrics := &DiagramMetrics{
		PageRank:    map[string]float64{"a": 0.5},
		Betweenness: map[string]float64{"a": 0.1},
		DegreeIn:    map[string]int{"a": 1},
		DegreeOut:   map[string]int{"a": 0},
		GodObjects:  nil,
		KCore:       map[string]int{"a": 1},
		SCCs:        [][]string{{"a"}},
	}
	s := MetricsToString(metrics)
	if s == "no metrics" {
		t.Error("expected formatted string, got 'no metrics'")
	}
}
