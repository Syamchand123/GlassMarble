package knowledge_aging

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// daysPtr constructs the tri-state StaleGraceDays pointer in tests (config's
// intPtr is unexported).
func daysPtr(v int) *int { return &v }

// testEvent builds a minimal valid ArchEvent for seeding memory via the
// MemoryBuilder pipeline.
func testEvent(id string, kind archmodel.EventKind, components []string, tags []string, ts time.Time) archmodel.ArchEvent {
	return archmodel.ArchEvent{
		ID:         id,
		Kind:       kind,
		Timestamp:  ts,
		Title:      "test event " + id,
		Components: components,
		Tags:       tags,
		Evidence: evidence.NewBundle(evidence.EvidenceItem{
			Source:     evidence.SourceGit,
			Confidence: 1.0,
			Timestamp:  ts,
		}),
	}
}

// seedByBuilder ingests events through the real builder so tests exercise
// the same WAL path as the pipeline (events.jsonl → Rebuild).
func seedByBuilder(t *testing.T, store *developer_memory.MemoryStore, events []archmodel.ArchEvent) {
	t.Helper()
	b := developer_memory.NewMemoryBuilder(store)
	if _, err := b.ProcessEvents(events); err != nil {
		t.Fatalf("seed events: %v", err)
	}
}

// seedMemory writes a crafted aggregate straight to memory.json, as if a
// previous pass had persisted it. This is how tests pin exact states
// (DEPRECATED / EXPERIMENTAL / REMOVED seeds, exact LastSeen values) that
// the event-driven builder cannot express directly.
func seedMemory(t *testing.T, store *developer_memory.MemoryStore, mem *developer_memory.DeveloperMemory) {
	t.Helper()
	if mem.ComponentMemory == nil {
		mem.ComponentMemory = map[string]developer_memory.ComponentHistory{}
	}
	if err := store.SaveMemoryAndTimeline(mem); err != nil {
		t.Fatalf("seed memory.json: %v", err)
	}
}

// testSnapshot builds a minimal architecture snapshot containing the given
// components.
func testSnapshot(timestamp time.Time, components ...string) *archmodel.ArchSnapshot {
	snap := &archmodel.ArchSnapshot{Timestamp: timestamp}
	for _, name := range components {
		snap.Components = append(snap.Components, archmodel.DetectedComponent{
			ID:         "id-" + name,
			Name:       name,
			Kind:       archmodel.ComponentService,
			Confidence: 0.9,
			Evidence: evidence.NewBundle(evidence.EvidenceItem{
				Source:     evidence.SourceCode,
				Confidence: 0.9,
				Timestamp:  timestamp,
			}),
		})
	}
	return snap
}

func newTestAger(t *testing.T, store *developer_memory.MemoryStore, mutate func(*config.AgingConfig)) *Ager {
	t.Helper()
	cfg := config.DefaultAgingConfig()
	if mutate != nil {
		mutate(cfg)
	}
	return NewAger(store, WithConfig(cfg))
}

