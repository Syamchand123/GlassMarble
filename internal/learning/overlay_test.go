package learning

import (
	"reflect"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

func testClaim(id, subject, state string, conf float64) developer_memory.KnowledgeClaim {
	return developer_memory.KnowledgeClaim{
		ID:        id,
		Subject:   subject,
		Predicate: "uses_technology",
		Object:    "redis",
		ClaimKind: developer_memory.ClaimFact,
		State:     developer_memory.KnowledgeState(state),
		Evidence:  evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceCode, Reference: "x.go", Confidence: conf}),
	}
}

func testEvent(id, title, intent string, conf float64) archmodel.ArchEvent {
	return archmodel.ArchEvent{
		ID:         id,
		Kind:       archmodel.EventPatternDetected,
		Title:      title,
		Intent:     intent,
		Components: []string{"CLEAN_ARCHITECTURE"},
		Evidence:   evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Reference: "abc123", Confidence: conf}),
	}
}

func testResult() *developer_memory.MemoryQueryResult {
	return &developer_memory.MemoryQueryResult{
		Query: "redis",
		Claims: []developer_memory.KnowledgeClaim{
			testClaim("claim-1", "PaymentService", "CURRENT", 0.95),
			testClaim("claim-2", "AuthService", "CURRENT", 0.9),
		},
		Events: []archmodel.ArchEvent{
			testEvent("event-1", "Pattern Detected: CLEAN_ARCHITECTURE", "unknown", 0.85),
		},
		Components: []developer_memory.ComponentHistory{
			{Name: "PaymentService", State: developer_memory.StateActive},
		},
	}
}

func TestApplyLeavesSourceUntouched(t *testing.T) {
	orig := testResult()
	snapshot := cloneQueryResult(orig)

	corrections := []Correction{
		{Kind: CorrectionKindState, TargetID: "claim-1", CorrectedValue: "DEPRECATED"},
		{Kind: CorrectionKindIntent, TargetID: "event-1", CorrectedValue: "real reason"},
		{Kind: CorrectionKindLabel, TargetID: "PaymentService", CorrectedValue: "Payments"},
	}
	proj := Apply(orig, corrections)

	// The projection reflects the corrections...
	if proj.Claims[0].State != developer_memory.StateDeprecated {
		t.Errorf("projected claim state = %s, want DEPRECATED", proj.Claims[0].State)
	}
	if proj.Events[0].Intent != "real reason" {
		t.Errorf("projected event intent = %q, want the corrected one", proj.Events[0].Intent)
	}
	if proj.Components[0].Name != "Payments" {
		t.Errorf("projected component name = %q, want Payments", proj.Components[0].Name)
	}

	// ...but the source result is byte-identical.
	if orig.Claims[0].State != developer_memory.StateActive {
		t.Errorf("source claim state mutated: %s", orig.Claims[0].State)
	}
	if orig.Events[0].Intent != "unknown" {
		t.Errorf("source event intent mutated: %q", orig.Events[0].Intent)
	}
	if orig.Components[0].Name != "PaymentService" {
		t.Errorf("source component name mutated: %q", orig.Components[0].Name)
	}
	if !sameResult(orig, snapshot) {
		t.Errorf("source result changed in unexpected ways")
	}
}

func TestApplyRejectFlagsWithoutTouchingState(t *testing.T) {
	orig := testResult()
	corrections := []Correction{{Kind: CorrectionKindReject, TargetID: "claim-2"}}
	proj := Apply(orig, corrections)

	// D3 regression: rejection must NOT rewrite the temporal state.
	if proj.Claims[1].State != developer_memory.StateActive {
		t.Errorf("REJECT rewrote temporal state to %s — must stay CURRENT", proj.Claims[1].State)
	}
	entry := proj.CorrectionsApplied[0]
	if !entry.Applied || entry.After != "rejected" {
		t.Errorf("rejection not recorded as flag: %+v", entry)
	}
	if entry.TargetType != TargetClaim {
		t.Errorf("target type = %s, want claim", entry.TargetType)
	}
}

func TestApplyConfidenceOverrides(t *testing.T) {
	orig := testResult()
	corrections := []Correction{{Kind: CorrectionKindConfidence, TargetID: "claim-1", CorrectedValue: "0.30"}}
	proj := Apply(orig, corrections)

	if got := proj.Claims[0].Evidence.AggConfidence; got != 0.30 {
		t.Errorf("projected confidence = %f, want 0.3", got)
	}
	entry := proj.CorrectionsApplied[0]
	if entry.Before != "0.950" || entry.After != "0.300" {
		t.Errorf("audit before/after = %q/%q, want 0.950/0.300", entry.Before, entry.After)
	}
}

