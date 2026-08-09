package developer_memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

var baseTime = time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)

// testEvent builds a valid event with evidence, deterministic ID and
// timestamp. Zero values are overridden per-test where the intent is to
// exercise validation failures.
func testEvent(id string, kind archmodel.EventKind, ts time.Time, components []string) archmodel.ArchEvent {
	b := evidence.NewBundle(evidence.EvidenceItem{
		Source:     evidence.SourceGit,
		Reference:  "commit-" + id,
		Confidence: 0.9,
		Timestamp:  ts,
	})
	return archmodel.ArchEvent{
		ID:         id,
		Kind:       kind,
		CommitHash: "commit-" + id,
		Timestamp:  ts,
		Title:      "change " + id,
		Components: components,
		Evidence:   b,
	}
}

func newTestStore(t *testing.T) *MemoryStore {
	t.Helper()
	dir, err := os.MkdirTemp("", "developer_memory_test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return NewMemoryStore(dir)
}

func countWALLines(t *testing.T, store *MemoryStore, filename string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(store.Dir(), filename))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read %s: %v", filename, err)
	}
	n := 0
	for _, line := range splitLines(string(data)) {
		if trimSpace(line) != "" {
			n++
		}
	}
	return n
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

func TestProcessEvents_AppendsAndRebuilds(t *testing.T) {
	store := newTestStore(t)
	builder := NewMemoryBuilder(store)

	events := []archmodel.ArchEvent{
		testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"PaymentService"}),
		testEvent("e2", archmodel.EventPatternDetected, baseTime.Add(time.Hour), []string{"PaymentService", "OrderService"}),
	}
	appended, err := builder.ProcessEvents(events)
	if err != nil {
		t.Fatalf("ProcessEvents: %v", err)
	}
	if appended != 2 {
		t.Errorf("appended = %d, want 2", appended)
	}

	mem, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if mem.TotalEvents != 2 {
		t.Errorf("TotalEvents = %d, want 2", mem.TotalEvents)
	}
	if len(mem.Timeline) != 2 {
		t.Errorf("timeline len = %d, want 2", len(mem.Timeline))
	}
	if len(mem.Events) != 2 {
		t.Errorf("aggregate events len = %d, want 2", len(mem.Events))
	}
	comp, ok := mem.ComponentMemory["PaymentService"]
	if !ok {
		t.Fatalf("PaymentService missing from component memory")
	}
	if comp.State != StateActive {
		t.Errorf("PaymentService state = %s, want CURRENT", comp.State)
	}
	if len(comp.Events) != 2 {
		t.Errorf("PaymentService event ids = %v, want 2 events", comp.Events)
	}
	if countWALLines(t, store, eventsFile) != 2 {
		t.Errorf("events.jsonl should have 2 lines")
	}
}

func TestProcessEvents_Idempotent(t *testing.T) {
	store := newTestStore(t)
	builder := NewMemoryBuilder(store)

	events := []archmodel.ArchEvent{
		testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"PaymentService"}),
		testEvent("e2", archmodel.EventServiceRemoved, baseTime.Add(time.Hour), []string{"PaymentService"}),
	}
	if _, err := builder.ProcessEvents(events); err != nil {
		t.Fatalf("first ProcessEvents: %v", err)
	}
	if _, err := builder.ProcessEvents(events); err != nil {
		t.Fatalf("second ProcessEvents: %v", err)
	}

	mem, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if mem.TotalEvents != 2 {
		t.Errorf("TotalEvents = %d, want 2 (re-processing must not duplicate)", mem.TotalEvents)
	}
	if len(mem.Timeline) != 2 {
		t.Errorf("timeline len = %d, want 2", len(mem.Timeline))
	}
	if len(mem.Events) != 2 {
		t.Errorf("aggregate events len = %d, want 2", len(mem.Events))
	}
	if countWALLines(t, store, eventsFile) != 2 {
		t.Errorf("events.jsonl should still have 2 lines")
	}

	history := mem.ComponentMemory["PaymentService"]
	if len(history.Events) != 2 {
		t.Errorf("history events = %v, want exactly [e1 e2]", history.Events)
	}
}