func TestAger_DisabledConfig_IsNoOp(t *testing.T) {
	store := developer_memory.NewMemoryStore(t.TempDir())
	seedByBuilder(t, store, []archmodel.ArchEvent{
		testEvent("e1", archmodel.EventServiceAdded, []string{"Orders"}, nil, baseNow.Add(-100*24*time.Hour)),
	})

	ager := newTestAger(t, store, func(c *config.AgingConfig) {
		f := false
		c.Enabled = &f
	})

	results, err := ager.Age(testSnapshot(baseNow), baseNow)
	if err != nil {
		t.Fatalf("Age: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("disabled aging must not transition, got %d results", len(results))
	}
	events, _ := store.LoadEvents()
	if len(events) != 1 {
		t.Errorf("disabled aging must not append events, got %d", len(events))
	}
}

func TestAger_CurrentToDeprecated_FullPass(t *testing.T) {
	store := developer_memory.NewMemoryStore(t.TempDir())
	seedByBuilder(t, store, []archmodel.ArchEvent{
		testEvent("e1", archmodel.EventServiceAdded, []string{"Orders"}, nil, baseNow.Add(-100*24*time.Hour)),
	})

	ager := newTestAger(t, store, nil)
	results, err := ager.Age(testSnapshot(baseNow), baseNow)
	if err != nil {
		t.Fatalf("Age: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 transition, got %d: %+v", len(results), results)
	}
	r := results[0]
	if r.Component != "Orders" || r.NewState != developer_memory.StateDeprecated ||
		r.OldState != developer_memory.StateActive || r.RuleID != ruleCurrentDeprecated {
		t.Errorf("unexpected transition: %+v", r)
	}
	if r.Event == nil || r.Event.Kind != archmodel.EventStateChanged ||
		archmodel.StateFromTags(r.Event.Tags) != string(developer_memory.StateDeprecated) {
		t.Errorf("transition event must carry the state tag: %+v", r.Event)
	}

	events, err := store.LoadEvents()
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("want 2 events in WAL (seed + transition), got %d", len(events))
	}

	rebuilt, err := store.Rebuild()
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if got := rebuilt.ComponentMemory["Orders"].State; got != developer_memory.StateDeprecated {
		t.Errorf("WAL replay must reproduce DEPRECATED, got %s", got)
	}

	persisted, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	if got := persisted.ComponentMemory["Orders"].State; got != developer_memory.StateDeprecated {
		t.Errorf("persisted aggregate state = %s, want DEPRECATED", got)
	}
	for _, claim := range persisted.GlobalMemory {
		// Every persisted claim keeps its WAL-derived state — the
		// projection is read-time only.
		if claim.State != developer_memory.StateActive {
			t.Errorf("persisted claim %s must keep its WAL-derived state, got %s", claim.ID, claim.State)
		}
		// The original FACT claim is 100 days old and must be decayed;
		// the transition event's own claim is brand-new (created at now)
		// and is legitimately fresh.
		if claim.Predicate == "was_added" && claim.FreshnessScore >= 1.0 {
			t.Errorf("persisted claim %s must be freshness-stamped, score = %.3f", claim.ID, claim.FreshnessScore)
		}
	}
}

