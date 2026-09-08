package grounding

import (
	"testing"

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

	sheet := AssembleFactSheet(doc, sec, graph, dossier, priorMarkdown)
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
