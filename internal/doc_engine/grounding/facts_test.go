package grounding

import (
	"fmt"
	"sort"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssembleFactSheet_EndToEnd(t *testing.T) {
	graph := buildTestGraph()

	doc := &config.DocSpec{
		ID:         "auth-spec",
		TargetPath: "docs/auth.md",
		Scope: config.ScopeRule{
			Paths:       []string{"internal/auth/**"},
			EntryPoints: []string{"internal/auth/jwt.go::ValidateToken"},
		},
		Diagrams: []config.DiagramRef{
			{Type: "callgraph", Entry: "internal/auth/jwt.go::ValidateToken"},
		},
	}

	sec := &config.SectionSpec{
		ID:          "interface",
		Title:       "Interface",
		Instruction: "Document public exported types",
		GroundWith:  []string{"signatures", "exported_symbols", "sentinels"},
		Managed:     true,
	}

	dossier := &config.GlobalCommitDossier{
		AddedSymbols: []config.SymbolFact{
			{
				FQN:       "internal/auth/new.go::NewAuthService",
				File:      "internal/auth/new.go",
				Signature: "func NewAuthService(key string)",
				Doc:       "Uses AKIAIOSFODNN7EXAMPLE", // contains AWS secret to test scrubbing!
			},
		},
	}

	priorMarkdown := "Existing text referencing [ValidateToken](internal/auth/jwt.go#L10-L15)."

	sheet := AssembleFactSheet(doc, sec, graph, dossier, priorMarkdown, "")
	require.NotNil(t, sheet)

	assert.Equal(t, "auth-spec", sheet.DocID)
	assert.Equal(t, "interface", sheet.SectionID)

	// Verify symbol collected from graph
	assert.NotEmpty(t, sheet.GroundTruth.Symbols)

	// Verify added symbol from dossier is included
	require.Len(t, sheet.GroundTruth.AddedSymbols, 1)
	assert.Equal(t, "internal/auth/new.go::NewAuthService", sheet.GroundTruth.AddedSymbols[0].FQN)

	// Verify secret was SCRUBBED from added symbol!
	assert.Contains(t, sheet.GroundTruth.AddedSymbols[0].Doc, "[REDACTED:aws_access_key]")

	// Verify diagram was generated
	require.Len(t, sheet.GroundTruth.Diagrams, 1)
	assert.Contains(t, sheet.GroundTruth.Diagrams[0].Content, "```mermaid")

	// Verify permalink self-healed (ValidateToken is at L25-L50 in test graph)
	assert.Contains(t, sheet.PriorSectionMarkdown, "internal/auth/jwt.go#L25-L50")
}

func factTestDoc() *config.DocSpec {
	return &config.DocSpec{
		ID:         "auth-spec",
		TargetPath: "docs/auth.md",
		Scope: config.ScopeRule{
			Paths:       []string{"internal/auth/**"},
			EntryPoints: []string{"internal/auth/jwt.go::ValidateToken"},
		},
	}
}

func factTestSection() *config.SectionSpec {
	return &config.SectionSpec{
		ID:          "interface",
		Title:       "Interface",
		Instruction: "Document public exported types",
		GroundWith:  []string{"signatures"},
		Managed:     true,
	}
}

func TestAssembleFactSheet_CallersIncludeUnchangedSymbolCallers(t *testing.T) {
	graph := buildTestGraph()
	// Inbound caller of parseRaw, which is an UNCHANGED in-scope symbol:
	// the B3 union covers ALL payload symbols, not just dossier deltas.
	addEdgeBoth(graph, "internal/auth/other.go::Helper", "internal/auth/jwt.go::parseRaw", link.EdgeCalls)

	sheet := AssembleFactSheet(factTestDoc(), factTestSection(), graph, &config.GlobalCommitDossier{}, "", "")
	require.NotNil(t, sheet)
	assert.Contains(t, sheet.GroundTruth.Callers, "internal/auth/other.go::Helper")
}

func TestAssembleFactSheet_CallersUnionCapped(t *testing.T) {
	graph := buildTestGraph()
	allSources := make(map[string]bool)
	for i := 0; i < 25; i++ {
		src := fmt.Sprintf("internal/auth/extra%02d.go::Caller%02d", i, i)
		allSources[src] = true
		addEdgeBoth(graph, src, "internal/auth/jwt.go::ValidateToken", link.EdgeCalls)
	}
	other := "internal/auth/other.go::Helper"
	allSources[other] = true
	addEdgeBoth(graph, other, "internal/auth/jwt.go::parseRaw", link.EdgeCalls)

	sheet := AssembleFactSheet(factTestDoc(), factTestSection(), graph, &config.GlobalCommitDossier{}, "", "")
	require.NotNil(t, sheet)

	callers := sheet.GroundTruth.Callers
	require.Len(t, callers, maxPayloadCallers, "callers must be capped at %d", maxPayloadCallers)
	assert.True(t, sort.StringsAreSorted(callers), "callers must be sorted")
	seen := make(map[string]bool, len(callers))
	for _, c := range callers {
		assert.True(t, allSources[c], "caller %q must come from inbound edge sources", c)
		require.False(t, seen[c], "caller %q must not repeat", c)
		seen[c] = true
	}
}

// TestAssembleFactSheet_DossierConfigVarsAndSentinelsScopedToGroundWith
// guards against a regression where dossier.AddedConfigVars/AddedSentinels/
// ModifiedSentinels were merged into payload.ConfigVars/payload.Sentinels
// unconditionally — regardless of whether the section's own ground_with
// ever asked for that kind of grounding. factTestSection() here only
// requests "signatures", so a dossier config-var/sentinel delta in scope
// must NOT appear in either table: a "System Overview" section grounded in
// arch_intelligence alone should never render a Configuration Variables
// table sourced entirely from incidental per-commit dossier contents.
func TestAssembleFactSheet_DossierConfigVarsAndSentinelsScopedToGroundWith(t *testing.T) {
	graph := buildTestGraph()
	dossier := &config.GlobalCommitDossier{
		AddedConfigVars: []config.ConfigVarFact{
			{Name: "internal/auth/config.go::JWTSecretKey", File: "internal/auth/config.go"},
		},
		AddedSentinels: []config.SentinelFact{
			{FQN: "internal/auth/errors.go::ErrNewSentinel", File: "internal/auth/errors.go"},
		},
	}

	sheet := AssembleFactSheet(factTestDoc(), factTestSection(), graph, dossier, "", "")
	require.NotNil(t, sheet)
	assert.Empty(t, sheet.GroundTruth.ConfigVars, "section did not request config_vars grounding")
	assert.Empty(t, sheet.GroundTruth.Sentinels, "section did not request sentinels grounding")
}

// TestAssembleFactSheet_DossierConfigVarsAndSentinelsMergeWhenRequested is
// the companion positive check: a section whose ground_with DOES include
// config_vars/sentinels must still receive the dossier's in-scope deltas,
// same as before this fix.
func TestAssembleFactSheet_DossierConfigVarsAndSentinelsMergeWhenRequested(t *testing.T) {
	graph := buildTestGraph()
	dossier := &config.GlobalCommitDossier{
		AddedConfigVars: []config.ConfigVarFact{
			{Name: "internal/auth/config.go::JWTSecretKey", File: "internal/auth/config.go"},
		},
		AddedSentinels: []config.SentinelFact{
			{FQN: "internal/auth/errors.go::ErrNewSentinel", File: "internal/auth/errors.go"},
		},
	}
	sec := &config.SectionSpec{
		ID:         "errors",
		GroundWith: []string{"config_vars", "sentinels"},
		Managed:    true,
	}

	sheet := AssembleFactSheet(factTestDoc(), sec, graph, dossier, "", "")
	require.NotNil(t, sheet)
	configVarNames := make([]string, len(sheet.GroundTruth.ConfigVars))
	for i, cv := range sheet.GroundTruth.ConfigVars {
		configVarNames[i] = cv.Name
	}
	assert.Contains(t, configVarNames, "internal/auth/config.go::JWTSecretKey",
		"dossier-added config var must merge in when the section requests config_vars")
	sentinelFQNs := make([]string, len(sheet.GroundTruth.Sentinels))
	for i, sf := range sheet.GroundTruth.Sentinels {
		sentinelFQNs[i] = sf.FQN
	}
	assert.Contains(t, sentinelFQNs, "internal/auth/errors.go::ErrNewSentinel",
		"dossier-added sentinel must merge in when the section requests sentinels")
}

// TestAssembleFactSheet_CallersExcludeDossierOnlySymbols guards against a
// regression where the Callers union was seeded from dossier.AddedSymbols/
// ModifiedSymbols in addition to the section's own Symbols/AllSymbols. On a
// genesis run (no base graph yet) or any run where a symbol happened to
// be touched, the dossier's Added/Modified set is effectively "every node
// in the graph" or an incidental grab-bag unrelated to this section's
// scope/ground_with focus — so a caller into a symbol that is ONLY in the
// dossier (never in this section's own Symbols/AllSymbols) must not leak
// into the rendered "Direct Callers" list, or the same unchanged section
// would show a different caller list on every regeneration depending on
// what else happened to be in that commit's dossier.
func TestAssembleFactSheet_CallersExcludeDossierOnlySymbols(t *testing.T) {
	graph := buildTestGraph()
	// DossierOnlyHelper is in-scope (matches factTestDoc's "internal/auth/**")
	// so it survives the dossier's own scope filter, but it is never added
	// as a graph node, so no collector ever puts it in Symbols/AllSymbols —
	// the only way it reaches the payload at all is via dossier.AddedSymbols.
	// Sneaky is its only caller.
	dossierOnlyFQN := "internal/auth/other.go::DossierOnlyHelper"
	addEdgeBoth(graph, "internal/auth/other.go::Sneaky", dossierOnlyFQN, link.EdgeCalls)

	dossier := &config.GlobalCommitDossier{
		AddedSymbols: []config.SymbolFact{{FQN: dossierOnlyFQN, File: "internal/auth/other.go"}},
	}

	sheet := AssembleFactSheet(factTestDoc(), factTestSection(), graph, dossier, "", "")
	require.NotNil(t, sheet)
	assert.NotContains(t, sheet.GroundTruth.Callers, "internal/auth/other.go::Sneaky")
}

// TestAssembleFactSheet_DiagramEntryAutoFilledFromScope guards against a
// regression where a document's default diagrams: list (docs.yaml's
// DocSpec.Diagrams — what every archetype actually configures, and the
// ONLY diagram mechanism `gmb doc init` produces) never got an Entry
// auto-filled from Scope.EntryPoints, so a callgraph/sequence diagram
// rendered "No call graph edges detected" even when EntryPoints was
// correctly set. A separate, opt-in inline `gmb:diagram` directive path
// (renderer/engine.go) already did this auto-fill; facts.go's main
// diagram-rendering loop — used for every default archetype — did not.
func TestAssembleFactSheet_DiagramEntryAutoFilledFromScope(t *testing.T) {
	graph := buildTestGraph()

	doc := &config.DocSpec{
		ID:         "auth-spec",
		TargetPath: "docs/auth.md",
		Scope: config.ScopeRule{
			Paths:       []string{"internal/auth/**"},
			EntryPoints: []string{"internal/auth/jwt.go::ValidateToken"},
		},
		// Exactly what ApplyArchetype/docs.yaml produces: no Entry set on
		// the DiagramRef itself, unlike TestAssembleFactSheet_EndToEnd's
		// fixture above (which sets Entry directly and so never exercised
		// the auto-fill path this test targets).
		Diagrams: []config.DiagramRef{
			{Type: "callgraph"},
		},
	}
	sec := &config.SectionSpec{
		ID:         "interface",
		Title:      "Interface",
		GroundWith: []string{"signatures"},
		Managed:    true,
	}

	sheet := AssembleFactSheet(doc, sec, graph, &config.GlobalCommitDossier{}, "", "")
	require.NotNil(t, sheet)
	require.Len(t, sheet.GroundTruth.Diagrams, 1)
	content := sheet.GroundTruth.Diagrams[0].Content
	assert.NotContains(t, content, "No call graph edges detected",
		"callgraph diagram must use Scope.EntryPoints[0] when the DiagramRef has no Entry of its own:\n%s", content)
	assert.Contains(t, content, "parseRaw", "expected the ValidateToken -> parseRaw call edge in the diagram:\n%s", content)
}