func TestAger_IdempotentReRun_AppendsNothing(t *testing.T) {
	store := developer_memory.NewMemoryStore(t.TempDir())
	seedByBuilder(t, store, []archmodel.ArchEvent{
		testEvent("e1", archmodel.EventServiceAdded, []string{"Orders"}, nil, baseNow.Add(-100*24*time.Hour)),
	})
	ager := newTestAger(t, store, nil)

	if _, err := ager.Age(testSnapshot(baseNow), baseNow); err != nil {
		t.Fatalf("first Age: %v", err)
	}
	second, err := ager.Age(testSnapshot(baseNow), baseNow)
	if err != nil {
		t.Fatalf("second Age: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("re-running Age on unchanged memory must transition nothing, got %+v", second)
	}
	events, _ := store.LoadEvents()
	if len(events) != 2 {
		t.Errorf("re-running Age must not append events, WAL has %d", len(events))
	}
}

func TestAger_PinnedComponent_IsNeverTransitioned(t *testing.T) {
	store := developer_memory.NewMemoryStore(t.TempDir())
	seedByBuilder(t, store, []archmodel.ArchEvent{
		testEvent("e1", archmodel.EventServiceAdded, []string{"Locked"}, nil, baseNow.Add(-100*24*time.Hour)),
	})

	ager := NewAger(store,
		WithConfig(config.DefaultAgingConfig()),
		WithPinnedStates(map[string]developer_memory.KnowledgeState{
			"Locked": developer_memory.StateActive,
		}),
	)

	results, err := ager.Age(testSnapshot(baseNow), baseNow)
	if err != nil {
		t.Fatalf("Age: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("pinned component must not transition, got %+v", results)
	}
	persisted, _ := store.LoadMemory()
	if got := persisted.ComponentMemory["Locked"].State; got != developer_memory.StateActive {
		t.Errorf("pinned component state = %s, want CURRENT", got)
	}
	events, _ := store.LoadEvents()
	if len(events) != 1 {
		t.Errorf("pinned component must not append transition events, WAL has %d", len(events))
	}
}

func TestAger_StaleGrace(t *testing.T) {
	tests := []struct {
		name       string
		graceDays  *int
		lastSeen   time.Time
		now        time.Time
		want       developer_memory.KnowledgeState
		wantNoRule bool
	}{
		{"within grace: no transition", daysPtr(7), baseNow.Add(-2 * 24 * time.Hour), baseNow, developer_memory.StateActive, true},
		{"beyond grace: deprecate", daysPtr(7), baseNow.Add(-10 * 24 * time.Hour), baseNow, developer_memory.StateDeprecated, false},
		{"explicit zero grace: immediate", daysPtr(0), baseNow.Add(-time.Hour), baseNow, developer_memory.StateDeprecated, false},
		{"no observation: never stuck in limbo", daysPtr(7), time.Time{}, baseNow, developer_memory.StateDeprecated, false},
	}
		for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := developer_memory.NewMemoryStore(t.TempDir())
			seedMemory(t, store, &developer_memory.DeveloperMemory{
				ComponentMemory: map[string]developer_memory.ComponentHistory{
					"Svc": {
						Name:      "Svc",
						State:     developer_memory.StateActive,
						FirstSeen: baseNow.Add(-30 * 24 * time.Hour),
						LastSeen:  tt.lastSeen,
						Events:    []string{"e1"},
					},
				},
				// An ACTIVE claim referencing Svc, so the absent+elapsed
				// cases deterministically demote to DEPRECATED (not REMOVED).
				GlobalMemory: []developer_memory.KnowledgeClaim{
					{
						ID:        "c-ref",
						Subject:   "Svc",
						Predicate: "depends_on",
						Object:    "Db",
						State:     developer_memory.StateActive,
						ValidFrom: baseNow.Add(-30 * 24 * time.Hour),
						Evidence:  evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Confidence: 1.0, Timestamp: baseNow.Add(-30 * 24 * time.Hour)}),
					},
				},
			})

			ager := newTestAger(t, store, func(c *config.AgingConfig) {
				c.StaleGraceDays = tt.graceDays
			})
			results, err := ager.Age(testSnapshot(tt.now), tt.now)
			if err != nil {
				t.Fatalf("Age: %v", err)
			}
			if tt.wantNoRule {
				if len(results) != 0 {
					t.Fatalf("want no transition, got %+v", results)
				}
				return
			}
			if len(results) != 1 || results[0].NewState != tt.want {
				t.Fatalf("want transition to %s, got %+v", tt.want, results)
			}
			persisted, _ := store.LoadMemory()
			if got := persisted.ComponentMemory["Svc"].State; got != tt.want {
				t.Errorf("persisted state = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestAger_DeprecatedToHistorical_AfterCoolingPeriod(t *testing.T) {
	store := developer_memory.NewMemoryStore(t.TempDir())
	deprecatedSince := baseNow.Add(-200 * 24 * time.Hour)
	seedMemory(t, store, &developer_memory.DeveloperMemory{
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"Legacy": {
				Name:     "Legacy",
				State:    developer_memory.StateDeprecated,
				LastSeen: deprecatedSince,
				Events:   []string{"e1", "e2"},
			},
		},
	})

	ager := newTestAger(t, store, nil)
	results, err := ager.Age(testSnapshot(baseNow), baseNow)
	if err != nil {
		t.Fatalf("Age: %v", err)
	}
	if len(results) != 1 || results[0].NewState != developer_memory.StateHistorical ||
		results[0].RuleID != ruleDeprecatedHistorical {
		t.Fatalf("want HISTORICAL via %s, got %+v", ruleDeprecatedHistorical, results)
	}
}

func TestAger_DeprecatedRestoredWhenPresent(t *testing.T) {
	store := developer_memory.NewMemoryStore(t.TempDir())
	seedMemory(t, store, &developer_memory.DeveloperMemory{
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"Back": {
				Name:     "Back",
				State:    developer_memory.StateDeprecated,
				LastSeen: baseNow.Add(-20 * 24 * time.Hour),
				Events:   []string{"e1", "e2"},
			},
		},
	})

	ager := newTestAger(t, store, nil)
	results, err := ager.Age(testSnapshot(baseNow, "Back"), baseNow)
	if err != nil {
		t.Fatalf("Age: %v", err)
	}
	if len(results) != 1 || results[0].NewState != developer_memory.StateActive ||
		results[0].RuleID != ruleDeprecatedRestored {
		t.Fatalf("want restore to CURRENT via %s, got %+v", ruleDeprecatedRestored, results)
	}
	persisted, _ := store.LoadMemory()
	if got := persisted.ComponentMemory["Back"].State; got != developer_memory.StateActive {
		t.Errorf("persisted state = %s, want CURRENT", got)
	}
}

