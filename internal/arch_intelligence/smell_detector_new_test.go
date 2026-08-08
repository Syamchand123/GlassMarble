package arch_intelligence

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/config"
)

// smellContext builds a RuleContext from a graph with default config.
func smellContext(graph *akg.CodePropertyGraph) *RuleContext {
	return &RuleContext{
		Graph: NewGraphSnapshot(graph),
		Cfg:   config.DefaultIntelligenceConfig(),
		Clock: testClock,
	}
}

func TestSD01GodObject(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("god", &stage4.ResolvedNode{ID: "god", Name: "GodClass", Kind: "STRUCT"})
	// 20 distinct callers -> fan-in 20 > 15.
	for i := 0; i < 20; i++ {
		id := string(rune('A' + i))
		graph.Nodes = graph.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Name: "Caller" + id})
		addStructuralEdge(graph, id, "god", stage4.EdgeCalls)
	}
	// 35 methods -> method count 35 > 30.
	for i := 0; i < 35; i++ {
		m := "m" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		graph.Nodes = graph.Nodes.Set(m, &stage4.ResolvedNode{ID: m, Kind: "FUNCTION"})
		addStructuralEdge(graph, "god", m, stage4.EdgeHasReceiver)
	}

	smells := RunSmellDetectionContext(smellContext(graph))
	found := false
	for _, s := range smells {
		if s.Kind == archmodel.SmellGodObject {
			found = true
			if s.Severity != archmodel.SeverityHigh {
				t.Errorf("expected HIGH severity, got %s", s.Severity)
			}
			if len(s.AffectedIDs) != 1 || s.AffectedIDs[0] != "god" {
				t.Errorf("expected affected god node, got %v", s.AffectedIDs)
			}
		}
	}
	if !found {
		t.Error("expected SD-01 god object smell")
	}
}

func TestSD01GodObject_NotDetected(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("god", &stage4.ResolvedNode{ID: "god", Name: "SmallClass", Kind: "STRUCT"})
	for i := 0; i < 20; i++ {
		id := string(rune('A' + i))
		graph.Nodes = graph.Nodes.Set(id, &stage4.ResolvedNode{ID: id})
		addStructuralEdge(graph, id, "god", stage4.EdgeCalls)
	}
	// Only 2 methods -> below threshold.
	for i := 0; i < 2; i++ {
		m := "m" + string(rune('a'+i))
		graph.Nodes = graph.Nodes.Set(m, &stage4.ResolvedNode{ID: m, Kind: "FUNCTION"})
		addStructuralEdge(graph, "god", m, stage4.EdgeHasReceiver)
	}
	for _, s := range RunSmellDetectionContext(smellContext(graph)) {
		if s.Kind == archmodel.SmellGodObject {
			t.Error("expected no god object smell")
		}
	}
}

func TestSD02CyclicDependency(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	for _, id := range []string{"a", "b", "c"} {
		graph.Nodes = graph.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "FUNCTION"})
	}
	addStructuralEdge(graph, "a", "b", stage4.EdgeCalls)
	addStructuralEdge(graph, "b", "c", stage4.EdgeCalls)
	addStructuralEdge(graph, "c", "a", stage4.EdgeCalls)

	found := false
	for _, s := range RunSmellDetectionContext(smellContext(graph)) {
		if s.Kind == archmodel.SmellCyclicDependency {
			found = true
			if len(s.AffectedIDs) != 3 {
				t.Errorf("expected 3 affected nodes, got %d", len(s.AffectedIDs))
			}
		}
	}
	if !found {
		t.Error("expected SD-02 cyclic dependency smell")
	}
}

func TestSD03DeadCode(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	graph.Entrypoints = []string{"main"}
	for _, id := range []string{"main", "a", "orphan"} {
		graph.Nodes = graph.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "FUNCTION", Name: id})
	}
	addStructuralEdge(graph, "main", "a", stage4.EdgeCalls)

	found := false
	for _, s := range RunSmellDetectionContext(smellContext(graph)) {
		if s.Kind == archmodel.SmellDeadCode {
			found = true
			hasOrphan := false
			for _, id := range s.AffectedIDs {
				if id == "orphan" {
					hasOrphan = true
				}
			}
			if !hasOrphan {
				t.Errorf("expected orphan in dead code, got %v", s.AffectedIDs)
			}
		}
	}
	if !found {
		t.Error("expected SD-03 dead code smell")
	}
}

func TestSD04LayerViolation(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	cfg := layeredCfg()
	// cmd node (presentation) and internal/domain node.
	addNodeWithPath(graph, "c1", "cmd/web/handler.go")
	addNodeWithPath(graph, "d1", "internal/domain/x.go")
	addNodeWithPath(graph, "d2", "internal/domain/y.go")
	// Upward violation: domain -> cmd.
	addStructuralEdge(graph, "d1", "c1", stage4.EdgeDependsOn)

	ctx := &RuleContext{
		Graph:         NewGraphSnapshot(graph),
		LayerAssigner: NewLayerAssigner(cfg.ArchLayers),
		Cfg:           cfg,
		Clock:         testClock,
	}
	found := false
	for _, s := range RunSmellDetectionContext(ctx) {
		if s.Kind == archmodel.SmellLayerViolation {
			found = true
		}
	}
	if !found {
		t.Error("expected SD-04 layer violation smell")
	}
}

func TestSD04LayerViolation_NoLayers(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	addNodeWithPath(graph, "c1", "cmd/web/handler.go")
	addNodeWithPath(graph, "d1", "internal/domain/x.go")
	addStructuralEdge(graph, "d1", "c1", stage4.EdgeDependsOn)

	for _, s := range RunSmellDetectionContext(smellContext(graph)) {
		if s.Kind == archmodel.SmellLayerViolation {
			t.Error("expected no layer violation without declared layers")
		}
	}
}

