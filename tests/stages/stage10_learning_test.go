// Stage 10 (learning) tests: correction log, overlay, pattern feedback,
// conventions.
//
// Discrepancies vs the stage-10 spec: harness.SeedCorrections writes
// {"target":"cache",...} but Correction.TargetID is serialized as
// "target_id", so the harness line loads with an empty TargetID and has no
// effect on PatternFeedback/overlays — tests here record corrections through
// learning.Learner.Correct instead. Undo of REJECT/ACCEPT returns
// ErrNotUndoable (flags are not auto-reverted); Undo of a value-changing
// kind appends a compensating correction; unknown IDs return ErrNotFound.
// There is no "preferred label mapping" field in ProjectConventions; the
// closest is PreferredPatterns/RejectedPatterns plus the deterministic
// LearnConventions extraction.
package stages_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
	"github.com/Syamchand123/GlassMarble/internal/learning"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// s10helperBundle builds a one-item evidence bundle with a deterministic
// reference.
func s10helperBundle(ref string, ts time.Time) evidence.Bundle {
	return evidence.NewBundle(evidence.EvidenceItem{
		Source:     evidence.SourceGit,
		Reference:  ref,
		Excerpt:    "fixture",
		Confidence: 0.8,
		Timestamp:  ts,
	})
}

// s10helperMemory builds the fixture aggregate used across the overlay
// tests: one claim, one caching event.
func s10helperMemory(now time.Time) *developer_memory.DeveloperMemory {
	return &developer_memory.DeveloperMemory{
		ProjectID:   "proj_learn",
		LastUpdated: now,
		TotalEvents: 1,
		GlobalMemory: []developer_memory.KnowledgeClaim{
			{
				ID:            "claim_cache_0001",
				Subject:       "cache",
				Predicate:     "serves",
				Object:        "lookups",
				ClaimKind:     developer_memory.ClaimFact,
				State:         developer_memory.StateActive,
				ValidFrom:     now.Add(-24 * time.Hour),
				Evidence:      s10helperBundle("ref-claim", now),
				FreshnessScore: 1.0,
			},
		},
		Events: []archmodel.ArchEvent{
			{
				ID:         "evt_cache_0001",
				Kind:       archmodel.EventCachingAdded,
				Timestamp:  now.Add(-48 * time.Hour),
				Title:      "add cache layer",
				Components: []string{"cache"},
				Intent:     "original intent",
				Evidence:   s10helperBundle("ref-evt", now),
			},
		},
	}
}

