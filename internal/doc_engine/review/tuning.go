// Package review — tuning.go
//
// SuggestTuning aggregates resolved review outcomes into prompt/style tuning
// suggestions (improvement-plan gap: the tuning model). It reads the same
// review.json queue as the rest of the package (stdlib only) and groups
// non-pending items by Kind+Reason:
//
//   - conflicts            → prompt/merge-note improvements
//   - snippet-fix rejects  → grounding-gap fixes
//   - adr-draft rejects    → better auto-generation triggers
//   - reverts with reasons → style/prompt changes
//   - observed reverts     → doc corrections
//
// Suggestions are sorted by occurrence count descending (deterministic
// tie-break on Area, then Action), capped at 10, with a minimum of 1
// occurrence (every observed group yields a suggestion).
package review

import (
	"fmt"
	"sort"
	"strings"
)

// TuningSuggestion is one actionable prompt/style/process improvement
// derived from repeated human-review outcomes.
type TuningSuggestion struct {
	Area     string `json:"area"`
	Evidence string `json:"evidence"`
	Action   string `json:"action"`
}

// maxTuningSuggestions caps the returned suggestion list.
const maxTuningSuggestions = 10

// SuggestTuning aggregates non-pending review items by Kind+Reason and
// returns tuning suggestions ordered by count descending. A missing queue
// yields an empty (non-nil) slice and nil error.
func SuggestTuning(repoRoot string) ([]TuningSuggestion, error) {
	items, err := load(repoRoot)
	if err != nil {
		return nil, err
	}
	type groupKey struct {
		kind   string
		reason string
	}
	counts := make(map[groupKey]int)
	statusOf := make(map[groupKey]string)
	for _, it := range items {
		if it.Status == StatusPending || it.Status == "" {
			continue
		}
		k := groupKey{kind: it.Kind, reason: strings.TrimSpace(it.Reason)}
		counts[k]++
		// All members of a group share the same Area/Action mapping, which
		// depends only on Kind (+ observed vs resolved for reverts); keep
		// the "most terminal" status seen for the mapping.
		if statusOf[k] != StatusObserved {
			statusOf[k] = it.Status
		}
	}
	out := make([]TuningSuggestion, 0, len(counts))
	for k, n := range counts {
		area, action := tuningAction(k.kind, statusOf[k])
		evidence := fmt.Sprintf("%dx kind=%q reason=%q", n, k.kind, k.reason)
		out = append(out, TuningSuggestion{Area: area, Evidence: evidence, Action: action})
	}
	sort.Slice(out, func(i, j int) bool {
		ci, cj := tuningCount(out[i].Evidence), tuningCount(out[j].Evidence)
		if ci != cj {
			return ci > cj
		}
		if out[i].Area != out[j].Area {
			return out[i].Area < out[j].Area
		}
		return out[i].Action < out[j].Action
	})
	if len(out) > maxTuningSuggestions {
		out = out[:maxTuningSuggestions]
	}
	if out == nil {
		out = []TuningSuggestion{}
	}
	return out, nil
}

// tuningCount extracts the leading "Nx" occurrence count from an Evidence
// string produced by SuggestTuning (0 when unparseable).
func tuningCount(evidence string) int {
	var n int
	if _, err := fmt.Sscanf(evidence, "%dx", &n); err != nil {
		return 0
	}
	return n
}

// tuningAction maps a review Kind (+ observed status for reverts) to the
// suggestion Area and Action text.
func tuningAction(kind, status string) (area, action string) {
	lower := strings.ToLower(strings.TrimSpace(kind))
	switch {
	case strings.Contains(lower, "conflict"):
		return "merge-conflict prompts",
			"Improve merge/prompt notes for conflicted sections: add explicit merge guidance and narrower section instructions so repeated conflict patterns stop recurring."
	case strings.Contains(lower, "snippet"):
		return "snippet grounding",
			"Close grounding gaps behind snippet-fix rejections: refresh symbol signatures and arity data feeding snippet rendering."
	case strings.Contains(lower, "adr"):
		return "adr triggers",
			"Tighten ADR auto-generation triggers: rejected drafts indicate over-eager event mapping — raise the evidence bar before drafting."
	case strings.Contains(lower, "revert"):
		if status == StatusObserved {
			return "doc corrections",
				"Apply observed human reverts as doc corrections: the reverted text was already fixed by a human, so update the source sections and prompts to match."
		}
		return "style/prompts",
			"Adjust style/prompts from revert feedback with recorded reasons: align tone, structure, and instruction wording with what reviewers kept."
	default:
		if strings.TrimSpace(kind) == "" {
			kind = "review"
		}
		return kind,
			fmt.Sprintf("Review repeated %q outcomes and adjust the corresponding prompts, checks, or triggers.", kind)
	}
}