func TestApplyLastCorrectionWins(t *testing.T) {
	orig := testResult()
	corrections := []Correction{
		{Kind: CorrectionKindState, TargetID: "claim-1", CorrectedValue: "DEPRECATED",
			Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Kind: CorrectionKindState, TargetID: "claim-1", CorrectedValue: "HISTORICAL",
			Timestamp: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
	}
	proj := Apply(orig, corrections)
	if proj.Claims[0].State != developer_memory.StateHistorical {
		t.Errorf("state = %s, want the later HISTORICAL", proj.Claims[0].State)
	}
	if len(proj.CorrectionsApplied) != 2 {
		t.Fatalf("expected 2 audit entries, got %d", len(proj.CorrectionsApplied))
	}
	if proj.CorrectionsApplied[1].Before != "DEPRECATED" {
		t.Errorf("second audit Before = %q, want the value displayed before it (DEPRECATED)",
			proj.CorrectionsApplied[1].Before)
	}
}

func TestApplyKindNotApplicable(t *testing.T) {
	orig := testResult()
	corrections := []Correction{
		{Kind: CorrectionKindIntent, TargetID: "claim-1", CorrectedValue: "nope"},           // claims have no intent
		{Kind: CorrectionKindState, TargetID: "event-1", CorrectedValue: "DEPRECATED"},      // events have no state
		{Kind: CorrectionKindConfidence, TargetID: "PaymentService", CorrectedValue: "0.5"}, // components have no confidence
	}
	proj := Apply(orig, corrections)
	for i, entry := range proj.CorrectionsApplied {
		if entry.Applied {
			t.Errorf("correction %d should be not-applicable but applied: %+v", i, entry)
		}
		if entry.Note == "" {
			t.Errorf("correction %d has no note", i)
		}
	}
}

func TestApplyTargetNotFound(t *testing.T) {
	orig := testResult()
	proj := Apply(orig, []Correction{{Kind: CorrectionKindReject, TargetID: "ghost-claim"}})
	entry := proj.CorrectionsApplied[0]
	if entry.Applied {
		t.Fatal("ghost target must not be applied")
	}
	if entry.Note != "target not found in results" {
		t.Errorf("note = %q", entry.Note)
	}
}

func TestApplyEmptyResultAndNil(t *testing.T) {
	proj := Apply(nil, []Correction{{Kind: CorrectionKindReject, TargetID: "x"}})
	if proj == nil || proj.MemoryQueryResult == nil {
		t.Fatal("Apply(nil, ...) must return a usable projection")
	}
	if len(proj.CorrectionsApplied) != 1 || proj.CorrectionsApplied[0].Applied {
		t.Fatal("correction on nil result must be recorded as not applied")
	}

	empty := Apply(&developer_memory.MemoryQueryResult{Query: "q"}, nil)
	if empty.CorrectionsApplied != nil || empty.Claims != nil {
		t.Fatal("no corrections → no audit entries")
	}
}

func TestApplyToMemoryProjectsAggregate(t *testing.T) {
	mem := &developer_memory.DeveloperMemory{
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"PaymentService": {Name: "PaymentService", State: developer_memory.StateActive},
		},
		GlobalMemory: []developer_memory.KnowledgeClaim{
			testClaim("claim-1", "PaymentService", "CURRENT", 0.95),
		},
		Events: []archmodel.ArchEvent{
			testEvent("event-1", "Pattern Detected: CLEAN_ARCHITECTURE", "unknown", 0.85),
		},
	}
	corrections := []Correction{
		{Kind: CorrectionKindState, TargetID: "PaymentService", OriginalValue: "CURRENT", CorrectedValue: "DEPRECATED"},
		{Kind: CorrectionKindLabel, TargetID: "claim-1", OriginalValue: "PaymentService", CorrectedValue: "Payment Service"},
	}
	proj, applied := ApplyToMemory(mem, corrections)

	if proj.ComponentMemory["PaymentService"].State != developer_memory.StateDeprecated {
		t.Errorf("projected component state = %s, want DEPRECATED", proj.ComponentMemory["PaymentService"].State)
	}
	if proj.GlobalMemory[0].Subject != "Payment Service" {
		t.Errorf("projected claim subject = %q", proj.GlobalMemory[0].Subject)
	}
	if len(applied) != 2 {
		t.Fatalf("expected 2 audit entries, got %d", len(applied))
	}
	// Source aggregate untouched.
	if mem.ComponentMemory["PaymentService"].State != developer_memory.StateActive {
		t.Errorf("source component state mutated: %s", mem.ComponentMemory["PaymentService"].State)
	}
	if mem.GlobalMemory[0].Subject != "PaymentService" {
		t.Errorf("source claim subject mutated: %q", mem.GlobalMemory[0].Subject)
	}
}

func TestApplyToMemoryComponentByName(t *testing.T) {
	mem := &developer_memory.DeveloperMemory{
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"RedisCache": {Name: "RedisCache", State: developer_memory.StateActive},
		},
	}
	proj, applied := ApplyToMemory(mem, []Correction{
		{Kind: CorrectionKindLabel, TargetID: "RedisCache", CorrectedValue: "Redis"},
	})
	h := proj.ComponentMemory["RedisCache"]
	if h.Name != "Redis" {
		t.Errorf("projected name = %q, want Redis", h.Name)
	}
	if len(applied) != 1 || applied[0].TargetType != TargetComponent {
		t.Fatalf("audit entry missing: %+v", applied)
	}
}

func sameResult(a, b *developer_memory.MemoryQueryResult) bool {
	if a.Query != b.Query || len(a.Claims) != len(b.Claims) ||
		len(a.Events) != len(b.Events) || len(a.Components) != len(b.Components) {
		return false
	}
	for i := range a.Claims {
		if !reflect.DeepEqual(a.Claims[i], b.Claims[i]) {
			return false
		}
	}
	for i := range a.Events {
		if !reflect.DeepEqual(a.Events[i], b.Events[i]) {
			return false
		}
	}
	for i := range a.Components {
		if !reflect.DeepEqual(a.Components[i], b.Components[i]) {
			return false
		}
	}
	return true
}
