package renderer

import (
	"errors"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// BenchmarkBuildSystemPrompt measures the immutable system-prompt builder
// (pure string formatting over a small rule list).
func BenchmarkBuildSystemPrompt(b *testing.B) {
	style := &config.StyleSpec{
		Voice:           "active, second-person, present tense",
		Tone:            "neutral",
		JargonBlacklist: []string{"simply", "just", "leverage", "obviously", "utilize"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildSystemPrompt(style, 250)
	}
}

// BenchmarkBuildUserPrompt measures user-prompt construction over a
// representative FactSheet (dominated by JSON marshaling of ground truth).
func BenchmarkBuildUserPrompt(b *testing.B) {
	fs := &config.FactSheet{
		DocID:                "docs/bench.md",
		SectionID:            "overview",
		SectionInstruction:   "Describe the subsystem interface completely.",
		PriorSectionMarkdown: "Prior prose with `Service0` and `Service1` references.",
		GroundTruth: config.GroundTruthPayload{
			Symbols: []config.SymbolFact{
				{FQN: "internal/bench.Service0", Kind: "func", Signature: "func Service0() error", Doc: "Service0 does x.", File: "internal/bench/service.go", Line: 10},
				{FQN: "internal/bench.Service1", Kind: "func", Signature: "func Service1() error", Doc: "Service1 does y.", File: "internal/bench/service.go", Line: 20},
			},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := BuildUserPrompt(fs); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBuildRepairPrompt measures repair-prompt construction after a
// quality-firewall rejection.
func BenchmarkBuildRepairPrompt(b *testing.B) {
	fs := &config.FactSheet{
		DocID:     "docs/bench.md",
		SectionID: "overview",
	}
	gateErr := errors.New("gate 3: unresolved symbol: `NonExistentSymbol`")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildRepairPrompt(fs, "Faulty output with `NonExistentSymbol`", gateErr)
	}
}

// BenchmarkQuadrantPrompt measures Diátaxis quadrant-guidance lookup.
func BenchmarkQuadrantPrompt(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = QuadrantPrompt("reference")
	}
}
