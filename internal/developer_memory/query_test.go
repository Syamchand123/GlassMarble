package developer_memory

import (
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		query string
		want  []string
	}{
		{"Why was the Redis cache added?", []string{"redis", "cache"}},
		{"the and was why", nil},
		{"PaymentService", []string{"paymentservice"}},
		{"SAGA 42", []string{"saga", "42"}},
		{"", nil},
	}
	for _, tt := range tests {
		got := tokenize(tt.query)
		if len(got) != len(tt.want) {
			t.Errorf("tokenize(%q) = %v, want %v", tt.query, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("tokenize(%q) = %v, want %v", tt.query, got, tt.want)
				break
			}
		}
	}
}

func TestMatchScore(t *testing.T) {
	tests := []struct {
		text string
		tok  string
		want float64
	}{
		{"PaymentService", "paymentservice", 1.0}, // exact word match
		{"PaymentService", "payment", 0.9},        // prefix match
		{"PaymentService", "service", 0.8},        // substring match
		{"RedisCacheLayer", "redis", 0.9},         // prefix
		{"Payments", "pay", 0.9},                  // prefix
		{"UserAgentManager", "agent", 0.8},        // substring
		{"OrderService", "orderservicex", 0.8},    // token contains text
		{"abc", "xyz", 0.0},                       // no match
	}
	for _, tt := range tests {
		got := matchScore(tt.text, []string{tt.tok})
		if got != tt.want {
			t.Errorf("matchScore(%q, %q) = %v, want %v", tt.text, tt.tok, got, tt.want)
		}
	}
}

func TestQueryMemoryFromMemory_RanksCurrentAboveRemoved(t *testing.T) {
	mem := memoryFixture()

	// Both components contain "redis"; the CURRENT one must rank first.
	res := QueryMemoryFromMemory(mem, "redis", DefaultTopK)
	if len(res.Components) < 2 {
		t.Fatalf("expected 2 components, got %d", len(res.Components))
	}
	if res.Components[0].Name != "RedisCache" {
		t.Errorf("rank[0] = %s, want RedisCache (CURRENT outranks REMOVED)", res.Components[0].Name)
	}
	if res.Components[1].Name != "redis-old" {
		t.Errorf("rank[1] = %s, want redis-old", res.Components[1].Name)
	}
}

func TestQueryMemoryFromMemory_TopK(t *testing.T) {
	mem := memoryFixture()
	for i := 0; i < 30; i++ {
		mem.ComponentMemory["service-"+string(rune('a'+i%26))+string(rune('a'+(i/26)%26))] = ComponentHistory{
			Name: "service-" + string(rune('a'+i%26)), State: StateActive,
		}
	}
	res := QueryMemoryFromMemory(mem, "service", 25)
	if len(res.Components) != 25 {
		t.Errorf("components returned = %d, want 25 (top-k)", len(res.Components))
	}
}

func TestQueryMemoryFromMemory_ClaimsRankedByConfidence(t *testing.T) {
	mem := memoryFixture()

	mem.GlobalMemory[1].ID = "claim-low"
	mem.GlobalMemory[1].Evidence.AggConfidence = 0.1
	mem.GlobalMemory[0].ID = "claim-high"

	res := QueryMemoryFromMemory(mem, "paymentservice", DefaultTopK)
	if len(res.Claims) == 0 {
		t.Fatalf("expected claim results")
	}
	if res.Claims[0].ID != "claim-high" {
		t.Errorf("rank[0] = %s, want claim-high (higher confidence first)", res.Claims[0].ID)
	}
}

func TestQueryMemoryFromMemory_EmptyQueryReturnsNothing(t *testing.T) {
	res := QueryMemoryFromMemory(memoryFixture(), "the and was why", DefaultTopK)
	if len(res.Components) != 0 || len(res.Claims) != 0 || len(res.Events) != 0 {
		t.Errorf("stopword-only query must return no results: %+v", res)
	}
}

func TestQueryMemoryFromMemory_NilMemory(t *testing.T) {
	res := QueryMemoryFromMemory(nil, "redis", DefaultTopK)
	if res == nil || res.Query != "redis" {
		t.Fatalf("expected empty result for nil memory")
	}
	if len(res.Components) != 0 {
		t.Errorf("nil memory must return no components")
	}
}

func TestGetComponentTimelineFromMemory_SubstringCaseInsensitive(t *testing.T) {
	mem := memoryFixture()

	// Query "redis" must match entries mentioning RedisCache (substring,
	// case-insensitive).
	entries := GetComponentTimelineFromMemory(mem, "redis")
	if len(entries) == 0 {
		t.Fatalf("expected entries mentioning RedisCache")
	}
	for _, e := range entries {
		found := false
		for _, c := range e.Components {
			if containsFold(c, "redis") {
				found = true
			}
		}
		if !found {
			t.Errorf("entry without redis mention returned: %+v", e)
		}
	}

	if len(GetComponentTimelineFromMemory(mem, "nonexistent")) != 0 {
		t.Errorf("expected no entries for nonexistent component")
	}
}

func TestGetFullTimelineFromMemory_WindowBounds(t *testing.T) {
	mem := memoryFixture()

	all := GetFullTimelineFromMemory(mem, time.Time{}, time.Time{})
	if len(all) != len(mem.Timeline) {
		t.Errorf("zero-bounds window = %d entries, want %d", len(all), len(mem.Timeline))
	}

	// Window covering only the first entry.
	first := mem.Timeline[0]
	window := GetFullTimelineFromMemory(mem, first.Timestamp.Add(-time.Hour), first.Timestamp.Add(time.Minute))
	if len(window) != 1 {
		t.Errorf("bounded window = %d entries, want 1", len(window))
	}
}

