package commit_reasoning

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/git"
)

func TestExtractRelatedRefs(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		body    string
		prs     []string
		issues  []string
	}{
		{
			name:    "squash merge marker",
			subject: "Add billing service (#123)",
			prs:     []string{"123"},
		},
		{
			name:    "explicit PR and keyword issue",
			subject: "fixes #42",
			body:    "PR #12",
			prs:     []string{"12"},
			issues:  []string{"42"},
		},
		{
			name:   "pull request and closes",
			body:   "closes #7, merge pull request #55 from feature/x",
			prs:    []string{"55"},
			issues: []string{"7"},
		},
		{
			name:    "issue keyword must not double classify a PR",
			subject: "Fixes #42 (#45)",
			prs:     []string{"45"},
			issues:  []string{"42"},
		},
		{
			name:    "bare issue numbers",
			subject: "tweak #101 and #102",
			issues:  []string{"101", "102"},
		},
		{
			name:   "refs and resolves forms",
			body:   "refs #3; resolve #4; closed #5",
			issues: []string{"3", "4", "5"},
		},
		{
			name: "pull/123 convention",
			body: "see pull/88",
			prs:  []string{"88"},
		},
		{
			name:    "no references",
			subject: "fix typo",
		},
		{
			name:   "dedupe repeated references",
			body:   "fixes #9, closes #9",
			issues: []string{"9"},
		},
		{
			name:    "uppercase keywords",
			subject: "FIXES #13",
			issues:  []string{"13"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := &git.CommitMeta{Subject: tt.subject, Body: tt.body}
			ExtractRelatedRefs(meta)
			assertStrings(t, "RelatedPRs", meta.RelatedPRs, tt.prs)
			assertStrings(t, "RelatedIssues", meta.RelatedIssues, tt.issues)
		})
	}
}

func TestExtractRelatedRefs_NilMeta(t *testing.T) {
	// Must not panic.
	ExtractRelatedRefs(nil)
}

func TestMaskSpan(t *testing.T) {
	if got := maskSpan("abc123", 0, 3); got != "   123" {
		t.Errorf("maskSpan prefix = %q", got)
	}
	if got := maskSpan("abc123", 3, 6); got != "abc   " {
		t.Errorf("maskSpan suffix = %q", got)
	}
	// Out-of-range spans must be returned untouched.
	if got := maskSpan("abc", 1, 9); got != "abc" {
		t.Errorf("maskSpan out-of-range = %q", got)
	}
}

func assertStrings(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %q, want %q", label, i, got[i], want[i])
		}
	}
}
