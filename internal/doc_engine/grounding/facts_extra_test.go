// Package grounding — facts_extra_test.go
//
// Whitebox coverage for AssembleFactSheet population (Callers, CallFlow,
// DiagramMermaid, DocComments) including the empty-graph path, the
// precision-layer provenance override rules, the context-layer cap and
// determinism, and cross-repo fallback gating. Reuses buildTestGraph and
// addEdgeBoth from collector_test.go (same package).
package grounding

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/grounding/resolve"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ────────────────────────────────────────────────────────────────────────────
// Callers / CallFlow / DiagramMermaid / DocComments population
// ────────────────────────────────────────────────────────────────────────────

func TestExtraCallersDirectAndSorted(t *testing.T) {
	graph := buildTestGraph()
	addEdgeBoth(graph, "internal/auth/other.go::HelperB", "internal/auth/jwt.go::ValidateToken", link.EdgeCalls)
	addEdgeBoth(graph, "internal/auth/other.go::HelperA", "internal/auth/jwt.go::ValidateToken", link.EdgeCalls)

	sheet := AssembleFactSheet(factTestDoc(), factTestSection(), graph, &config.GlobalCommitDossier{}, "", "")
	require.NotNil(t, sheet)
	require.Contains(t, sheet.GroundTruth.Callers, "internal/auth/other.go::HelperA")
	require.Contains(t, sheet.GroundTruth.Callers, "internal/auth/other.go::HelperB")
	assert.True(t, sort.StringsAreSorted(sheet.GroundTruth.Callers), "callers must be sorted: %v", sheet.GroundTruth.Callers)
	// Self-edges and empty sources never leak in.
	assert.NotContains(t, sheet.GroundTruth.Callers, "")
}

func TestExtraCallFlowFromEntryPoints(t *testing.T) {
	graph := buildTestGraph() // outbound: ValidateToken --calls--> parseRaw
	sheet := AssembleFactSheet(factTestDoc(), factTestSection(), graph, &config.GlobalCommitDossier{}, "", "")
	require.NotNil(t, sheet)
	require.NotEmpty(t, sheet.GroundTruth.CallFlow)
	assert.Equal(t, "internal/auth/jwt.go::ValidateToken", sheet.GroundTruth.CallFlow[0])
	assert.Contains(t, sheet.GroundTruth.CallFlow, "internal/auth/jwt.go::parseRaw")
}

func TestExtraDocCommentsIndexed(t *testing.T) {
	graph := buildTestGraph()
	sheet := AssembleFactSheet(factTestDoc(), factTestSection(), graph, &config.GlobalCommitDossier{}, "", "")
	require.NotNil(t, sheet)
	require.NotNil(t, sheet.GroundTruth.DocComments)
	// Exported symbol doc + sentinel doc both land in the index.
	assert.Equal(t, "ValidateToken parses and validates a signed JWT.",
		sheet.GroundTruth.DocComments["internal/auth/jwt.go::ValidateToken"])
	assert.Equal(t, "ErrTokenExpired is returned when the token is past its exp.",
		sheet.GroundTruth.DocComments["internal/auth/errors.go::ErrTokenExpired"])
	// Undocumented symbols stay out of the index.
	_, ok := sheet.GroundTruth.DocComments["internal/auth/jwt.go::parseRaw"]
	assert.False(t, ok, "symbols without doc comments must not be indexed")
}

func TestExtraDiagramMermaidPopulation(t *testing.T) {
	doc := factTestDoc()
	doc.Diagrams = []config.DiagramRef{{Type: "callgraph", Entry: "internal/auth/jwt.go::ValidateToken"}}
	sec := factTestSection()

	withGraph := AssembleFactSheet(doc, sec, buildTestGraph(), &config.GlobalCommitDossier{}, "", "")
	require.NotNil(t, withGraph)
	require.Len(t, withGraph.GroundTruth.Diagrams, 1)
	assert.Contains(t, withGraph.GroundTruth.Diagrams[0].Content, "```mermaid")
	assert.Contains(t, withGraph.GroundTruth.Diagrams[0].Content, "-->")
	assert.Equal(t, withGraph.GroundTruth.Diagrams[0].Content, withGraph.GroundTruth.DiagramMermaid,
		"section-level pointer must be the first rendered diagram verbatim")

	// A doc with no diagram refs renders none and leaves the pointer empty.
	plain := AssembleFactSheet(factTestDoc(), sec, buildTestGraph(), &config.GlobalCommitDossier{}, "", "")
	require.NotNil(t, plain)
	assert.Empty(t, plain.GroundTruth.Diagrams)
	assert.Empty(t, plain.GroundTruth.DiagramMermaid)
}

