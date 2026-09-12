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
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

// ────────────────────────────────────────────────────────────────────────────
// TuningApply — safe auto-fix consumption loop (ADD ONLY)
// ────────────────────────────────────────────────────────────────────────────

// TuningApply applies a single TuningSuggestion to the repo when the fix is
// provably safe, and returns an error otherwise.
//
// SAFE auto-fixes (the only kind applied):
//   - Area names a style concern (contains "style", case-insensitive —
//     currently the "style/prompts" area from resolved revert feedback) AND
//     the suggestion Evidence names a single jargon term in quotes
//     (e.g. reason=`overuses "synergize"`): the term is appended to the
//     global style.jargon_blacklist in .glassmarble/docs.yaml (created when
//     the style block or the key is absent; everything else byte-preserved).
//     Re-applying an already-listed term is a no-op success.
//
// Everything else returns an error containing "manual apply required"
// (unknown areas, missing jargon term, missing docs.yaml): the caller
// reports it and moves on. The YAML edit is minimal and line-based (stdlib
// only): review must stay free of the config package's yaml dependency.
//
// Evidence is parsed with strconv.Unquote off the trailing `reason=...`
// %q-encoded field produced by SuggestTuning; the term is the first
// double- (or single-) quoted single word (letters/hyphens, length ≥ 3).
func TuningApply(repoRoot string, suggestion TuningSuggestion) error {
	if !strings.Contains(strings.ToLower(suggestion.Area), "style") {
		return fmt.Errorf("review: manual apply required for area %q: no safe auto-fix", suggestion.Area)
	}
	term, ok := tuningJargonTerm(suggestion.Evidence)
	if !ok {
		return fmt.Errorf("review: manual apply required: style suggestion names no quoted jargon term")
	}
	return appendJargonTerm(repoRoot, term)
}

// tuningJargonTerm extracts the first quoted single-word jargon term from a
// SuggestTuning Evidence string (`Nx kind="K" reason="R"`). It unquotes the
// trailing reason field and returns the first quoted word inside it.
func tuningJargonTerm(evidence string) (string, bool) {
	idx := strings.Index(evidence, `reason=`)
	if idx < 0 {
		return "", false
	}
	reason, err := strconv.Unquote(strings.TrimSpace(evidence[idx+len(`reason=`):]))
	if err != nil {
		return "", false
	}
	for _, quote := range []byte{'"', '\''} {
		if term, ok := firstQuotedWord(reason, quote); ok {
			return term, true
		}
	}
	return "", false
}

// firstQuotedWord returns the first quoted span (delimited by quote) that is
// a single word: letters and hyphens only, length ≥ 3. The result is
// lowercased (blacklist matching is case-insensitive downstream).
func firstQuotedWord(s string, quote byte) (string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] != quote {
			continue
		}
		end := strings.IndexByte(s[i+1:], quote)
		if end < 0 {
			return "", false
		}
		cand := strings.TrimSpace(s[i+1 : i+1+end])
		if isJargonWord(cand) {
			return strings.ToLower(cand), true
		}
		i += end
	}
	return "", false
}

// isJargonWord reports whether cand looks like a blacklistable jargon term:
// length ≥ 3, letters and interior hyphens only, no spaces.
func isJargonWord(cand string) bool {
	if len(cand) < 3 {
		return false
	}
	for i := 0; i < len(cand); i++ {
		c := cand[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			continue
		}
		if c == '-' && i > 0 && i < len(cand)-1 {
			continue
		}
		return false
	}
	return true
}

// docsYAMLPath mirrors the config loader's search order: .glassmarble/
// docs.yaml first, then repo-root docs.yaml. It returns "" when neither
// exists (the caller reports "manual apply required" instead of inventing
// configuration).
func docsYAMLPath(repoRoot string) string {
	primary := filepath.Join(repoRoot, ".glassmarble", "docs.yaml")
	if _, err := os.Stat(primary); err == nil {
		return primary
	}
	fallback := filepath.Join(repoRoot, "docs.yaml")
	if _, err := os.Stat(fallback); err == nil {
		return fallback
	}
	return ""
}