func TestProcessEvents_ValidationFailures(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*archmodel.ArchEvent)
		wantErr string
	}{
		{
			name:    "empty evidence is a bug",
			mutate:  func(e *archmodel.ArchEvent) { e.Evidence = evidence.Bundle{} },
			wantErr: "empty evidence",
		},
		{
			name:    "zero timestamp",
			mutate:  func(e *archmodel.ArchEvent) { e.Timestamp = time.Time{} },
			wantErr: "zero timestamp",
		},
		{
			name:    "empty id",
			mutate:  func(e *archmodel.ArchEvent) { e.ID = "" },
			wantErr: "empty ID",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			builder := NewMemoryBuilder(store)

			ev := testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"PaymentService"})
			tt.mutate(&ev)

			if _, err := builder.ProcessEvents([]archmodel.ArchEvent{ev}); err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			// Nothing may be written when validation fails — the batch must
			// be all-or-nothing.
			if n := countWALLines(t, store, eventsFile); n != 0 {
				t.Errorf("events.jsonl has %d lines after rejected batch, want 0", n)
			}
		})
	}
}

func TestProcessEvents_AllOrNothingBatch(t *testing.T) {
	store := newTestStore(t)
	builder := NewMemoryBuilder(store)

	good := testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"PaymentService"})
	bad := testEvent("e2", archmodel.EventServiceRemoved, baseTime.Add(time.Hour), []string{"PaymentService"})
	bad.Evidence = evidence.Bundle{}

	_, err := builder.ProcessEvents([]archmodel.ArchEvent{good, bad})
	if err == nil {
		t.Fatalf("expected validation error for bad event, got nil")
	}
	mem, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if mem.TotalEvents != 0 {
		t.Errorf("TotalEvents = %d, want 0 (nothing may be applied from a rejected batch)", mem.TotalEvents)
	}
}

func TestProcessEvents_ComponentLifecycle(t *testing.T) {
	store := newTestStore(t)
	builder := NewMemoryBuilder(store)

	t1 := baseTime
	t2 := baseTime.Add(24 * time.Hour)
	t3 := baseTime.Add(48 * time.Hour)
	events := []archmodel.ArchEvent{
		testEvent("e1", archmodel.EventServiceAdded, t1, []string{"PaymentService"}),
		testEvent("e2", archmodel.EventServiceRemoved, t2, []string{"PaymentService"}),
		testEvent("e3", archmodel.EventServiceAdded, t3, []string{"PaymentService"}),
	}
	if _, err := builder.ProcessEvents(events); err != nil {
		t.Fatalf("ProcessEvents: %v", err)
	}

	mem, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}

	history := mem.ComponentMemory["PaymentService"]
	if history.State != StateActive {
		t.Errorf("state after re-add = %s, want CURRENT", history.State)
	}
	if !history.FirstSeen.Equal(t1) || !history.LastSeen.Equal(t3) {
		t.Errorf("first/last seen = %v/%v, want %v/%v", history.FirstSeen, history.LastSeen, t1, t3)
	}

	var removalClaim, readdClaim *KnowledgeClaim
	for i := range mem.GlobalMemory {
		c := &mem.GlobalMemory[i]
		if c.Subject == "PaymentService" && c.Predicate == "was_removed" {
			removalClaim = c
		}
		if c.Subject == "PaymentService" && c.Predicate == "was_added" && c.ValidFrom.Equal(t3) {
			readdClaim = c
		}
	}
	if removalClaim == nil {
		t.Fatalf("no was_removed claim in global memory")
	}
	if removalClaim.State != StateRemoved {
		t.Errorf("removal claim state = %s, want REMOVED", removalClaim.State)
	}
	if removalClaim.ValidUntil == nil || !removalClaim.ValidUntil.Equal(t2) {
		t.Errorf("removal claim ValidUntil = %v, want %v", removalClaim.ValidUntil, t2)
	}
	if readdClaim == nil {
		t.Fatalf("no re-add was_added claim in global memory")
	}
	if readdClaim.ValidUntil != nil {
		t.Errorf("re-add claim ValidUntil = %v, want nil", readdClaim.ValidUntil)
	}
	if len(mem.GlobalMemory) != 3 {
		t.Errorf("global memory len = %d, want 3 (two adds + one removal — historical claims are never deleted)", len(mem.GlobalMemory))
	}
}

