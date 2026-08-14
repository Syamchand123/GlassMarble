package stages_test

// Architecture Intelligence (arch_intelligence) tests against a REAL analyzed graph produced
// by running phases 1-4 and committing through the AKG transaction manager
// (see pipeline_test.go).
//
// Discrepancies from API_REFERENCE.md:
//   - The pattern/smell rule ID constants (PR01-PR07, SD01-SD07) are not
//     exported; rule IDs surface as evidence EvidenceItem.Reference strings
//     "PR-01".."PR-07" / "SD-01".."SD-07".
//   - archmodel.DetectedPattern has no ID field (only Kind/Name/Evidence).
//   - RunPatternDetection / RunSmellDetection take (graph, metrics) and
//     return slices, matching reality in pattern_detector.go /
//     smell_detector.go.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// firstReference returns the reference of the first evidence item in the
// bundle (rule IDs such as "PR-01" / "SD-01" surface there).
func firstReference(b evidence.Bundle) string {
	if b.IsEmpty() {
		return ""
	}
	return b.Items[0].Reference
}

func TestIntelligenceRunOnAnalyzedGraph(t *testing.T) {
	sb := harness.NewSandbox(t)
	graph := analyzeProject(t, sb)

	engine := arch_intelligence.NewEngine(graph)
	res := engine.Run()

	if got := engine.Graph(); got != graph {
		t.Error("Engine.Graph() does not return the analyzed graph")
	}
	if res.Metrics.TotalNodes == 0 {
		t.Error("Metrics.TotalNodes = 0, want > 0 on the sample project")
	}
	if len(res.Components) == 0 {
		t.Fatalf("no components inferred from a %d-node graph", res.Metrics.TotalNodes)
	}
	for _, c := range res.Components {
		if len(c.Directories) == 0 {
			t.Errorf("component %q has no directories", c.Name)
		}
	}
	// The sample project has a "Repo" struct: PR-07 (Repository Pattern)
	// must fire.
	if len(res.Patterns) == 0 {
		t.Error("no patterns detected (expected at least PR-07: the sample has a Repo struct)")
	}
	for _, p := range res.Patterns {
		ref := firstReference(p.Evidence)
		if !strings.HasPrefix(ref, "PR-") {
			t.Errorf("pattern %q evidence reference = %q, want PR- prefix", p.Name, ref)
		}
	}
	for _, s := range res.Smells {
		ref := firstReference(s.Evidence)
		if !strings.HasPrefix(ref, "SD-") {
			t.Errorf("smell %q evidence reference = %q, want SD- prefix", s.Title, ref)
		}
	}
	if res.GraphHash == "" {
		t.Error("GraphHash is empty")
	}
}

func TestIntelligenceGraphHashDeterminism(t *testing.T) {
	sb := harness.NewSandbox(t)
	graph := analyzeProject(t, sb)

	first := arch_intelligence.NewEngine(graph).Run()
	second := arch_intelligence.NewEngine(graph).Run()

	if first.GraphHash != second.GraphHash {
		t.Errorf("GraphHash not deterministic: %q vs %q", first.GraphHash, second.GraphHash)
	}
	if len(first.Components) != len(second.Components) ||
		len(first.Patterns) != len(second.Patterns) ||
		len(first.Smells) != len(second.Smells) {
		t.Errorf("result sizes differ between runs: components %d/%d patterns %d/%d smells %d/%d",
			len(first.Components), len(second.Components),
			len(first.Patterns), len(second.Patterns),
			len(first.Smells), len(second.Smells))
	}
}

func TestIntelligenceLoadLatestResult(t *testing.T) {
	sb := harness.NewSandbox(t)
	graph := analyzeProject(t, sb)
	res := arch_intelligence.NewEngine(graph).Run()

	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatalf("marshal IntelligenceResult: %v", err)
	}
	sb.WriteFile(filepath.Join(".glassmarble", "intelligence", "latest.json"), string(data))

	loaded, err := arch_intelligence.LoadLatestResult(sb.GmDir)
	if err != nil {
		t.Fatalf("LoadLatestResult: %v", err)
	}
	if loaded.GraphHash != res.GraphHash {
		t.Errorf("loaded GraphHash = %q, want %q", loaded.GraphHash, res.GraphHash)
	}
	if loaded.Metrics.TotalNodes != res.Metrics.TotalNodes {
		t.Errorf("loaded TotalNodes = %d, want %d", loaded.Metrics.TotalNodes, res.Metrics.TotalNodes)
	}

	if _, err := arch_intelligence.LoadLatestResult(filepath.Join(sb.Root, "no-such-dir")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing latest.json: got %v, want os.ErrNotExist", err)
	}
}