func TestS10PatternFeedbackPreferredAndRejected(t *testing.T) {
	sb := harness.NewSandbox(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	store := developer_memory.NewStoreForRepo(sb.Root)

	evCache := archmodel.ArchEvent{
		ID: "evt_pattern_cache", Kind: archmodel.EventPatternDetected,
		Timestamp: now, Title: "cache pattern", Components: []string{"cache"},
		Evidence: s10helperBundle("rule.cache", now),
	}
	evAuth := archmodel.ArchEvent{
		ID: "evt_pattern_auth", Kind: archmodel.EventPatternDetected,
		Timestamp: now, Title: "auth pattern", Components: []string{"auth"},
		Evidence: s10helperBundle("rule.auth", now),
	}
	mem := &developer_memory.DeveloperMemory{Events: []archmodel.ArchEvent{evCache, evAuth}, TotalEvents: 2}
	if err := store.SaveMemory(mem); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	learner := learning.NewLearnerForRepo(sb.Root)
	if _, err := learner.Correct(learning.Correction{Kind: learning.CorrectionKindReject, TargetID: "evt_pattern_cache", Reason: "not a pattern"}, mem); err != nil {
		t.Fatalf("Correct reject: %v", err)
	}
	if _, err := learner.Correct(learning.Correction{Kind: learning.CorrectionKindAccept, TargetID: "evt_pattern_auth"}, mem); err != nil {
		t.Fatalf("Correct accept: %v", err)
	}

	preferred, rejected, err := learner.PatternFeedback(mem)
	if err != nil {
		t.Fatalf("PatternFeedback: %v", err)
	}
	if len(rejected) != 1 || rejected[0] != "cache" {
		t.Errorf("rejected = %v, want [cache]", rejected)
	}
	if len(preferred) != 1 || preferred[0] != "auth" {
		t.Errorf("preferred = %v, want [auth]", preferred)
	}

	// Nil memory: no error, no feedback.
	if pref, rej, err := learner.PatternFeedback(nil); err != nil || pref != nil || rej != nil {
		t.Errorf("PatternFeedback(nil) = %v/%v/%v, want nil/nil/nil", pref, rej, err)
	}
}

func TestS10CorrectValidationIsTableDriven(t *testing.T) {
	sb := harness.NewSandbox(t)
	learner := learning.NewLearnerForRepo(sb.Root)

	cases := []struct {
		name string
		corr learning.Correction
		wantErr bool
	}{
		{name: "empty target", corr: learning.Correction{Kind: learning.CorrectionKindReject}, wantErr: true},
		{name: "unknown kind", corr: learning.Correction{Kind: "NONSENSE", TargetID: "x"}, wantErr: true},
		{name: "state without value", corr: learning.Correction{Kind: learning.CorrectionKindState, TargetID: "x"}, wantErr: true},
		{name: "state invalid value", corr: learning.Correction{Kind: learning.CorrectionKindState, TargetID: "x", CorrectedValue: "SOMETIMES"}, wantErr: true},
		{name: "confidence out of range", corr: learning.Correction{Kind: learning.CorrectionKindConfidence, TargetID: "x", CorrectedValue: "1.5"}, wantErr: true},
		{name: "intent without value", corr: learning.Correction{Kind: learning.CorrectionKindIntent, TargetID: "x"}, wantErr: true},
		{name: "valid reject", corr: learning.Correction{Kind: learning.CorrectionKindReject, TargetID: "x"}},
		{name: "valid state", corr: learning.Correction{Kind: learning.CorrectionKindState, TargetID: "x", OriginalValue: "CURRENT", CorrectedValue: "DEPRECATED"}},
		{name: "valid confidence", corr: learning.Correction{Kind: learning.CorrectionKindConfidence, TargetID: "x", CorrectedValue: "0.4"}},
	}
	for _, tc := range cases {
		_, err := learner.Correct(tc.corr, nil)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: error = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}

	// The log exists on disk and carries the persisted JSON.
	logPath := sb.Path(".glassmarble", "memory", "corrections.jsonl")
	raw, err := readFileTestHelper(logPath)
	if err != nil {
		t.Fatalf("read corrections.jsonl: %v", err)
	}
	if !strings.Contains(raw, `"kind":"REJECT"`) || !strings.Contains(raw, `"kind":"STATE"`) {
		t.Errorf("corrections.jsonl missing persisted kinds:\n%s", raw)
	}

	all, err := learner.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("List = %d corrections, want 3 (only valid ones appended)", len(all))
	}
}

