package learning

import (
	"testing"
	"time"
)

// TestCorrectionIDIsContentAddressed pins the documented contract on
// Correction.ID: "Appending the same correction twice yields the same ID".
//
// Regression: Append set Timestamp = time.Now() when zero and correctionID
// folded that timestamp in, so two identical CLI invocations produced
// different IDs and both survived LoadAll's dedup.
func TestCorrectionIDIsContentAddressed(t *testing.T) {
	t1 := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 30, 23, 59, 59, 999, time.UTC)

	a := correctionID(CorrectionKindIntent, "event-1", "decided in ADR-014", t1)
	b := correctionID(CorrectionKindIntent, "event-1", "decided in ADR-014", t2)
	if a != b {
		t.Errorf("same content at different times produced different IDs:\n %s\n %s", a, b)
	}

	if c := correctionID(CorrectionKindIntent, "event-2", "decided in ADR-014", t1); c == a {
		t.Error("different target produced the same ID")
	}
	if c := correctionID(CorrectionKindIntent, "event-1", "something else", t1); c == a {
		t.Error("different value produced the same ID")
	}
	if c := correctionID(CorrectionKindState, "event-1", "decided in ADR-014", t1); c == a {
		t.Error("different kind produced the same ID")
	}
}

// TestCorrectionIDFieldsAreUnambiguous covers the hash-domain flaw: the old
// derivation skipped empty parts, so (kind, target, "") and (kind, "", target)
// hashed identically.
func TestCorrectionIDFieldsAreUnambiguous(t *testing.T) {
	ts := time.Now()
	withTarget := correctionID(CorrectionKindIntent, "alpha", "", ts)
	withValue := correctionID(CorrectionKindIntent, "", "alpha", ts)
	if withTarget == withValue {
		t.Error("a value in the target field hashes the same as the same value in the corrected-value field")
	}
}

// TestAppendIsIdempotent exercises the real store path twice with no explicit
// timestamp, the way the CLI does.
func TestAppendIsIdempotent(t *testing.T) {
	s := NewStore(t.TempDir() + "/corrections.jsonl")
	c := Correction{
		Kind:           CorrectionKindIntent,
		TargetID:       "event-1",
		CorrectedValue: "decided in ADR-014",
		Author:         "tester",
	}
	first, err := s.Append(c)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	second, err := s.Append(c)
	if err != nil {
		t.Fatalf("append again: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("re-appending the same correction produced a new ID: %s vs %s", first.ID, second.ID)
	}
	all, err := s.LoadAll()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected the duplicate to be deduplicated, got %d entries", len(all))
	}
}
