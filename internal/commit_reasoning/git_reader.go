package commit_reasoning

import (
	"regexp"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/git"
)

// This file owns the reasoning-specific view of git metadata: extracting
// pull-request and issue references from commit messages. Raw git access
// (CommitMeta, ReadCommit, ReadCommitRange) lives in internal/git per
// v2_master_implementaion_plan.md §6.2 — it is a pure fact layer with no
// interpretation, while this package interprets.

var (
	// prSquashRegex matches the GitHub squash-merge marker "(#123)" that
	// trails the subject. It is the strongest PR signal and runs first.
	prSquashRegex = regexp.MustCompile(`\(#(\d+)\)`)
	// prExplicitRegex matches explicit pull-request mentions:
	// "pull request #123", "Pull/123", "PR #123", "pr/123", "PR-123".
	prExplicitRegex = regexp.MustCompile(`(?i)(?:pull\s+request\s+#?|pull/|pr\s+#?|pr/|pr-)\s*(\d+)`)
	// issueKeywordRegex matches issue-tracker keywords:
	// "fixes #42", "fix #42", "close #42", "closes #42", "closed #42",
	// "resolve #42", "resolves #42", "resolved #42", "refs #42",
	// "references #42", with an optional "issue" between keyword and number.
	// Single capture group — a two-branch alternative would leave one group
	// at -1 on the other branch's matches.
	issueKeywordRegex = regexp.MustCompile(`(?i)\b(?:fix(?:es|ed)?|clos(?:e|es|ed)|resolv(?:e|es|ed)|re(?:fs|ferences?))(?:\s+issue)?\s+#?(\d+)`)
	// issueExplicitRegex matches the "issue #42" form on its own.
	issueExplicitRegex = regexp.MustCompile(`(?i)\bissue\s+#?(\d+)`)
	// bareIssueRegex catches any remaining "#42" after stronger patterns
	// have claimed their matches.
	bareIssueRegex = regexp.MustCompile(`#(\d+)`)
)

// ExtractRelatedRefs populates meta.RelatedPRs and meta.RelatedIssues from the
// commit subject and body. Detection order is by decreasing signal strength:
//
//  1. GitHub squash-merge markers "(#123)"       → PR
//  2. Explicit mentions "PR #123", "pull/123"    → PR
//  3. Issue keywords "fixes #42", "closes #42"   → issue
//  4. Explicit "issue #42"                       → issue
//  5. Bare "#42"                                 → issue
//
// Every matched span is masked before the next pattern runs so one number is
// never classified twice (a "Fixes #42 (#45)" subject yields issue 42 and
// PR 45, never a PR named 42). Results are deduplicated in first-seen order,
// which keeps extraction deterministic.
func ExtractRelatedRefs(meta *git.CommitMeta) {
	if meta == nil {
		return
	}
	msg := meta.Subject + "\n" + meta.Body
	var prs, issues []string
	seenPR := make(map[string]bool)
	seenIssue := make(map[string]bool)

	appendUnique := func(dst *[]string, seen map[string]bool, num string) {
		if num == "" || seen[num] {
			return
		}
		seen[num] = true
		*dst = append(*dst, num)
	}

	claim := func(re *regexp.Regexp, dst *[]string, seen map[string]bool) {
		for _, m := range re.FindAllStringSubmatchIndex(msg, -1) {
			// The capture group (2,3) holds the number; the whole match
			// (0,1) is masked afterwards.
			appendUnique(dst, seen, msg[m[2]:m[3]])
			msg = maskSpan(msg, m[0], m[1])
		}
	}

	claim(prSquashRegex, &prs, seenPR)
	claim(prExplicitRegex, &prs, seenPR)
	claim(issueKeywordRegex, &issues, seenIssue)
	claim(issueExplicitRegex, &issues, seenIssue)
	claim(bareIssueRegex, &issues, seenIssue)

	meta.RelatedPRs = prs
	meta.RelatedIssues = issues
}

// maskSpan replaces s[start:end] with spaces so later patterns cannot
// re-match the same text while byte offsets of other spans stay valid.
func maskSpan(s string, start, end int) string {
	if start < 0 || end < start || end > len(s) {
		return s
	}
	return s[:start] + strings.Repeat(" ", end-start) + s[end:]
}
