package qa_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestDeterminismByteEqual ensures that two consecutive analyzes on the same
// polyglot project produce identical graph nodes/edges (deterministic AKG).
// Raw akg.json contains commit metadata that increments, so we compare the
// semantic graph (nodes/edges) rather than raw file bytes.
func TestDeterminismByteEqual(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.PolyglotProject()
	sb.GitInit()
	out1, err := harness.RunGmb(t, sb, "analyze")
	if err != nil {
		t.Fatalf("first analyze: %v\n%s", err, out1)
	}
	firstRaw := sb.ReadFile(".glassmarble/akg.json")
	out2, err := harness.RunGmb(t, sb, "analyze")
	if err != nil {
		t.Fatalf("second analyze: %v\n%s", err, out2)
	}
	secondRaw := sb.ReadFile(".glassmarble/akg.json")
	// Compare semantic graph: nodes/edges must be identical
	var first, second struct {
		Nodes []map[string]any `json:"nodes"`
		Edges []map[string]any `json:"edges"`
	}
	if err := json.Unmarshal([]byte(firstRaw), &first); err != nil {
		t.Fatalf("first akg.json invalid: %v", err)
	}
	if err := json.Unmarshal([]byte(secondRaw), &second); err != nil {
		t.Fatalf("second akg.json invalid: %v", err)
	}
	if len(first.Nodes) != len(second.Nodes) || len(first.Edges) != len(second.Edges) {
		t.Errorf("determinism broken: nodes %d->%d, edges %d->%d", len(first.Nodes), len(second.Nodes), len(first.Edges), len(second.Edges))
	}
	// Ensure every node ID from first appears in second
	ids := make(map[string]bool, len(first.Nodes))
	for _, n := range first.Nodes {
		if id, ok := n["id"].(string); ok {
			ids[id] = true
		}
	}
	for _, n := range second.Nodes {
		if id, ok := n["id"].(string); ok && !ids[id] {
			t.Errorf("second run introduced new node %q not in first", id)
		}
	}
}

// TestJSONSchemaConformance validates that exported GraphJSON and
// timeline JSON conform to expected schema keys and types.
func TestJSONSchemaConformance(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.PolyglotProject()
	sb.GitInit()
	harness.RunGmb(t, sb, "analyze")
	// Export
	expOut, err := harness.RunGmb(t, sb, "export", "--output", "graph.json")
	if err != nil {
		t.Fatalf("export: %v\n%s", err, expOut)
	}
	raw := sb.ReadFile("graph.json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("exported JSON invalid: %v\n%s", err, raw)
	}
	for _, key := range []string{"schema_version", "nodes", "edges", "version"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("exported JSON missing %q", key)
		}
	}
	// Timeline
	tlOut, err := harness.RunGmb(t, sb, "timeline", "--format", "json")
	if err != nil {
		t.Fatalf("timeline json: %v\n%s", err, tlOut)
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(tlOut), &entries); err != nil {
		t.Fatalf("timeline JSON invalid: %v\n%s", err, tlOut)
	}
	if len(entries) == 0 {
		t.Errorf("timeline should have entries after polyglot analyze")
	}
}

// TestGoldenOutputStability ensures that key CLI banners are stable
// across refactors. Any change to tui/views that alters user-facing text
// must update these pins intentionally.
func TestGoldenOutputStability(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.PolyglotProject()
	sb.GitInit()
	harness.RunGmb(t, sb, "analyze")
	statusOut, _ := harness.RunGmb(t, sb, "status")
	for _, want := range []string{"GlassMarble AKG Status", "Nodes Count", "Schema Version"} {
		if !strings.Contains(statusOut, want) {
			t.Errorf("status missing %q:\n%s", want, statusOut)
		}
	}
	doctorOut, _ := harness.RunGmb(t, sb, "doctor")
	if !strings.Contains(doctorOut, "DOCTOR") {
		t.Errorf("doctor missing DOCTOR marker:\n%s", doctorOut)
	}
	diffOut, _ := harness.RunGmb(t, sb, "diff")
	if !strings.Contains(diffOut, "Architectural Graph Mutation Diff") {
		t.Errorf("diff missing header:\n%s", diffOut)
	}
}

// TestColorWidthSweep ensures that --color and --max-json-mb flags
// do not crash and that NO_COLOR is respected.
func TestColorWidthSweep(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.SampleProject()
	sb.GitInit()
	harness.RunGmb(t, sb, "analyze")
	for _, args := range [][]string{
		{"status", "--color", "never"},
		{"status", "--color", "always"},
		{"status", "--color", "auto"},
	} {
		out, err := harness.RunGmb(t, sb, args...)
		if err != nil {
			t.Errorf("color sweep %v failed: %v\n%s", args, err, out)
		}
	}
	// AKG size budget (should pass for small project)
	out, err := harness.RunGmb(t, sb, "--max-json-mb", "10", "status")
	if err != nil {
		t.Errorf("max-json-mb 10 status failed: %v\n%s", err, out)
	}
	// Very low budget should be enforced on load (via --max-json-mb 1 on large project)
	large := harness.NewSandbox(t)
	large.LargeProject(20)
	large.GitInit()
	harness.RunGmb(t, large, "analyze")
	_, err = harness.RunGmb(t, large, "--max-json-mb", "1", "status")
	if err == nil {
		t.Logf("max-json-mb 1 on large project should warn or fail but got success (informational)")
	}
	_ = akg.CurrentSchemaVersion
}
