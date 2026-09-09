// Gate 5: Semantic diff compression.
// Accepts new content only when it contains at least one factual AST-level change
// compared to the old content. Rejects churn-only rewrites (whitespace, synonym
// substitution, bullet reordering) that would produce noise in git diffs.
package verifier

import (
	"strings"
)

// synonyms maps common "equivalent" word pairs that an LLM might swap without
// changing meaning. Presence of only synonym swaps → churn.
var synonymPairs = [][2]string{
	{"returns", "return"},
	{"utilizes", "uses"},
	{"provides", "gives"},
	{"performs", "does"},
	{"initializes", "inits"},
	{"initialise", "initialize"},
	{"optimises", "optimizes"},
}

// isSemanticNoOp returns true when old and new content are semantically
// equivalent — i.e., the diff is pure churn with no factual change.
//
// Strategy (in order):
//  1. Byte-identical → trivial no-op.
//  2. Normalized-identical (collapse whitespace, lowercase) → no-op.
//  3. Structural diff: count tokens that appear in new but not old and vice-versa.
//     If the only difference is synonyms → no-op.
//
// ponytail: word-set diff. Upgrade to AST-level if synonym false-negatives appear.
func isSemanticNoOp(old, new string) bool {
	if old == new {
		return true
	}
	if normalize(old) == normalize(new) {
		return true
	}
	// Compute added/removed word sets.
	oldWords := wordSet(normalize(old))
	newWords := wordSet(normalize(new))

	added := setDiff(newWords, oldWords)
	removed := setDiff(oldWords, newWords)

	// Filter out synonym pairs — compute both filtered sets before reassigning
	// so neither call sees a half-filtered counterpart.
	filteredAdded := removeMatchedSynonyms(added, removed)
	filteredRemoved := removeMatchedSynonyms(removed, added)

	return len(filteredAdded) == 0 && len(filteredRemoved) == 0
}

func normalize(s string) string {
	s = strings.ToLower(s)
	// Collapse all whitespace runs to a single space.
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !inSpace {
				b.WriteRune(' ')
				inSpace = true
			}
		} else {
			inSpace = false
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func wordSet(s string) map[string]bool {
	words := strings.Fields(s)
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

func setDiff(a, b map[string]bool) map[string]bool {
	diff := make(map[string]bool)
	for k := range a {
		if !b[k] {
			diff[k] = true
		}
	}
	return diff
}

// removeMatchedSynonyms removes words from `words` that are synonyms of words in `counterpart`.
func removeMatchedSynonyms(words, counterpart map[string]bool) map[string]bool {
	result := make(map[string]bool)
	for w := range words {
		matched := false
		for _, pair := range synonymPairs {
			if (w == pair[0] && counterpart[pair[1]]) ||
				(w == pair[1] && counterpart[pair[0]]) {
				matched = true
				break
			}
		}
		if !matched {
			result[w] = true
		}
	}
	return result
}