func TestExtraEmptyGraph(t *testing.T) {
	doc := factTestDoc()
	doc.Diagrams = []config.DiagramRef{{Type: "callgraph", Entry: "internal/auth/jwt.go::ValidateToken"}}
	sheet := AssembleFactSheet(doc, factTestSection(), nil, nil, "prior body", "")
	require.NotNil(t, sheet, "nil graph + nil dossier must still yield a sheet")
	assert.Empty(t, sheet.GroundTruth.Callers)
	assert.Empty(t, sheet.GroundTruth.CallFlow)
	assert.Empty(t, sheet.GroundTruth.Symbols)
	assert.Empty(t, sheet.GroundTruth.AddedSymbols)
	require.NotNil(t, sheet.GroundTruth.DocComments)
	assert.Empty(t, sheet.GroundTruth.DocComments)
	// Nil graph renders the honest empty-callgraph placeholder, never an error.
	assert.Contains(t, sheet.GroundTruth.DiagramMermaid, "```mermaid")
	assert.Equal(t, "prior body", sheet.PriorSectionMarkdown)
	assert.Equal(t, "auth-spec", sheet.DocID)
	assert.Equal(t, "interface", sheet.SectionID)
}

func TestExtraNilDocYieldsNil(t *testing.T) {
	if got := AssembleFactSheet(nil, factTestSection(), buildTestGraph(), nil, "", ""); got != nil {
		t.Errorf("nil doc must yield nil sheet, got %+v", got)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Precision-layer provenance override rules
// ────────────────────────────────────────────────────────────────────────────

func TestExtraProvenanceRank(t *testing.T) {
	for _, tc := range []struct {
		current, next, want string
	}{
		{"", "unresolved", "unresolved"},
		{"", "ast", "ast"},
		{"", "lsp", "lsp"},
		{"", "scip", "scip"},
		{"ast", "ast", "ast"},
		{"ast", "lsp", "lsp"},       // lsp wins over ast
		{"ast", "scip", "scip"},     // scip wins over ast
		{"lsp", "ast", "lsp"},       // ast merely confirms, never overrides
		{"lsp", "scip", "scip"},     // scip wins over lsp
		{"scip", "lsp", "scip"},     // nothing beats scip
		{"scip", "ast", "scip"},     // ast never downgrades
		{"ast", "unresolved", "ast"}, // unresolved never wins
		{"scip", "scip:xrepo", "scip"},
		{"lsp", "scip:xrepo", "scip:xrepo"},
		{"ast", "scip:xrepo", "scip:xrepo"},
	} {
		if got := provenanceOr(tc.current, tc.next); got != tc.want {
			t.Errorf("provenanceOr(%q, %q) = %q, want %q", tc.current, tc.next, got, tc.want)
		}
	}
}

func TestExtraPrecisionLayerSCIPOverrides(t *testing.T) {
	repo := t.TempDir()
	sidecarDir := filepath.Join(repo, ".glassmarble", "scip")
	require.NoError(t, os.MkdirAll(sidecarDir, 0755))
	// The collector extracted a stale location; the SCIP sidecar is
	// authoritative and must override file/line + permalink + provenance.
	sidecar := `[{"fqn":"internal/auth/jwt.go::ValidateToken","file":"internal/auth/jwt.go","line":25,"end_line":50}]`
	require.NoError(t, os.WriteFile(filepath.Join(sidecarDir, "index.json"), []byte(sidecar), 0644))

	doc := factTestDoc()
	payload := &config.GroundTruthPayload{
		Symbols: []config.SymbolFact{
			{FQN: "internal/auth/jwt.go::ValidateToken", File: "stale/path.go", Line: 1},
		},
	}
	applyPrecisionLayer(doc, payload, buildTestGraph(), repo)
	got := payload.Symbols[0]
	assert.Equal(t, "internal/auth/jwt.go", got.File, "scip must override the file")
	assert.Equal(t, 25, got.Line, "scip must override the line")
	assert.Equal(t, "internal/auth/jwt.go#L25-L50", got.Permalink)
	assert.Equal(t, "scip", got.Provenance)
}

func TestExtraPrecisionLayerASTConfirmsOnly(t *testing.T) {
	// No sidecar, no gopls-backed repo: resolution degrades to AST, which
	// confirms provenance but must NOT move the extracted location.
	repo := t.TempDir()
	doc := factTestDoc()
	payload := &config.GroundTruthPayload{
		Symbols: []config.SymbolFact{
			{FQN: "internal/auth/jwt.go::ValidateToken", File: "keep/me.go", Line: 7},
		},
		AllSymbols: []config.SymbolFact{
			{FQN: "internal/auth/jwt.go::parseRaw", File: "keep/other.go", Line: 9},
		},
	}
	applyPrecisionLayer(doc, payload, buildTestGraph(), repo)
	assert.Equal(t, "keep/me.go", payload.Symbols[0].File, "ast must not override the file")
	assert.Equal(t, 7, payload.Symbols[0].Line, "ast must not override the line")
	assert.Equal(t, "keep/other.go", payload.AllSymbols[0].File)
	assert.Equal(t, "ast", payload.Symbols[0].Provenance)
}

func TestExtraPrecisionLayerNilSafe(t *testing.T) {
	// Nil payload, nil doc, and empty symbol lists are all no-ops.
	applyPrecisionLayer(nil, nil, nil, "")
	applyPrecisionLayer(factTestDoc(), nil, nil, "")
	applyPrecisionLayer(factTestDoc(), &config.GroundTruthPayload{}, nil, "")
	applyPrecisionLayer(nil, &config.GroundTruthPayload{
		Symbols: []config.SymbolFact{{FQN: "x"}},
	}, nil, "")
}

// ────────────────────────────────────────────────────────────────────────────
// Context-layer cap + determinism
// ────────────────────────────────────────────────────────────────────────────

func extraBushyGraph(t *testing.T, n int) *akg.CodePropertyGraph {
	t.Helper()
	g := akg.NewCodePropertyGraph("bushy")
	hub := "internal/hub/hub.go::Hub"
	g.Nodes = g.Nodes.Set(hub, &link.ResolvedNode{
		ID:   hub,
		Name: "Hub",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      "internal/hub/hub.go",
			LineStart: 1,
			LineEnd:   10,
		},
		Properties: map[string]string{"signature": "func Hub()"},
	})
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("internal/leaf%02d/leaf.go::Leaf%02d", i, i)
		g.Nodes = g.Nodes.Set(id, &link.ResolvedNode{
			ID:   id,
			Name: fmt.Sprintf("Leaf%02d", i),
			Kind: "FUNCTION",
			FileSpec: link.LocationMeta{
				Path:      fmt.Sprintf("internal/leaf%02d/leaf.go", i),
				LineStart: 1,
				LineEnd:   5,
			},
			Properties: map[string]string{"signature": fmt.Sprintf("func Leaf%02d()", i)},
		})
		addEdgeBoth(g, hub, id, link.EdgeCalls)
	}
	return g
}