func TestProcessEvents_ProjectIDSetOnce(t *testing.T) {
	store := newTestStore(t)
	builder := NewMemoryBuilderWithOptions(store, WithProjectID("proj-1"))

	if _, err := builder.ProcessEvents([]archmodel.ArchEvent{
		testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"PaymentService"}),
	}); err != nil {
		t.Fatalf("ProcessEvents: %v", err)
	}

	mem, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if mem.ProjectID != "proj-1" {
		t.Errorf("ProjectID = %q, want proj-1", mem.ProjectID)
	}

	// A subsequent ingestion must not wipe the ID.
	if _, err := builder.ProcessEvents([]archmodel.ArchEvent{
		testEvent("e2", archmodel.EventPatternDetected, baseTime.Add(time.Hour), []string{"OrderService"}),
	}); err != nil {
		t.Fatalf("second ProcessEvents: %v", err)
	}
	mem, err = store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if mem.ProjectID != "proj-1" {
		t.Errorf("ProjectID after second ingestion = %q, want proj-1", mem.ProjectID)
	}
}

func TestClaimsFromEvent_Taxonomy(t *testing.T) {
	tests := []struct {
		name      string
		src       evidence.Source
		intent    string
		wantKind  ClaimKind
		wantClaim bool
	}{
		{"git commit message is an explicit human reason", evidence.SourceGit, "added Redis for caching", ClaimExplicitReason, true},
		{"pr description is an explicit human reason", evidence.SourcePR, "fixes payment timeout", ClaimExplicitReason, true},
		{"docs/adr is an explicit human reason", evidence.SourceDocs, "per ADR-004", ClaimExplicitReason, true},
		{"issue context is an explicit human reason", evidence.SourceIssue, "closes #42", ClaimExplicitReason, true},
		{"user correction is an explicit human reason", evidence.SourceUser, "per developer", ClaimExplicitReason, true},
		{"llm intent extraction is inference", evidence.SourceLLM, "latency concerns", ClaimInference, true},
		{"heuristic extraction is inference", evidence.SourceHeuristic, "naming suggests cache", ClaimInference, true},
		{"rule extraction is inference", evidence.SourceRule, "rule says x", ClaimInference, true},
		{"unknown source is speculation", "mystery", "who knows", ClaimSpeculation, true},
		{"empty source with intent is speculation", "", "who knows", ClaimSpeculation, true},
		{"no intent produces no reason claim", evidence.SourceGit, "", ClaimSpeculation, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"PaymentService"})
			ev.Intent = tt.intent
			ev.IntentSrc = tt.src

			claims := claimsFromEvent(ev)

			var reasonClaims int
			for _, c := range claims {
				if c.Subject == "PaymentService" && c.Predicate == "was_changed_because" {
					reasonClaims++
					if c.ClaimKind != tt.wantKind {
						t.Errorf("reason claim kind = %s, want %s", c.ClaimKind, tt.wantKind)
					}
					if c.Object != tt.intent {
						t.Errorf("reason claim object = %q, want %q", c.Object, tt.intent)
					}
				}
				if c.ClaimKind == ClaimFact && c.Predicate != "was_added" {
					t.Errorf("unexpected fact claim predicate %q", c.Predicate)
				}
			}
			if tt.wantClaim && reasonClaims != 1 {
				t.Errorf("reason claims = %d, want 1", reasonClaims)
			}
			if !tt.wantClaim && reasonClaims != 0 {
				t.Errorf("reason claims = %d, want 0 (memory must never invent a reason)", reasonClaims)
			}
		})
	}
}

