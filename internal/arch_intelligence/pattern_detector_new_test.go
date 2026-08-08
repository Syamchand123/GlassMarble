package arch_intelligence

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/config"
)

// layeredCfg returns the standard three-layer config used by PR-01 tests.
func layeredCfg() *config.IntelligenceConfig {
	cfg := config.DefaultIntelligenceConfig()
	cfg.ArchLayers = []config.DriftLayer{
		{Name: "presentation", Paths: []string{"cmd/**"}},
		{Name: "domain", Paths: []string{"internal/domain/**"}},
		{Name: "infra", Paths: []string{"internal/infra/**"}},
	}
	return cfg
}

// layeredGraph builds 3 nodes per layer and returns the graph plus the
// layer->node mapping so tests can wire edges. Node paths match the layer
// globs (cmd/** etc.) so every node is assigned to exactly one layer.
func layeredGraph() (*akg.CodePropertyGraph, map[string][]string) {
	graph := akg.NewCodePropertyGraph("test")
	layers := map[string][]string{
		"cmd":             {"cmd1", "cmd2", "cmd3"},
		"internal/domain": {"dom1", "dom2", "dom3"},
		"internal/infra":  {"inf1", "inf2", "inf3"},
	}
	for layer, ids := range layers {
		for _, id := range ids {
			addNodeWithPath(graph, id, layer+"/"+id+".go")
		}
	}
	return graph, layers
}

func TestPR01Layered(t *testing.T) {
	graph, layers := layeredGraph()
	// 4 edges per layer pair, all downward: cmd->domain, cmd->infra, domain->infra.
	for _, src := range layers["cmd"] {
		addStructuralEdge(graph, src, layers["internal/domain"][0], stage4.EdgeDependsOn)
		addStructuralEdge(graph, src, layers["internal/infra"][0], stage4.EdgeDependsOn)
	}
	for _, src := range layers["internal/domain"] {
		addStructuralEdge(graph, src, layers["internal/infra"][0], stage4.EdgeDependsOn)
	}
	// One extra downward edge to clear the 10-edge minimum.
	addStructuralEdge(graph, layers["cmd"][2], layers["internal/domain"][1], stage4.EdgeDependsOn)
	// 10 cross-layer edges, 0 violations.

	ctx := &RuleContext{
		Graph:         NewGraphSnapshot(graph),
		LayerAssigner: NewLayerAssigner(layeredCfg().ArchLayers),
		Cfg:           layeredCfg(),
		Clock:         testClock,
	}
	patterns := RunPatternDetectionContext(ctx)
	found := false
	for _, p := range patterns {
		if p.Kind == archmodel.PatternLayered {
			found = true
			if p.Confidence < 0.8 {
				t.Errorf("layered confidence too low: %.2f", p.Confidence)
			}
		}
	}
	if !found {
		t.Error("expected PR-01 layered pattern")
	}
}

func TestPR01Layered_ViolationFails(t *testing.T) {
	graph, layers := layeredGraph()
	// Introduce violations: infra depends on cmd (upward).
	for _, src := range layers["internal/infra"] {
		addStructuralEdge(graph, src, layers["cmd"][0], stage4.EdgeDependsOn)
	}
	// One downward edge per pair so total edges are meaningful.
	addStructuralEdge(graph, layers["cmd"][0], layers["internal/domain"][0], stage4.EdgeDependsOn)
	addStructuralEdge(graph, layers["cmd"][0], layers["internal/infra"][0], stage4.EdgeDependsOn)
	addStructuralEdge(graph, layers["internal/domain"][0], layers["internal/infra"][0], stage4.EdgeDependsOn)
	// 6 edges total, 3 violations -> consistency 0.5 < 0.8.

	ctx := &RuleContext{
		Graph:         NewGraphSnapshot(graph),
		LayerAssigner: NewLayerAssigner(layeredCfg().ArchLayers),
		Cfg:           layeredCfg(),
		Clock:         testClock,
	}
	for _, p := range RunPatternDetectionContext(ctx) {
		if p.Kind == archmodel.PatternLayered {
			t.Error("expected NO layered pattern with violations present")
		}
	}
}

func TestPR02CleanArchitecture(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	addNodeWithPath(graph, "d1", "internal/domain/x.go")
	addNodeWithPath(graph, "d2", "internal/domain/y.go")
	addNodeWithPath(graph, "d3", "internal/domain/z.go")
	addNodeWithPath(graph, "i1", "internal/infra/x.go")
	addNodeWithPath(graph, "i2", "internal/infra/y.go")
	addNodeWithPath(graph, "i3", "internal/infra/z.go")
	// Infra depends on domain (inversion) — clean.
	addStructuralEdge(graph, "i1", "d1", stage4.EdgeDependsOn)
	addStructuralEdge(graph, "i2", "d2", stage4.EdgeDependsOn)
	addStructuralEdge(graph, "i3", "d3", stage4.EdgeDependsOn)

	ctx := &RuleContext{Graph: NewGraphSnapshot(graph), Cfg: config.DefaultIntelligenceConfig(), Clock: testClock}
	found := false
	for _, p := range RunPatternDetectionContext(ctx) {
		if p.Kind == archmodel.PatternCleanArchitecture {
			found = true
		}
	}
	if !found {
		t.Error("expected PR-02 clean architecture pattern")
	}
}

