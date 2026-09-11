package catalog

import (
	"fmt"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// BenchmarkCatalogMatches measures MatchingDocs over a 1000-document catalog
// with distinct per-package scope globs.
//
// Ballpark vs plan §13.4 budgets (catalog <100ms, grounding <50ms,
// gates <10ms, write <30ms, deterministic <20ms): measured 2026-09-11
// (i7-1255U windows/amd64, -benchtime=100x) ~404,000-407,000 ns/op
// (~0.40-0.41ms) vs the catalog <100ms budget — PASS with ~245x headroom. A
// linear 1000-scope glob scan is expected to sit well under budget.
func BenchmarkCatalogMatches(b *testing.B) {
	docs := make([]config.DocSpec, 0, 1000)
	for i := 0; i < 1000; i++ {
		docs = append(docs, config.DocSpec{
			ID:         fmt.Sprintf("doc-%04d", i),
			TargetPath: fmt.Sprintf("docs/pkg%d.md", i),
			Scope: config.ScopeRule{
				Paths: []string{fmt.Sprintf("pkg/mod%d/**", i%100)},
			},
		})
	}
	cat := New(docs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cat.MatchingDocs("pkg/mod42/service.go")
	}
}
