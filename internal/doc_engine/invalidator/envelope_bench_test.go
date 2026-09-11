package invalidator

import (
	"fmt"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// BenchmarkSectionHash measures HashSection over a synthetic section with 50
// symbols and 3 diagram refs.
//
// Ballpark vs plan §13.4 budgets (catalog <100ms, grounding <50ms,
// gates <10ms, write <30ms, deterministic <20ms): measured 2026-09-11
// (i7-1255U windows/amd64, -benchtime=100x) ~21,000-89,000 ns/op
// (~0.02-0.09ms) vs the deterministic <20ms budget — PASS with >200x
// headroom. The hash path is pure in-memory SHA256 over a small deterministic
// input, so it is expected to sit orders of magnitude under budget.
func BenchmarkSectionHash(b *testing.B) {
	sec := &config.SectionSpec{
		ID:          "bench-interface",
		Title:       "Exported Interface",
		Instruction: "Table of exported types, methods, and sentinel errors",
		GroundWith:  []string{"signatures", "exported_symbols", "sentinels"},
		Managed:     true,
	}
	symbols := make([]config.SymbolFact, 0, 50)
	for i := 0; i < 50; i++ {
		symbols = append(symbols, config.SymbolFact{
			FQN:       fmt.Sprintf("internal/bench/pkg%d.Service%d", i%5, i),
			Kind:      "func",
			Signature: fmt.Sprintf("func Service%d(ctx context.Context, req Request%d) (Response%d, error)", i, i, i),
			Doc:       fmt.Sprintf("Service%d handles request type %d with retries and backoff.", i, i),
			File:      fmt.Sprintf("internal/bench/pkg%d/service.go", i%5),
			Line:      10 + i,
			Permalink: fmt.Sprintf("internal/bench/pkg%d/service.go#L%d", i%5, 10+i),
		})
	}
	diagrams := []config.DiagramRef{
		{Type: "callgraph", Entry: "internal/bench/pkg0.Service0"},
		{Type: "sequence", Entry: "internal/bench/pkg1.Service1"},
		{Type: "c4container", Scope: "folder:internal/bench"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = HashSection(sec, symbols, diagrams)
	}
}
