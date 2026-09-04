// Package evidence defines the provenance and proof primitives used throughout GlassMarble V2.
//
// WHY THIS EXISTS:
//
//	Every piece of knowledge that GlassMarble produces — detected patterns, architectural events,
//	developer memory claims — must carry a traceable proof chain. The evidence package provides
//	that chain. Without provenance, we cannot distinguish "we detected this from code" (high
//	confidence) from "an LLM inferred this" (lower confidence, fast-decaying).
//
// WHAT THIS IS:
//   - Source: an enum identifying where a knowledge claim originated (code, git, PR, docs, LLM, user).
//   - EvidenceItem: a single piece of backing proof — a commit hash, a PR URL, a code excerpt.
//   - Bundle: a group of EvidenceItems backing one claim, with aggregate confidence.
//
// HOW TO USE:
//
//	When any phase (5-12) creates an ArchEvent, DetectedPattern, ArchSmell, or KnowledgeClaim,
//	it MUST populate at least one EvidenceItem in the Bundle. An empty Bundle is a bug and is
//	caught by tests. Use Bundle.Add() to append items, Bundle.Aggregate() to compute the
//	aggregate confidence (weighted minimum across all items).
//
// DESIGN RULES:
//   - Evidence is immutable once created — corrections go in learning/corrections.jsonl.
//   - Confidence is always 0.0–1.0. Never invent a confidence without a stated rationale.
//   - Source reliability order (highest → lowest):
//     SourceCode(1.0) > SourceUser(0.98) > SourceDocs/SourcePR(0.9) >
//     SourceGit(0.8) > SourceRule(0.75) > SourceHeuristic(0.7) > SourceLLM(0.65)
//
// PACKAGE POSITION IN DAG:
//
//	This is a LEAF package. It imports NO internal GlassMarble packages.
//	All other V2 packages (archmodel, arch_intelligence, developer_memory, ...) import evidence.
package evidence

import "time"

// Source identifies where a piece of knowledge came from.
// The string values are stored in JSON and must never be changed once committed.
type Source string

const (
	// SourceCode means the claim was derived directly from AKG/CPG static analysis.
	// Highest reliability — the code is the ground truth.
	SourceCode Source = "code"

	// SourceGit means the claim came from git log, diff, or blame output.
	// High reliability — git history is authoritative.
	SourceGit Source = "git"

	// SourcePR means the claim came from a pull-request description or review comment.
	// Good reliability — explicit developer intent, but may be aspirational.
	SourcePR Source = "pr"

	// SourceIssue means the claim came from an issue tracker (GitHub Issues, Jira, Linear).
	// Good reliability — captures requirements context.
	SourceIssue Source = "issue"

	// SourceDocs means the claim came from README, ADR, or other markdown documentation.
	// Good reliability for formal ADRs; lower for informal READMEs.
	SourceDocs Source = "docs"

	// SourceLLM means the claim was produced by LLM inference.
	// Lowest automatic reliability — always used as a last resort, decays fastest.
	// LLM is the journalist, not the scientist. It articulates; it does not discover.
	SourceLLM Source = "llm"

	// SourceUser means the developer explicitly corrected or annotated a claim.
	// Second-highest reliability — explicit human knowledge. Stored in corrections.jsonl.
	SourceUser Source = "user"

	// SourceRule means the claim came from a deterministic rule engine (pattern rules, smell rules).
	// High reliability — rules are authored, tested, and versioned.
	SourceRule Source = "rule"

	// SourceHeuristic means the claim came from a name/pattern heuristic
	// (e.g., a struct named "UserRepository" is inferred to be a repository).
	// Moderate reliability — correct for well-named code, wrong for poor naming.
	SourceHeuristic Source = "heuristic"
)

// SourceReliability returns the baseline reliability weight for a given source.
// Used by Bundle.Aggregate() to compute weighted confidence.
// Values are 0.0–1.0 and correspond to the reliability order in the package doc.
func SourceReliability(s Source) float64 {
	switch s {
	case SourceCode:
		return 1.0
	case SourceUser:
		return 0.98
	case SourceDocs, SourcePR:
		return 0.90
	case SourceGit:
		return 0.80
	case SourceRule:
		return 0.75
	case SourceHeuristic:
		return 0.70
	case SourceLLM:
		return 0.65
	default:
		return 0.50
	}
}

