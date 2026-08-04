package drift

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/config"
)

func testGraph() *akg.CodePropertyGraph {
	g := akg.NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a::web", &stage4.ResolvedNode{ID: "a::web", FileSpec: stage4.LocationMeta{Path: "cmd/web/handler.go"}})
	g.Nodes = g.Nodes.Set("b::svc", &stage4.ResolvedNode{ID: "b::svc", FileSpec: stage4.LocationMeta{Path: "internal/svc/svc.go"}})
	g.Nodes = g.Nodes.Set("c::repo", &stage4.ResolvedNode{ID: "c::repo", FileSpec: stage4.LocationMeta{Path: "internal/repo/repo.go"}})
	g.Nodes = g.Nodes.Set("d::db", &stage4.ResolvedNode{ID: "d::db", FileSpec: stage4.LocationMeta{Path: "internal/db/db.go"}})
	return g
}

func defaultConfig() config.DriftConfig {
	return config.DriftConfig{
		Layers: []config.DriftLayer{
			{Name: "web", Paths: []string{"cmd/web/**"}},
			{Name: "svc", Paths: []string{"internal/svc/**"}},
			{Name: "db", Paths: []string{"internal/db/**"}},
		},
		ForbiddenDeps: []config.ForbiddenDepRule{
			{Source: "web", Target: "db"},
		},
	}
}

func addEdge(g *akg.CodePropertyGraph, src, tgt string, typ stage4.RelationshipType) {
	edge := stage4.ResolvedEdge{SourceID: src, TargetID: tgt, Type: typ}
	out, _ := g.OutboundEdges.Get(src)
	g.OutboundEdges = g.OutboundEdges.Set(src, append(out, edge))
	in, _ := g.InboundEdges.Get(tgt)
	g.InboundEdges = g.InboundEdges.Set(tgt, append(in, edge))
}

func TestAnalyzeForbiddenDependencyDetected(t *testing.T) {
	g := testGraph()
	// web -> db is forbidden by config.
	addEdge(g, "a::web", "d::db", stage4.EdgeCalls)
	// web -> svc and svc -> db are allowed.
	addEdge(g, "a::web", "b::svc", stage4.EdgeCalls)
	addEdge(g, "b::svc", "d::db", stage4.EdgeCalls)

	rep := Analyze(g, defaultConfig())
	if rep.ForbiddenEdges != 1 {
		t.Fatalf("ForbiddenEdges = %d, want 1", rep.ForbiddenEdges)
	}
	if len(rep.Violations) != 1 {
		t.Fatalf("violations = %d, want 1", len(rep.Violations))
	}
	v := rep.Violations[0]
	if v.Kind != KindForbiddenDep || v.SourceLayer != "web" || v.TargetLayer != "db" {
		t.Errorf("unexpected violation: %+v", v)
	}
}

func TestAnalyzeNoViolations(t *testing.T) {
	g := testGraph()
	addEdge(g, "a::web", "b::svc", stage4.EdgeCalls)
	addEdge(g, "b::svc", "c::repo", stage4.EdgeCalls)

	rep := Analyze(g, defaultConfig())
	if rep.ForbiddenEdges != 0 {
		t.Errorf("ForbiddenEdges = %d, want 0", rep.ForbiddenEdges)
	}
	if len(rep.Violations) != 0 {
		t.Errorf("violations = %d, want 0", len(rep.Violations))
	}
}

func TestAnalyzeUnassignedNodesIgnored(t *testing.T) {
	g := testGraph()
	// c::repo lives under internal/repo which matches no layer.
	addEdge(g, "a::web", "c::repo", stage4.EdgeCalls)

	rep := Analyze(g, defaultConfig())
	if rep.ForbiddenEdges != 0 {
		t.Errorf("ForbiddenEdges = %d, want 0 (target unassigned)", rep.ForbiddenEdges)
	}
}

func TestAnalyzeCycleBudgetExceeded(t *testing.T) {
	g := testGraph()
	// svc <-> db forms a two-layer cycle.
	addEdge(g, "b::svc", "d::db", stage4.EdgeCalls)
	addEdge(g, "d::db", "b::svc", stage4.EdgeCalls)

	cfg := defaultConfig()
	cfg.CycleBudget = 0
	rep := Analyze(g, cfg)
	if rep.CycleCount != 1 {
		t.Fatalf("CycleCount = %d, want 1", rep.CycleCount)
	}
	if !rep.ExceedsBudget() {
		t.Error("expected cycle to exceed zero budget")
	}
}

func TestAnalyzeCycleBudgetWithinLimit(t *testing.T) {
	g := testGraph()
	addEdge(g, "b::svc", "d::db", stage4.EdgeCalls)
	addEdge(g, "d::db", "b::svc", stage4.EdgeCalls)

	cfg := defaultConfig()
	cfg.CycleBudget = 3
	rep := Analyze(g, cfg)
	if rep.CycleCount != 1 {
		t.Fatalf("CycleCount = %d, want 1", rep.CycleCount)
	}
	if rep.ExceedsBudget() {
		t.Error("did not expect budget exceed with budget 3")
	}
}

func TestAnalyzeNilGraph(t *testing.T) {
	rep := Analyze(nil, defaultConfig())
	if rep.CycleCount != 0 || rep.ForbiddenEdges != 0 {
		t.Errorf("expected empty report for nil graph, got %+v", rep)
	}
	if rep.ExceedsBudget() {
		t.Error("empty report should not exceed budget")
	}
}

func TestLayerIndexAssign(t *testing.T) {
	li := &layerIndex{layers: defaultConfig().Layers}
	cases := map[string]string{
		"cmd/web/handler.go":    "web",
		"cmd/web/sub/page.go":   "web",
		"internal/svc/svc.go":   "svc",
		"internal/db/db.go":     "db",
		"internal/repo/repo.go": "",
		"cmd/cli/main.go":       "",
		"not/a/match.go":        "",
	}
	for in, want := range cases {
		if got := li.Assign(in); got != want {
			t.Errorf("Assign(%q) = %q, want %q", in, got, want)
		}
	}
}