func TestIntelligenceMetricSummary(t *testing.T) {
	sb := harness.NewSandbox(t)
	graph := analyzeProject(t, sb)
	metrics := arch_intelligence.CalculateMetrics(graph)
	if metrics.TotalNodes == 0 {
		t.Fatal("CalculateMetrics produced an empty metric set")
	}
	summary := arch_intelligence.MetricSummary(metrics)
	if summary == "" || !strings.Contains(summary, "nodes") {
		t.Errorf("MetricSummary = %q, want a non-empty node-count summary", summary)
	}
}

func TestIntelligenceWithClock(t *testing.T) {
	sb := harness.NewSandbox(t)
	graph := analyzeProject(t, sb)
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	res := arch_intelligence.NewEngineWithOptions(graph,
		arch_intelligence.WithClock(func() time.Time { return fixed }),
	).Run()

	if res.Metrics.TotalNodes == 0 || len(res.Components) == 0 {
		t.Fatalf("fixed-clock run produced an empty result (%d nodes, %d components)",
			res.Metrics.TotalNodes, len(res.Components))
	}
	for _, c := range res.Components {
		for _, item := range c.Evidence.Items {
			if !item.Timestamp.Equal(fixed) {
				t.Errorf("component %q evidence timestamp = %v, want fixed clock %v", c.Name, item.Timestamp, fixed)
			}
		}
	}
}

func TestIntelligenceLayerForbidden(t *testing.T) {
	sb := harness.NewSandbox(t)
	graph := analyzeProject(t, sb)

	cfg := config.DefaultIntelligenceConfig()
	cfg.ArchLayers = []config.DriftLayer{
		{Name: "UI", Paths: []string{"cmd/**"}},
		{Name: "App", Paths: []string{"internal/service/**"}},
		{Name: "Data", Paths: []string{"internal/repo/**"}},
	}
	forbidden := []config.ForbiddenDepRule{
		{Source: "App", Target: "Data", Reason: "services must not reach the repository layer directly"},
	}

	// Declared layers alone (no forbidden pairs) must not produce violations
	// for the sample's cmd->service->repo chain (it is consistently downward).
	baseline := arch_intelligence.NewEngineWithOptions(graph, arch_intelligence.WithConfig(cfg)).Run()
	if baseline.Metrics.LayerViolationCount != 0 {
		t.Fatalf("baseline LayerViolationCount = %d, want 0", baseline.Metrics.LayerViolationCount)
	}

	res := arch_intelligence.NewEngineWithOptions(graph,
		arch_intelligence.WithConfig(cfg),
		arch_intelligence.WithLayerForbidden(forbidden),
	).Run()
	if res.Metrics.LayerViolationCount == 0 {
		t.Fatal("forbidden App->Data pair produced no layer violations (service -> repo edges exist)")
	}
	found := false
	for _, s := range res.Smells {
		if s.Kind == archmodel.SmellLayerViolation {
			found = true
			if !strings.HasPrefix(firstReference(s.Evidence), "SD-") {
				t.Errorf("layer-violation smell reference = %q, want SD- prefix", firstReference(s.Evidence))
			}
		}
	}
	if !found {
		t.Errorf("no LAYER_VIOLATION smell despite %d forbidden edges", res.Metrics.LayerViolationCount)
	}
}

func TestIntelligenceRuleDetectionFunctions(t *testing.T) {
	sb := harness.NewSandbox(t)
	graph := analyzeProject(t, sb)

	metrics := arch_intelligence.CalculateMetrics(graph)

	patterns := arch_intelligence.RunPatternDetection(graph, metrics)
	if len(patterns) == 0 {
		t.Fatal("RunPatternDetection returned no patterns on the sample project")
	}
	for _, p := range patterns {
		if !strings.HasPrefix(firstReference(p.Evidence), "PR-") {
			t.Errorf("pattern %q evidence reference = %q, want PR- prefix", p.Name, firstReference(p.Evidence))
		}
	}

	smells := arch_intelligence.RunSmellDetection(graph, metrics)
	for _, s := range smells {
		if !strings.HasPrefix(firstReference(s.Evidence), "SD-") {
			t.Errorf("smell %q evidence reference = %q, want SD- prefix", s.Title, firstReference(s.Evidence))
		}
	}
}

func TestIntelligenceInferComponents(t *testing.T) {
	sb := harness.NewSandbox(t)
	graph := analyzeProject(t, sb)

	components := arch_intelligence.InferComponents(graph)
	if len(components) == 0 {
		t.Fatal("InferComponents returned no components")
	}
	for _, c := range components {
		if c.ID == "" || c.Name == "" {
			t.Errorf("component with empty identity: %+v", c)
		}
		if len(c.Directories) == 0 {
			t.Errorf("component %q has no directories", c.Name)
		}
		if len(c.NodeIDs) == 0 {
			t.Errorf("component %q has no nodes", c.Name)
		}
	}
}
