package stages_test

// Drift tests: a graph built from a hand-written GraphJSON document via
// akg.ImportGraphJSON (graph_json.go:239) evaluated against
// config.DriftConfig invariants with drift.Analyze. Direct API calls only.
//
// Discrepancies from API_REFERENCE.md:
//   - drift.Analyze returns *Report with NO error value (drift.go:87); the
//     reference documents (*Report, error). All callers must treat the
//     report as always present.
//   - DetectCategories, TrackEntryTrends, CompareEntryTrends and EntryTrend
//     (API_REFERENCE.md drift section, "drift.go:91-100") DO NOT EXIST
//     anywhere in internal/ — only Analyze, Report, Violation, LayerIndex
//     and ExceedsBudget are implemented. The category/trend tests are
//     therefore omitted until those APIs land.
//   - Layer globs must end in "/**" to bucket nested paths:
//     path.Match("cmd/*", "cmd/api/main.go") is false (single-segment glob),
//     while the "/**" prefix rule (drift.go:70-74) catches every descendant.
//   - Nodes whose paths match no layer (or whose edge stays within one
//     layer) are silently ignored; unassigned targets never violate
//     (drift.go:112).
//   - ForbiddenDeps rules match exact layer pairs (drift.go:131-138): a rule
//     {api, internal} does NOT cover the distinct "private" sub-layer. An
//     architect must declare one rule per named layer (see INF-0002).

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/drift"
)

// s12driftGraphJSON renders the drift fixture graph as a GraphJSON document:
// api entrypoints under cmd/api, service/repo under internal, and an
// optional main→private CALLS edge (the architecturally forbidden one).
func s12driftGraphJSON(withPrivateEdge bool) string {
	edges := `{"source_id":"cmd/api/main.go::Main","target_id":"internal/service/service.go::Service","type":"CALLS","line_number":5},
    {"source_id":"internal/service/service.go::Service","target_id":"internal/repo/repo.go::Repo","type":"CALLS","line_number":9}`
	if withPrivateEdge {
		edges += `,
    {"source_id":"cmd/api/main.go::Main","target_id":"internal/private/secret.go::Secret","type":"CALLS","line_number":12}`
	}
	return fmt.Sprintf(`{
  "schema_version": %d,
  "commit_hash": "deadbeef",
  "version": 3,
  "nodes": [
    {"id":"cmd/api/main.go::Main","kind":"FUNCTION","name":"Main","file_spec":{"path":"cmd/api/main.go","line_start":10,"line_end":20}},
    {"id":"cmd/api/router.go::Router","kind":"STRUCT","name":"Router","file_spec":{"path":"cmd/api/router.go","line_start":1,"line_end":30}},
    {"id":"internal/service/service.go::Service","kind":"STRUCT","name":"Service","file_spec":{"path":"internal/service/service.go","line_start":1,"line_end":40}},
    {"id":"internal/repo/repo.go::Repo","kind":"STRUCT","name":"Repo","file_spec":{"path":"internal/repo/repo.go","line_start":1,"line_end":50}},
    {"id":"internal/private/secret.go::Secret","kind":"STRUCT","name":"Secret","file_spec":{"path":"internal/private/secret.go","line_start":1,"line_end":60}}
  ],
  "edges": [
    %s
  ]
}`, akg.CurrentSchemaVersion, edges)
}

// s12importDriftGraph parses the hand-written fixture into a
// CodePropertyGraph (import is pure: the graph is never persisted).
func s12importDriftGraph(t *testing.T, withPrivateEdge bool) *akg.CodePropertyGraph {
	t.Helper()
	graph, err := akg.ImportGraphJSON(strings.NewReader(s12driftGraphJSON(withPrivateEdge)))
	if err != nil {
		t.Fatalf("ImportGraphJSON: %v", err)
	}
	if got := graph.SchemaVersion; got != akg.CurrentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got, akg.CurrentSchemaVersion)
	}
	return graph
}