func TestExtraContextLayerCapAndDeterminism(t *testing.T) {
	graph := extraBushyGraph(t, 30)
	doc := &config.DocSpec{
		ID:         "hub-doc",
		TargetPath: "docs/hub.md",
		Scope: config.ScopeRule{
			Paths:       []string{"internal/hub/**"},
			EntryPoints: []string{"internal/hub/hub.go::Hub"},
		},
	}
	sec := &config.SectionSpec{ID: "hub", Title: "Hub", Instruction: "Describe the hub", Managed: true, GroundWith: []string{"signatures"}}

	first := AssembleFactSheet(doc, sec, graph, &config.GlobalCommitDossier{}, "", "")
	second := AssembleFactSheet(doc, sec, graph, &config.GlobalCommitDossier{}, "", "")
	require.NotNil(t, first)
	require.NotNil(t, second)

	var firstCtx, secondCtx []string
	for _, s := range first.GroundTruth.Symbols {
		if s.Kind == "context" {
			firstCtx = append(firstCtx, s.FQN)
		}
	}
	for _, s := range second.GroundTruth.Symbols {
		if s.Kind == "context" {
			secondCtx = append(secondCtx, s.FQN)
		}
	}
	assert.Equal(t, firstCtx, secondCtx, "context layer must be deterministic across runs")
	if len(firstCtx) > maxContextSymbols {
		t.Errorf("context symbols = %d, exceeds cap %d", len(firstCtx), maxContextSymbols)
	}
	// Every context fact carries pagerank provenance and a permalink.
	for _, s := range first.GroundTruth.Symbols {
		if s.Kind != "context" {
			continue
		}
		assert.Equal(t, "pagerank", s.Provenance)
		assert.NotEmpty(t, s.Permalink)
	}
	// Callers stay capped and sorted even on a bushy graph.
	if len(first.GroundTruth.Callers) > maxPayloadCallers {
		t.Errorf("callers = %d, exceeds cap %d", len(first.GroundTruth.Callers), maxPayloadCallers)
	}
	assert.True(t, sort.StringsAreSorted(first.GroundTruth.Callers))
}

