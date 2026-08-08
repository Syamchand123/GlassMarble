package knowledge_fusion

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// stateClaim builds a claim on an exclusive-style predicate.
func stateClaim(id, object string, ts time.Time) developer_memory.KnowledgeClaim {
	c := testClaim(id, "mod1", object, ts)
	c.Predicate = "state"
	return c
}

func TestResolveConflicts_IdenticalClaimsFolded(t *testing.T) {
	ts := time.Now().UTC()
	a := stateClaim("a", "accepted", ts)
	b := stateClaim("b", "accepted", ts.Add(-time.Hour))
	// b's evidence must reference a DIFFERENT source, or the merge would
	// deduplicate it away as the same provenance.
	b.Evidence.Items[0].Reference = "docs/adr/0002.md"

	resolved := ResolveConflicts([]developer_memory.KnowledgeClaim{a, b}, map[string]bool{"state": true})
	if len(resolved) != 1 {
		t.Fatalf("got %d claims, want 1 folded", len(resolved))
	}
	// The earliest timestamp wins — the claim became true when the first
	// source recorded it.
	if !resolved[0].ValidFrom.Equal(b.ValidFrom) {
		t.Errorf("valid_from = %v, want earliest %v", resolved[0].ValidFrom, b.ValidFrom)
	}
	if len(resolved[0].Evidence.Items) != 2 {
		t.Errorf("evidence items = %d, want 2 (provenance of both sources)", len(resolved[0].Evidence.Items))
	}
}

func TestResolveConflicts_IdenticalClaimsDeduplicatedEvidence(t *testing.T) {
	ts := time.Now().UTC()
	a := stateClaim("a", "accepted", ts)
	b := stateClaim("b", "accepted", ts)
	// b's evidence references the same source: it must be deduplicated.
	item := a.Evidence.Items[0]
	b.Evidence = evidence.NewBundle(item)

	resolved := ResolveConflicts([]developer_memory.KnowledgeClaim{a, b}, map[string]bool{"state": true})
	if len(resolved) != 1 {
		t.Fatalf("got %d claims, want 1", len(resolved))
	}
	if len(resolved[0].Evidence.Items) != 1 {
		t.Errorf("evidence items = %d, want 1 after dedup", len(resolved[0].Evidence.Items))
	}
}

func TestResolveConflicts_ExclusiveContradictionLosesRetained(t *testing.T) {
	ts := time.Now().UTC()
	winner := stateClaim("aaa", "accepted", ts) // lower ID wins ties
	loser := stateClaim("bbb", "proposed", ts)

	resolved := ResolveConflicts([]developer_memory.KnowledgeClaim{winner, loser}, map[string]bool{"state": true})
	if len(resolved) != 2 {
		t.Fatalf("got %d claims, want 2 (loser is never deleted)", len(resolved))
	}

	// Exactly one claim keeps StateActive and wins the contradiction.
	activeCount, historicalCount := 0, 0
	for _, c := range resolved {
		switch c.State {
		case developer_memory.StateActive:
			activeCount++
		case developer_memory.StateHistorical:
			historicalCount++
			if c.ValidUntil == nil {
				t.Error("historical loser has no ValidUntil")
			}
			if c.Object != "proposed" {
				t.Errorf("loser object = %q, want the superseded statement", c.Object)
			}
			if len(c.Evidence.Items) != 1 {
				t.Errorf("loser evidence = %d items, want original provenance retained", len(c.Evidence.Items))
			}
		}
	}
	if activeCount != 1 || historicalCount != 1 {
		t.Errorf("states = active %d / historical %d, want 1/1", activeCount, historicalCount)
	}
}

func TestResolveConflicts_MultiValuedPredicateCoexists(t *testing.T) {
	ts := time.Now().UTC()
	a := testClaim("a", "mod1", "Use Redis", ts)
	b := testClaim("b", "mod1", "Use Postgres", ts)

	resolved := ResolveConflicts([]developer_memory.KnowledgeClaim{a, b}, map[string]bool{"state": true})
	if len(resolved) != 2 {
		t.Fatalf("got %d claims, want 2 (decided_to is multi-valued)", len(resolved))
	}
	for _, c := range resolved {
		if c.State != developer_memory.StateActive {
			t.Errorf("claim %s lost its state: %s", c.ID, c.State)
		}
	}
}

func TestResolveConflicts_DeterministicRegardlessOfInputOrder(t *testing.T) {
	ts := time.Now().UTC()
	a := stateClaim("aaa", "accepted", ts)
	b := stateClaim("bbb", "proposed", ts)

	r1 := ResolveConflicts([]developer_memory.KnowledgeClaim{a, b}, map[string]bool{"state": true})
	r2 := ResolveConflicts([]developer_memory.KnowledgeClaim{b, a}, map[string]bool{"state": true})

	if len(r1) != len(r2) {
		t.Fatalf("result sizes differ: %d vs %d", len(r1), len(r2))
	}
	for i := range r1 {
		if r1[i].ID != r2[i].ID || r1[i].State != r2[i].State {
			t.Errorf("input order changed resolution at %d: %s/%s vs %s/%s",
				i, r1[i].ID, r1[i].State, r2[i].ID, r2[i].State)
		}
	}
}

func TestResolveConflicts_SingleClaimPassesThrough(t *testing.T) {
	ts := time.Now().UTC()
	a := stateClaim("aaa", "accepted", ts)

	resolved := ResolveConflicts([]developer_memory.KnowledgeClaim{a}, map[string]bool{"state": true})
	if len(resolved) != 1 {
		t.Fatalf("got %d claims, want 1", len(resolved))
	}
	if resolved[0].State != developer_memory.StateActive {
		t.Errorf("state = %s, want ACTIVE untouched", resolved[0].State)
	}
}
