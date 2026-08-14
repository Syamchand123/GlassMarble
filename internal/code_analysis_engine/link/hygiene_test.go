package link

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// TestApplySyntheticHygiene covers §5.4.4/A-14:
//   - path derivable from node ID → real FileSpec (scopeable)
//   - no path derivable → synthetic marker, no belongsToFile source
//   - orphan CFG branch nodes without paths are dropped with their edges
func TestApplySyntheticHygiene(t *testing.T) {
	cpg := NewLinkOutput("")
	cpg.GraphNodes["internal/tui/programs/analyze/program.go::Dispatcher::Dispatch::CFG_SUMMARY"] = &ResolvedNode{
		ID:   "internal/tui/programs/analyze/program.go::Dispatcher::Dispatch::CFG_SUMMARY",
		Kind: "CFG_SUMMARY",
		Name: "Control Flow Summary",
	}
	cpg.GraphNodes["virt:QUEUE"] = &ResolvedNode{ID: "virt:QUEUE", Kind: "VIRTUAL_QUEUE", Name: "queue"}
	cpg.GraphNodes["ext:internal%2Ferrors"] = &ResolvedNode{ID: "ext:internal%2Ferrors", Kind: "EXTERNAL_SDK", Name: "errors"}
	cpg.GraphNodes["main.go::helper::IF_BRANCH"] = &ResolvedNode{ID: "main.go::helper::IF_BRANCH", Kind: "IF_BRANCH", Name: "IF_BRANCH"}
	cpg.GraphNodes["VirtualBranch::LOOP_BRANCH"] = &ResolvedNode{ID: "VirtualBranch::LOOP_BRANCH", Kind: "LOOP_BRANCH", Name: "LOOP_BRANCH"}
	cpg.GraphNodes["orphan"] = &ResolvedNode{ID: "orphan", Kind: "CFG_FLOW", Name: "orphan"}

	cpg.AddEdge("caller", "virt:QUEUE", EdgeSendsTo, 1)
	cpg.AddEdge("orphan", "caller", EdgeControlFlow, 2)
	cpg.AddEdge("caller", "orphan", EdgeControlFlow, 3)

	ApplySyntheticHygiene(cpg)

	// 1. Derivable path stamped.
	summary, ok := cpg.GetNode("internal/tui/programs/analyze/program.go::Dispatcher::Dispatch::CFG_SUMMARY")
	if !ok {
		t.Fatal("CFG_SUMMARY node missing after hygiene")
	}
	if summary.FileSpec.Path != "internal/tui/programs/analyze/program.go" {
		t.Errorf("CFG_SUMMARY FileSpec.Path = %q, want derived path", summary.FileSpec.Path)
	}

	// 2. Un-derivable virtuals marked synthetic, no path.
	for _, id := range []string{"virt:QUEUE", "ext:internal%2Ferrors"} {
		n, ok := cpg.GetNode(id)
		if !ok {
			t.Fatalf("node %s missing after hygiene", id)
		}
		if n.FileSpec.Path != "" {
			t.Errorf("%s FileSpec.Path = %q, want empty (no belongsToFile)", id, n.FileSpec.Path)
		}
		if n.Properties[ont.PredSynthetic] != "true" {
			t.Errorf("%s missing gm:synthetic marker", id)
		}
	}

	// 3. Orphan CFG-only nodes dropped together with their edges.
	if _, ok := cpg.GetNode("orphan"); ok {
		t.Error("orphan CFG_FLOW node not dropped")
	}
	if _, ok := cpg.GetNode("VirtualBranch::LOOP_BRANCH"); ok {
		t.Error("pathless LOOP_BRANCH (no slash, no extension) not dropped")
	}
	// main.go::helper::IF_BRANCH derives "main.go" — it is scopeable and survives.
	if _, ok := cpg.GetNode("main.go::helper::IF_BRANCH"); !ok {
		t.Error("IF_BRANCH with derivable top-level path should survive")
	}
	for _, edges := range cpg.OutboundEdges {
		for _, e := range edges {
			if e.SourceID == "orphan" || e.TargetID == "orphan" {
				t.Errorf("edge touching dropped orphan survives: %s → %s", e.SourceID, e.TargetID)
			}
		}
	}
}

// TestDeriveFilePath covers the ID→path extractor.
func TestDeriveFilePath(t *testing.T) {
	cases := []struct{ id, want string }{
		{"internal/tui/programs/analyze/program.go::Dispatcher::Dispatch", "internal/tui/programs/analyze/program.go"},
		{"main.go::helper::CFG_SUMMARY", "main.go"},
		{"ext:internal%2Ferrors::New", ""},
		{"file:src/a.go", ""},
		{"module:internal/tui", ""},
		{"virt:QUEUE", ""},
		{"noNamespaceAtAll", ""},
		{"src/store.go", ""}, // no "::" → not a CPG-style ID
	}
	for _, tc := range cases {
		if got := deriveFilePath(tc.id); got != tc.want {
			t.Errorf("deriveFilePath(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

// TestRemoveNode verifies incident edges are cleaned up on node removal.
func TestRemoveNode(t *testing.T) {
	cpg := NewLinkOutput("")
	cpg.GraphNodes["a"] = &ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"}
	cpg.GraphNodes["b"] = &ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"}
	cpg.AddEdge("a", "b", EdgeCalls, 1)
	cpg.AddEdge("b", "a", EdgeCalls, 2)
	cpg.AddEdge("a", "c", EdgeCalls, 3)

	cpg.RemoveNode("a")

	if _, ok := cpg.GetNode("a"); ok {
		t.Error("node a not removed")
	}
	for _, edges := range cpg.OutboundEdges {
		for _, e := range edges {
			if e.SourceID == "a" || e.TargetID == "a" {
				t.Errorf("edge touching removed node survives: %s → %s (%s)", e.SourceID, e.TargetID, e.Type)
			}
		}
	}
	if len(cpg.OutboundEdges["b"]) != 0 {
		t.Errorf("b still has %d outbound edges after a removed", len(cpg.OutboundEdges["b"]))
	}
}