func TestExtraContextLayerNilGraphNoOp(t *testing.T) {
	doc := factTestDoc()
	payload := &config.GroundTruthPayload{
		Symbols: []config.SymbolFact{{FQN: "a::B"}},
	}
	applyContextLayer(doc, factTestSection(), payload, nil)
	assert.Len(t, payload.Symbols, 1, "nil graph must add no context symbols")
	applyContextLayer(nil, nil, nil, nil)
}

// ────────────────────────────────────────────────────────────────────────────
// Cross-repo fallback gating (no env → no-op)
// ────────────────────────────────────────────────────────────────────────────

func TestExtraCrossRepoGating(t *testing.T) {
	t.Setenv("GMB_EXTRA_REPOS", "")
	if roots := resolve.ExtraRepoRoots(); roots != nil {
		t.Errorf("unset GMB_EXTRA_REPOS must yield nil roots, got %v", roots)
	}
	res := resolve.ResolveCrossRepo("otherrepo/pkg/file.go::Symbol", nil, nil)
	assert.Equal(t, resolve.ProvenanceUnresolved, res.Provenance)

	// A configured-but-empty repo is still a safe no-op, never an error.
	t.Setenv("GMB_EXTRA_REPOS", t.TempDir())
	roots := resolve.ExtraRepoRoots()
	require.Len(t, roots, 1)
	res = resolve.ResolveCrossRepo("otherrepo/pkg/file.go::Symbol", roots, nil)
	assert.Equal(t, resolve.ProvenanceUnresolved, res.Provenance)

	// Precision layer with a repo-qualified FQN but no sidecars anywhere:
	// locations stay exactly as extracted.
	doc := factTestDoc()
	payload := &config.GroundTruthPayload{
		Symbols: []config.SymbolFact{{FQN: "otherrepo/pkg/file.go::Symbol", File: "local.go", Line: 3}},
	}
	applyPrecisionLayer(doc, payload, buildTestGraph(), t.TempDir())
	assert.Equal(t, "local.go", payload.Symbols[0].File)
}

func TestExtraCrossRepoSidecarHit(t *testing.T) {
	extra := t.TempDir()
	sidecarDir := filepath.Join(extra, ".glassmarble", "scip")
	require.NoError(t, os.MkdirAll(sidecarDir, 0755))
	sidecar := `[{"fqn":"otherrepo/pkg/file.go::Symbol","file":"pkg/file.go","line":11,"end_line":20}]`
	require.NoError(t, os.WriteFile(filepath.Join(sidecarDir, "index.json"), []byte(sidecar), 0644))

	t.Setenv("GMB_EXTRA_REPOS", extra)
	roots := resolve.ExtraRepoRoots()
	require.Len(t, roots, 1)
	res := resolve.ResolveCrossRepo("otherrepo/pkg/file.go::Symbol", roots, nil)
	assert.Equal(t, resolve.ProvenanceCrossRepo, res.Provenance)
	assert.Equal(t, "pkg/file.go", res.File)
	assert.Equal(t, 11, res.Line)
}

// ────────────────────────────────────────────────────────────────────────────
// Dossier-scope filtering inside AssembleFactSheet
// ────────────────────────────────────────────────────────────────────────────

func TestExtraDossierScopeFiltering(t *testing.T) {
	graph := buildTestGraph()
	dossier := &config.GlobalCommitDossier{
		AddedSymbols: []config.SymbolFact{
			{FQN: "internal/auth/new.go::NewThing", File: "internal/auth/new.go"},
			{FQN: "internal/other/x.go::Other", File: "internal/other/x.go"},
		},
		RemovedSymbols: []string{"internal/auth/jwt.go::ValidateToken", "unscoped::Gone"},
	}
	sheet := AssembleFactSheet(factTestDoc(), factTestSection(), graph, dossier, "", "")
	require.NotNil(t, sheet)
	// In-scope delta survives; out-of-scope file is dropped.
	require.Len(t, sheet.GroundTruth.AddedSymbols, 1)
	assert.Equal(t, "internal/auth/new.go::NewThing", sheet.GroundTruth.AddedSymbols[0].FQN)
	// Removed symbols filter on the FQN file part.
	assert.Contains(t, sheet.GroundTruth.RemovedSymbols, "internal/auth/jwt.go::ValidateToken")
	for _, r := range sheet.GroundTruth.RemovedSymbols {
		if strings.Contains(r, "unscoped") {
			t.Errorf("out-of-scope removed symbol leaked: %q", r)
		}
	}
}
