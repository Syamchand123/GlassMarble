package stage4

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// TestCleanupCPGSelfHealsMangledExtID verifies the A-11 cleanup pass:
// legacy mangled ext: node IDs ("ext:akgerrs \"path\"") are rewritten to
// the canonical ext:<escaped> key with edge endpoints remapped.
func TestCleanupCPGSelfHealsMangledExtID(t *testing.T) {
	cpg := NewStage4Output("")
	legacyID := "ext:akgerrs \"github.com/Syamchand123/GlassMarble/internal/errors\""
	want := stage3.ResolveExternalKey("internal/errors")
	apiID := want + "::New"

	cpg.GraphNodes[legacyID] = &ResolvedNode{ID: legacyID, Kind: "EXTERNAL_SDK", Name: "akgerrs"}
	cpg.GraphNodes[apiID] = &ResolvedNode{
		ID:   apiID,
		Kind: "EXTERNAL_API",
		Name: "New",
	}
	cpg.AddEdge(legacyID, apiID, EdgeContains, 0)
	cpg.AddEdge("file:internal/errors/bridge.go", legacyID, EdgeDependsOn, 0)

	// Workspace module prefix is needed to map the full path to the
	// module-relative ext key (§4.1 — module path lives in properties).
	out := &stage3.Stage3Output{
		WorkspaceCtx: &stage3.WorkspaceContext{ModulePrefix: "github.com/Syamchand123/GlassMarble"},
	}
	CleanupCPG(out, cpg)

	if _, ok := cpg.GetNode(legacyID); ok {
		t.Fatalf("mangled node still present after cleanup: %q", legacyID)
	}
	cleaned, ok := cpg.GetNode(want)
	if !ok {
		t.Fatalf("canonical node %q missing after cleanup", want)
	}
	if cleaned.Name != "akgerrs" {
		t.Errorf("Name = %q, want %q (alias preserved)", cleaned.Name, "akgerrs")
	}

	foundContains := false
	foundDepends := false
	for _, edges := range cpg.OutboundEdges {
		for _, e := range edges {
			switch e.Type {
			case EdgeContains:
				if e.SourceID == want && e.TargetID == apiID {
					foundContains = true
				}
			case EdgeDependsOn:
				if e.SourceID == "file:internal/errors/bridge.go" && e.TargetID == want {
					foundDepends = true
				}
			}
		}
	}
	if !foundContains {
		t.Error("CONTAINS edge not remapped to canonical ext ID")
	}
	if !foundDepends {
		t.Error("DEPENDS_ON edge not remapped to canonical ext ID")
	}
}

// TestCleanupCPGStripsModulePrefix verifies module-path removal from
// mangled IDs when the workspace module prefix is known (§4.1).
func TestCleanupCPGStripsModulePrefix(t *testing.T) {
	out := &stage3.Stage3Output{
		WorkspaceCtx: &stage3.WorkspaceContext{ModulePrefix: "github.com/Syamchand123/GlassMarble"},
	}
	cpg := NewStage4Output("")
	legacyID := "ext:github.com/Syamchand123/GlassMarble/internal/errors"
	cpg.GraphNodes[legacyID] = &ResolvedNode{ID: legacyID, Kind: "EXTERNAL_SDK", Name: "github.com/Syamchand123/GlassMarble/internal/errors"}

	CleanupCPG(out, cpg)

	if _, ok := cpg.GetNode(legacyID); ok {
		t.Fatalf("module-prefixed ID not cleaned: %q", legacyID)
	}
	if _, ok := cpg.GetNode(stage3.ResolveExternalKey("internal/errors")); !ok {
		t.Error("expected canonical ext node after module-prefix strip")
	}
}

// TestCleanupCPGProvenanceDefaults verifies §5.4.7: edges without evidence
// get gm:provenance "heuristic"; stamped edges are untouched.
func TestCleanupCPGProvenanceDefaults(t *testing.T) {
	cpg := NewStage4Output("")
	cpg.AddEdge("a", "b", EdgeExtends, 0)
	cpg.AddEdgeProperties("a", "c", EdgeImplements, 0, 1.0,
		map[string]string{"gm:provenance": "signature-match"})

	CleanupCPG(nil, cpg)

	var heuristic, stamped bool
	for _, edges := range cpg.OutboundEdges {
		for _, e := range edges {
			switch e.TargetID {
			case "b":
				if e.Properties["gm:provenance"] != "heuristic" {
					t.Errorf("edge → b provenance = %q, want heuristic", e.Properties["gm:provenance"])
				}
				heuristic = true
			case "c":
				if e.Properties["gm:provenance"] != "signature-match" {
					t.Errorf("edge → c provenance = %q, want signature-match (stamped edges untouched)", e.Properties["gm:provenance"])
				}
				stamped = true
			}
		}
	}
	if !heuristic {
		t.Error("no heuristic-default edge found")
	}
	if !stamped {
		t.Error("stamped edge missing after cleanup")
	}
}

// TestCleanExtID verifies the mangled-ID extractor in isolation.
func TestCleanExtID(t *testing.T) {
	cases := []struct {
		id     string
		prefix string
		want   string
		ok     bool
	}{
		{`ext:akgerrs "github.com/Syamchand123/GlassMarble/internal/errors"`, "github.com/Syamchand123/GlassMarble",
			stage3.ResolveExternalKey("internal/errors"), true},
		{`ext:alias "net/http"`, "", stage3.ResolveExternalKey("net/http"), true},
		{`ext:old path with spaces`, "", stage3.ResolveExternalKey("path with spaces"), true},
		{`ext:internal/errors`, "", stage3.ResolveExternalKey("internal/errors"), true}, // unescaped → self-heal
		{"ext:internal%2Ferrors", "", "", false},                                        // canonical spelling: untouched
		{"module:internal/tui", "", "", false},                                          // not an ext ID
	}
	for _, tc := range cases {
		got, ok := cleanExtID(tc.id, tc.prefix)
		if ok != tc.ok {
			t.Errorf("cleanExtID(%q) ok = %v, want %v", tc.id, ok, tc.ok)
			continue
		}
		if tc.ok && got != tc.want {
			t.Errorf("cleanExtID(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}