// appendJargonTerm appends term to the global style.jargon_blacklist with a
// minimal line-based YAML edit. An already-listed term (case-insensitive)
// is a no-op success. Block lists, flow lists (`[a, b]`), a missing key
// under an existing style block, and a missing style block (appended at
// end of file) are all handled; every other line is byte-preserved.
func appendJargonTerm(repoRoot, term string) error {
	path := docsYAMLPath(repoRoot)
	if path == "" {
		return fmt.Errorf("review: manual apply required: no docs.yaml found for jargon term %q", term)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("review: reading docs.yaml: %w", err)
	}
	lines := strings.Split(string(raw), "\n")
	lower := strings.ToLower(term)

	// Already listed? Scan block items (`- term`) and flow lists.
	inBlacklist := false
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(strings.ToLower(t), "jargon_blacklist:") {
			inBlacklist = true
			// Flow style on the same line: jargon_blacklist: [a, b].
			if open := strings.Index(t, "["); open >= 0 {
				if close := strings.Index(t, "]"); close > open {
					for _, item := range strings.Split(t[open+1:close], ",") {
						if strings.ToLower(strings.Trim(strings.TrimSpace(item), `"'`)) == lower {
							return nil
						}
					}
				}
			}
			continue
		}
		if inBlacklist {
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			item := strings.TrimSpace(strings.TrimPrefix(t, "-"))
			if strings.HasPrefix(t, "-") || strings.HasPrefix(t, "- ") {
				if strings.ToLower(strings.Trim(item, `"'`)) == lower {
					return nil
				}
				continue
			}
			inBlacklist = false
		}
	}

	// Locate the key line (top-level `style:` block assumed at column 0;
	// nested style keys live indented under their document — the global one
	// is what we edit).
	keyIdx := -1
	keyIndent := ""
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(strings.ToLower(t), "jargon_blacklist:") {
			// Prefer the top-level (least-indented) occurrence.
			indent := ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
			if keyIdx == -1 || len(indent) < len(keyIndent) {
				keyIdx, keyIndent = i, indent
			}
		}
	}
	if keyIdx >= 0 {
		lines = insertJargonItem(lines, keyIdx, keyIndent, term)
	} else {
		lines = ensureStyleBlacklist(lines, term)
	}

	out := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(out), 0644); err != nil {
		return fmt.Errorf("review: writing docs.yaml: %w", err)
	}
	return nil
}

// insertJargonItem inserts `- term` into the jargon_blacklist at keyIdx:
// appended after the trailing run of block items, rewritten into a flow
// list when inline, or added on the next line when the key has no values.
func insertJargonItem(lines []string, keyIdx int, keyIndent, term string) []string {
	line := lines[keyIdx]
	trimmed := strings.TrimSpace(line)
	// Flow style: jargon_blacklist: [a, b] (or []).
	if open := strings.Index(trimmed, "["); open >= 0 {
		if close := strings.Index(trimmed, "]"); close > open {
			inner := strings.TrimSpace(trimmed[open+1 : close])
			var items []string
			if inner != "" {
				items = strings.Split(inner, ",")
				for i := range items {
					items[i] = strings.TrimSpace(items[i])
				}
			}
			items = append(items, term)
			lines[keyIdx] = line[:strings.Index(line, "[")+1] + strings.Join(items, ", ") + "]"
			return lines
		}
	}
	// Block style: find the end of the trailing item run (skipping blanks
	// and comments), then insert with the items' indent (or key+2 spaces).
	itemIndent := keyIndent + "  "
	j := keyIdx + 1
	for j < len(lines) {
		t := strings.TrimSpace(lines[j])
		if t == "" || strings.HasPrefix(t, "#") {
			j++
			continue
		}
		if strings.HasPrefix(t, "-") {
			itemIndent = lines[j][:len(lines[j])-len(strings.TrimLeft(lines[j], " \t"))]
			j++
			continue
		}
		break
	}
	item := itemIndent + "- " + term
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:j]...)
	out = append(out, item)
	out = append(out, lines[j:]...)
	return out
}

// ensureStyleBlacklist adds a global style.jargon_blacklist entry when the
// key is absent: nested under the existing top-level `style:` block, or a
// new trailing block when there is none.
func ensureStyleBlacklist(lines []string, term string) []string {
	styleIdx := -1
	for i, ln := range lines {
		if strings.TrimSpace(strings.ToLower(ln)) == "style:" && strings.TrimLeft(ln, " \t") == ln {
			styleIdx = i
			break
		}
	}
	if styleIdx == -1 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		return append(lines, "style:", "  jargon_blacklist:", "    - "+term)
	}
	// Insert directly under `style:` (mapping order is irrelevant to YAML).
	out := make([]string, 0, len(lines)+2)
	out = append(out, lines[:styleIdx+1]...)
	out = append(out, "  jargon_blacklist:", "    - "+term)
	out = append(out, lines[styleIdx+1:]...)
	return out
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
