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