func TestAger_ExperimentalPromotion(t *testing.T) {
	store := developer_memory.NewMemoryStore(t.TempDir())
	seedMemory(t, store, &developer_memory.DeveloperMemory{
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"Proto": {
				Name:     "Proto",
				State:    developer_memory.StateExperimental,
				LastSeen: baseNow.Add(-time.Hour),
				Events:   []string{"e1", "e2", "e3"},
			},
		},
	})

	ager := newTestAger(t, store, nil)
	results, err := ager.Age(testSnapshot(baseNow, "Proto"), baseNow)
	if err != nil {
		t.Fatalf("Age: %v", err)
	}
	if len(results) != 1 || results[0].NewState != developer_memory.StateActive ||
		results[0].RuleID != ruleExperimentalPromoted {
		t.Fatalf("want promotion via %s, got %+v", ruleExperimentalPromoted, results)
	}
}

func TestAger_UnreferencedComponent_IsRemoved(t *testing.T) {
	store := developer_memory.NewMemoryStore(t.TempDir())
	seedMemory(t, store, &developer_memory.DeveloperMemory{
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"Orphan": {
				Name:     "Orphan",
				State:    developer_memory.StateActive,
				LastSeen: baseNow.Add(-60 * 24 * time.Hour),
				Events:   []string{"e1"},
			},
		},
		GlobalMemory: []developer_memory.KnowledgeClaim{
			{
				ID:        "c-other",
				Subject:   "Unrelated",
				Predicate: "depends_on",
				Object:    "AnotherThing",
				State:     developer_memory.StateActive,
				ValidFrom: baseNow.Add(-60 * 24 * time.Hour),
				Evidence:  evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Confidence: 1.0, Timestamp: baseNow.Add(-60 * 24 * time.Hour)}),
			},
		},
	})

	ager := newTestAger(t, store, nil)
	results, err := ager.Age(testSnapshot(baseNow), baseNow)
	if err != nil {
		t.Fatalf("Age: %v", err)
	}
	if len(results) != 1 || results[0].NewState != developer_memory.StateRemoved ||
		results[0].RuleID != ruleCurrentRemoved {
		t.Fatalf("want REMOVED via %s, got %+v", ruleCurrentRemoved, results)
	}
}

