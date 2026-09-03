package commit_reasoning

import (
	"context"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/git"
)

// TestIntent_RealWorldCommits pins intent classification against commit
// messages that previously classified wrongly.
//
// Regression: a docs-only commit on this repository ("docs: record
// correctness, performance and CLI work in the changelog", touching
// CHANGELOG.md) was classified PERFORMANCE on all 44 events it produced.
// The Performance keyword rule preceded the Docs rule, and there was no
// structural rule for documentation files, so the word "performance" inside
// the description of the docs won over the docs signal itself.
func TestIntent_RealWorldCommits(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		files   []string
		want    Intent
	}{
		{
			name:    "docs commit mentioning performance",
			subject: "docs: record correctness, performance and CLI work in the changelog",
			files:   []string{"CHANGELOG.md"},
			want:    IntentDocs,
		},
		{
			name:    "conventional feat beats the word fix in the body",
			subject: "feat(api): add pagination, fixes the slow listing endpoint",
			files:   []string{"internal/api/list.go"},
			want:    IntentAddFeature,
		},
		{
			name:    "conventional perf",
			subject: "perf: batch index maintenance",
			files:   []string{"internal/akg/transaction_manager.go"},
			want:    IntentPerformance,
		},
		{
			name:    "conventional test",
			subject: "test: cover the delta path",
			files:   []string{"internal/akg/tm_test.go"},
			want:    IntentTest,
		},
		{
			name:    "readme only, no conventional prefix",
			subject: "update the getting started guide",
			files:   []string{"README.md"},
			want:    IntentDocs,
		},
		{
			name:    "plain bug fix still classifies",
			subject: "fix nil dereference in status handler",
			files:   []string{"internal/visualizer/server.go"},
			want:    IntentFixBug,
		},
	}

	ex := NewIntentExtractor()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ex.Extract(context.Background(), &git.CommitMeta{
				Hash:    "deadbeef",
				Subject: tc.subject,
				Files:   tc.files,
			}, "")
			if got.Intent != tc.want {
				t.Errorf("subject %q files %v\n got intent %s (level %v)\nwant intent %s",
					tc.subject, tc.files, got.Intent, got.Level, tc.want)
			}
		})
	}
}
