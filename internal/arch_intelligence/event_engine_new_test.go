package arch_intelligence

import (
	"context"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/config"
)

func testMeta() CommitMeta {
	return CommitMeta{Hash: "abc123", Timestamp: time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)}
}

func TestGenerateEvents_DeterministicIDs(t *testing.T) {
	base := &archmodel.ArchSnapshot{Components: []archmodel.DetectedComponent{}}
	head := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{{ID: "comp_a", Name: "a"}},
	}
	diff := &akg.GraphDiff{}
	meta := testMeta()

	events1 := GenerateEvents(base, head, diff, meta)
	events2 := GenerateEvents(base, head, diff, meta)
	if len(events1) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events1))
	}
	if events1[0].Kind != archmodel.EventServiceAdded {
		t.Errorf("expected EventServiceAdded, got %s", events1[0].Kind)
	}
	if events1[0].ID != events2[0].ID {
		t.Errorf("event IDs must be deterministic: %s vs %s", events1[0].ID, events2[0].ID)
	}
	if len(events1[0].ID) != len("evt_")+16 {
		t.Errorf("expected evt_ + 16 hex chars, got %q", events1[0].ID)
	}
	if events1[0].Evidence.IsEmpty() {
		t.Error("event must carry non-empty evidence")
	}
}

func TestGenerateEvents_FullSet(t *testing.T) {
	base := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{
			{ID: "comp_gone", Name: "gone", NodeIDs: []string{"g1"}},
			{ID: "comp_stay", Name: "stay", NodeIDs: []string{"s1"}, Dependencies: []string{"comp_gone"}},
		},
		Patterns: []archmodel.DetectedPattern{{Kind: archmodel.PatternLayered, Name: "Layered"}},
		Smells:   []archmodel.ArchSmell{{Kind: archmodel.SmellGodObject, Title: "x"}},
		Metrics:  archmodel.ArchMetrics{CycleCount: 0, DeadCodeNodeCount: 0, LayerViolationCount: 0},
	}
	head := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{
			{ID: "comp_new", Name: "new", NodeIDs: []string{"n1"}},
			{ID: "comp_stay", Name: "stay", NodeIDs: []string{"s1"}, Dependencies: []string{"comp_new"}},
		},
		Patterns: []archmodel.DetectedPattern{{Kind: archmodel.PatternCQRS, Name: "CQRS"}},
		Smells:   []archmodel.ArchSmell{{Kind: archmodel.SmellDeadCode, Title: "y"}},
		Metrics:  archmodel.ArchMetrics{CycleCount: 2, DeadCodeNodeCount: 5, LayerViolationCount: 1},
	}
	events := GenerateEvents(base, head, &akg.GraphDiff{}, testMeta())

	want := map[archmodel.EventKind]bool{
		archmodel.EventServiceAdded:      true, // comp_new
		archmodel.EventServiceRemoved:    true, // comp_gone
		archmodel.EventDependencyAdded:   true, // comp_stay -> comp_new
		archmodel.EventDependencyRemoved: true, // comp_stay -> comp_gone
		archmodel.EventPatternDetected:   true,
		archmodel.EventPatternLost:       true,
		archmodel.EventSmellDetected:     true,
		archmodel.EventSmellResolved:     true,
		archmodel.EventCycleIntroduced:   true,
		archmodel.EventDeadCodeDetected:  true,
		archmodel.EventLayerViolation:    true,
	}
	got := map[archmodel.EventKind]int{}
	for _, e := range events {
		got[e.Kind]++
	}
	for kind := range want {
		if got[kind] < 1 {
			t.Errorf("expected event kind %s, events: %v", kind, got)
		}
	}
}

func TestGenerateEvents_NoChanges(t *testing.T) {
	comp := []archmodel.DetectedComponent{{ID: "comp_a", Name: "a", Dependencies: []string{"comp_b"}}}
	compB := []archmodel.DetectedComponent{{ID: "comp_b", Name: "b"}}
	base := &archmodel.ArchSnapshot{
		Components: append(append([]archmodel.DetectedComponent{}, comp...), compB...),
		Patterns:   []archmodel.DetectedPattern{{Kind: archmodel.PatternLayered, Name: "Layered"}},
		Smells:     []archmodel.ArchSmell{{Kind: archmodel.SmellDeadCode, Title: "d"}},
		Metrics:    archmodel.ArchMetrics{CycleCount: 1, DeadCodeNodeCount: 2},
	}
	head := &archmodel.ArchSnapshot{
		Components: append(append([]archmodel.DetectedComponent{}, comp...), compB...),
		Patterns:   []archmodel.DetectedPattern{{Kind: archmodel.PatternLayered, Name: "Layered"}},
		Smells:     []archmodel.ArchSmell{{Kind: archmodel.SmellDeadCode, Title: "d"}},
		Metrics:    archmodel.ArchMetrics{CycleCount: 1, DeadCodeNodeCount: 2},
	}
	events := GenerateEvents(base, head, &akg.GraphDiff{}, testMeta())
	if len(events) != 0 {
		t.Errorf("expected 0 events for identical snapshots, got %d: %v", len(events), events)
	}
}