// s12layers returns the drift fixture's layer map: api entrypoints, a
// private zone bucketed BEFORE the catch-all internal layer (first match
// wins, drift.go:61-79).
func s12layers() []config.DriftLayer {
	return []config.DriftLayer{
		{Name: "api", Paths: []string{"cmd/**"}},
		{Name: "private", Paths: []string{"internal/private/**"}},
		{Name: "internal", Paths: []string{"internal/**"}},
	}
}

// TestDriftAnalyzeForbiddenDependency asserts the api→internal boundary is
// enforced: both the main→service and the main→private CALLS edges cross it
// and must surface as FORBIDDEN_DEPENDENCY violations naming their layers.
// Rules are exact layer pairs, so the private sub-layer needs its own rule.
func TestDriftAnalyzeForbiddenDependency(t *testing.T) {
	graph := s12importDriftGraph(t, true)
	cfg := config.DriftConfig{
		Layers: s12layers(),
		ForbiddenDeps: []config.ForbiddenDepRule{
			{Source: "api", Target: "internal", Reason: "api must not reach internal"},
			{Source: "api", Target: "private", Reason: "api must not reach private"},
		},
	}

	rep := drift.Analyze(graph, cfg)
	if rep.LayersDefined != len(cfg.Layers) {
		t.Errorf("LayersDefined = %d, want %d", rep.LayersDefined, len(cfg.Layers))
	}
	if rep.ForbiddenEdges != 2 {
		t.Fatalf("ForbiddenEdges = %d, want 2 (main→service and main→private)", rep.ForbiddenEdges)
	}
	if len(rep.Violations) != 2 {
		t.Fatalf("Violations = %d, want 2", len(rep.Violations))
	}
	sawPrivate := false
	for _, v := range rep.Violations {
		if v.Kind != drift.KindForbiddenDep {
			t.Errorf("violation kind = %q, want %q", v.Kind, drift.KindForbiddenDep)
		}
		if v.SourceLayer != "api" {
			t.Errorf("violation source layer = %q, want api", v.SourceLayer)
		}
		if v.EdgeType != "CALLS" {
			t.Errorf("violation edge type = %q, want CALLS", v.EdgeType)
		}
		if !strings.Contains(v.Message, "api") {
			t.Errorf("violation message %q must name the source layer", v.Message)
		}
		if v.TargetID == "internal/private/secret.go::Secret" {
			sawPrivate = true
			if v.TargetLayer != "private" {
				t.Errorf("violation target layer = %q, want private", v.TargetLayer)
			}
		}
	}
	if !sawPrivate {
		t.Error("main→private dependency missing from violations")
	}
}

// TestDriftAnalyzeCleanGraph asserts the forbidden pair fires only when the
// forbidden edge exists: removing the main→private edge leaves a violation-
// free report.
func TestDriftAnalyzeCleanGraph(t *testing.T) {
	cfg := config.DriftConfig{
		Layers: s12layers(),
		ForbiddenDeps: []config.ForbiddenDepRule{
			{Source: "api", Target: "private"},
		},
	}

	dirty := s12importDriftGraph(t, true)
	rep := drift.Analyze(dirty, cfg)
	if len(rep.Violations) != 1 {
		t.Fatalf("Violations = %d, want 1 (main→private)", len(rep.Violations))
	}
	v := rep.Violations[0]
	if v.SourceID != "cmd/api/main.go::Main" || v.TargetID != "internal/private/secret.go::Secret" {
		t.Errorf("unexpected violation: %+v", v)
	}
	if v.SourceLayer != "api" || v.TargetLayer != "private" {
		t.Errorf("violation layers = %q→%q, want api→private", v.SourceLayer, v.TargetLayer)
	}

	clean := s12importDriftGraph(t, false)
	rep = drift.Analyze(clean, cfg)
	if rep.ForbiddenEdges != 0 || len(rep.Violations) != 0 {
		t.Errorf("clean graph: ForbiddenEdges = %d, Violations = %d, want 0", rep.ForbiddenEdges, len(rep.Violations))
	}
}

