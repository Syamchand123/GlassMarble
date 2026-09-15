// Package invalidator — dossier_extra_test.go
//
// Whitebox coverage for BuildDossier structural events (component, cycle,
// layer, surface), keyword suppression when graphs are present, the
// nil-graph fallback, and SectionHash stability. Reuses dossierTestNode,
// dossierTestGraph, and dossierAddEdge from dossier_test.go (same package).
package invalidator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ────────────────────────────────────────────────────────────────────────────
// Structural events through the real BuildDossier entry point
// ────────────────────────────────────────────────────────────────────────────

func TestExtraDossierComponentAdded(t *testing.T) {
	serve := dossierTestNode("internal/a/f.go::Serve", "internal/a/f.go", "Serve", "func Serve()")
	base := dossierTestGraph("base", serve)
	head := dossierTestGraph("head", serve,
		dossierTestNode("internal/b/g.go::Handle", "internal/b/g.go", "Handle", "func Handle()"))

	dossier, err := BuildDossier("", "", base, head)
	require.NoError(t, err)
	require.NotNil(t, dossier)
	assert.Contains(t, dossier.ArchEvents, "COMPONENT_ADDED")
	assert.Contains(t, dossier.ArchEvents, "PUBLIC_SURFACE_CHANGED")
	require.Len(t, dossier.AddedSymbols, 1)
	assert.Equal(t, "internal/b/g.go::Handle", dossier.AddedSymbols[0].FQN)
}

func TestExtraDossierComponentRemoved(t *testing.T) {
	serve := dossierTestNode("internal/a/f.go::Serve", "internal/a/f.go", "Serve", "func Serve()")
	gone := dossierTestNode("internal/b/g.go::Handle", "internal/b/g.go", "Handle", "func Handle()")
	base := dossierTestGraph("base", serve, gone)
	head := dossierTestGraph("head", serve)

	dossier, err := BuildDossier("", "", base, head)
	require.NoError(t, err)
	assert.Contains(t, dossier.ArchEvents, "COMPONENT_REMOVED")
	assert.Contains(t, dossier.RemovedSymbols, "internal/b/g.go::Handle")
}

func TestExtraDossierCycleIntroduced(t *testing.T) {
	a := dossierTestNode("internal/svc/a.go::A", "internal/svc/a.go", "A", "func A()")
	b := dossierTestNode("internal/svc/a.go::B", "internal/svc/a.go", "B", "func B()")
	base := dossierTestGraph("base", a, b)
	head := dossierTestGraph("head",
		dossierTestNode("internal/svc/a.go::A", "internal/svc/a.go", "A", "func A()"),
		dossierTestNode("internal/svc/a.go::B", "internal/svc/a.go", "B", "func B()"))
	dossierAddEdge(head, a.ID, b.ID, link.EdgeCalls)
	dossierAddEdge(head, b.ID, a.ID, link.EdgeCalls)

	dossier, err := BuildDossier("", "", base, head)
	require.NoError(t, err)
	assert.Equal(t, []string{"CYCLE_INTRODUCED"}, dossier.ArchEvents)
}

func TestExtraDossierLayerViolation(t *testing.T) {
	load := dossierTestNode("pkg/store/s.go::Load", "pkg/store/s.go", "Load", "func Load()")
	main := dossierTestNode("cmd/app/m.go::Main", "cmd/app/m.go", "Main", "func Main()")
	base := dossierTestGraph("base", load, main)
	head := dossierTestGraph("head",
		dossierTestNode("pkg/store/s.go::Load", "pkg/store/s.go", "Load", "func Load()"),
		dossierTestNode("cmd/app/m.go::Main", "cmd/app/m.go", "Main", "func Main()"))
	// Upward dependency pkg (rank 1) → cmd (rank 3) is forbidden.
	dossierAddEdge(head, load.ID, main.ID, link.EdgeCalls)

	dossier, err := BuildDossier("", "", base, head)
	require.NoError(t, err)
	assert.Equal(t, []string{"LAYER_VIOLATION"}, dossier.ArchEvents)
}

func TestExtraDossierPublicSurfaceChanged(t *testing.T) {
	base := dossierTestGraph("base",
		dossierTestNode("internal/a/f.go::DoThing", "internal/a/f.go", "DoThing", "func DoThing() int"))
	head := dossierTestGraph("head",
		dossierTestNode("internal/a/f.go::DoThing", "internal/a/f.go", "DoThing", "func DoThing() string"))

	dossier, err := BuildDossier("", "", base, head)
	require.NoError(t, err)
	assert.Equal(t, []string{"PUBLIC_SURFACE_CHANGED"}, dossier.ArchEvents)
	require.Len(t, dossier.ModifiedSymbols, 1)
	assert.Equal(t, "internal/a/f.go::DoThing", dossier.ModifiedSymbols[0].FQN)
}

