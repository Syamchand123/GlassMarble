package knowledge_fusion

import (
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// ResolveConflicts groups claims by fact identity — (subject, subjectID,
// predicate) — and applies the Stage 9 conflict semantics (master plan §7.5):
//
//   - NEVER DELETE A CLAIM. Every claim survives resolution; the loser of a
//     contradiction is marked HISTORICAL with a ValidUntil, never dropped.
//   - The group key includes SubjectID so that entity-linker expansions —
//     one claim per AKG node defined in the same file — are DIFFERENT facts
//     and never merge into each other: two claims with identical text but
//     different resolved entities both survive with their own SubjectID.
//   - Exclusive predicates (config.ExclusivePredicates: "state", "status",
//     "version", ...) are single-valued per subject. Two claims on the same
//     subject with DIFFERENT objects are a real contradiction: the claim
//     with the higher-reliability source stays primary, every other claim
//     in the group becomes HISTORICAL (its own evidence preserved).
//   - Identical claims (same subject, predicate AND object, AND the same
//     resolved subject entity) from different sources are merged into one
//     claim with a combined evidence bundle (deduplicated by source+
//     reference) — the provenance of every source is retained, nothing is
//     overwritten.
//   - Multi-valued predicates ("uses_technology", "was_modified_by_pr",
//     "decided_to", ...) never contradict: different objects coexist.
//
// Determinism: within a group, the primary claim is chosen by source
// reliability (evidence.SourceReliability), then aggregate confidence, then
// claim ID — never by map iteration order. The returned slice is sorted by
// claim ID, so the resolution is fully reproducible regardless of input
// order or grouping iteration order.
func ResolveConflicts(claims []developer_memory.KnowledgeClaim, exclusive map[string]bool) []developer_memory.KnowledgeClaim {
	groups := make(map[string][]int)
	for i, c := range claims {
		key := c.Subject + "\x00" + c.SubjectID + "\x00" + c.Predicate
		groups[key] = append(groups[key], i)
	}

	var resolved []developer_memory.KnowledgeClaim
	for _, group := range groups {
		sort.SliceStable(group, func(a, b int) bool {
			return claims[a].ID < claims[b].ID
		})
		resolved = append(resolved, resolveGroup(claims, group, exclusive)...)
	}

	// Restore the caller's deterministic input order by stable-sorting on
	// the original position of each surviving claim.
	sort.SliceStable(resolved, func(i, j int) bool {
		return resolved[i].ID < resolved[j].ID
	})
	return resolved
}

// resolveGroup folds one (subject, predicate) group into its resolved
// claims. Returns at least one claim — never zero, never nil.
func resolveGroup(claims []developer_memory.KnowledgeClaim, group []int, exclusive map[string]bool) []developer_memory.KnowledgeClaim {
	if len(group) == 1 {
		return []developer_memory.KnowledgeClaim{claims[group[0]]}
	}

	// Collapse identical (subject, predicate, object) claims first: merged
	// bundles keep every source's provenance.
	var uniq []developer_memory.KnowledgeClaim
	byObject := make(map[string]int) // object -> index in uniq
	for _, idx := range group {
		c := claims[idx]
		if i, ok := byObject[c.Object]; ok {
			uniq[i] = mergeIdentical(uniq[i], c)
			continue
		}
		byObject[c.Object] = len(uniq)
		uniq = append(uniq, c)
	}
	if len(uniq) == 1 {
		return uniq
	}

	predicate := uniq[0].Predicate
	if exclusive[predicate] {
		// Single-valued predicate with differing objects: a contradiction.
		// The highest-reliability claim is primary; the rest become
		// HISTORICAL (never deleted, ValidUntil = the winner's ValidFrom).
		sort.SliceStable(uniq, func(a, b int) bool {
			return claimDominates(uniq[a], uniq[b])
		})
		winner := uniq[0]
		until := winner.ValidFrom
		for i := 1; i < len(uniq); i++ {
			uniq[i].State = developer_memory.StateHistorical
			uniq[i].ValidUntil = &until
		}
		return uniq
	}

	// Multi-valued predicate: different objects coexist as-is.
	return uniq
}

// claimDominates reports whether a should be chosen over b as the primary
// claim of a contradiction group: higher source reliability wins, then
// higher aggregate confidence, then lexicographically smaller ID (a total
// order, so the result is deterministic regardless of input order).
func claimDominates(a, b developer_memory.KnowledgeClaim) bool {
	ra := evidence.SourceReliability(a.Evidence.PrimarySource)
	rb := evidence.SourceReliability(b.Evidence.PrimarySource)
	if ra != rb {
		return ra > rb
	}
	if a.Evidence.AggConfidence != b.Evidence.AggConfidence {
		return a.Evidence.AggConfidence > b.Evidence.AggConfidence
	}
	return a.ID < b.ID
}

// mergeIdentical folds two claims with the same (subject, predicate, object)
// into one. The primary claim is the one with the higher-reliability source;
// the other claim's evidence items are appended (deduplicated by
// source+reference), and the aggregate confidence is recomputed. The
// earliest ValidFrom is kept — the claim became true when the first source
// recorded it.
func mergeIdentical(a, b developer_memory.KnowledgeClaim) developer_memory.KnowledgeClaim {
	if !claimDominates(a, b) {
		a, b = b, a
	}
	for _, item := range b.Evidence.Items {
		a.Evidence.Add(item)
	}
	// Bundle.Add appends blindly; dedup by (source, reference) so the same
	// evidence from the same place never appears twice in one bundle.
	a.Evidence = dedupeEvidence(a.Evidence)
	if b.ValidFrom.Before(a.ValidFrom) && !b.ValidFrom.IsZero() {
		a.ValidFrom = b.ValidFrom
	}
	return a
}

// dedupeEvidence removes duplicate evidence items (same source AND same
// reference) while preserving the first occurrence, then recomputes the
// aggregate.
func dedupeEvidence(b evidence.Bundle) evidence.Bundle {
	seen := make(map[string]bool)
	var items []evidence.EvidenceItem
	for _, it := range b.Items {
		key := string(it.Source) + "\x00" + it.Reference
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, it)
	}
	b.Items = items
	b.Aggregate()
	return b
}

// exclusivePredicateSet converts a config predicate list into the lookup map
// ResolveConflicts expects.
func exclusivePredicateSet(predicates []string) map[string]bool {
	set := make(map[string]bool, len(predicates))
	for _, p := range predicates {
		if p = strings.TrimSpace(p); p != "" {
			set[p] = true
		}
	}
	return set
}