func TestAger_GlobalMemoryOnly_AgesFine(t *testing.T) {
	store := developer_memory.NewMemoryStore(t.TempDir())
	seedMemory(t, store, &developer_memory.DeveloperMemory{
		GlobalMemory: []developer_memory.KnowledgeClaim{
			{
				ID:        "c1",
				Subject:   "architecture",
				Predicate: "has_property",
				Object:    "monolith",
				State:     developer_memory.StateActive,
				ValidFrom: baseNow.Add(-100 * 24 * time.Hour),
				Evidence:  evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceDocs, Confidence: 0.9, Timestamp: baseNow.Add(-100 * 24 * time.Hour)}),
			},
		},
	})

	ager := newTestAger(t, store, nil)
	results, err := ager.Age(testSnapshot(baseNow), baseNow)
	if err != nil {
		t.Fatalf("Age must handle GlobalMemory-only memory: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("no components to transition, got %+v", results)
	}
	persisted, _ := store.LoadMemory()
	if len(persisted.GlobalMemory) != 1 || persisted.GlobalMemory[0].FreshnessScore >= 1.0 {
		t.Errorf("global-only memory must be freshness-stamped, got %+v", persisted.GlobalMemory)
	}
}

func TestAger_NilSnapshot_NoTransitions(t *testing.T) {
	store := developer_memory.NewMemoryStore(t.TempDir())
	seedByBuilder(t, store, []archmodel.ArchEvent{
		testEvent("e1", archmodel.EventServiceAdded, []string{"Orders"}, nil, baseNow.Add(-100*24*time.Hour)),
	})

	ager := newTestAger(t, store, nil)
	results, err := ager.Age(nil, baseNow)
	if err != nil {
		t.Fatalf("Age: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("nil snapshot = no information, must not transition, got %+v", results)
	}
	persisted, _ := store.LoadMemory()
	if got := persisted.ComponentMemory["Orders"].State; got != developer_memory.StateActive {
		t.Errorf("state = %s, want CURRENT under nil snapshot", got)
	}
}

func TestFreshenMemoryWithSnapshot_MissingEntityClaims(t *testing.T) {
	mem := &developer_memory.DeveloperMemory{
		GlobalMemory: []developer_memory.KnowledgeClaim{
			{
				ID:        "c-gone",
				Subject:   "RedisCache",
				Predicate: "was_added",
				State:     developer_memory.StateActive,
				ValidFrom: baseNow.Add(-30 * 24 * time.Hour),
				Evidence:  evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Confidence: 1.0, Timestamp: baseNow.Add(-30 * 24 * time.Hour)}),
			},
			{
				ID:        "c-here",
				Subject:   "Orders",
				Predicate: "was_added",
				State:     developer_memory.StateActive,
				ValidFrom: baseNow.Add(-30 * 24 * time.Hour),
				Evidence:  evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Confidence: 1.0, Timestamp: baseNow.Add(-30 * 24 * time.Hour)}),
			},
		},
	}

	proj := FreshenMemoryWithSnapshot(mem, testSnapshot(baseNow, "Orders"), baseNow, config.DefaultAgingConfig())
	got := map[string]developer_memory.KnowledgeState{}
	for _, c := range proj.GlobalMemory {
		got[c.ID] = c.State
	}
	if got["c-gone"] != developer_memory.StateHistorical {
		t.Errorf("claim about vanished entity must project HISTORICAL, got %s", got["c-gone"])
	}
	if got["c-here"] != developer_memory.StateActive {
		t.Errorf("claim about present entity must stay CURRENT, got %s", got["c-here"])
	}
	if _, missing := missingSet(mem, testSnapshot(baseNow, "Orders")); len(missing) != 1 {
		t.Errorf("MissingEntityClaims must flag exactly c-gone")
	}
	if mem.GlobalMemory[0].State != developer_memory.StateActive {
		t.Errorf("projection must not mutate the source aggregate")
	}
}

func TestFreshenMemoryWithSnapshot_NilSnapshot_BehavesLikeFreshenMemory(t *testing.T) {
	mem := &developer_memory.DeveloperMemory{
		GlobalMemory: []developer_memory.KnowledgeClaim{
			{
				ID:        "c1",
				Subject:   "Anything",
				Predicate: "was_added",
				State:     developer_memory.StateActive,
				ValidFrom: baseNow.Add(-10 * 24 * time.Hour),
				Evidence:  evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Confidence: 1.0, Timestamp: baseNow.Add(-10 * 24 * time.Hour)}),
			},
		},
	}
	proj := FreshenMemoryWithSnapshot(mem, nil, baseNow, config.DefaultAgingConfig())
	if proj.GlobalMemory[0].State != developer_memory.StateActive {
		t.Errorf("nil snapshot must not mark claims historical (absence of information is not absence)")
	}
}

