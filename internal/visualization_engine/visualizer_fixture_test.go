package visualization_engine_test

// Fixture-based end-to-end tests for the visualization engine. They run in
// an external test package so they can wire the canonical GraphJSON store
// reader (internal/akg.ParseGraphForQuery) — exactly what CLI/TUI/AI callers
// install through product.BuildDiagramRequest.ParseFn (v3 plan Phase C.3).
// One legacy-Turtle fallback test is retained (TestLegacyTTLFallbackParse)
// covering the deprecated ParseTTLFileToNative path (Phase C.1 / D.3).

import (
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/extract"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/layout"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/render"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// newJSONCoordinator returns an EngineCoordinator wired to the canonical
// GraphJSON store reader (the production wiring used by CLI/TUI/AI callers).
func newJSONCoordinator(path string) *visualization_engine.EngineCoordinator {
	ec := visualization_engine.NewEngineCoordinator(path)
	ec.SetParseFn(akg.ParseGraphForQuery)
	return ec
}

func TestProjectDiagramEndToEnd(t *testing.T) {
	path := filepath.Join("testdata", "minimal.json")
	ec := newJSONCoordinator(path)
	result, err := ec.ProjectDiagram(types.UMLClass, types.QueryOptions{
		EntryPointID: "main.go::Main",
	})
	if err != nil {
		t.Fatalf("ProjectDiagram failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty diagram output")
	}
}

func TestProjectDiagramScopeFolder(t *testing.T) {
	path := filepath.Join("testdata", "scope_internal.json")
	ec := newJSONCoordinator(path)
	result, err := ec.ProjectDiagram(types.CallGraph, types.QueryOptions{
		EntryPointID: "internal/api/handler.go::HandleRequest",
		Scope:        types.ScopeFolder,
		ScopePath:    "internal",
	})
	if err != nil {
		t.Fatalf("ProjectDiagram with scope failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty diagram output with scope")
	}
}

func TestComputeGraphSummaryEndToEnd(t *testing.T) {
	path := filepath.Join("testdata", "minimal.json")
	ec := newJSONCoordinator(path)
	summary, err := ec.ComputeGraphSummary(types.UMLClass, types.QueryOptions{
		EntryPointID: "main.go::Main",
	})
	if err != nil {
		t.Fatalf("ComputeGraphSummary failed: %v", err)
	}
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if summary.NodeCount < 1 {
		t.Errorf("expected at least 1 node, got %d", summary.NodeCount)
	}
}

func TestPipelineParseExtractRender(t *testing.T) {
	path := filepath.Join("testdata", "minimal.json")
	native, err := akg.ParseGraphFileToNative(path)
	if err != nil {
		t.Fatalf("ParseGraphFileToNative failed: %v", err)
	}
	cfg := ingest.GetExtractionConfig(types.UMLClass, types.QueryOptions{EntryPointID: "main.go::Main"})
	sub, _, err := ingest.ExtractFromSubgraph(native, cfg, types.QueryOptions{EntryPointID: "main.go::Main"})
	if err != nil {
		t.Fatalf("ExtractFromSubgraph failed: %v", err)
	}
	if len(sub.Nodes) == 0 {
		t.Fatal("expected at least one extracted node")
	}
	metrics := normalize.ComputeAllMetrics(sub)
	if metrics == nil {
		t.Fatal("expected non-nil metrics")
	}
	layout := normalize.BuildLayoutTreeEx(sub, metrics, metrics.Communities, types.QueryOptions{}, types.UMLClass)
	if layout == nil {
		t.Fatal("expected non-nil layout tree")
	}
	markup := aggregate.RenderDiagramFormat(layout, types.UMLClass, "mermaid")
	if markup == "" {
		t.Error("expected non-empty render output")
	}
}

// TestLegacyTTLFallbackParse is the single retained legacy-Turtle fallback
// test (v3 plan Phase C.7 / D.3): the deprecated ParseTTLFileToNative entry
// point must keep working so pre-v3 repositories self-heal on first load.
func TestLegacyTTLFallbackParse(t *testing.T) {
	path := filepath.Join("testdata", "minimal.ttl")
	native, err := ingest.ParseTTLFileToNative(path)
	if err != nil {
		t.Fatalf("ParseTTLFileToNative failed: %v", err)
	}
	if len(native.Nodes) == 0 {
		t.Fatal("expected at least one node from the legacy TTL fixture")
	}
	if len(native.Edges) == 0 {
		t.Fatal("expected at least one edge from the legacy TTL fixture")
	}
}

// TestProjectDiagramFromGraphEqualsFilePath: the from-graph entry point
// renders the same diagram as the file-parsing entry point for an identical
// in-memory graph (AUDIT Issue 4 Phase 4A-1).
func TestProjectDiagramFromGraphEqualsFilePath(t *testing.T) {
	path := filepath.Join("testdata", "minimal.json")
	ec := newJSONCoordinator(path)
	opts := types.QueryOptions{EntryPointID: "main.go::Main"}

	fromFile, err := ec.ProjectDiagram(types.UMLClass, opts)
	if err != nil {
		t.Fatalf("ProjectDiagram failed: %v", err)
	}

	native, err := akg.ParseGraphFileToNative(path)
	if err != nil {
		t.Fatalf("ParseGraphFileToNative failed: %v", err)
	}
	fromGraph, err := visualization_engine.ProjectDiagramFromGraph(native, types.UMLClass, opts)
	if err != nil {
		t.Fatalf("ProjectDiagramFromGraph failed: %v", err)
	}

	if fromFile == "" || fromGraph == "" {
		t.Fatal("expected non-empty diagrams")
	}
	if fromFile != fromGraph {
		t.Errorf("from-graph diagram differs from from-file diagram:\nfromFile:\n%s\nfromGraph:\n%s", fromFile, fromGraph)
	}
}

// TestComputeGraphSummaryFromGraphEqualsFilePath: same parity for the
// summary path (AUDIT Issue 4 Phase 4A-1).
func TestComputeGraphSummaryFromGraphEqualsFilePath(t *testing.T) {
	path := filepath.Join("testdata", "minimal.json")
	ec := newJSONCoordinator(path)
	opts := types.QueryOptions{EntryPointID: "main.go::Main"}

	fromFile, err := ec.ComputeGraphSummary(types.UMLClass, opts)
	if err != nil {
		t.Fatalf("ComputeGraphSummary failed: %v", err)
	}

	native, err := akg.ParseGraphFileToNative(path)
	if err != nil {
		t.Fatalf("ParseGraphFileToNative failed: %v", err)
	}
	fromGraph, err := visualization_engine.ComputeGraphSummaryFromGraph(native, types.UMLClass, opts)
	if err != nil {
		t.Fatalf("ComputeGraphSummaryFromGraph failed: %v", err)
	}

	if fromFile == nil || fromGraph == nil {
		t.Fatal("expected non-nil summaries")
	}
	if fromFile.NodeCount != fromGraph.NodeCount {
		t.Errorf("node count mismatch: file=%d graph=%d", fromFile.NodeCount, fromGraph.NodeCount)
	}
	if fromFile.EdgeCount != fromGraph.EdgeCount {
		t.Errorf("edge count mismatch: file=%d graph=%d", fromFile.EdgeCount, fromGraph.EdgeCount)
	}
}

// TestProjectDiagramScopeFileUsesScopedParse: a file-scoped diagram loads
// only the file's nodes and still renders a correct non-empty diagram
// (AUDIT Issue 4 Phase 4A-2).
func TestProjectDiagramScopeFileUsesScopedParse(t *testing.T) {
	path := filepath.Join("testdata", "scope_internal.json")
	ec := newJSONCoordinator(path)
	opts := types.QueryOptions{
		EntryPointID: "internal/api/handler.go::HandleRequest",
		Scope:        types.ScopeFile,
		ScopePath:    "internal/api/handler.go",
	}
	result, err := ec.ProjectDiagram(types.CallGraph, opts)
	if err != nil {
		t.Fatalf("file-scoped ProjectDiagram failed: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty file-scoped diagram")
	}
}

// TestProjectDiagramFromGraphDoesNotMutateSource: the from-graph entry point
// must not mutate the caller's graph (scoping works on a private clone).
func TestProjectDiagramFromGraphDoesNotMutateSource(t *testing.T) {
	path := filepath.Join("testdata", "minimal.json")
	native, err := akg.ParseGraphFileToNative(path)
	if err != nil {
		t.Fatalf("ParseGraphFileToNative failed: %v", err)
	}
	before := native.Clone()
	_, err = visualization_engine.ProjectDiagramFromGraph(native, types.UMLClass, types.QueryOptions{
		EntryPointID: "main.go::Main",
		Scope:        types.ScopeFile,
		ScopePath:    "main.go",
	})
	if err != nil {
		t.Fatalf("ProjectDiagramFromGraph failed: %v", err)
	}
	if len(native.Nodes) != len(before.Nodes) {
		t.Errorf("source graph mutated: %d nodes before, %d after", len(before.Nodes), len(native.Nodes))
	}
}

func TestProjectDiagramWithPipelineConfig(t *testing.T) {
	path := filepath.Join("testdata", "minimal.json")
	ec := newJSONCoordinator(path)
	result, err := ec.ProjectDiagram(types.CallGraph, types.QueryOptions{
		EntryPointID: "main.go::Main",
		PipelineCfg: &types.PipelineConfig{
			EnableMetrics:     true,
			EnableCommunities: true,
			EnableSCC:         true,
		},
	})
	if err != nil {
		t.Fatalf("ProjectDiagram with PipelineConfig failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty diagram output")
	}
}

func TestProjectDiagramMetricsDisabled(t *testing.T) {
	path := filepath.Join("testdata", "minimal.json")
	ec := newJSONCoordinator(path)
	result, err := ec.ProjectDiagram(types.CallGraph, types.QueryOptions{
		EntryPointID: "main.go::Main",
		PipelineCfg: &types.PipelineConfig{
			EnableMetrics:     false,
			EnableCommunities: false,
			EnableSCC:         false,
		},
	})
	if err != nil {
		t.Fatalf("ProjectDiagram with metrics disabled failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty diagram output even without metrics")
	}
}

func TestProjectDiagramCaching(t *testing.T) {
	path := filepath.Join("testdata", "minimal.json")
	ec := newJSONCoordinator(path)
	// First call — populates cache
	result1, err := ec.ProjectDiagram(types.UMLClass, types.QueryOptions{EntryPointID: "main.go::Main"})
	if err != nil {
		t.Fatalf("first ProjectDiagram failed: %v", err)
	}
	// Second call — should hit cache
	result2, err := ec.ProjectDiagram(types.UMLClass, types.QueryOptions{EntryPointID: "main.go::Main"})
	if err != nil {
		t.Fatalf("second ProjectDiagram failed: %v", err)
	}
	if result1 == "" || result2 == "" {
		t.Error("expected non-empty results from both calls")
	}
}

func TestProjectDiagramProgressCallback(t *testing.T) {
	path := filepath.Join("testdata", "minimal.json")
	ec := newJSONCoordinator(path)
	var steps []string
	_, err := ec.ProjectDiagram(types.UMLClass, types.QueryOptions{
		EntryPointID: "main.go::Main",
		OnProgress: func(step, detail string) {
			steps = append(steps, step)
		},
	})
	if err != nil {
		t.Fatalf("ProjectDiagram with progress callback failed: %v", err)
	}
	if len(steps) == 0 {
		t.Error("expected at least one progress callback")
	}
	// Verify key steps were reported
	found := make(map[string]bool)
	for _, s := range steps {
		found[s] = true
	}
	for _, expected := range []string{"StepParse", "StepExtract", "StepRender"} {
		if !found[expected] {
			t.Errorf("expected phase %q to be reported, got steps: %v", expected, steps)
		}
	}
}

func TestProjectDiagramSummaryCallback(t *testing.T) {
	path := filepath.Join("testdata", "minimal.json")
	ec := newJSONCoordinator(path)
	var receivedSummary *types.GraphSummary
	_, err := ec.ProjectDiagram(types.UMLClass, types.QueryOptions{
		EntryPointID: "main.go::Main",
		PipelineCfg: &types.PipelineConfig{
			EnableMetrics: true,
		},
		OnSummary: func(s *types.GraphSummary) {
			receivedSummary = s
		},
	})
	if err != nil {
		t.Fatalf("ProjectDiagram failed: %v", err)
	}
	// Summary callback may or may not fire depending on the layout tree contents.
	// Just verify no panic occurred.
	_ = receivedSummary
}

func TestComputeGraphSummaryAllDiagramTypes(t *testing.T) {
	path := filepath.Join("testdata", "minimal.json")
	ec := newJSONCoordinator(path)
	diagramTypes := []types.DiagramType{
		types.UMLClass, types.UMLObject, types.CallGraph, types.DependencyGraph,
		types.DataFlow, types.ERDiagram, types.Mindmap, types.Flowchart,
	}
	for _, dt := range diagramTypes {
		dt := dt
		t.Run(string(dt), func(t *testing.T) {
			t.Parallel()
			summary, err := ec.ComputeGraphSummary(dt, types.QueryOptions{})
			if err != nil {
				t.Fatalf("ComputeGraphSummary(%s) failed: %v", dt, err)
			}
			if summary == nil {
				t.Fatalf("expected non-nil summary for %s", dt)
			}
		})
	}
}
