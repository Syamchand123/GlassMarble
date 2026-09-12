package renderer

import (
	"errors"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// Prompt-builder budget: every builder below must complete a single call in
// <5ms on ordinary CI hardware (pure string formatting / one small JSON
// marshal — microseconds in practice). The benchmarks that follow measure
// steady-state throughput; TestPromptBudgets is the ceiling tripwire.

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

// TestPromptBudgets asserts the <5ms single-call ceiling for each prompt
// builder (see budget note above). These are order-of-magnitude tripwires
// only — builders run in microseconds; failure means a path regressed to
// milliseconds-plus (e.g. accidental I/O or runaway marshal).
func TestPromptBudgets(t *testing.T) {
	style := &config.StyleSpec{
		Voice:           "active, second-person, present tense",
		Tone:            "neutral",
		JargonBlacklist: []string{"simply", "just", "leverage"},
	}
	fs := &config.FactSheet{
		DocID:                "docs/bench.md",
		SectionID:            "overview",
		SectionInstruction:   "Describe the subsystem interface completely.",
		PriorSectionMarkdown: "Prior prose.",
		GroundTruth: config.GroundTruthPayload{
			Symbols: []config.SymbolFact{
				{FQN: "internal/bench.Service0", Kind: "func", Signature: "func Service0() error"},
			},
		},
	}
	cases := map[string]func(){
		"BuildSystemPrompt": func() { _ = BuildSystemPrompt(style, 250) },
		"BuildUserPrompt": func() {
			if _, err := BuildUserPrompt(fs); err != nil {
				t.Fatalf("BuildUserPrompt failed: %v", err)
			}
		},
		"BuildRepairPrompt": func() {
			_ = BuildRepairPrompt(fs, "prev", errors.New("gate 3: unresolved symbol"))
		},
		"QuadrantPrompt": func() { _ = QuadrantPrompt("reference") },
	}
	for name, fn := range cases {
		start := time.Now()
		fn()
		if elapsed := time.Since(start); elapsed > 5*time.Millisecond {
			t.Errorf("%s took %v, want <5ms", name, elapsed)
		}
	}
}