// TestDriftLayerIndexBucketing asserts node→layer assignment through the
// exported LayerIndex, including the private zone winning over the
// catch-all internal layer.
func TestDriftLayerIndexBucketing(t *testing.T) {
	li := &drift.LayerIndex{Layers: s12layers()}
	cases := map[string]string{
		"cmd/api/main.go":             "api",
		"cmd/api/router.go":           "api",
		"internal/service/service.go": "internal",
		"internal/repo/repo.go":       "internal",
		"internal/private/secret.go":  "private",
		"pkg/util/util.go":            "",
	}
	for in, want := range cases {
		if got := li.AssignLayer(in); got != want {
			t.Errorf("AssignLayer(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDriftEmptyConfigTolerates asserts a default (empty) DriftConfig
// produces a zero-violation report: nothing is forbidden and no layers are
// declared.
func TestDriftEmptyConfigTolerates(t *testing.T) {
	graph := s12importDriftGraph(t, true)

	rep := drift.Analyze(graph, config.DriftConfig{})
	if rep.LayersDefined != 0 || rep.ForbiddenEdges != 0 || len(rep.Violations) != 0 {
		t.Errorf("empty config: LayersDefined = %d, ForbiddenEdges = %d, Violations = %d, want all zero",
			rep.LayersDefined, rep.ForbiddenEdges, len(rep.Violations))
	}
}

// TestDriftNilAndEmptyGraphSafety asserts Analyze never panics on a nil or
// empty graph and reports zero violations for both.
func TestDriftNilAndEmptyGraphSafety(t *testing.T) {
	cfg := config.DriftConfig{Layers: s12layers(), ForbiddenDeps: []config.ForbiddenDepRule{{Source: "api", Target: "internal"}}}

	rep := drift.Analyze(nil, cfg)
	if len(rep.Violations) != 0 || rep.ForbiddenEdges != 0 {
		t.Errorf("nil graph: Violations = %d, ForbiddenEdges = %d, want 0", len(rep.Violations), rep.ForbiddenEdges)
	}

	empty, err := akg.ImportGraphJSON(strings.NewReader(fmt.Sprintf(
		`{"schema_version": %d, "commit_hash": "deadbeef", "version": 0, "nodes": [], "edges": []}`, akg.CurrentSchemaVersion)))
	if err != nil {
		t.Fatalf("ImportGraphJSON(empty): %v", err)
	}
	rep = drift.Analyze(empty, cfg)
	if len(rep.Violations) != 0 || rep.ForbiddenEdges != 0 || rep.CycleCount != 0 {
		t.Errorf("empty graph: Violations = %d, ForbiddenEdges = %d, CycleCount = %d, want 0",
			len(rep.Violations), rep.ForbiddenEdges, rep.CycleCount)
	}
}

// TestDriftCycleDetection asserts the layer-level cycle counter: a back edge
// from the internal layer to the api layer forms one two-member SCC that
// trips the default zero budget.
func TestDriftCycleDetection(t *testing.T) {
	graph := s12importDriftGraph(t, false)
	// Add the cycle-forming back edge service→main directly on the graph
	// (mirrors link.AddEdge's dual index update).
	back := link.ResolvedEdge{
		SourceID: "internal/service/service.go::Service",
		TargetID: "cmd/api/main.go::Main",
		Type:     link.EdgeCalls,
	}
	out, _ := graph.OutboundEdges.Get(back.SourceID)
	graph.OutboundEdges = graph.OutboundEdges.Set(back.SourceID, append(out, back))
	in, _ := graph.InboundEdges.Get(back.TargetID)
	graph.InboundEdges = graph.InboundEdges.Set(back.TargetID, append(in, back))

	rep := drift.Analyze(graph, config.DriftConfig{
		Layers:      s12layers(),
		CycleBudget: 0,
	})
	if rep.CycleCount != 1 {
		t.Fatalf("CycleCount = %d, want 1 (api↔internal SCC)", rep.CycleCount)
	}
	if !rep.ExceedsBudget() {
		t.Error("ExceedsBudget() = false with a 0 budget and 1 cycle")
	}

	rep = drift.Analyze(graph, config.DriftConfig{
		Layers:      s12layers(),
		CycleBudget: 3,
	})
	if rep.ExceedsBudget() {
		t.Error("ExceedsBudget() = true with a 3 budget and 1 cycle")
	}
}
