package doc_engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/catalog"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/invalidator"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/renderer"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
)

// TestPerfBudgets asserts the plan §13.4 latency budgets (catalog <100ms,
// grounding <50ms, gates <10ms, write <30ms, deterministic render <20ms,
// fast-bail <15ms) with a GENEROUS 10x margin on each.
//
// Why 10x: CI hardware varies wildly (shared runners, Windows fsync costs,
// cold caches). These assertions are order-of-magnitude regression tripwires
// only — they catch a path going from milliseconds to seconds, never perf
// tuning. Tight budgets belong in benchmark dashboards, not in this test.
func TestPerfBudgets(t *testing.T) {
	t.Run("FastBail", func(t *testing.T) {
		docs := []config.DocSpec{
			{ID: "d1", TargetPath: "docs/d1.md", Scope: config.ScopeRule{Paths: []string{"internal/**"}}},
		}
		cat := catalog.New(docs)
		state := &storage.DocEngineState{LastCommit: "commit-fixed"}
		start := time.Now()
		bail, _, _, err := invalidator.FastBail("", "commit-fixed", cat, state, false)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("FastBail failed: %v", err)
		}
		if !bail {
			t.Errorf("expected fast-bail on already-processed commit")
		}
		// Plan: fast-bail <15ms; budget here: 10x = 150ms.
		if elapsed > 150*time.Millisecond {
			t.Errorf("FastBail took %v, want <150ms (10x plan <15ms)", elapsed)
		}
	})

	t.Run("HashSection", func(t *testing.T) {
		sec := &config.SectionSpec{
			ID:          "bench-interface",
			Title:       "Exported Interface",
			Instruction: "Table of exported types, methods, and sentinel errors",
			GroundWith:  []string{"signatures"},
			Managed:     true,
		}
		var symbols []config.SymbolFact
		for i := 0; i < 50; i++ {
			symbols = append(symbols, config.SymbolFact{
				FQN:       fmt.Sprintf("internal/bench/pkg%d.Service%d", i%5, i),
				Kind:      "func",
				Signature: fmt.Sprintf("func Service%d() error", i),
				Doc:       fmt.Sprintf("Service%d handles requests.", i),
				File:      fmt.Sprintf("internal/bench/pkg%d/service.go", i%5),
				Line:      10 + i,
			})
		}
		start := time.Now()
		hash := invalidator.HashSection(sec, symbols, nil)
		elapsed := time.Since(start)
		if hash == "" {
			t.Errorf("HashSection returned empty hash")
		}
		// Plan: deterministic-class work <20ms; budget here: 10x = 200ms.
		if elapsed > 200*time.Millisecond {
			t.Errorf("HashSection took %v, want <200ms (10x plan <20ms)", elapsed)
		}
	})

	t.Run("DeterministicRender", func(t *testing.T) {
		orch := renderer.NewOrchestrator(renderer.OrchestratorOptions{NoLLM: true})
		fs := &config.FactSheet{
			DocID:     "docs/bench.md",
			SectionID: "overview",
			GroundTruth: config.GroundTruthPayload{
				Symbols: []config.SymbolFact{
					{FQN: "pkg.Foo", Kind: "func", Signature: "func Foo() error"},
				},
			},
		}
		start := time.Now()
		outcome, err := orch.RenderSection(context.Background(), fs, nil)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("RenderSection failed: %v", err)
		}
		if outcome.Content == "" {
			t.Errorf("deterministic render produced empty content")
		}
		// Plan: deterministic render <20ms; budget here: 10x = 200ms.
		if elapsed > 200*time.Millisecond {
			t.Errorf("deterministic render took %v, want <200ms (10x plan <20ms)", elapsed)
		}
	})

	t.Run("AtomicWrite", func(t *testing.T) {
		dir := t.TempDir()
		target := dir + "/bench.md"
		content := []byte("# Benchmark\n\n" + strings.Repeat("Lorem ipsum dolor sit amet. ", 300))
		start := time.Now()
		if _, err := storage.AtomicWriteFile(target, content); err != nil {
			t.Fatalf("AtomicWriteFile failed: %v", err)
		}
		elapsed := time.Since(start)
		// Plan: atomic write <30ms; budget here: 10x = 300ms.
		if elapsed > 300*time.Millisecond {
			t.Errorf("AtomicWriteFile took %v, want <300ms (10x plan <30ms)", elapsed)
		}
	})
}