func TestClaimsFromEvent_DependencyObject(t *testing.T) {
	ev := testEvent("e1", archmodel.EventDependencyAdded, baseTime, []string{"PaymentService", "OrderService"})
	claims := claimsFromEvent(ev)

	bySubject := map[string]string{}
	for _, c := range claims {
		bySubject[c.Subject] = c.Predicate + "->" + c.Object
	}
	if bySubject["PaymentService"] != "depends_on->OrderService" {
		t.Errorf("PaymentService claim = %q, want depends_on->OrderService", bySubject["PaymentService"])
	}
	if bySubject["OrderService"] != "depends_on->PaymentService" {
		t.Errorf("OrderService claim = %q, want depends_on->PaymentService", bySubject["OrderService"])
	}
}

func TestClaimsFromEvent_DeterministicIDs(t *testing.T) {
	a := testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"PaymentService"})
	b := testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"PaymentService"})

	claimsA := claimsFromEvent(a)
	claimsB := claimsFromEvent(b)
	if len(claimsA) != len(claimsB) {
		t.Fatalf("claim counts differ: %d vs %d", len(claimsA), len(claimsB))
	}
	for i := range claimsA {
		if claimsA[i].ID != claimsB[i].ID {
			t.Errorf("claim %d ID not deterministic: %q vs %q", i, claimsA[i].ID, claimsB[i].ID)
		}
		if !claimsA[i].ValidFrom.Equal(claimsB[i].ValidFrom) {
			t.Errorf("claim %d timestamp not deterministic", i)
		}
	}
}

func TestClaimsFromEvent_PatternAndSmellClaims(t *testing.T) {
	tests := []struct {
		name        string
		kind        archmodel.EventKind
		wantFact    string
		wantRemoved bool
	}{
		{"pattern detected", archmodel.EventPatternDetected, "pattern_detected", false},
		{"pattern lost", archmodel.EventPatternLost, "pattern_lost", true},
		{"smell introduced", archmodel.EventSmellDetected, "smell_introduced", false},
		{"smell resolved", archmodel.EventSmellResolved, "smell_resolved", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := testEvent("e1", tt.kind, baseTime, []string{"OrderService"})
			ev.AffectedIDs = []string{"GOD_SERVICE"}
			claims := claimsFromEvent(ev)
			if len(claims) != 1 {
				t.Fatalf("claims = %d, want 1", len(claims))
			}
			c := claims[0]
			if c.Subject != "OrderService" {
				t.Errorf("subject = %q, want OrderService", c.Subject)
			}
			if c.Predicate != tt.wantFact {
				t.Errorf("predicate = %q, want %q", c.Predicate, tt.wantFact)
			}
			if c.ClaimKind != ClaimFact {
				t.Errorf("kind = %s, want FACT", c.ClaimKind)
			}
			if tt.wantRemoved {
				if c.State != StateRemoved || c.ValidUntil == nil {
					t.Errorf("expected removed state with ValidUntil for %q", tt.name)
				}
			} else if c.ValidUntil != nil {
				t.Errorf("unexpected ValidUntil for %q", tt.name)
			}
		})
	}
}

func TestMemoryAggregateJSONRoundtrip(t *testing.T) {
	store := newTestStore(t)
	builder := NewMemoryBuilderWithOptions(store, WithProjectID("proj-1"))
	if _, err := builder.ProcessEvents([]archmodel.ArchEvent{
		testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"PaymentService"}),
	}); err != nil {
		t.Fatalf("ProcessEvents: %v", err)
	}

	mem, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	data, err := json.Marshal(mem)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back DeveloperMemory
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.ProjectID != "proj-1" || back.TotalEvents != 1 {
		t.Errorf("roundtrip lost data: %+v", back)
	}
	if len(back.Timeline) != 1 || len(back.Events) != 1 {
		t.Errorf("roundtrip lost timeline/events: %+v", back)
	}
}

