package commit_reasoning

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

func TestExtractIntent(t *testing.T) {
	tests := []struct {
		name           string
		meta           *CommitMeta
		expectedIntent string
		expectedSrc    evidence.Source
	}{
		{
			name: "Level 2 structural - because",
			meta: &CommitMeta{
				Subject: "Add Redis cache because the DB is slow",
			},
			expectedIntent: "the db is slow",
			expectedSrc:    evidence.SourceGit,
		},
		{
			name: "Level 1 keyword - refactor",
			meta: &CommitMeta{
				Subject: "Refactor auth module",
			},
			expectedIntent: "refactoring",
			expectedSrc:    evidence.SourceGit,
		},
		{
			name: "No intent",
			meta: &CommitMeta{
				Subject: "Fix typo in README",
			},
			expectedIntent: "",
			expectedSrc:    evidence.SourceGit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, src, _ := ExtractIntent(tt.meta, "")
			if intent != tt.expectedIntent {
				t.Errorf("Expected intent %q, got %q", tt.expectedIntent, intent)
			}
			if src != tt.expectedSrc {
				t.Errorf("Expected src %v, got %v", tt.expectedSrc, src)
			}
		})
	}
}