func TestPR02CleanArchitecture_DomainToInfraFails(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	addNodeWithPath(graph, "d1", "internal/domain/x.go")
	addNodeWithPath(graph, "d2", "internal/domain/y.go")
	addNodeWithPath(graph, "d3", "internal/domain/z.go")
	addNodeWithPath(graph, "i1", "internal/infra/x.go")
	addNodeWithPath(graph, "i2", "internal/infra/y.go")
	addNodeWithPath(graph, "i3", "internal/infra/z.go")
	// Domain depends on infra — the violation that kills the pattern.
	addStructuralEdge(graph, "d1", "i1", stage4.EdgeDependsOn)
	addStructuralEdge(graph, "d2", "i2", stage4.EdgeDependsOn)
	addStructuralEdge(graph, "d3", "i3", stage4.EdgeDependsOn)

	ctx := &RuleContext{Graph: NewGraphSnapshot(graph), Cfg: config.DefaultIntelligenceConfig(), Clock: testClock}
	for _, p := range RunPatternDetectionContext(ctx) {
		if p.Kind == archmodel.PatternCleanArchitecture {
			t.Error("expected NO clean architecture pattern when domain depends on infra")
		}
	}
}

func TestPR03Microservices(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	// Service A with its own DB; Service B with its own endpoint.
	// Three nodes per service so they do not merge into a parent component.
	addNodeWithPath(graph, "a1", "svc/a/main.go")
	addNodeWithPath(graph, "a2", "svc/a/db.go")
	addNodeWithPath(graph, "a3", "svc/a/extra.go")
	addNodeWithPath(graph, "b1", "svc/b/main.go")
	addNodeWithPath(graph, "b2", "svc/b/api.go")
	addNodeWithPath(graph, "b3", "svc/b/extra.go")
	addStructuralEdge(graph, "a2", "dbconn", stage4.EdgeQueriesDB)
	addStructuralEdge(graph, "b2", "ep", stage4.EdgeExposesEndpoint)
	// External targets must exist as nodes to survive the snapshot filter.
	graph.Nodes = graph.Nodes.Set("dbconn", testNode("dbconn", "svc/a/dbconn.go"))
	graph.Nodes = graph.Nodes.Set("ep", testNode("ep", "svc/b/ep.go"))

	snap := NewGraphSnapshot(graph)
	components := InferComponentsFromSnapshot(snap, nil, testClock)
	ctx := &RuleContext{
		Graph:      snap,
		Components: components,
		Cfg:        config.DefaultIntelligenceConfig(),
		Clock:      testClock,
	}
	found := false
	for _, p := range RunPatternDetectionContext(ctx) {
		if p.Kind == archmodel.PatternMicroservices {
			found = true
			if len(p.Components) < 2 {
				t.Errorf("expected 2 service components, got %v", p.Components)
			}
		}
	}
	if !found {
		t.Error("expected PR-03 microservices pattern")
	}
}

func TestPR04BoundedContext(t *testing.T) {
	components := []archmodel.DetectedComponent{
		{ID: "comp_a", Name: "a", NodeIDs: []string{"a1", "a2"}, Dependencies: []string{"comp_b"}},
		{ID: "comp_b", Name: "b", NodeIDs: []string{"b1", "b2"}, Dependencies: nil},
		{ID: "comp_c", Name: "c", NodeIDs: []string{"c1", "c2"}, Dependencies: []string{"comp_d"}},
		{ID: "comp_d", Name: "d", NodeIDs: []string{"d1", "d2"}, Dependencies: nil},
	}
	ctx := &RuleContext{
		Graph:      &GraphSnapshot{NodeIDs: []string{"a1", "a2", "b1", "b2", "c1", "c2", "d1", "d2"}, Nodes: map[string]*stage4.ResolvedNode{}},
		Components: components,
		Cfg:        config.DefaultIntelligenceConfig(),
		Clock:      testClock,
	}
	found := false
	for _, p := range RunPatternDetectionContext(ctx) {
		if p.Kind == archmodel.PatternDDD {
			found = true
		}
	}
	if !found {
		t.Error("expected PR-04 DDD bounded context pattern")
	}
}