func TestGetRelatedTimeline_NewestFirstAndDedup(t *testing.T) {
	mem := memoryFixture()

	entries := GetRelatedTimeline(mem, []string{"RedisCache"})
	if len(entries) == 0 {
		t.Fatalf("expected related timeline entries")
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].Timestamp.After(entries[i-1].Timestamp) {
			t.Errorf("timeline not newest-first at index %d", i)
		}
	}

	// Same entity repeated must not duplicate results.
	again := GetRelatedTimeline(mem, []string{"RedisCache", "rediscache"})
	if len(again) != len(entries) {
		t.Errorf("dedup failed: %d vs %d entries", len(again), len(entries))
	}
}

func TestQueryMemory_ViaStore(t *testing.T) {
	store := newTestStore(t)
	builder := NewMemoryBuilder(store)
	ev := testEvent("e1", archmodel.EventServiceAdded, baseTime, []string{"PaymentService"})
	ev.Intent = "added because the old service was slow"
	ev.IntentSrc = evidence.SourceGit
	if _, err := builder.ProcessEvents([]archmodel.ArchEvent{ev}); err != nil {
		t.Fatalf("ProcessEvents: %v", err)
	}

	res := QueryMemory(store, "Why was PaymentService added?")
	if len(res.Components) != 1 || res.Components[0].Name != "PaymentService" {
		t.Errorf("components = %+v, want PaymentService", res.Components)
	}
	if len(res.Events) != 1 {
		t.Errorf("events = %d, want 1", len(res.Events))
	}
	var foundReason bool
	for _, c := range res.Claims {
		if c.Predicate == "was_changed_because" {
			foundReason = true
		}
	}
	if !foundReason {
		t.Errorf("expected the explicit reason claim in results")
	}
}

// --- fixtures ---

// memoryFixture builds a small memory with known state for ranking tests.
func memoryFixture() *DeveloperMemory {
	t1 := baseTime
	t2 := baseTime.Add(24 * time.Hour)
	t3 := baseTime.Add(48 * time.Hour)

	evAddRedis := testEvent("e-redis-1", archmodel.EventServiceAdded, t1, []string{"RedisCache"})
	evAddPay := testEvent("e-pay-1", archmodel.EventServiceAdded, t1.Add(time.Hour), []string{"PaymentService"})
	evRemoveRedis := testEvent("e-redis-2", archmodel.EventServiceRemoved, t2, []string{"RedisCache"})
	evAddRedis2 := testEvent("e-redis-3", archmodel.EventServiceAdded, t3, []string{"RedisCache"})
	evPattern := testEvent("e-pat-1", archmodel.EventPatternDetected, t3.Add(time.Hour), []string{"PaymentService"})
	evPattern.Intent = "hexagonal architecture per ADR-007"
	evPattern.IntentSrc = evidence.SourceDocs

	mem := &DeveloperMemory{
		ProjectID:   "proj-test",
		LastUpdated: t3.Add(time.Hour),
		TotalEvents: 5,
		Events:      []archmodel.ArchEvent{evAddRedis, evAddPay, evRemoveRedis, evAddRedis2, evPattern},
		Timeline:    []archmodel.TimelineEntry{},
		ComponentMemory: map[string]ComponentHistory{
			"RedisCache": {
				Name: "RedisCache", FirstSeen: t1, LastSeen: t3, State: StateActive,
				Events: []string{"e-redis-1", "e-redis-2", "e-redis-3"},
			},
			"PaymentService": {
				Name: "PaymentService", FirstSeen: t1.Add(time.Hour), LastSeen: t3.Add(time.Hour), State: StateActive,
				Events: []string{"e-pay-1", "e-pat-1"},
			},
			"redis-old": {
				Name: "redis-old", FirstSeen: baseTime.Add(-720 * time.Hour), LastSeen: baseTime.Add(-600 * time.Hour),
				State: StateRemoved, Events: []string{"e-old"},
			},
		},
		GlobalMemory: []KnowledgeClaim{
			{
				ID: "claim-hi", Subject: "PaymentService", Predicate: "was_changed_because",
				Object: "hexagonal architecture per ADR-007", ClaimKind: ClaimExplicitReason,
				State: StateActive, ValidFrom: t3.Add(time.Hour),
				Evidence: evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceDocs, Reference: "adr-007", Confidence: 0.9}),
			},
			{
				ID: "claim-lo", Subject: "PaymentService", Predicate: "was_added", Object: "",
				ClaimKind: ClaimFact, State: StateActive, ValidFrom: t1.Add(time.Hour),
				Evidence: evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceLLM, Reference: "guess", Confidence: 0.4}),
			},
		},
	}
	mem.Timeline = []archmodel.TimelineEntry{
		{Timestamp: t1, CommitHash: "c1", Title: "add redis cache", EventKind: archmodel.EventServiceAdded, Components: []string{"RedisCache"}},
		{Timestamp: t1.Add(time.Hour), CommitHash: "c2", Title: "add payment service", EventKind: archmodel.EventServiceAdded, Components: []string{"PaymentService"}},
		{Timestamp: t2, CommitHash: "c3", Title: "remove redis", EventKind: archmodel.EventServiceRemoved, Components: []string{"RedisCache"}},
		{Timestamp: t3, CommitHash: "c4", Title: "re-add redis", EventKind: archmodel.EventServiceAdded, Components: []string{"RedisCache"}},
	}
	return mem
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
