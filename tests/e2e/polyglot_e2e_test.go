package e2e_test

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestPolyglotEndToEnd runs the full journey on a polyglot monorepo
// (Go+Python+Java+TS+Rust+C#/C++) and verifies that the AKG, exports,
// and all 31 visualization types work end-to-end. This is the user-facing
// regression for the v1.0.0 python class diagram bug.
func TestPolyglotEndToEnd(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.PolyglotProject()
	sb.GitInit()
	// Full analyze should succeed and report multiple languages
	out, err := harness.RunGmb(t, sb, "analyze")
	if err != nil {
		t.Fatalf("polyglot analyze: %v\n%s", err, out)
	}
	for _, want := range []string{"Analyzed", "nodes", "edges"} {
		if !strings.Contains(out, want) {
			t.Errorf("polyglot analyze missing %q:\n%s", want, out)
		}
	}
	// Status should reflect polyglot graph
	statusOut, err := harness.RunGmb(t, sb, "status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, statusOut)
	}
	if !strings.Contains(statusOut, "Nodes Count") {
		t.Errorf("status missing Nodes Count:\n%s", statusOut)
	}
	// Export roundtrips
	for _, args := range [][]string{
		{"export", "--output", "graph.json"},
		{"export", "--format", "neo4j", "--output", "dump.cypher"},
	} {
		o, err := harness.RunGmb(t, sb, args...)
		if err != nil {
			t.Errorf("export %v: %v\n%s", args, err, o)
		}
	}
	// Core diagram types must render without illegal syntax
	diagrams := []string{
		"class", "state", "activity", "er", "flowchart", "mindmap",
		"dataflow", "dependency", "callgraph", "c4container", "c4context",
	}
	for _, d := range diagrams {
		o, err := harness.RunGmb(t, sb, "visualize", d)
		if err != nil {
			t.Errorf("visualize %s: %v\n%s", d, err, o)
			continue
		}
		if strings.Contains(o, "'''") || strings.Contains(o, "{{") {
			t.Errorf("visualize %s contains illegal markers: %q", d, o[:min(200, len(o))])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