func TestSD05GodPackage(t *testing.T) {
	// comp_big: 30 nodes (share > 40%), Ca >= 3.
	components := []archmodel.DetectedComponent{
		{ID: "comp_big", Name: "big", NodeIDs: nodeIDs("big", 30)},
		{ID: "comp_s1", Name: "s1", NodeIDs: nodeIDs("s1", 2)},
		{ID: "comp_s2", Name: "s2", NodeIDs: nodeIDs("s2", 2)},
		{ID: "comp_s3", Name: "s3", NodeIDs: nodeIDs("s3", 2)},
	}
	couplings := []ComponentCoupling{
		{ComponentID: "comp_big", Name: "big", Ca: 3, Ce: 0, Weight: 30},
		{ComponentID: "comp_s1", Name: "s1", Ca: 0, Ce: 1, Weight: 2},
		{ComponentID: "comp_s2", Name: "s2", Ca: 0, Ce: 1, Weight: 2},
		{ComponentID: "comp_s3", Name: "s3", Ca: 0, Ce: 1, Weight: 2},
	}
	ctx := &RuleContext{
		Graph:             &GraphSnapshot{NodeIDs: append(nodeIDs("big", 30), nodeIDs("s1", 2)...), Nodes: map[string]*stage4.ResolvedNode{}},
		Components:        components,
		ComponentCoupling: couplings,
		Cfg:               config.DefaultIntelligenceConfig(),
		Clock:             testClock,
	}
	found := false
	for _, s := range RunSmellDetectionContext(ctx) {
		if s.Kind == archmodel.SmellGodPackage {
			found = true
			if len(s.AffectedIDs) != 1 || s.AffectedIDs[0] != "comp_big" {
				t.Errorf("expected comp_big affected, got %v", s.AffectedIDs)
			}
		}
	}
	if !found {
		t.Error("expected SD-05 god package smell")
	}
}

func TestSD06TightCoupling(t *testing.T) {
	// target has Ca=5 and Ce=5.
	var comps []archmodel.DetectedComponent
	var couplings []ComponentCoupling
	comps = append(comps, archmodel.DetectedComponent{ID: "comp_t", Name: "t", NodeIDs: nodeIDs("t", 5)})
	couplings = append(couplings, ComponentCoupling{ComponentID: "comp_t", Name: "t", Ca: 5, Ce: 5, Weight: 5})
	for i := 0; i < 10; i++ {
		id := "comp_d" + string(rune('a'+i))
		comps = append(comps, archmodel.DetectedComponent{ID: id, Name: id, NodeIDs: nodeIDs(id, 1)})
		couplings = append(couplings, ComponentCoupling{ComponentID: id, Name: id, Ca: 0, Ce: 0, Weight: 1})
	}
	ctx := &RuleContext{
		Graph:             &GraphSnapshot{NodeIDs: nodeIDs("t", 5), Nodes: map[string]*stage4.ResolvedNode{}},
		Components:        comps,
		ComponentCoupling: couplings,
		Cfg:               config.DefaultIntelligenceConfig(),
		Clock:             testClock,
	}
	found := false
	for _, s := range RunSmellDetectionContext(ctx) {
		if s.Kind == archmodel.SmellTightCoupling {
			found = true
			if s.AffectedIDs[0] != "comp_t" {
				t.Errorf("expected comp_t affected, got %v", s.AffectedIDs)
			}
		}
	}
	if !found {
		t.Error("expected SD-06 tight coupling smell")
	}
}

func TestSD07UnstableAbstraction(t *testing.T) {
	components := []archmodel.DetectedComponent{
		{ID: "comp_u", Name: "u", NodeIDs: nodeIDs("u", 3)},
		{ID: "comp_a", Name: "a", NodeIDs: nodeIDs("a", 3)},
		{ID: "comp_b", Name: "b", NodeIDs: nodeIDs("b", 3)},
	}
	couplings := []ComponentCoupling{
		{ComponentID: "comp_u", Name: "u", Ca: 0, Ce: 3, Instability: 1.0, Weight: 3},
		{ComponentID: "comp_a", Name: "a", Ca: 1, Ce: 0, Instability: 0, Weight: 3},
		{ComponentID: "comp_b", Name: "b", Ca: 1, Ce: 0, Instability: 0, Weight: 3},
	}
	ctx := &RuleContext{
		Graph:             &GraphSnapshot{NodeIDs: nodeIDs("u", 3), Nodes: map[string]*stage4.ResolvedNode{}},
		Components:        components,
		ComponentCoupling: couplings,
		Cfg:               config.DefaultIntelligenceConfig(),
		Clock:             testClock,
	}
	found := false
	for _, s := range RunSmellDetectionContext(ctx) {
		if s.Kind == archmodel.SmellUnstableAbstraction {
			found = true
			if s.AffectedIDs[0] != "comp_u" {
				t.Errorf("expected comp_u affected, got %v", s.AffectedIDs)
			}
		}
	}
	if !found {
		t.Error("expected SD-07 unstable abstraction smell")
	}
}

func TestRunSmellDetection_Compat(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	for _, id := range []string{"a", "b"} {
		graph.Nodes = graph.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "FUNCTION"})
	}
	addStructuralEdge(graph, "a", "b", stage4.EdgeCalls)
	smells := RunSmellDetection(graph, archmodel.ArchMetrics{})
	if len(smells) != 0 {
		t.Errorf("expected 0 smells on clean graph, got %d", len(smells))
	}
}

// nodeIDs generates n deterministic ids with the given prefix.
func nodeIDs(prefix string, n int) []string {
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = prefix + "_n" + string(rune('0'+i/10)) + string(rune('0'+i%10))
	}
	return ids
}