func TestEngine_RunContext_EmptyGraph(t *testing.T) {
	engine := NewEngine(akg.NewCodePropertyGraph("test"))
	res := engine.Run()
	if res.Metrics.TotalNodes != 0 {
		t.Errorf("expected 0 nodes, got %d", res.Metrics.TotalNodes)
	}
	if res.GraphHash != "" {
		t.Errorf("expected empty graph hash, got %q", res.GraphHash)
	}
	if len(res.Components) != 0 {
		t.Errorf("expected 0 components, got %d", len(res.Components))
	}
}

func TestEngine_SnapshotCaching(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	addNodeWithPath(graph, "n1", "internal/app/x.go")
	engine := NewEngine(graph)
	res1 := engine.Run()
	if engine.Snapshot() == nil {
		t.Fatal("Snapshot() must return non-nil")
	}
	if len(res1.GraphHash) != 16 {
		t.Errorf("expected 16-char graph hash, got %q", res1.GraphHash)
	}
	engine.InvalidateSnapshot()
	res2 := engine.Run()
	if res1.GraphHash != res2.GraphHash {
		t.Errorf("graph hash must be stable: %q vs %q", res1.GraphHash, res2.GraphHash)
	}
}

func TestEngine_RunRules(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("god", &link.ResolvedNode{ID: "god", Name: "God", Kind: "STRUCT"})
	for i := 0; i < 20; i++ {
		id := string(rune('A' + i))
		graph.Nodes = graph.Nodes.Set(id, &link.ResolvedNode{ID: id})
		addStructuralEdge(graph, id, "god", link.EdgeCalls)
	}
	for i := 0; i < 35; i++ {
		m := "m" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		graph.Nodes = graph.Nodes.Set(m, &link.ResolvedNode{ID: m, Kind: "FUNCTION"})
		addStructuralEdge(graph, "god", m, link.EdgeHasReceiver)
	}

	cfg := config.DefaultIntelligenceConfig()
	cfg.RunRules = []string{"patterns"}
	engine := NewEngineWithOptions(graph, WithConfig(cfg), WithClock(testClock))
	res := engine.Run()
	if len(res.Smells) != 0 {
		t.Errorf("expected 0 smells when smells disabled, got %d", len(res.Smells))
	}
}

func TestEngine_WithClock(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	addNodeWithPath(graph, "n1", "internal/app/x.go")
	engine := NewEngineWithOptions(graph, WithClock(testClock))
	res := engine.Run()
	if len(res.Components) == 0 {
		t.Fatal("expected at least one component")
	}
	for _, c := range res.Components {
		for _, item := range c.Evidence.Items {
			if !item.Timestamp.Equal(testClock()) {
				t.Errorf("expected evidence timestamp %v, got %v", testClock(), item.Timestamp)
			}
		}
	}
}

func TestEngine_ComponentCouplingFilled(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	addNodeWithPath(graph, "a1", "internal/a/x.go")
	addNodeWithPath(graph, "a2", "internal/a/y.go")
	addNodeWithPath(graph, "a3", "internal/a/z.go")
	addNodeWithPath(graph, "b1", "internal/b/x.go")
	addNodeWithPath(graph, "b2", "internal/b/y.go")
	addNodeWithPath(graph, "b3", "internal/b/z.go")
	addStructuralEdge(graph, "a1", "b1", link.EdgeCalls)

	engine := NewEngine(graph)
	res := engine.Run()
	if len(res.ComponentCoupling) != 2 {
		t.Fatalf("expected 2 component couplings, got %d", len(res.ComponentCoupling))
	}
	byID := map[string]ComponentCoupling{}
	for _, cc := range res.ComponentCoupling {
		byID[cc.ComponentID] = cc
	}
	if ccA := byID["comp_internal_a"]; ccA.Ce != 1 || ccA.Ca != 0 {
		t.Errorf("comp_a expected Ce=1 Ca=0, got %+v", ccA)
	}
	if ccB := byID["comp_internal_b"]; ccB.Ca != 1 || ccB.Ce != 0 {
		t.Errorf("comp_b expected Ca=1 Ce=0, got %+v", ccB)
	}
	for _, c := range res.Components {
		if c.ID == "comp_internal_a" && c.Ce != 1 {
			t.Errorf("component a should have Ce=1 recorded, got %+v", c)
		}
	}
}

func TestEngine_CancelledContext(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	addNodeWithPath(graph, "n1", "internal/app/x.go")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine := NewEngine(graph)
	res := engine.RunContext(ctx)
	// Must not panic; result is safe to inspect.
	if res.GraphHash != "" && len(res.GraphHash) != 16 {
		t.Errorf("unexpected graph hash %q", res.GraphHash)
	}
}
