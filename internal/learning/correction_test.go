package learning

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

func TestStoreAppendAndLoadRoundTrip(t *testing.T) {
	repoDir := t.TempDir()
	store := NewStore(repoDir)

	c1 := Correction{
		Kind:          CorrectionKindReject,
		TargetID:      "claim-1",
		OriginalValue: "shown",
		Reason:        "Wrong claim",
		Author:        "alice",
	}
	c2 := Correction{
		Kind:           CorrectionKindIntent,
		TargetID:       "event-1",
		OriginalValue:  "Unknown",
		CorrectedValue: "Fixed cache bug",
		Reason:         "Added better intent",
	}

	got1, err := store.Append(c1)
	if err != nil {
		t.Fatalf("append c1: %v", err)
	}
	got2, err := store.Append(c2)
	if err != nil {
		t.Fatalf("append c2: %v", err)
	}
	if got1.ID == "" || got2.ID == "" {
		t.Fatal("append must assign deterministic IDs")
	}
	if got1.Timestamp.IsZero() || got2.Timestamp.IsZero() {
		t.Fatal("append must default the timestamp to now")
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 corrections, got %d", len(all))
	}
	if all[0].ID != got1.ID || all[1].ID != got2.ID {
		t.Errorf("loaded corrections are not in append order")
	}
	if all[1].CorrectedValue != "Fixed cache bug" {
		t.Errorf("corrected value lost: %q", all[1].CorrectedValue)
	}

	// The file must live at the master-plan location (§8.2).
	if _, err := os.Stat(filepath.Join(repoDir, ".glassmarble", "memory", "corrections.jsonl")); err != nil {
		t.Errorf("corrections log not at .glassmarble/memory/corrections.jsonl: %v", err)
	}
}

func TestStoreAppendIsIdempotentByContent(t *testing.T) {
	repoDir := t.TempDir()
	store := NewStore(repoDir)
	ts := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)

	c := Correction{
		Timestamp:      ts,
		Kind:           CorrectionKindState,
		TargetID:       "comp-x",
		OriginalValue:  "CURRENT",
		CorrectedValue: "DEPRECATED",
	}
	for i := 0; i < 3; i++ {
		if _, err := store.Append(c); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected duplicate appends to collapse to 1, got %d", len(all))
	}
}

func TestCorrectionValidation(t *testing.T) {
	tests := []struct {
		name    string
		corr    Correction
		wantErr string
	}{
		{name: "valid intent", corr: Correction{Kind: CorrectionKindIntent, TargetID: "e1", CorrectedValue: "because"}},
		{name: "valid label", corr: Correction{Kind: CorrectionKindLabel, TargetID: "c1", CorrectedValue: "Payment Service"}},
		{name: "valid state", corr: Correction{Kind: CorrectionKindState, TargetID: "c1", OriginalValue: "CURRENT", CorrectedValue: "DEPRECATED"}},
		{name: "valid confidence", corr: Correction{Kind: CorrectionKindConfidence, TargetID: "c1", CorrectedValue: "0.4"}},
		{name: "valid reject without value", corr: Correction{Kind: CorrectionKindReject, TargetID: "c1"}},
		{name: "valid accept without value", corr: Correction{Kind: CorrectionKindAccept, TargetID: "c1"}},

		{name: "unknown kind", corr: Correction{Kind: "NONSENSE", TargetID: "c1"}, wantErr: "unknown correction kind"},
		{name: "empty target", corr: Correction{Kind: CorrectionKindReject}, wantErr: "empty target_id"},
		{name: "intent without value", corr: Correction{Kind: CorrectionKindIntent, TargetID: "e1"}, wantErr: "needs a non-empty corrected value"},
		{name: "state without value", corr: Correction{Kind: CorrectionKindState, TargetID: "c1"}, wantErr: "needs a non-empty corrected value"},
		{name: "state with invalid value", corr: Correction{Kind: CorrectionKindState, TargetID: "c1", CorrectedValue: "SOMETIMES"}, wantErr: "not a valid knowledge state"},
		{name: "state with lowercase value", corr: Correction{Kind: CorrectionKindState, TargetID: "c1", CorrectedValue: "deprecated"}, wantErr: "not a valid knowledge state"},
		{name: "confidence non-numeric", corr: Correction{Kind: CorrectionKindConfidence, TargetID: "c1", CorrectedValue: "high"}, wantErr: "not a confidence"},
		{name: "confidence out of range", corr: Correction{Kind: CorrectionKindConfidence, TargetID: "c1", CorrectedValue: "1.5"}, wantErr: "not a confidence"},
		{name: "confidence negative", corr: Correction{Kind: CorrectionKindConfidence, TargetID: "c1", CorrectedValue: "-0.1"}, wantErr: "not a confidence"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.corr.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErr, err)
			}
		})
	}
}

func TestStoreAppendRejectsInvalid(t *testing.T) {
	repoDir := t.TempDir()
	store := NewStore(repoDir)
	if _, err := store.Append(Correction{Kind: "BOGUS", TargetID: "x"}); err == nil {
		t.Fatal("expected invalid correction to be rejected before any write")
	}
	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("invalid correction must not be persisted, got %d", len(all))
	}
}

func TestStoreLoadAllSkipsCorruptLines(t *testing.T) {
	repoDir := t.TempDir()
	store := NewStore(repoDir)

	if _, err := store.Append(Correction{Kind: CorrectionKindReject, TargetID: "c1"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, ".glassmarble", "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(store.Path(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("this is not json\n")
	f.Close()

	var warnings []string
	store.WithLogger(func(format string, args ...any) {
		warnings = append(warnings, format)
	})

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected the valid correction to survive the corrupt line, got %d", len(all))
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning for the corrupt line")
	}
}

func TestLoadForTargetsFilters(t *testing.T) {
	repoDir := t.TempDir()
	store := NewStore(repoDir)
	for _, c := range []Correction{
		{Kind: CorrectionKindReject, TargetID: "claim-1"},
		{Kind: CorrectionKindIntent, TargetID: "event-1", CorrectedValue: "why"},
		{Kind: CorrectionKindAccept, TargetID: "claim-2"},
	} {
		if _, err := store.Append(c); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	got, err := store.LoadForTargets("claim-1", "claim-2")
	if err != nil {
		t.Fatalf("load for targets: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 corrections for targets, got %d", len(got))
	}
}

func TestSortedCorrectionsOrdersByTimestamp(t *testing.T) {
	older := Correction{ID: "a", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	newer := Correction{ID: "b", Timestamp: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)}
	same := Correction{ID: "c", Timestamp: older.Timestamp} // tie → keeps log order

	// Log order: b, c, a. After sorting: a and c tie on time and keep log
	// order (c before a), then b.
	out := SortedCorrections([]Correction{newer, same, older})
	if out[0].ID != "c" || out[1].ID != "a" || out[2].ID != "b" {
		t.Fatalf("wrong order: %v", []string{out[0].ID, out[1].ID, out[2].ID})
	}
}

func TestValidKnowledgeStatesCoverAllConstants(t *testing.T) {
	// Every persisted knowledge state must be correctable via STATE —
	// the validation list and the memory model must never drift.
	for _, s := range []developer_memory.KnowledgeState{
		developer_memory.StateActive,
		developer_memory.StateDeprecated,
		developer_memory.StateRemoved,
		developer_memory.StateHistorical,
		developer_memory.StateExperimental,
		developer_memory.StateUnknown,
	} {
		if !validKnowledgeState(string(s)) {
			t.Errorf("state %q should be valid but is not", s)
		}
	}
}