func TestPR05CQRS(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	addNodeWithPath(graph, "c1", "internal/user/CreateUserCommand.go")
	graph.Nodes = graph.Nodes.Set("c1", &stage4.ResolvedNode{
		ID: "c1", Name: "CreateUserCommand", Kind: "STRUCT",
		FileSpec: stage4.LocationMeta{Path: "internal/user/CreateUserCommand.go"},
	})
	addNodeWithPath(graph, "c2", "internal/user/DeleteUserCommand.go")
	graph.Nodes = graph.Nodes.Set("c2", &stage4.ResolvedNode{
		ID: "c2", Name: "DeleteUserCommand", Kind: "STRUCT",
		FileSpec: stage4.LocationMeta{Path: "internal/user/DeleteUserCommand.go"},
	})
	addNodeWithPath(graph, "q1", "internal/user/GetUserQuery.go")
	graph.Nodes = graph.Nodes.Set("q1", &stage4.ResolvedNode{
		ID: "q1", Name: "GetUserQuery", Kind: "STRUCT",
		FileSpec: stage4.LocationMeta{Path: "internal/user/GetUserQuery.go"},
	})
	addNodeWithPath(graph, "q2", "internal/user/ListUsersQuery.go")
	graph.Nodes = graph.Nodes.Set("q2", &stage4.ResolvedNode{
		ID: "q2", Name: "ListUsersQuery", Kind: "STRUCT",
		FileSpec: stage4.LocationMeta{Path: "internal/user/ListUsersQuery.go"},
	})
	addNodeWithPath(graph, "h1", "internal/user/CreateUserCommandHandler.go")
	graph.Nodes = graph.Nodes.Set("h1", &stage4.ResolvedNode{
		ID: "h1", Name: "CreateUserCommandHandler", Kind: "STRUCT",
		FileSpec: stage4.LocationMeta{Path: "internal/user/CreateUserCommandHandler.go"},
	})

	ctx := &RuleContext{Graph: NewGraphSnapshot(graph), Cfg: config.DefaultIntelligenceConfig(), Clock: testClock}
	found := false
	for _, p := range RunPatternDetectionContext(ctx) {
		if p.Kind == archmodel.PatternCQRS {
			found = true
		}
	}
	if !found {
		t.Error("expected PR-05 CQRS pattern")
	}
}

func TestPR06EventDriven(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	for i := 0; i < 30; i++ {
		src := "caller" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		tgt := "callee" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		addNodeWithPath(graph, src, "internal/x/"+src+".go")
		addNodeWithPath(graph, tgt, "internal/x/"+tgt+".go")
	}
	// 10 event edges.
	for i := 0; i < 10; i++ {
		src := "pub" + string(rune('a'+i))
		tgt := "sub" + string(rune('a'+i))
		addNodeWithPath(graph, src, "internal/evt/"+src+".go")
		addNodeWithPath(graph, tgt, "internal/evt/"+tgt+".go")
		addStructuralEdge(graph, src, tgt, stage4.EdgePublishes)
	}
	// 30 structural call edges between the caller/callee pairs.
	for i := 0; i < 30; i++ {
		src := "caller" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		tgt := "callee" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		addStructuralEdge(graph, src, tgt, stage4.EdgeCalls)
	}
	// ratio 10/40 = 0.25 >= 0.15.

	ctx := &RuleContext{Graph: NewGraphSnapshot(graph), Cfg: config.DefaultIntelligenceConfig(), Clock: testClock}
	found := false
	for _, p := range RunPatternDetectionContext(ctx) {
		if p.Kind == archmodel.PatternEventDriven {
			found = true
		}
	}
	if !found {
		t.Error("expected PR-06 event-driven pattern")
	}
}

func TestPR07Repository(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	addNodeWithPath(graph, "r1", "internal/repo/UserRepository.go")
	graph.Nodes = graph.Nodes.Set("r1", &stage4.ResolvedNode{
		ID: "r1", Name: "UserRepository", Kind: "STRUCT",
		FileSpec: stage4.LocationMeta{Path: "internal/repo/UserRepository.go"},
	})
	addNodeWithPath(graph, "db", "internal/db/db.go")
	addStructuralEdge(graph, "r1", "db", stage4.EdgeQueriesDB)

	ctx := &RuleContext{Graph: NewGraphSnapshot(graph), Cfg: config.DefaultIntelligenceConfig(), Clock: testClock}
	found := false
	for _, p := range RunPatternDetectionContext(ctx) {
		if p.Kind == archmodel.PatternRepository {
			found = true
			if p.Confidence != 0.9 {
				t.Errorf("expected 0.9 confidence with direct DB access, got %.2f", p.Confidence)
			}
		}
	}
	if !found {
		t.Error("expected PR-07 repository pattern")
	}
}

func TestRunPatternDetection_Compat(t *testing.T) {
	graph := akg.NewCodePropertyGraph("test")
	patterns := RunPatternDetection(graph, archmodel.ArchMetrics{})
	if len(patterns) != 0 {
		t.Errorf("expected 0 patterns on empty graph, got %d", len(patterns))
	}
}
