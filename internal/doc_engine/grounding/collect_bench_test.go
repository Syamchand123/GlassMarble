package grounding

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// BenchmarkCollectSectionFacts measures CollectSectionFacts over the existing
// buildTestGraph helper (7 nodes, 1 call edge) with a multi-directive section.
//
// Ballpark vs plan §13.4 budgets (catalog <100ms, grounding <50ms,
// gates <10ms, write <30ms, deterministic <20ms): measured 2026-09-11
// (i7-1255U windows/amd64, -benchtime=100x) ~58,000-83,000 ns/op
// (~0.06-0.08ms) vs the grounding <50ms/section budget — PASS with >600x
// headroom. In-memory AKG iteration over a handful of nodes is expected to
// sit far under budget.
func BenchmarkCollectSectionFacts(b *testing.B) {
	g := buildTestGraph()
	c := NewCollector(g)
	scope := &config.ScopeRule{
		Paths:       []string{"internal/auth/**"},
		EntryPoints: []string{"internal/auth/jwt.go::ValidateToken"},
	}
	sec := &config.SectionSpec{
		ID: "bench",
		GroundWith: []string{
			"signatures",
			"exported_symbols",
			"comments",
			"sentinels",
			"error_returns",
			"concurrency_primitives",
			"config_vars",
			"http_handlers",
			"callgraph",
			"arch_intelligence",
			"arch_events",
			"ingress_points",
			"dependencies",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := c.CollectSectionFacts(sec, scope)
		if err != nil {
			b.Fatal(err)
		}
	}
}
