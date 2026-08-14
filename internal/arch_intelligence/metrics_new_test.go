package arch_intelligence

import (
	"reflect"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

func TestComputeComponentCoupling(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	for _, id := range []string{"a1", "a2", "b1", "c1"} {
		addTestNode(graph, id, "FUNCTION", id)
	}
	addTestEdge(graph, "a1", "b1", link.EdgeCalls)
	addTestEdge(graph, "a2", "c1", link.EdgeDependsOn)

	components := []archmodel.DetectedComponent{
		{ID: "comp_a", Name: "A", NodeIDs: []string{"a1", "a2"}},
		{ID: "comp_b", Name: "B", NodeIDs: []string{"b1"}},
		{ID: "comp_c", Name: "C", NodeIDs: []string{"c1"}},
	}
	couplings, ca, ce, instability := ComputeComponentCoupling(NewGraphSnapshot(graph), components)

	if ca != 2 || ce != 2 || instability != 0.5 {
		t.Fatalf("expected global ca=2 ce=2 instability=0.5, got ca=%v ce=%v inst=%v", ca, ce, instability)
	}
	if len(couplings) != 3 {
		t.Fatalf("expected 3 couplings, got %v", couplings)
	}
	for i, want := range []string{"comp_a", "comp_b", "comp_c"} {
		if couplings[i].ComponentID != want {
			t.Fatalf("expected couplings sorted by ID (%s at index %d), got %v", want, i, couplings)
		}
	}
	byID := make(map[string]ComponentCoupling, len(couplings))
	for _, c := range couplings {
		byID[c.ComponentID] = c
	}
	a := byID["comp_a"]
	if a.Ca != 0 || a.Ce != 2 || a.Instability != 1.0 || a.Weight != 2 {
		t.Fatalf("comp_a: expected Ca=0 Ce=2 Instability=1.0 Weight=2, got %+v", a)
	}
	b := byID["comp_b"]
	if b.Ca != 1 || b.Ce != 0 || b.Instability != 0.0 || b.Weight != 1 {
		t.Fatalf("comp_b: expected Ca=1 Ce=0 Instability=0 Weight=1, got %+v", b)
	}
	c := byID["comp_c"]
	if c.Ca != 1 || c.Ce != 0 || c.Instability != 0.0 || c.Weight != 1 {
		t.Fatalf("comp_c: expected Ca=1 Ce=0 Instability=0 Weight=1, got %+v", c)
	}
}

func TestComputeComponentCoupling_NoCrossEdges(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	for _, id := range []string{"a1", "a2", "b1"} {
		addTestNode(graph, id, "FUNCTION", id)
	}
	addTestEdge(graph, "a1", "a2", link.EdgeCalls) // intra-component only

	components := []archmodel.DetectedComponent{
		{ID: "comp_a", Name: "A", NodeIDs: []string{"a1", "a2"}},
		{ID: "comp_b", Name: "B", NodeIDs: []string{"b1"}},
	}
	couplings, ca, ce, instability := ComputeComponentCoupling(NewGraphSnapshot(graph), components)
	if ca != 0 || ce != 0 || instability != 0.0 {
		t.Fatalf("expected zero global coupling, got ca=%v ce=%v inst=%v", ca, ce, instability)
	}
	for _, c := range couplings {
		if c.Ca != 0 || c.Ce != 0 || c.Instability != 0.0 {
			t.Fatalf("expected zero coupling for %s, got %+v", c.ComponentID, c)
		}
	}
}

func TestCalculateMetricsFromSnapshot_Empty(t *testing.T) {
	snap := NewGraphSnapshot(akg.NewCodePropertyGraph("test"))
	m := CalculateMetricsFromSnapshot(snap)
	if m.TotalNodes != 0 || m.TotalEdges != 0 || m.GraphDensity != 0 {
		t.Fatalf("expected all-zero size metrics, got %+v", m)
	}
	if m.StronglyConnectedComponents != 0 || m.CycleCount != 0 {
		t.Fatalf("expected no SCCs/cycles on empty graph, got %+v", m)
	}
}

func TestCalculateMetricsFromSnapshot_SmallGraph(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	for _, id := range []string{"a", "b", "c"} {
		addTestNode(graph, id, "FUNCTION", id)
	}
	addTestEdge(graph, "a", "b", link.EdgeCalls)
	addTestEdge(graph, "b", "c", link.EdgeCalls)

	m := CalculateMetricsFromSnapshot(NewGraphSnapshot(graph))
	if m.TotalNodes != 3 {
		t.Errorf("TotalNodes: got %d, want 3", m.TotalNodes)
	}
	// NOTE: the brief said TotalEdges=3, but a 3-node chain a->b->c has
	// exactly 2 edges (EdgeCount sums the outbound adjacency lists). The
	// correct value for this graph is 2 — see final report.
	if m.TotalEdges != 2 {
		t.Errorf("TotalEdges: got %d, want 2", m.TotalEdges)
	}
	if m.MaxFanIn != 1 || m.MaxFanOut != 1 {
		t.Errorf("fan metrics: got MaxFanIn=%d MaxFanOut=%d, want 1/1", m.MaxFanIn, m.MaxFanOut)
	}
	if m.CycleCount != 0 {
		t.Errorf("CycleCount: got %d, want 0", m.CycleCount)
	}
	if m.StronglyConnectedComponents != 3 {
		t.Errorf("StronglyConnectedComponents: got %d, want 3", m.StronglyConnectedComponents)
	}
}

func TestCalculateMetrics_Compatibility(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	for _, id := range []string{"a", "b", "c"} {
		addTestNode(graph, id, "FUNCTION", id)
	}
	addTestEdge(graph, "a", "b", link.EdgeCalls)
	addTestEdge(graph, "b", "c", link.EdgeCalls)

	fromGraph := CalculateMetrics(graph)
	fromSnapshot := CalculateMetricsFromSnapshot(NewGraphSnapshot(graph))
	if !reflect.DeepEqual(fromGraph, fromSnapshot) {
		t.Fatalf("CalculateMetrics(graph) must match CalculateMetricsFromSnapshot:\ngot  %+v\nwant %+v", fromGraph, fromSnapshot)
	}
}