// readFileTestHelper reads a file without a harness (os-only helper kept
// local so the file needs no extra imports).
func readFileTestHelper(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func TestS10UndoSemantics(t *testing.T) {
	sb := harness.NewSandbox(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	learner := learning.NewLearnerForRepo(sb.Root)
	mem := s10helperMemory(now)

	// REJECT is a flag: not auto-revertible.
	rej, err := learner.Correct(learning.Correction{Kind: learning.CorrectionKindReject, TargetID: "evt_cache_0001"}, mem)
	if err != nil {
		t.Fatalf("Correct reject: %v", err)
	}
	if _, err := learner.Undo(rej.ID); err != learning.ErrNotUndoable {
		t.Errorf("Undo(REJECT) error = %v, want ErrNotUndoable", err)
	}

	// STATE with captured original value: Undo appends a compensating
	// correction restoring CURRENT.
	corr, err := learner.Correct(learning.Correction{Kind: learning.CorrectionKindState, TargetID: "claim_cache_0001", CorrectedValue: "DEPRECATED"}, mem)
	if err != nil {
		t.Fatalf("Correct state: %v", err)
	}
	if corr.OriginalValue != "CURRENT" {
		t.Errorf("original_value auto-captured = %q, want CURRENT", corr.OriginalValue)
	}
	undo, err := learner.Undo(corr.ID)
	if err != nil {
		t.Fatalf("Undo(state): %v", err)
	}
	if undo.CorrectedValue != "CURRENT" {
		t.Errorf("undo corrected_value = %q, want CURRENT", undo.CorrectedValue)
	}
	if !strings.Contains(undo.Reason, "undo") {
		t.Errorf("undo reason = %q, want it to mention undo", undo.Reason)
	}
	all, err := learner.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("List = %d, want 3 (reject + state + compensating undo)", len(all))
	}

	// Unknown ID: ErrNotFound, and a second Undo of the same id stays not
	// found (the log is append-only, nothing is removed).
	if _, err := learner.Undo("corr_no_such_id"); err != learning.ErrNotFound {
		t.Errorf("Undo(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestS10OverlayQueryAppliesCorrections(t *testing.T) {
	sb := harness.NewSandbox(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	learner := learning.NewLearnerForRepo(sb.Root)
	mem := s10helperMemory(now)

	if _, err := learner.Correct(learning.Correction{Kind: learning.CorrectionKindState, TargetID: "claim_cache_0001", CorrectedValue: "DEPRECATED"}, mem); err != nil {
		t.Fatalf("Correct state: %v", err)
	}
	if _, err := learner.Correct(learning.Correction{Kind: learning.CorrectionKindIntent, TargetID: "evt_cache_0001", CorrectedValue: "reduce db load"}, mem); err != nil {
		t.Fatalf("Correct intent: %v", err)
	}
	if _, err := learner.Correct(learning.Correction{Kind: learning.CorrectionKindConfidence, TargetID: "claim_cache_0001", CorrectedValue: "0.42"}, mem); err != nil {
		t.Fatalf("Correct confidence: %v", err)
	}

	res := developer_memory.QueryMemoryFromMemory(mem, "cache", 10)
	corrected, err := learner.OverlayQuery(res)
	if err != nil {
		t.Fatalf("OverlayQuery: %v", err)
	}
	if len(corrected.CorrectionsApplied) != 3 {
		t.Fatalf("corrections applied = %d, want 3", len(corrected.CorrectionsApplied))
	}
	if len(corrected.Claims) == 0 || corrected.Claims[0].State != developer_memory.StateDeprecated {
		t.Errorf("projected claim state = %v, want DEPRECATED", corrected.Claims)
	}
	var intentApplied bool
	for _, ev := range corrected.Events {
		if ev.ID == "evt_cache_0001" && ev.Intent == "reduce db load" {
			intentApplied = true
		}
	}
	if !intentApplied {
		t.Error("event intent was not overridden by the INTENT correction")
	}
	for _, a := range corrected.CorrectionsApplied {
		if !a.Applied {
			t.Errorf("audit entry not applied: %+v", a)
		}
	}
	// The overlay never mutates the source query result.
	if len(res.Claims) == 0 || res.Claims[0].State != developer_memory.StateActive {
		t.Error("source query result was mutated by the overlay")
	}
}

func TestS10OverlayQueryZeroCorrectionsNoPanic(t *testing.T) {
	sb := harness.NewSandbox(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	learner := learning.NewLearnerForRepo(sb.Root)
	mem := s10helperMemory(now)

	res := developer_memory.QueryMemoryFromMemory(mem, "cache", 10)
	corrected, err := learner.OverlayQuery(res)
	if err != nil {
		t.Fatalf("OverlayQuery: %v", err)
	}
	if corrected == nil || corrected.MemoryQueryResult == nil {
		t.Fatal("OverlayQuery returned nil projection")
	}
	if len(corrected.CorrectionsApplied) != 0 {
		t.Errorf("corrections applied = %d, want 0", len(corrected.CorrectionsApplied))
	}
	if len(corrected.Claims) != 1 {
		t.Errorf("claims = %d, want 1 (results pass through untouched)", len(corrected.Claims))
	}
}

func TestS10OverlayMemoryProjectionLeavesSourceUntouched(t *testing.T) {
	sb := harness.NewSandbox(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	learner := learning.NewLearnerForRepo(sb.Root)
	mem := s10helperMemory(now)

	if _, err := learner.Correct(learning.Correction{Kind: learning.CorrectionKindState, TargetID: "claim_cache_0001", OriginalValue: "CURRENT", CorrectedValue: "HISTORICAL"}, mem); err != nil {
		t.Fatalf("Correct: %v", err)
	}
	if _, err := learner.Correct(learning.Correction{Kind: learning.CorrectionKindIntent, TargetID: "evt_cache_0001", CorrectedValue: "rewritten intent"}, mem); err != nil {
		t.Fatalf("Correct intent: %v", err)
	}

	proj, applied, err := learner.OverlayMemory(mem)
	if err != nil {
		t.Fatalf("OverlayMemory: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied = %d, want 2 (STATE + INTENT)", len(applied))
	}
	if len(proj.GlobalMemory) == 0 || proj.GlobalMemory[0].State != developer_memory.StateHistorical {
		t.Errorf("projected claim state = %v, want HISTORICAL", proj.GlobalMemory)
	}
	if len(proj.Events) == 0 || proj.Events[0].Intent != "rewritten intent" {
		t.Errorf("projected event intent = %v, want rewritten intent", proj.Events)
	}
	// Source aggregate stays byte-identical in the temporal model.
	if mem.GlobalMemory[0].State != developer_memory.StateActive {
		t.Error("source memory claim was mutated")
	}
	if mem.Events[0].Intent != "original intent" {
		t.Error("source memory event was mutated")
	}
}

func TestS10ConventionsStoreSaveLoadRoundTrip(t *testing.T) {
	sb := harness.NewSandbox(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	store := learning.NewConventionsStore(sb.Root)
	if store.Path() != sb.Path(".glassmarble", "memory", "conventions.json") {
		t.Errorf("conventions path = %s", store.Path())
	}

	conv := &learning.ProjectConventions{
		ServiceNamingPattern: learning.Convention{Value: "*Service", Confidence: 0.8, Evidence: 4},
		LayerDirectories:     []learning.Convention{{Value: "domain", Confidence: 0.5, Evidence: 2}},
		TestFilePattern:      learning.Convention{Value: "*_test.go", Confidence: 1.0, Evidence: 3},
		ADRDirectory:         learning.Convention{Value: "docs/adr", Confidence: 1.0, Evidence: 2},
		PreferredPatterns:    []string{"layered"},
		RejectedPatterns:     []string{"microservices"},
		LearnedAt:            now,
	}
	if err := store.Save(conv); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("Load returned nil conventions")
	}
	if got.ServiceNamingPattern.Value != "*Service" || got.ServiceNamingPattern.Evidence != 4 {
		t.Errorf("service pattern = %+v, want *Service x4", got.ServiceNamingPattern)
	}
	if len(got.PreferredPatterns) != 1 || got.PreferredPatterns[0] != "layered" {
		t.Errorf("preferred patterns = %v, want [layered]", got.PreferredPatterns)
	}
	if len(got.RejectedPatterns) != 1 || got.RejectedPatterns[0] != "microservices" {
		t.Errorf("rejected patterns = %v, want [microservices]", got.RejectedPatterns)
	}
	if !got.LearnedAt.Equal(now) {
		t.Errorf("learned_at = %v, want %v", got.LearnedAt, now)
	}

	// Fresh store without a file: (nil, nil), not an error.
	fresh := learning.NewConventionsStore(harness.NewSandbox(t).Root)
	if got, err := fresh.Load(); err != nil || got != nil {
		t.Errorf("Load on fresh store = %v/%v, want nil/nil", got, err)
	}
	if err := store.Save(nil); err == nil {
		t.Error("Save(nil): expected error, got nil")
	}
}

func TestS10LearnConventionsFromGraphAndMemory(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	g := akg.NewCodePropertyGraph("conv")
	fileNode := func(file string) map[string]bool { return map[string]bool{file + "::n": true} }
	g.Nodes = g.Nodes.Set("internal/services/order.go::OrderService", &stage4.ResolvedNode{ID: "internal/services/order.go::OrderService", Kind: "STRUCT", Name: "OrderService", FileSpec: stage4.LocationMeta{Path: "internal/services/order.go"}})
	g.Nodes = g.Nodes.Set("internal/services/billing.go::BillingService", &stage4.ResolvedNode{ID: "internal/services/billing.go::BillingService", Kind: "STRUCT", Name: "BillingService", FileSpec: stage4.LocationMeta{Path: "internal/services/billing.go"}})
	g.FileNodeIndex = g.FileNodeIndex.Set("internal/services/order.go", fileNode("internal/services/order.go"))
	g.FileNodeIndex = g.FileNodeIndex.Set("internal/services/billing.go", fileNode("internal/services/billing.go"))
	g.FileNodeIndex = g.FileNodeIndex.Set("internal/services/order_test.go", fileNode("internal/services/order_test.go"))
	g.FileNodeIndex = g.FileNodeIndex.Set("internal/services/billing_test.go", fileNode("internal/services/billing_test.go"))

	mem := &developer_memory.DeveloperMemory{Events: []archmodel.ArchEvent{
		{ID: "ev_p1", Kind: archmodel.EventPatternDetected, Components: []string{"cache"}, Evidence: s10helperBundle("r1", now)},
		{ID: "ev_p2", Kind: archmodel.EventPatternDetected, Components: []string{"cache"}, Evidence: s10helperBundle("r2", now)},
	}}

	conv := learning.LearnConventions(g, mem,
		learning.WithMinEvidence(2),
		learning.WithPatternFeedback([]string{"layered"}, []string{"microservices"}),
		learning.WithLearnedAt(now),
	)
	if conv == nil {
		t.Fatal("LearnConventions returned nil")
	}
	if conv.ServiceNamingPattern.Value != "*Service" || conv.ServiceNamingPattern.Evidence != 2 {
		t.Errorf("service naming = %+v, want *Service x2", conv.ServiceNamingPattern)
	}
	if len(conv.LayerDirectories) != 1 || conv.LayerDirectories[0].Value != "services" {
		t.Errorf("layer directories = %+v, want [services]", conv.LayerDirectories)
	}
	if conv.TestFilePattern.Value != "*_test.go" || conv.TestFilePattern.Evidence != 2 {
		t.Errorf("test pattern = %+v, want *_test.go x2", conv.TestFilePattern)
	}
	if len(conv.PreferredPatterns) != 1 || conv.PreferredPatterns[0] != "layered" {
		t.Errorf("preferred = %v, want [layered]", conv.PreferredPatterns)
	}
	if len(conv.LearnedPatterns) != 1 || conv.LearnedPatterns[0].Value != "cache" || conv.LearnedPatterns[0].Evidence != 2 {
		t.Errorf("learned patterns = %+v, want cache x2", conv.LearnedPatterns)
	}

	// Nil graph: still returns the feedback conventions, no panic.
	conv = learning.LearnConventions(nil, mem, learning.WithPatternFeedback(nil, []string{"cache"}))
	if conv == nil || len(conv.RejectedPatterns) != 1 || conv.RejectedPatterns[0] != "cache" {
		t.Errorf("LearnConventions(nil graph) = %+v, want rejected [cache]", conv)
	}
}