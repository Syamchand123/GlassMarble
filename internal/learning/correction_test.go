package learning

import (
	"os"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

func TestStoreAppendAndLoad(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "correction_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store := NewStore(tempDir)

	c1 := Correction{
		ID:             "corr-1",
		Kind:           CorrectionKindReject,
		TargetID:       "claim-1",
		OriginalValue:  "ACTIVE",
		CorrectedValue: "REMOVED",
		Reason:         "Wrong claim",
	}

	c2 := Correction{
		ID:             "corr-2",
		Kind:           CorrectionKindIntent,
		TargetID:       "event-1",
		OriginalValue:  "Unknown",
		CorrectedValue: "Fixed cache bug",
		Reason:         "Added better intent",
	}

	if err := store.Append(c1); err != nil {
		t.Fatalf("Failed to append c1: %v", err)
	}
	if err := store.Append(c2); err != nil {
		t.Fatalf("Failed to append c2: %v", err)
	}

	corrections, err := store.LoadAll()
	if err != nil {
		t.Fatalf("Failed to load corrections: %v", err)
	}

	if len(corrections) != 2 {
		t.Fatalf("Expected 2 corrections, got %d", len(corrections))
	}

	if corrections[0].ID != "corr-1" || corrections[1].ID != "corr-2" {
		t.Errorf("Corrections loaded out of order or invalid")
	}
}

func TestApplyCorrections(t *testing.T) {
	result := &MemoryQueryResult{
		Claims: []developer_memory.KnowledgeClaim{
			{ID: "claim-1", State: developer_memory.StateActive},
			{ID: "claim-2", State: developer_memory.StateActive},
		},
		Events: []archmodel.ArchEvent{
			{ID: "ev-1", Intent: "Unknown", Title: "Event 1"},
		},
	}

	corrections := []Correction{
		{Kind: CorrectionKindReject, TargetID: "claim-1"},
		{Kind: CorrectionKindState, TargetID: "claim-2", CorrectedValue: "DEPRECATED"},
		{Kind: CorrectionKindIntent, TargetID: "ev-1", CorrectedValue: "Fix memory leak"},
	}

	res := ApplyCorrections(result, corrections)

	// Check claims
	if res.Claims[0].State != developer_memory.StateRemoved {
		t.Errorf("Expected claim-1 to be REMOVED, got %s", res.Claims[0].State)
	}
	if res.Claims[1].State != developer_memory.StateDeprecated {
		t.Errorf("Expected claim-2 to be DEPRECATED, got %s", res.Claims[1].State)
	}

	// Check events
	if res.Events[0].Intent != "Fix memory leak" {
		t.Errorf("Expected ev-1 intent to be 'Fix memory leak', got %s", res.Events[0].Intent)
	}
	if res.Events[0].Title != "Event 1 (Corrected)" {
		t.Errorf("Expected ev-1 title to be modified, got %s", res.Events[0].Title)
	}
}
