package learning

import (
	"errors"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

func TestLearnerCorrectAutoFillsOriginalValue(t *testing.T) {
	repoDir := t.TempDir()
	learner := NewLearnerForRepo(repoDir)
	mem := &developer_memory.DeveloperMemory{
		Events: []archmodel.ArchEvent{
			testEvent("event-1", "Pattern Detected: CLEAN_ARCHITECTURE", "unknown", 0.85),
		},
	}

	c, err := learner.Correct(Correction{
		Kind:           CorrectionKindIntent,
		TargetID:       "event-1",
		CorrectedValue: "decided in ADR-014",
	}, mem)
	if err != nil {
		t.Fatalf("correct: %v", err)
	}
	if c.OriginalValue != "unknown" {
		t.Errorf("original value not auto-captured: %q", c.OriginalValue)
	}

	// A second run of the same correction must not duplicate the log.
	if _, err := learner.Correct(Correction{
		Kind:           CorrectionKindIntent,
		TargetID:       "event-1",
		OriginalValue:  "unknown",
		CorrectedValue: "decided in ADR-014",
	}, mem); err != nil {
		t.Fatalf("correct again: %v", err)
	}
	all, err := learner.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 entries (timestamps differ → distinct IDs), got %d", len(all))
	}
}

func TestLearnerUndo(t *testing.T) {
	repoDir := t.TempDir()
	learner := NewLearnerForRepo(repoDir)

	c, err := learner.Correct(Correction{
		Kind:           CorrectionKindState,
		TargetID:       "comp-x",
		OriginalValue:  "CURRENT",
		CorrectedValue: "DEPRECATED",
		Reason:         "marked by team",
	}, nil)
	if err != nil {
		t.Fatalf("correct: %v", err)
	}

	undo, err := learner.Undo(c.ID)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if undo.TargetID != "comp-x" || undo.CorrectedValue != "CURRENT" {
		t.Errorf("compensating correction wrong: %+v", undo)
	}
	if undo.Reason != "undo of "+c.ID {
		t.Errorf("undo reason not recorded: %q", undo.Reason)
	}

	// The original correction must still be in the log (append-only).
	all, err := learner.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected original + compensating correction, got %d", len(all))
	}
}

func TestLearnerUndoErrors(t *testing.T) {
	repoDir := t.TempDir()
	learner := NewLearnerForRepo(repoDir)

	if _, err := learner.Undo("corr_does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing correction: got %v, want ErrNotFound", err)
	}

	reject, err := learner.Correct(Correction{Kind: CorrectionKindReject, TargetID: "c1"}, nil)
	if err != nil {
		t.Fatalf("correct: %v", err)
	}
	if _, err := learner.Undo(reject.ID); !errors.Is(err, ErrNotUndoable) {
		t.Errorf("reject undo: got %v, want ErrNotUndoable", err)
	}

	// An INTENT correction without an original value cannot be undone
	// safely — there is nothing to restore.
	noOriginal, err := learner.Correct(Correction{
		Kind: CorrectionKindLabel, TargetID: "e1", CorrectedValue: "New name",
	}, nil)
	if err != nil {
		t.Fatalf("correct: %v", err)
	}
	if _, err := learner.Undo(noOriginal.ID); err == nil {
		t.Error("undo without original value must fail")
	}
}

func TestLearnerPatternFeedback(t *testing.T) {
	repoDir := t.TempDir()
	learner := NewLearnerForRepo(repoDir)
	mem := &developer_memory.DeveloperMemory{
		Events: []archmodel.ArchEvent{
			{ID: "ev-pattern-a", Kind: archmodel.EventPatternDetected, Title: "Pattern Detected: CLEAN_ARCHITECTURE", Components: []string{"CLEAN_ARCHITECTURE"}},
			{ID: "ev-pattern-b", Kind: archmodel.EventPatternDetected, Title: "Pattern Detected: CQRS", Components: []string{"CQRS"}},
			{ID: "ev-other", Kind: archmodel.EventDependencyAdded, Title: "DEPENDENCY_ADDED: a <-> b", Components: []string{"a", "b"}},
		},
	}
	if _, err := learner.Correct(Correction{Kind: CorrectionKindAccept, TargetID: "ev-pattern-a"}, mem); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, err := learner.Correct(Correction{Kind: CorrectionKindReject, TargetID: "ev-pattern-b"}, mem); err != nil {
		t.Fatalf("reject: %v", err)
	}
	// Corrections targeting non-pattern events must be ignored.
	if _, err := learner.Correct(Correction{Kind: CorrectionKindReject, TargetID: "ev-other"}, mem); err != nil {
		t.Fatalf("reject other: %v", err)
	}

	preferred, rejected, err := learner.PatternFeedback(mem)
	if err != nil {
		t.Fatalf("pattern feedback: %v", err)
	}
	if len(preferred) != 1 || preferred[0] != "CLEAN_ARCHITECTURE" {
		t.Errorf("preferred = %v", preferred)
	}
	if len(rejected) != 1 || rejected[0] != "CQRS" {
		t.Errorf("rejected = %v", rejected)
	}
}

