package developer_memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

func TestNewStoreForRepo_ResolvesMemoryDir(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreForRepo(dir)

	want := filepath.Join(dir, ".glassmarble", "memory")
	if store.Dir() != want {
		t.Errorf("Dir() = %q, want %q", store.Dir(), want)
	}

	// Appending must lazily create the full directory chain.
	if err := store.AppendEvent(testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"A"})); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("memory dir not created: %v", err)
	}
}

func TestAppendLoad_EventsAndClaims(t *testing.T) {
	store := newTestStore(t)

	ev := testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"PaymentService"})
	if err := store.AppendEvent(ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	events, err := store.LoadEvents()
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(events) != 1 || events[0].ID != "e1" {
		t.Fatalf("LoadEvents = %+v, want [e1]", events)
	}
	if !events[0].Timestamp.Equal(baseTime) {
		t.Errorf("timestamp lost in roundtrip: %v", events[0].Timestamp)
	}

	claim := newClaim(ev, "PaymentService", "was_added", "", ClaimFact, false)
	if err := store.AppendClaim(claim); err != nil {
		t.Fatalf("AppendClaim: %v", err)
	}
	claims, err := store.LoadClaims()
	if err != nil {
		t.Fatalf("LoadClaims: %v", err)
	}
	if len(claims) != 1 || claims[0].ID != claim.ID {
		t.Fatalf("LoadClaims = %+v, want [%s]", claims, claim.ID)
	}
}

func TestScanJSONL_CorruptLineSkipped(t *testing.T) {
	store := newTestStore(t)

	good := testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"A"})
	if err := store.AppendEvent(good); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	corrupt := `{"id": "e2", "kind": "SERVICE_ADDED"` + "\n" + "\n"
	f, err := os.OpenFile(filepath.Join(store.Dir(), eventsFile), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open WAL for corruption: %v", err)
	}
	if _, err := f.WriteString(corrupt); err != nil {
		t.Fatalf("corrupt WAL: %v", err)
	}
	f.Close()

	warned := false
	store.WithLogger(func(string, ...any) { warned = true })

	events, err := store.LoadEvents()
	if err != nil {
		t.Fatalf("LoadEvents with corrupt line must not fail: %v", err)
	}
	if len(events) != 1 || events[0].ID != "e1" {
		t.Errorf("events = %+v, want only the valid line", events)
	}
	if !warned {
		t.Errorf("expected a warning for the skipped corrupt line")
	}
}

func TestRebuild_FromWALAndSorted(t *testing.T) {
	store := newTestStore(t)

	// Append out of chronological order to prove the timeline is sorted.
	late := testEvent("e2", archmodel.EventPatternDetected, baseTime.Add(48*time.Hour), []string{"A"})
	early := testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"A"})
	if err := store.AppendEvent(late); err != nil {
		t.Fatalf("append late: %v", err)
	}
	if err := store.AppendEvent(early); err != nil {
		t.Fatalf("append early: %v", err)
	}

	mem, err := store.Rebuild()
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if len(mem.Timeline) != 2 {
		t.Fatalf("timeline len = %d, want 2", len(mem.Timeline))
	}
	if !mem.Timeline[0].Timestamp.Equal(baseTime) {
		t.Errorf("timeline[0] = %v, want oldest first (%v)", mem.Timeline[0].Timestamp, baseTime)
	}
	if mem.LastUpdated.IsZero() {
		t.Errorf("LastUpdated not derived from events")
	}
}

func TestRebuild_DeduplicatesByEventID(t *testing.T) {
	store := newTestStore(t)

	ev := testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"A"})
	if err := store.AppendEvent(ev); err != nil {
		t.Fatalf("append: %v", err)
	}
	// Simulate a duplicate append that slipped past the builder's check.
	if err := store.AppendEvent(ev); err != nil {
		t.Fatalf("append duplicate: %v", err)
	}

	mem, err := store.Rebuild()
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if mem.TotalEvents != 1 {
		t.Errorf("TotalEvents = %d, want 1 (dedup by ID)", mem.TotalEvents)
	}
	if len(mem.Timeline) != 1 {
		t.Errorf("timeline len = %d, want 1", len(mem.Timeline))
	}
}

func TestSaveLoadMemory_Atomic(t *testing.T) {
	store := newTestStore(t)

	mem := &DeveloperMemory{
		ProjectID:       "proj-1",
		TotalEvents:     7,
		LastUpdated:     baseTime,
		Timeline:        []archmodel.TimelineEntry{{Timestamp: baseTime, CommitHash: "c1", Title: "t"}},
		ComponentMemory: map[string]ComponentHistory{"A": {Name: "A", State: StateActive}},
		GlobalMemory:    []KnowledgeClaim{{ID: "c1", Subject: "A"}},
		Events:          []archmodel.ArchEvent{testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"A"})},
	}
	if err := store.SaveMemory(mem); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	// No temp files may linger after an atomic save.
	matches, _ := filepath.Glob(filepath.Join(store.Dir(), ".memory.json.tmp-*"))
	if len(matches) != 0 {
		t.Errorf("leftover temp files: %v", matches)
	}

	got, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if got.ProjectID != "proj-1" || got.TotalEvents != 7 {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
	if got.ComponentMemory["A"].State != StateActive {
		t.Errorf("component memory lost: %+v", got.ComponentMemory)
	}
	if len(got.Timeline) != 1 || len(got.Events) != 1 {
		t.Errorf("timeline/events lost in roundtrip")
	}
}

func TestLoadMemory_SelfHealsFromCorruptAggregate(t *testing.T) {
	store := newTestStore(t)

	// Valid WAL, then a corrupt aggregate.
	if err := store.AppendEvent(testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"PaymentService"})); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir(), memoryFile), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("corrupt aggregate: %v", err)
	}

	warned := false
	store.WithLogger(func(string, ...any) { warned = true })

	mem, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory must self-heal from WAL: %v", err)
	}
	if mem.TotalEvents != 1 {
		t.Errorf("TotalEvents = %d, want 1 (rebuilt from WAL)", mem.TotalEvents)
	}
	if !warned {
		t.Errorf("expected a warning about the corrupt aggregate")
	}
}

func TestLoadMemory_MissingAggregateRebuilds(t *testing.T) {
	store := newTestStore(t)
	if err := store.AppendEvent(testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"A"})); err != nil {
		t.Fatalf("append: %v", err)
	}
	mem, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if mem == nil || mem.TotalEvents != 1 {
		t.Errorf("expected rebuilt memory, got %+v", mem)
	}
}

func TestStore_EmptyDirIsValid(t *testing.T) {
	store := newTestStore(t)
	mem, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory on empty dir: %v", err)
	}
	if mem == nil {
		t.Fatalf("LoadMemory returned nil for an empty store")
	}
	if mem.TotalEvents != 0 || len(mem.ComponentMemory) != 0 {
		t.Errorf("expected empty memory, got %+v", mem)
	}
}