// EvidenceItem is one piece of backing proof for a knowledge claim.
//
// Every EvidenceItem carries:
//   - Source: where the proof came from (code, git, pr, docs, llm, user, rule, heuristic)
//   - Reference: a stable pointer to the proof (commit hash, PR URL, file path, rule ID)
//   - Excerpt: the relevant quote or snippet, capped at 512 characters
//   - Confidence: 0.0–1.0, how much we trust this specific piece of evidence
//   - Timestamp: when this evidence was captured
type EvidenceItem struct {
	Source     Source    `json:"source"`
	Reference  string    `json:"reference"`  // commit hash, PR URL, file path, rule ID, etc.
	Excerpt    string    `json:"excerpt"`    // relevant quote/snippet, max 512 chars
	Confidence float64   `json:"confidence"` // 0.0–1.0
	Timestamp  time.Time `json:"timestamp"`
}

// Bundle groups related EvidenceItems backing a single knowledge claim.
//
// Every ArchEvent, ArchSmell, DetectedPattern, and KnowledgeClaim must carry a non-empty
// Bundle. A Bundle with no Items is a bug and is asserted in tests.
//
// AggConfidence is the aggregate confidence across all Items, computed as a weighted minimum
// that accounts for both item confidence and source reliability. Call Aggregate() to recompute
// after adding new items.
//
// PrimarySource is the highest-reliability source present in the bundle, used to drive
// freshness decay curves in knowledge aging (knowledge_aging).
type Bundle struct {
	Items         []EvidenceItem `json:"items"`
	AggConfidence float64        `json:"agg_confidence"` // recomputed by Aggregate()
	PrimarySource Source         `json:"primary_source"` // highest-reliability source in items
}

// Add appends a new EvidenceItem to the bundle and updates AggConfidence and PrimarySource.
// This is the preferred way to build bundles — never append to Items directly.
func (b *Bundle) Add(e EvidenceItem) {
	// Clamp excerpt to 512 runes to avoid bloating persisted JSON (rune-boundary safe).
	if len([]rune(e.Excerpt)) > 512 {
		e.Excerpt = string([]rune(e.Excerpt)[:512])
	}
	b.Items = append(b.Items, e)
	b.Aggregate()
}

// Aggregate recomputes AggConfidence and PrimarySource from the current Items.
//
// AggConfidence = max(item.Confidence * SourceReliability(item.Source)): a claim
// is as well supported as the strongest thing supporting it.
//
// This was a weighted MINIMUM, which inverted the meaning of a bundle. A bundle
// is corroboration -- several reasons to believe one claim -- not a chain of
// inference where the weakest step governs. Under a minimum, attaching evidence
// could only ever lower a claim's score, so the better-supported a claim was,
// the worse it ranked. On this repository every DEPENDENCY_ADDED event carried
// direct code evidence (0.9) plus the git commit that made the change (0.8) and
// scored 0.8 -- strictly worse than the identical claim with the corroborating
// commit removed. The package's own test pinned the extreme case: code evidence
// at 0.9 collapsed to 0.39 because an LLM rationale was attached alongside it,
// devaluing a claim the code itself proves by 57% for the sin of also asking a
// model about it.
//
// Taking the maximum keeps the half of the original intent that was sound --
// a weak item still cannot inflate a claim ABOVE what its best evidence
// supports, so an LLM guess never manufactures confidence -- while dropping the
// half that was not: a weak item no longer destroys confidence that better
// evidence has already established. Weak items remain in Items for provenance,
// which is what they are actually good for.
//
// Deliberately NOT rewarded: the NUMBER of corroborating items. Two independent
// code observations score the same as one. Rewarding count needs a disjunctive
// combiner (noisy-OR and relatives), and every such combiner also lets a
// speculative item raise the score, which is precisely the behaviour the
// original design set out to prevent. Ranking is the only consumer of this
// number, so the conservative rule is the right default.
//
// Returns 0.0 if the bundle is empty (empty bundle = bug, caught by tests).
func (b *Bundle) Aggregate() float64 {
	if len(b.Items) == 0 {
		b.AggConfidence = 0.0
		return 0.0
	}

	// Find primary source (highest reliability).
	best := b.Items[0].Source
	for _, item := range b.Items[1:] {
		if SourceReliability(item.Source) > SourceReliability(best) {
			best = item.Source
		}
	}
	b.PrimarySource = best

	strongest := 0.0
	for _, item := range b.Items {
		if w := item.Confidence * SourceReliability(item.Source); w > strongest {
			strongest = w
		}
	}

	b.AggConfidence = strongest
	return strongest
}

// IsEmpty returns true if the bundle has no evidence items.
// An empty bundle on a published ArchEvent, Smell, or Pattern is a bug.
func (b *Bundle) IsEmpty() bool {
	return len(b.Items) == 0
}

// NewBundle constructs a Bundle from a single EvidenceItem.
// Convenience function for the common case of creating a bundle with one item.
func NewBundle(item EvidenceItem) Bundle {
	b := Bundle{}
	b.Add(item)
	return b
}