func TestExtraDossierQuietGraphsNoEvents(t *testing.T) {
	serve := dossierTestNode("internal/a/f.go::Serve", "internal/a/f.go", "Serve", "func Serve()")
	base := dossierTestGraph("base", serve)
	head := dossierTestGraph("head",
		dossierTestNode("internal/a/f.go::Serve", "internal/a/f.go", "Serve", "func Serve()"))
	dossier, err := BuildDossier("", "somehash", base, head)
	require.NoError(t, err)
	assert.Empty(t, dossier.ArchEvents)
	assert.Equal(t, "somehash", dossier.CommitHash)
}

// ────────────────────────────────────────────────────────────────────────────
// Keyword suppression with graphs present (B3b policy via BuildDossier)
// ────────────────────────────────────────────────────────────────────────────

func extraKeywordRepo(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "extra@test.local")
	git("config", "user.name", "Extra Test")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	// Keyword-rich message: split + cycle + database + layer + service +
	// interface + auth → every keyword event fires without graphs.
	git("commit", "-qm", "Split auth service into layers\n\nExtract database interface to break cycle")
	return dir, git("rev-parse", "HEAD")
}

func TestExtraDossierKeywordFallbackWithoutGraphs(t *testing.T) {
	dir, hash := extraKeywordRepo(t)
	dossier, err := BuildDossier(dir, hash, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, dossier)
	for _, want := range []string{
		"COMPONENT_SPLIT", "CYCLE_INTRODUCED", "NEW_DATABASE_LAYER",
		"LAYER_VIOLATION", "SERVICE_ADDED", "INTERFACE_CHANGED",
		"SECURITY_BOUNDARY_CHANGED",
	} {
		assert.Contains(t, dossier.ArchEvents, want, "keyword fallback must fire without graphs")
	}
}

func TestExtraDossierKeywordSuppressedWithGraphs(t *testing.T) {
	dir, hash := extraKeywordRepo(t)
	// Structurally quiet graphs: identical nodes, no edges. The structural
	// pass is authoritative ("no architectural change"), so the keyword
	// proxies from the same commit message must be suppressed entirely.
	serve := dossierTestNode("internal/a/f.go::Serve", "internal/a/f.go", "Serve", "func Serve()")
	base := dossierTestGraph("base", serve)
	head := dossierTestGraph("head",
		dossierTestNode("internal/a/f.go::Serve", "internal/a/f.go", "Serve", "func Serve()"))

	dossier, err := BuildDossier(dir, hash, base, head)
	require.NoError(t, err)
	assert.Empty(t, dossier.ArchEvents,
		"keyword proxies must be suppressed whenever either graph is available")
}

func TestExtraMergeArchEventsForGraphsMatrix(t *testing.T) {
	head := dossierTestGraph("head",
		dossierTestNode("internal/a/f.go::Serve", "internal/a/f.go", "Serve", "func Serve()"))
	// Either graph present → keywords dropped, structural kept.
	assert.Equal(t, []string{"A"},
		mergeArchEventsForGraphs([]string{"A"}, []string{"B"}, nil, head))
	assert.Equal(t, []string{"A"},
		mergeArchEventsForGraphs([]string{"A"}, []string{"B"}, head, nil))
	assert.Equal(t, []string{"A"},
		mergeArchEventsForGraphs([]string{"A"}, []string{"B"}, head, head))
	// Both nil → keyword fallback appended after structural, deduped.
	assert.Equal(t, []string{"A", "B"},
		mergeArchEventsForGraphs([]string{"A"}, []string{"B"}, nil, nil))
	assert.Equal(t, []string{"B"},
		mergeArchEventsForGraphs(nil, []string{"B"}, nil, nil))
}

// ────────────────────────────────────────────────────────────────────────────
// Nil-graph fallback
// ────────────────────────────────────────────────────────────────────────────

func TestExtraDossierNilGraphs(t *testing.T) {
	dossier, err := BuildDossier("", "hash-only", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, dossier)
	assert.Equal(t, "hash-only", dossier.CommitHash)
	assert.Empty(t, dossier.AddedSymbols)
	assert.Empty(t, dossier.ModifiedSymbols)
	assert.Empty(t, dossier.RemovedSymbols)
	assert.Empty(t, dossier.ArchEvents)
	assert.Empty(t, dossier.DirtySections, "DirtySections stays empty until invalidation runs")
}

