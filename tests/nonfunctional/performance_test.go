package nonfunctional_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestAnalyzeBenchBudget verifies the `analyze --bench` performance gate:
// the sample project must pass every budget and the gate table must be
// reported (analyze total <= 120s, akg-commit <= 80s, state size <= 50MB).
func TestAnalyzeBenchBudget(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.SampleProject()
	sb.GitInit()

	start := time.Now()
	out, err := harness.RunGmb(t, sb, "analyze", "--bench")
	if err != nil {
		t.Fatalf("analyze --bench failed: %v\n%s", err, out)
	}
	if elapsed := time.Since(start); elapsed > 60*time.Second {
		t.Errorf("analyze --bench took %s, budgeted at 60s", elapsed)
	}
	for _, want := range []string{
		"=== GlassMarble Pipeline Benchmark Gate (Phase 8 / §12.0) ===",
		"analyze total",
		"akg-commit",
		"state size",
		"<= 120.0s",
		"<= 50.0MB",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("benchmark output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "EXCEEDED") {
		t.Errorf("sample project must pass every budget gate:\n%s", out)
	}
}

// TestStatsBenchStaticTable verifies `stats --bench` surfaces the static
// budget-gate table without requiring any prior analysis telemetry.
func TestStatsBenchStaticTable(t *testing.T) {
	sb := harness.NewSandbox(t)
	out, err := harness.RunGmb(t, sb, "stats", "--bench")
	if err != nil {
		t.Fatalf("stats --bench: %v", err)
	}
	for _, want := range []string{
		"GlassMarble Pipeline Benchmark Gate",
		"analyze total          <= 20.0s   PASS",
		"akg-commit             <= 8.0s    PASS",
		"full scan              <= 12.0s   PASS",
		"state size             <= 12.0MB  PASS",
		"json state file        <= 8.0MB   PASS",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stats --bench missing %q:\n%s", want, out)
		}
	}
}

// TestBigGraphScale guards scale behavior on synthetic graphs: status must
// report the seeded size, a full callgraph must render, and the direct
// import+quality path must stay under 10s for a 1000-node document.
func TestBigGraphScale(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteAKGState(harness.BigGraph(500))

	if got := statusNodeCount(t, sb); got != 500 {
		t.Fatalf("status nodes = %d, want 500", got)
	}

	out, err := harness.RunGmb(t, sb, "visualize", "callgraph", "--summary")
	if err != nil {
		t.Fatalf("visualize callgraph on 500-node graph: %v\n%s", err, out)
	}
	if !strings.Contains(out, "=== Graph Summary ===") {
		t.Errorf("summary missing:\n%s", out)
	}

	sb.WriteAKGState(harness.BigGraph(1000))
	raw := sb.ReadFile(".glassmarble/akg.json")
	graph, err := akg.ImportGraphJSON(bytes.NewReader([]byte(raw)))
	if err != nil {
		t.Fatalf("ImportGraphJSON(1000 nodes): %v", err)
	}
	start := time.Now()
	q := akg.MeasureGraphQuality(graph)
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("MeasureGraphQuality took %s, budgeted at 10s", elapsed)
	}
	if q.TotalNodes != 1000 || q.TotalEdges != 999 {
		t.Errorf("quality metrics = (%d nodes, %d edges), want (1000, 999)", q.TotalNodes, q.TotalEdges)
	}
}

// TestMaxJSONMBudgetRefused verifies the --max-json-mb state budget: an
// oversized akg.json must be refused at load, while a small state loads fine
// under the same budget.
func TestMaxJSONMBudgetRefused(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteAKGState(harness.BigGraph(30000))

	out, err := harness.RunGmb(t, sb, "patterns", "--max-json-mb", "1")
	if err == nil {
		t.Fatalf("expected budget refusal, got success:\n%s", out)
	}
	if !strings.Contains(err.Error(), "--max-json-mb budget") {
		t.Errorf("error %q missing budget mention", err.Error())
	}

	small := harness.NewSandbox(t)
	small.WriteAKGState(harness.TinyGraph())
	out, err = harness.RunGmb(t, small, "patterns", "--max-json-mb", "1")
	if err != nil {
		t.Fatalf("small state must load under a 1MB budget: %v\n%s", err, out)
	}
}

// TestMemoryAskLatency bounds the `memory --ask` retrieval path on a seeded
// memory aggregate.
func TestMemoryAskLatency(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.SeedMemory("sample-project")

	start := time.Now()
	out, err := harness.RunGmb(t, sb, "memory", "--ask", "cache")
	if err != nil {
		t.Fatalf("memory --ask: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("memory --ask took %s, budgeted at 5s", elapsed)
	}
	if !strings.Contains(out, "cache") {
		t.Errorf("ask result missing component mention:\n%s", out)
	}
}
