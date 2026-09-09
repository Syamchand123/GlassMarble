package renderer

import (
	"errors"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

func TestBuildSystemPrompt(t *testing.T) {
	style := &config.StyleSpec{
		Voice:           "passive, academic",
		JargonBlacklist: []string{"obviously", "trivial"},
	}

	prompt := BuildSystemPrompt(style, 250)

	if !strings.Contains(prompt, "passive, academic") {
		t.Errorf("missing custom voice in system prompt")
	}
	if !strings.Contains(prompt, "obviously, trivial") {
		t.Errorf("missing jargon blacklist in system prompt")
	}
	if !strings.Contains(prompt, "Keep this section under 250 words.") {
		t.Errorf("missing max words constraint in system prompt")
	}
	if !strings.Contains(prompt, "HARD RULES — these are absolute and non-negotiable:") {
		t.Errorf("missing hard rules header")
	}
}

func TestBuildUserPrompt(t *testing.T) {
	fs := &config.FactSheet{
		DocID:                "docs/ai.md",
		SectionID:            "provider",
		SectionInstruction:   "Summarize provider interface.",
		PriorSectionMarkdown: "Old prose here.",
		GroundTruth: config.GroundTruthPayload{
			Symbols: []config.SymbolFact{
				{FQN: "internal/ai.Provider", Kind: "interface"},
			},
		},
	}

	prompt, err := BuildUserPrompt(fs)
	if err != nil {
		t.Fatalf("BuildUserPrompt failed: %v", err)
	}

	if !strings.Contains(prompt, "FACT SHEET:") {
		t.Errorf("missing FACT SHEET header")
	}
	if !strings.Contains(prompt, `"internal/ai.Provider"`) {
		t.Errorf("missing symbol in serialized FactSheet")
	}
	if !strings.Contains(prompt, "SECTION INSTRUCTION:") {
		t.Errorf("missing SECTION INSTRUCTION header")
	}
	if !strings.Contains(prompt, "Summarize provider interface.") {
		t.Errorf("missing instruction text")
	}
	if !strings.Contains(prompt, "Old prose here.") {
		t.Errorf("missing prior section markdown")
	}
}

func TestBuildRepairPrompt(t *testing.T) {
	fs := &config.FactSheet{
		DocID:     "docs/sec.md",
		SectionID: "overview",
	}

	gateErr := errors.New("gate 3: unresolved symbol: `NonExistentSymbol`")
	prompt := BuildRepairPrompt(fs, "Faulty output with `NonExistentSymbol`", gateErr)

	if !strings.Contains(prompt, "unresolved symbol: `NonExistentSymbol`") {
		t.Errorf("missing gate error text in repair prompt")
	}
	if !strings.Contains(prompt, "Faulty output with `NonExistentSymbol`") {
		t.Errorf("missing previous output in repair prompt")
	}
	if !strings.Contains(prompt, "Do NOT invent, assume, or hallucinate") {
		t.Errorf("missing anti-hallucination instruction")
	}
}
