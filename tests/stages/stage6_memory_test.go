package stages_test

// Stage 6 (developer_memory) tests: WAL persistence, idempotent rebuild,
// query ranking and corruption tolerance, all against the harness sandbox.
//
// Discrepancies from API_REFERENCE.md:
//   - Rebuild() returns (*DeveloperMemory, error), not a bare call.
//   - Component memory state is NOT CURRENT for caching events: only
//     EventServiceAdded stamps CURRENT in builder.applyEvent; every other
//     kind (including EventCachingAdded) leaves the state at its default
//     UNKNOWN. The "defaults CURRENT" claim is false — verified against
//     internal/developer_memory/builder.go.
//   - The store performs NO claim validation: empty-subject and empty-evidence
//     claims are persisted verbatim. The evidence discipline is enforced
//     only for EVENTS by MemoryBuilder.ProcessEvents (validateEvent).
//   - QueryTerms("what is the cache doing?") returns ["cache", "doing"]:
//     "doing" is not in the stopword list.

import (
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// sampleEvent builds a valid caching event with full evidence provenance.
func sampleEvent(id string) archmodel.ArchEvent {
	now := time.Now().UTC()
	b := evidence.Bundle{}
	b.Add(evidence.EvidenceItem{
		Source:     evidence.SourceCode,
		Reference:  "snapshot",
		Excerpt:    "cache layer introduced in the code graph",
		Confidence: 0.8,
		Timestamp:  now,
	})
	return archmodel.ArchEvent{
		ID:          id,
		Kind:        archmodel.EventCachingAdded,
		CommitHash:  "abcdef1234567890",
		Timestamp:   now,
		Title:       "add cache layer",
		Components:  []string{"cache"},
		AffectedIDs: []string{"internal/cache/cache.go::Cache"},
		Evidence:    b,
		Intent:      "add cache layer",
		IntentSrc:   evidence.SourceCode,
		ValidFrom:   now,
	}
}

func TestMemoryStoreWALPersistence(t *testing.T) {
	sb := harness.NewSandbox(t)
	store := developer_memory.NewStoreForRepo(sb.Root)

	ev := sampleEvent("evt_0001")
	if err := store.AppendEvent(ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	claim := developer_memory.KnowledgeClaim{
		ID:            "claim_0001",
		Subject:       "cache",
		Predicate:     "serves",
		Object:        "greeting lookups",
		ClaimKind:     developer_memory.ClaimExplicitReason,
		State:         developer_memory.StateActive,
		FreshnessScore: 0.9,
		ValidFrom:     ev.Timestamp,
		Evidence:      ev.Evidence,
	}
	if err := store.AppendClaim(claim); err != nil {
		t.Fatalf("AppendClaim: %v", err)
	}

	for _, rel := range []string{".glassmarble/memory/events.jsonl", ".glassmarble/memory/claims.jsonl"} {
		if !sb.Exists(rel) {
			t.Errorf("%s not written", rel)
		}
	}
	if got := sb.ReadFile(".glassmarble/memory/events.jsonl"); !strings.Contains(got, "evt_0001") {
		t.Errorf("events.jsonl missing event id:\n%s", got)
	}

	mem, err := store.Rebuild()
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if mem.TotalEvents != 1 {
		t.Errorf("TotalEvents = %d, want 1", mem.TotalEvents)
	}
	if len(mem.Events) != 1 {
		t.Errorf("Rebuild Events = %d, want 1", len(mem.Events))
	}
	// The WAL claim + two event-derived claims: one FACT claim per
	// mentioned component plus one reason claim when the event carries
	// intent (sampleEvent does).
	if len(mem.GlobalMemory) != 3 {
		t.Errorf("Rebuild GlobalMemory = %d, want 3 (1 WAL claim + 1 event-derived FACT + 1 intent reason)", len(mem.GlobalMemory))
	}
	if len(mem.Timeline) != 1 {
		t.Errorf("Rebuild Timeline = %d, want 1", len(mem.Timeline))
	}

	loaded, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if loaded.TotalEvents != 1 || len(loaded.Events) != 1 || loaded.Events[0].ID != "evt_0001" {
		t.Errorf("LoadMemory round-trip mismatch: %+v", loaded)
	}

	if err := store.SaveMemoryAndTimeline(loaded); err != nil {
		t.Fatalf("SaveMemoryAndTimeline: %v", err)
	}
	for _, rel := range []string{".glassmarble/memory/memory.json", ".glassmarble/memory/timeline.json"} {
		if !sb.Exists(rel) {
			t.Errorf("%s not written", rel)
		}
	}
}

func TestMemoryEventEvidenceRequired(t *testing.T) {
	sb := harness.NewSandbox(t)
	store := developer_memory.NewStoreForRepo(sb.Root)

	ev := sampleEvent("evt_evidence_0001")
	if err := store.AppendEvent(ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	wal := sb.ReadFile(".glassmarble/memory/events.jsonl")
	if !strings.Contains(wal, "evt_evidence_0001") || !strings.Contains(wal, `"snapshot"`) {
		t.Errorf("WAL line missing event id or snapshot evidence reference:\n%s", wal)
	}

	builder := developer_memory.NewMemoryBuilder(store)
	bad := ev
	bad.ID = "evt_no_evidence"
	bad.Evidence = evidence.Bundle{}
	if _, err := builder.ProcessEvents([]archmodel.ArchEvent{bad}); err == nil {
		t.Error("ProcessEvents accepted an event with an empty evidence bundle")
	}
	bad = ev
	bad.ID = "evt_zero_time"
	bad.Timestamp = time.Time{}
	if _, err := builder.ProcessEvents([]archmodel.ArchEvent{bad}); err == nil {
		t.Error("ProcessEvents accepted an event with a zero timestamp")
	}
	if _, err := store.LoadEvents(); err != nil {
		t.Fatalf("LoadEvents after rejected batches: %v", err)
	}
}

func TestMemoryAppendIdempotent(t *testing.T) {
	sb := harness.NewSandbox(t)
	store := developer_memory.NewStoreForRepo(sb.Root)
	ev := sampleEvent("evt_dedup_0001")

	builder := developer_memory.NewMemoryBuilder(store)
	n, err := builder.ProcessEvents([]archmodel.ArchEvent{ev})
	if err != nil || n != 1 {
		t.Fatalf("first ProcessEvents = %d, %v; want 1, nil", n, err)
	}
	n, err = builder.ProcessEvents([]archmodel.ArchEvent{ev})
	if err != nil || n != 0 {
		t.Fatalf("second ProcessEvents = %d, %v; want 0 (idempotent by ID)", n, err)
	}

	// A manual duplicate WAL append is the second line of defense: Rebuild
	// deduplicates by ID.
	if err := store.AppendEvent(ev); err != nil {
		t.Fatalf("manual duplicate AppendEvent: %v", err)
	}
	mem, err := store.Rebuild()
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if mem.TotalEvents != 1 {
		t.Errorf("TotalEvents = %d, want 1 (WAL dedup by ID)", mem.TotalEvents)
	}
	if len(mem.Events) != 1 {
		t.Errorf("Events = %d entries, want 1", len(mem.Events))
	}
}

func TestMemoryBuilderProcessEvents(t *testing.T) {
	sb := harness.NewSandbox(t)
	store := developer_memory.NewStoreForRepo(sb.Root)

	added := sampleEvent("evt_added_0002")
	added.Kind = archmodel.EventServiceAdded
	added.Components = []string{"cache"}

	builder := developer_memory.NewMemoryBuilder(store)
	n, err := builder.ProcessEvents([]archmodel.ArchEvent{added})
	if err != nil || n != 1 {
		t.Fatalf("ProcessEvents = %d, %v; want 1, nil", n, err)
	}

	mem, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if len(mem.Timeline) != 1 {
		t.Fatalf("Timeline = %d, want 1", len(mem.Timeline))
	}
	entry := mem.Timeline[0]
	if entry.Title != "add cache layer" || entry.EventKind != archmodel.EventServiceAdded ||
		len(entry.Components) != 1 || entry.Components[0] != "cache" {
		t.Errorf("timeline entry mismatch: %+v", entry)
	}
	h, ok := mem.ComponentMemory["cache"]
	if !ok {
		t.Fatalf("no ComponentMemory for %q", "cache")
	}
	if h.State != developer_memory.StateActive {
		t.Errorf("component state = %q, want %q (EventServiceAdded -> CURRENT)", h.State, developer_memory.StateActive)
	}
	if len(h.Events) != 1 || h.Events[0] != added.ID {
		t.Errorf("component event ids = %v, want [%s]", h.Events, added.ID)
	}

	// A caching event carries no state transition: a component seen only
	// through it keeps the UNKNOWN default (verified against applyEvent).
	cached := sampleEvent("evt_cached_0003")
	cached.Components = []string{"memo"}
	if n, err := builder.ProcessEvents([]archmodel.ArchEvent{cached}); err != nil || n != 1 {
		t.Fatalf("caching ProcessEvents = %d, %v", n, err)
	}
	mem2, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory after caching event: %v", err)
	}
	if h2 := mem2.ComponentMemory["memo"]; h2.State != developer_memory.StateUnknown {
		t.Errorf("caching-event state = %q, want UNKNOWN (only EventServiceAdded stamps CURRENT)", h2.State)
	}
}

func TestMemoryQuery(t *testing.T) {
	sb := harness.NewSandbox(t)
	store := developer_memory.NewStoreForRepo(sb.Root)
	ev := sampleEvent("evt_query_0001")
	if _, err := developer_memory.NewMemoryBuilder(store).ProcessEvents([]archmodel.ArchEvent{ev}); err != nil {
		t.Fatalf("ProcessEvents: %v", err)
	}

	query := "what is the cache doing?"
	res := developer_memory.QueryMemory(store, query)
	if res.Query != query {
		t.Errorf("Query = %q, want %q", res.Query, query)
	}
	if len(res.Events) == 0 {
		t.Error("no events matched the cache query")
	}
	if len(res.Claims) == 0 {
		t.Error("no claims matched the cache query")
	}
	if len(res.Timeline) != 1 {
		t.Errorf("Timeline = %d, want 1", len(res.Timeline))
	}

	terms := developer_memory.QueryTerms(query)
	if !contains(terms, "cache") {
		t.Errorf("QueryTerms = %v, want token %q", terms, "cache")
	}
	for _, stop := range []string{"what", "is", "the"} {
		if contains(terms, stop) {
			t.Errorf("QueryTerms = %v, stopword %q not filtered", terms, stop)
		}
	}
	// "doing" is retained: it is not in the stopword list (query.go).
	if !contains(terms, "doing") {
		t.Errorf("QueryTerms = %v, want %q retained (not a stopword)", terms, "doing")
	}

	mem, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	capped := developer_memory.QueryMemoryFromMemory(mem, "cache", 1)
	if len(capped.Events) > 1 || len(capped.Claims) > 1 || len(capped.Timeline) > 1 {
		t.Errorf("topK=1 not respected: events=%d claims=%d timeline=%d",
			len(capped.Events), len(capped.Claims), len(capped.Timeline))
	}
}

func TestMemoryQuerySeeded(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.SeedMemory("proj_x")

	store := developer_memory.NewStoreForRepo(sb.Root)
	mem, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if mem.ProjectID != "proj_x" {
		t.Errorf("ProjectID = %q, want proj_x", mem.ProjectID)
	}

	res := developer_memory.QueryMemoryFromMemory(mem, "cache", 5)
	if res.Query != "cache" {
		t.Errorf("Query = %q", res.Query)
	}
	if len(res.Claims) == 0 {
		t.Error("no claims matched the seeded cache claim")
	}
	if len(res.Events) == 0 {
		t.Error("no events matched the seeded cache event")
	}
}

func TestMemoryClaimValidation(t *testing.T) {
	sb := harness.NewSandbox(t)
	store := developer_memory.NewStoreForRepo(sb.Root)

	// The store performs no claim validation: an empty-subject claim with an
	// empty evidence bundle is persisted verbatim (verified against store.go).
	claim := developer_memory.KnowledgeClaim{
		ID: "claim_empty_subject", Subject: "", Predicate: "depends_on", Object: "db",
		ClaimKind: developer_memory.ClaimFact, State: developer_memory.StateActive,
		Evidence: evidence.Bundle{},
	}
	if err := store.AppendClaim(claim); err != nil {
		t.Fatalf("AppendClaim with empty subject: %v (expected tolerated)", err)
	}
	claims, err := store.LoadClaims()
	if err != nil {
		t.Fatalf("LoadClaims: %v", err)
	}
	if len(claims) != 1 || claims[0].Subject != "" || claims[0].ID != "claim_empty_subject" {
		t.Errorf("claim round-trip mismatch: %+v", claims)
	}
	mem, err := store.Rebuild()
	if err != nil {
		t.Fatalf("Rebuild with unvalidated claim: %v", err)
	}
	if len(mem.GlobalMemory) != 1 {
		t.Errorf("GlobalMemory = %d, want 1", len(mem.GlobalMemory))
	}
}

func TestMemoryCorruptionTolerance(t *testing.T) {
	sb := harness.NewSandbox(t)
	store := developer_memory.NewStoreForRepo(sb.Root)
	ev := sampleEvent("evt_good_0001")
	if err := store.AppendEvent(ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	// Corrupt the WAL with unparseable lines; scanning must skip them.
	wal := sb.ReadFile(".glassmarble/memory/events.jsonl")
	sb.WriteFile(".glassmarble/memory/events.jsonl", wal+"{not valid json\nGARBAGE\n"+`{"id":"evt_truncated_0002",`+"\n")

	events, err := store.LoadEvents()
	if err != nil {
		t.Fatalf("LoadEvents with corrupt lines: %v", err)
	}
	if len(events) != 1 || events[0].ID != "evt_good_0001" {
		t.Errorf("LoadEvents = %+v, want only the good event", events)
	}
	mem, err := store.Rebuild()
	if err != nil {
		t.Fatalf("Rebuild after corruption: %v", err)
	}
	if mem.TotalEvents != 1 {
		t.Errorf("TotalEvents = %d, want 1", mem.TotalEvents)
	}
}

func contains(v []string, want string) bool {
	for _, s := range v {
		if s == want {
			return true
		}
	}
	return false
}