func TestFreshenMemory_SubjectStateInheritance(t *testing.T) {
	mem := &developer_memory.DeveloperMemory{
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"Legacy": {
				Name:  "Legacy",
				State: developer_memory.StateDeprecated,
			},
		},
		GlobalMemory: []developer_memory.KnowledgeClaim{
			{
				ID:        "c1",
				Subject:   "Legacy",
				Predicate: "was_added",
				State:     developer_memory.StateActive,
				ValidFrom: baseNow.Add(-10 * 24 * time.Hour),
				Evidence:  evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Confidence: 1.0, Timestamp: baseNow.Add(-10 * 24 * time.Hour)}),
			},
			{
				ID:        "c2",
				Subject:   "Archived",
				Predicate: "was_added",
				State:     developer_memory.StateHistorical,
				ValidFrom: baseNow.Add(-10 * 24 * time.Hour),
				Evidence:  evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Confidence: 1.0, Timestamp: baseNow.Add(-10 * 24 * time.Hour)}),
			},
			{
				ID:        "c3",
				Subject:   "RemovedThing",
				Predicate: "was_removed",
				State:     developer_memory.StateRemoved,
				ValidFrom: baseNow.Add(-10 * 24 * time.Hour),
				Evidence:  evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Confidence: 1.0, Timestamp: baseNow.Add(-10 * 24 * time.Hour)}),
			},
		},
	}

	proj := FreshenMemory(mem, baseNow, config.DefaultAgingConfig())
	got := map[string]developer_memory.KnowledgeState{}
	for _, c := range proj.GlobalMemory {
		got[c.ID] = c.State
	}
	if got["c1"] != developer_memory.StateDeprecated {
		t.Errorf("claim about deprecated subject must inherit DEPRECATED, got %s", got["c1"])
	}
	if got["c2"] != developer_memory.StateHistorical {
		t.Errorf("already-historical claim must stay HISTORICAL, got %s", got["c2"])
	}
	if got["c3"] != developer_memory.StateRemoved {
		t.Errorf("explicitly removed claim must stay REMOVED, got %s", got["c3"])
	}
}

func TestAger_DoesNotPersistProjection(t *testing.T) {
	store := developer_memory.NewMemoryStore(t.TempDir())
	seedByBuilder(t, store, []archmodel.ArchEvent{
		testEvent("e1", archmodel.EventServiceAdded, []string{"Orders"}, nil, baseNow.Add(-100*24*time.Hour)),
	})

	ager := newTestAger(t, store, nil)
	if _, err := ager.Age(testSnapshot(baseNow), baseNow); err != nil {
		t.Fatalf("Age: %v", err)
	}

	persisted, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	// The claim about the now-DEPRECATED component must stay ACTIVE in the
	// store (WAL-derived): only the read-time projection closes it.
	for _, claim := range persisted.GlobalMemory {
		if claim.Subject == "Orders" && claim.State != developer_memory.StateActive {
			t.Errorf("store must keep WAL-derived claim state, got %s", claim.State)
		}
	}
	// And the read-time projection must close it.
	proj := FreshenMemory(persisted, baseNow, config.DefaultAgingConfig())
	for _, claim := range proj.GlobalMemory {
		if claim.Subject == "Orders" && claim.State != developer_memory.StateDeprecated {
			t.Errorf("read-time projection must inherit DEPRECATED, got %s", claim.State)
		}
	}
}

// missingSet mirrors MissingEntityClaims for the assertion above without
// adding a helper of its own.
func missingSet(mem *developer_memory.DeveloperMemory, snap *archmodel.ArchSnapshot) ([]string, map[string]bool) {
	ids := MissingEntityClaims(snap, mem)
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return ids, set
}