func TestExtraDossierHeadOnlyTreatsBaseAsEmpty(t *testing.T) {
	head := dossierTestGraph("head",
		dossierTestNode("internal/a/f.go::Serve", "internal/a/f.go", "Serve", "func Serve()"))
	dossier, err := BuildDossier("", "", nil, head)
	require.NoError(t, err)
	require.Len(t, dossier.AddedSymbols, 1, "nil base means every head node is new")
	assert.Contains(t, dossier.ArchEvents, "COMPONENT_ADDED")
}

// TestExtraDossierConfigPackageDoesNotFalsePositiveAddedConfigVars guards
// against a real bug found via live end-to-end testing: isConfigVar checks
// the FQN for a "config"/"getenv" substring, which used to match on the
// PACKAGE PATH alone — so a brand new file, function, or parameter added
// under any "pkg/config/..." path (an extremely common package name) was
// reported as a newly added environment variable, even though none of
// them are one. Gating on Kind == "CALL" (the shape a real env-read
// expression node would have) fixes this without touching isConfigVar's
// own substring logic, which a genuine CALL-kind match still needs.
func TestExtraDossierConfigPackageDoesNotFalsePositiveAddedConfigVars(t *testing.T) {
	base := dossierTestGraph("base")
	head := dossierTestGraph("head",
		dossierTestNode("pkg/config/config.go::envInt", "pkg/config/config.go", "envInt", "func envInt(name string, def int) int"))

	dossier, err := BuildDossier("", "", base, head)
	require.NoError(t, err)
	require.Len(t, dossier.AddedSymbols, 1)
	assert.Empty(t, dossier.AddedConfigVars,
		"a FUNCTION node under a package merely named \"config\" must not be reported as an added config var: %+v", dossier.AddedConfigVars)
}

// TestExtraDossierCallKindConfigVarStillDetected is the positive
// counterpart to the guard above: a real Kind:"CALL" node whose FQN looks
// like an env read must still be picked up. AddedConfigVars.Name here is
// fact.FQN (the node's full id), not its short display name — matching
// BuildDossier's actual field wiring above, distinct from the grounding
// package's own collectConfigVars which keys off the node's short Name.
func TestExtraDossierCallKindConfigVarStillDetected(t *testing.T) {
	base := dossierTestGraph("base")
	envCall := dossierTestNode("internal/auth/config.go::JWTSecretKey", "internal/auth/config.go", "JWTSecretKey", "")
	envCall.Kind = "CALL"
	head := dossierTestGraph("head", envCall)

	dossier, err := BuildDossier("", "", base, head)
	require.NoError(t, err)
	require.Len(t, dossier.AddedConfigVars, 1)
	assert.Equal(t, "internal/auth/config.go::JWTSecretKey", dossier.AddedConfigVars[0].Name)
}

// ────────────────────────────────────────────────────────────────────────────
// SectionHash stability
// ────────────────────────────────────────────────────────────────────────────

func extraHashDoc() (*config.DocSpec, *config.SectionSpec) {
	doc := &config.DocSpec{
		ID:         "svc",
		TargetPath: "docs/svc.md",
		Scope:      config.ScopeRule{Paths: []string{"internal/svc/**"}},
	}
	sec := &config.SectionSpec{
		ID: "api", Title: "API", Instruction: "Table of endpoints",
		GroundWith: []string{"signatures"}, Managed: true,
	}
	return doc, sec
}

func TestExtraSectionHashStability(t *testing.T) {
	doc, sec := extraHashDoc()
	g := dossierTestGraph("head",
		dossierTestNode("internal/svc/a.go::Serve", "internal/svc/a.go", "Serve", "func Serve()"))

	first := SectionHash(doc, sec, g)
	second := SectionHash(doc, sec, g)
	if first == "" {
		t.Fatal("SectionHash must be non-empty for a real section")
	}
	if first != second {
		t.Errorf("SectionHash unstable: %q vs %q", first, second)
	}
	// Instruction participates: a rewrite must re-hash.
	altered := *sec
	altered.Instruction = "Table of endpoints and errors"
	if got := SectionHash(doc, &altered, g); got == first {
		t.Error("changed instruction must change SectionHash")
	}
	// Nil inputs hash to empty (callers treat "" as always-dirty, never clean).
	if got := SectionHash(nil, sec, g); got != "" {
		t.Errorf("nil doc SectionHash = %q, want empty", got)
	}
	if got := SectionHash(doc, nil, g); got != "" {
		t.Errorf("nil section SectionHash = %q, want empty", got)
	}
	// Nil graph still hashes deterministically (scope contributes nothing).
	empty1 := SectionHash(doc, sec, nil)
	empty2 := SectionHash(doc, sec, nil)
	if empty1 == "" || empty1 != empty2 {
		t.Errorf("nil-graph SectionHash must be stable non-empty, got %q vs %q", empty1, empty2)
	}
	if empty1 == first {
		t.Error("graph membership must participate in SectionHash")
	}
}
