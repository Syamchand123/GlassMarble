package commit_reasoning

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// ExtractIntent returns the architectural intent deterministically without LLM.
// Level 1: Deterministic keyword patterns
// Level 2: Structural patterns in commit message
func ExtractIntent(meta *CommitMeta, prDescription string) (intent string, src evidence.Source, confidence float64) {
	message := strings.ToLower(meta.Subject + " " + meta.Body + " " + prDescription)

	// Level 2: Structural patterns (higher priority)
	structuralPatterns := []string{
		"because ", "in order to ", "to fix ", "due to ",
	}
	for _, p := range structuralPatterns {
		idx := strings.Index(message, p)
		if idx != -1 {
			start := idx + len(p)
			end := strings.IndexAny(message[start:], ".\n")
			if end == -1 {
				end = len(message) - start
			}
			extracted := strings.TrimSpace(message[start : start+end])
			if len(extracted) > 0 {
				return extracted, evidence.SourceGit, 0.8
			}
		}
	}

	// Level 1: Keyword patterns
	keywords := map[string]string{
		"performance": "performance optimization",
		"slow":        "performance optimization",
		"security":    "security motivation",
		"refactor":    "refactoring",
		"extract":     "extraction/decomposition",
		"split":       "service split",
		"add redis":   "caching technology addition",
		"cache":       "caching optimization",
		"decouple":    "architectural decoupling",
	}

	for kw, mappedIntent := range keywords {
		if strings.Contains(message, kw) {
			return mappedIntent, evidence.SourceGit, 0.9
		}
	}

	// No intent found
	return "", evidence.SourceGit, 0.0
}
