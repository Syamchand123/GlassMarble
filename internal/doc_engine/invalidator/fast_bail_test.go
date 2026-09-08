package invalidator

import (
	"fmt"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/catalog"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFastBail_LatencyTarget verifies that FastBail completes in well under 15ms
// for repos with large catalog sizes and synthetic file lists.
func TestFastBail_LatencyTarget(t *testing.T) {
	// Build a large catalog with 50 documents
	var docs []config.DocSpec
	for i := 0; i < 50; i++ {
		docs = append(docs, config.DocSpec{
			ID:         fmt.Sprintf("doc-%d", i),
			TargetPath: fmt.Sprintf("docs/module_%d.md", i),
			Scope: config.ScopeRule{
				Paths:        []string{fmt.Sprintf("internal/pkg%d/**", i)},
				ExcludePaths: []string{fmt.Sprintf("internal/pkg%d/testutil/**", i)},
			},
		})
	}
	cat := catalog.New(docs)
	state := &storage.DocEngineState{
		LastCommit: "commit-abc12345",
	}

	// 1. Benchmark fast-bail on already processed commit (should take < 1ms)
	start := time.Now()
	bail, _, _, err := FastBail("", "commit-abc12345", cat, state, false)
	duration := time.Since(start)

	require.NoError(t, err)
	assert.True(t, bail)
	assert.Less(t, duration, 15*time.Millisecond, "FastBail on processed commit must complete in < 15ms")

	// 2. Measure catalog scope matching across 100 candidate file paths for a commit
	scopeStart := time.Now()
	matchedAny := false
	allDocs := cat.Docs()
	for j := 0; j < 100; j++ {
		testFile := fmt.Sprintf("unrelated/dir/file_%d.go", j)
		for _, d := range allDocs {
			if cat.MatchesPath(d.ID, testFile) {
				matchedAny = true
			}
		}
	}
	scopeDuration := time.Since(scopeStart)

	assert.False(t, matchedAny)
	assert.Less(t, scopeDuration, 15*time.Millisecond, "Scope matching 100 files across 50 docs must complete in < 15ms")
}

func BenchmarkFastBail_Processed(b *testing.B) {
	docs := []config.DocSpec{
		{ID: "d1", TargetPath: "docs/d1.md", Scope: config.ScopeRule{Paths: []string{"internal/**"}}},
	}
	cat := catalog.New(docs)
	state := &storage.DocEngineState{LastCommit: "commit-fixed"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = FastBail("", "commit-fixed", cat, state, false)
	}
}
