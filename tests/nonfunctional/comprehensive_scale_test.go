package nonfunctional_test

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestScaleLargeProject verifies that analyze scales to 100+ files and
// that the benchmark gates still pass. This extends the previous 6-file
// budget test to a realistic large-monorepo scenario.
func TestScaleLargeProject(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.LargeProject(80)
	sb.GitInit()
	out, err := harness.RunGmb(t, sb, "analyze", "--bench")
	if err != nil {
		t.Fatalf("analyze --bench on large project: %v\n%s", err, out)
	}
	for _, want := range []string{"analyze total", "akg-commit", "state size", "PASS"} {
		if !strings.Contains(out, want) {
			t.Errorf("large project bench missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "EXCEEDED") {
		t.Errorf("large project should pass budget gates:\n%s", out)
	}
}

// TestConcurrencyPolyglot ensures that ingestion with 4 workers on a
// polyglot repo does not deadlock or lose files.
func TestConcurrencyPolyglot(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.PolyglotProject()
	// Run twice with different worker counts and compare file counts
	out1, err1 := harness.RunGmb(t, sb, "analyze", "--workers", "1", "--json")
	if err1 != nil {
		t.Fatalf("analyze --workers 1: %v\n%s", err1, out1)
	}
	out2, err2 := harness.RunGmb(t, sb, "analyze", "--workers", "4", "--json")
	if err2 != nil {
		t.Fatalf("analyze --workers 4: %v\n%s", err2, out2)
	}
	cfg1, cfg2 := out1, out2
	if !strings.Contains(cfg1, "\"files_analyzed\"") || !strings.Contains(cfg2, "\"files_analyzed\"") {
		t.Errorf("concurrency test: json output missing files_analyzed")
	}
	// Both should analyze same number of files (deterministic)
	// Extract files_analyzed via simple string search (avoid json dep)
	extract := func(s string) string {
		idx := strings.Index(s, "\"files_analyzed\"")
		if idx == -1 {
			return ""
		}
		end := strings.Index(s[idx:], ",")
		if end == -1 {
			end = 20
		}
		return s[idx : idx+end]
	}
	if extract(cfg1) != extract(cfg2) {
		t.Errorf("worker count changed file count: 1-worker %q vs 4-worker %q", extract(cfg1), extract(cfg2))
	}
}

// TestResilienceVisualizationStress ensures that the full pipeline
// (analyze + visualize) does not panic on adversarial python code.
func TestResilienceVisualizationStress(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.VisualizationStressProject()
	sb.GitInit()
	out, err := harness.RunGmb(t, sb, "analyze")
	if err != nil {
		t.Fatalf("analyze stress: %v\n%s", err, out)
	}
	// Every diagram type should render without error, even on stress input
	for _, d := range []string{"class", "dependency", "er", "flowchart"} {
		o, err := harness.RunGmb(t, sb, "visualize", d)
		if err != nil {
			t.Errorf("visualize %s on stress project failed: %v\n%s", d, err, o)
		}
		if strings.TrimSpace(o) == "" {
			t.Errorf("visualize %s empty on stress project", d)
		}
	}
}