// TestProcessEvents_StateChangeTransition verifies the Stage 11 STATE_CHANGE
// event contract: the component state is set from the well-known tag, the
// claim says "<component> state_changed_to <state>", and a rebuild from the
// WAL reproduces the transition exactly (reproducibility).
func TestProcessEvents_StateChangeTransition(t *testing.T) {
	store := newTestStore(t)
	builder := NewMemoryBuilder(store)

	t1 := baseTime
	t2 := baseTime.Add(24 * time.Hour)

	added := testEvent("e1", archmodel.EventServiceAdded, t1, []string{"PaymentService"})
	transition := testEvent("e2", archmodel.EventStateChanged, t2, []string{"PaymentService"})
	transition.Tags = []string{"aging", archmodel.StateTag(string(StateDeprecated))}

	if _, err := builder.ProcessEvents([]archmodel.ArchEvent{added, transition}); err != nil {
		t.Fatalf("ProcessEvents: %v", err)
	}

	mem, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	history := mem.ComponentMemory["PaymentService"]
	if history.State != StateDeprecated {
		t.Errorf("state = %s, want DEPRECATED", history.State)
	}

	var transitionClaim *KnowledgeClaim
	for i := range mem.GlobalMemory {
		c := &mem.GlobalMemory[i]
		if c.Predicate == "state_changed_to" {
			transitionClaim = c
		}
	}
	if transitionClaim == nil {
		t.Fatalf("no state_changed_to claim in global memory")
	}
	if transitionClaim.Subject != "PaymentService" || transitionClaim.Object != "DEPRECATED" {
		t.Errorf("claim = %q state_changed_to %q, want PaymentService -> DEPRECATED",
			transitionClaim.Subject, transitionClaim.Object)
	}
	if transitionClaim.ClaimKind != ClaimFact {
		t.Errorf("transition claim kind = %s, want FACT", transitionClaim.ClaimKind)
	}
	if !transitionClaim.ValidFrom.Equal(t2) {
		t.Errorf("transition claim ValidFrom = %v, want %v", transitionClaim.ValidFrom, t2)
	}

	// Timeline carries the transition as a STATE_CHANGE row.
	foundTimeline := false
	for _, entry := range mem.Timeline {
		if entry.EventKind == archmodel.EventStateChanged && entry.Components[0] == "PaymentService" {
			foundTimeline = true
		}
	}
	if !foundTimeline {
		t.Errorf("timeline does not contain the STATE_CHANGE row")
	}

	// Rebuild reproduces the state: fresh aggregate from the WALs only.
	rebuilt, err := store.Rebuild()
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if rebuilt.ComponentMemory["PaymentService"].State != StateDeprecated {
		t.Errorf("rebuilt state = %s, want DEPRECATED (WAL replay must reproduce aging)", rebuilt.ComponentMemory["PaymentService"].State)
	}
}

// TestProcessEvents_StateChangeMissingTagIsNoOp pins the defensive behavior:
// a STATE_CHANGE event without the well-known tag changes nothing (a corrupt
// event must not corrupt memory).
func TestProcessEvents_StateChangeMissingTagIsNoOp(t *testing.T) {
	store := newTestStore(t)
	builder := NewMemoryBuilder(store)

	added := testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"PaymentService"})
	bad := testEvent("e2", archmodel.EventStateChanged, baseTime.Add(time.Hour), []string{"PaymentService"})
	bad.Tags = []string{"aging"} // no state= tag

	if _, err := builder.ProcessEvents([]archmodel.ArchEvent{added, bad}); err != nil {
		t.Fatalf("ProcessEvents: %v", err)
	}
	mem, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if got := mem.ComponentMemory["PaymentService"].State; got != StateActive {
		t.Errorf("state = %s, want CURRENT (missing tag must not change state)", got)
	}
}