func TestLearnerOverlayQueryHonorsConfig(t *testing.T) {
	repoDir := t.TempDir()
	store := NewStore(repoDir)
	if _, err := store.Append(Correction{Kind: CorrectionKindState, TargetID: "claim-1", CorrectedValue: "DEPRECATED"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	res := &developer_memory.MemoryQueryResult{
		Claims: []developer_memory.KnowledgeClaim{testClaim("claim-1", "X", "CURRENT", 0.9)},
	}

	// Default config applies corrections.
	on := NewLearner(store)
	proj, err := on.OverlayQuery(res)
	if err != nil {
		t.Fatalf("overlay on: %v", err)
	}
	if proj.Claims[0].State != developer_memory.StateDeprecated {
		t.Errorf("state = %s, want DEPRECATED", proj.Claims[0].State)
	}

	// apply_on_query: false disables the overlay but keeps the log.
	applyOff := false
	off := NewLearner(store, WithConfig(&config.LearningConfig{ApplyOnQuery: &applyOff}))
	proj, err = off.OverlayQuery(res)
	if err != nil {
		t.Fatalf("overlay off: %v", err)
	}
	if proj.Claims[0].State != developer_memory.StateActive {
		t.Errorf("state = %s, want untouched CURRENT", proj.Claims[0].State)
	}
	if proj.CorrectionsApplied != nil {
		t.Error("no audit entries expected when overlay is off")
	}
}

func TestLearnerQueryWrapsDeveloperMemoryQuery(t *testing.T) {
	repoDir := t.TempDir()
	// Build a real memory store with a claim so the integration path is
	// exercised end to end.
	memStore := developer_memory.NewStoreForRepo(repoDir)
	if err := memStore.AppendClaim(developer_memory.KnowledgeClaim{
		ID:        "claim-1",
		Subject:   "PaymentService",
		Predicate: "uses_technology",
		Object:    "redis",
		ClaimKind: developer_memory.ClaimFact,
		State:     developer_memory.StateActive,
		ValidFrom: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		Evidence: evidence.NewBundle(evidence.EvidenceItem{
			Source: evidence.SourceCode, Reference: "svc.go", Confidence: 0.9,
		}),
	}); err != nil {
		t.Fatalf("append claim: %v", err)
	}
	if err := memStore.SaveMemory(mustRebuild(t, memStore)); err != nil {
		t.Fatalf("save memory: %v", err)
	}

	learner := NewLearnerForRepo(repoDir)
	if _, err := learner.Correct(Correction{
		Kind: CorrectionKindState, TargetID: "claim-1",
		OriginalValue: "CURRENT", CorrectedValue: "EXPERIMENTAL",
	}, nil); err != nil {
		t.Fatalf("correct: %v", err)
	}

	proj, err := learner.Query(memStore, "PaymentService redis")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(proj.Claims) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(proj.Claims))
	}
	if proj.Claims[0].State != developer_memory.StateExperimental {
		t.Errorf("state = %s, want the corrected EXPERIMENTAL", proj.Claims[0].State)
	}
	if len(proj.CorrectionsApplied) != 1 || !proj.CorrectionsApplied[0].Applied {
		t.Errorf("audit entry missing: %+v", proj.CorrectionsApplied)
	}
}

func mustRebuild(t *testing.T, store *developer_memory.MemoryStore) *developer_memory.DeveloperMemory {
	t.Helper()
	mem, err := store.Rebuild()
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	return mem
}
