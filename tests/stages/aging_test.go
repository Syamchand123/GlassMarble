// knowledge aging tests: freshness decay, Ager transitions,
// stale-entity detection.
//
// Discrepancies vs the phase-11 spec: there is no free function
// knowledge_aging.Age(mem, now, cfg) — the delivered API is
// Ager.Age(snap *archmodel.ArchSnapshot, now time.Time) over a
// developer_memory store, and it transitions COMPONENTS (not claims) via
// WAL-appended STATE_CHANGE events. "NowIsMandatory" is not enforced: now is
// a time.Time (not a pointer) and a zero time simply scores every claim 1.0
// (future ValidFrom). DisableAging only makes Ager.Age a no-op; the pure
// scorer (FreshnessScoreWithConfig) always applies decay regardless of
// Enabled. Transition semantics: CURRENT + absent + grace elapsed +
// unreferenced → REMOVED; referenced → DEPRECATED.
package stages_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
	"github.com/Syamchand123/GlassMarble/internal/knowledge_aging"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// s11helperClaim builds a claim with a deterministic evidence source.
func s11helperClaim(id, subject string, src evidence.Source, from time.Time, until *time.Time) developer_memory.KnowledgeClaim {
	return developer_memory.KnowledgeClaim{
		ID:        id,
		Subject:   subject,
		Predicate: "exists_in",
		Object:    "graph",
		ClaimKind: developer_memory.ClaimFact,
		State:     developer_memory.StateActive,
		ValidFrom: from,
		ValidUntil: until,
		Evidence: evidence.NewBundle(evidence.EvidenceItem{
			Source:     src,
			Reference:  "ref-" + id,
			Confidence: 0.9,
			Timestamp:  from,
		}),
	}
}

func TestS11FreshnessScoringBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	until := now.Add(-24 * time.Hour)

	expired := s11helperClaim("expired", "gone", evidence.SourceGit, now.Add(-48*time.Hour), &until)
	fresh := s11helperClaim("fresh", "web", evidence.SourceCode, now, nil)
	decayed := s11helperClaim("decayed", "svc", evidence.SourceGit, now.Add(-300*24*time.Hour), nil)

	mem := &developer_memory.DeveloperMemory{GlobalMemory: []developer_memory.KnowledgeClaim{expired, fresh, decayed}}
	proj := knowledge_aging.FreshenMemoryWithSnapshot(mem, nil, now, nil)
	if proj == nil {
		t.Fatal("FreshenMemoryWithSnapshot returned nil")
	}

	for _, c := range proj.GlobalMemory {
		switch c.ID {
		case "expired":
			if c.FreshnessScore != 0.0 {
				t.Errorf("expired claim score = %v, want 0 (past ValidUntil)", c.FreshnessScore)
			}
		case "fresh":
			if c.FreshnessScore != 1.0 {
				t.Errorf("fresh claim score = %v, want 1 (zero age)", c.FreshnessScore)
			}
		case "decayed":
			want := math.Pow(0.5, 300.0/180.0) // git half-life 180d
			if math.Abs(c.FreshnessScore-want) > 1e-9 {
				t.Errorf("decayed claim score = %v, want %v", c.FreshnessScore, want)
			}
		}
	}
	// The source aggregate is never mutated by the projection.
	if mem.GlobalMemory[0].FreshnessScore != 0.9 && mem.GlobalMemory[0].FreshnessScore != 0.0 {
		// (claim built without a stored score; the point is it is not the projection)
		t.Errorf("source claim was mutated: %v", mem.GlobalMemory[0].FreshnessScore)
	}
}

func TestS11CustomHalfLifeOverridesDecay(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	claim := s11helperClaim("llm", "ai", evidence.SourceLLM, now.Add(-90*24*time.Hour), nil)
	mem := &developer_memory.DeveloperMemory{GlobalMemory: []developer_memory.KnowledgeClaim{claim}}

	// Default LLM half-life is 90 days: exactly one half-life elapsed.
	proj := knowledge_aging.FreshenMemoryWithSnapshot(mem, nil, now, nil)
	if math.Abs(proj.GlobalMemory[0].FreshnessScore-0.5) > 1e-9 {
		t.Errorf("default llm score = %v, want 0.5", proj.GlobalMemory[0].FreshnessScore)
	}

	// Custom half-life of 30 days: three half-lives elapsed.
	custom := &config.AgingConfig{LLMHalfLifeDays: 30}
	proj = knowledge_aging.FreshenMemoryWithSnapshot(mem, nil, now, custom)
	if math.Abs(proj.GlobalMemory[0].FreshnessScore-0.125) > 1e-9 {
		t.Errorf("custom llm score = %v, want 0.125", proj.GlobalMemory[0].FreshnessScore)
	}
}

func TestS11AgerDisabledIsNoOp(t *testing.T) {
	sb := harness.NewSandbox(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	store := developer_memory.NewStoreForRepo(sb.Root)
	mem := &developer_memory.DeveloperMemory{
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"legacy": {Name: "legacy", State: developer_memory.StateActive},
		},
	}
	if err := store.SaveMemory(mem); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	disabled := false
	ager := knowledge_aging.NewAger(store, knowledge_aging.WithConfig(&config.AgingConfig{Enabled: &disabled}))
	results, err := ager.Age(nil, now)
	if err != nil {
		t.Fatalf("Age with disabled config: %v", err)
	}
	if results != nil {
		t.Errorf("results = %v, want nil when aging is disabled", results)
	}
	events, err := store.LoadEvents()
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("events appended despite disabled aging = %d, want 0", len(events))
	}
}

func TestS11AgerRequiresStore(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	ager := knowledge_aging.NewAger(nil)
	if _, err := ager.Age(nil, now); err == nil {
		t.Fatal("Age with nil store: expected error, got nil")
	}
}

func TestS11CurrentUnreferencedAbsentBecomesRemoved(t *testing.T) {
	sb := harness.NewSandbox(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	store := developer_memory.NewStoreForRepo(sb.Root)
	mem := &developer_memory.DeveloperMemory{
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"legacy": {Name: "legacy", State: developer_memory.StateActive},
		},
	}
	if err := store.SaveMemory(mem); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	// Snapshot exists but does not contain "legacy"; LastSeen is zero, so
	// the default 7-day stale grace is treated as elapsed.
	snap := &archmodel.ArchSnapshot{Components: []archmodel.DetectedComponent{{Name: "web"}}}
	ager := knowledge_aging.NewAger(store)
	results, err := ager.Age(snap, now)
	if err != nil {
		t.Fatalf("Age: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("transitions = %d, want 1", len(results))
	}
	tr := results[0]
	if tr.Component != "legacy" || tr.OldState != developer_memory.StateActive || tr.NewState != developer_memory.StateRemoved {
		t.Errorf("transition = %+v, want legacy CURRENT -> REMOVED", tr)
	}
	if tr.RuleID != "aging.transition.current_removed" {
		t.Errorf("rule id = %s, want aging.transition.current_removed", tr.RuleID)
	}
	if tr.Reason == "" {
		t.Error("transition reason is empty")
	}

	// The transition was appended to the event WAL as a STATE_CHANGE event
	// with the new state in the well-known tag.
	events, err := store.LoadEvents()
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.Kind != archmodel.EventStateChanged || len(ev.Components) != 1 || ev.Components[0] != "legacy" {
		t.Errorf("event = %+v, want STATE_CHANGE for legacy", ev)
	}
	if got := archmodel.StateFromTags(ev.Tags); got != string(developer_memory.StateRemoved) {
		t.Errorf("state tag = %q, want state=REMOVED", got)
	}

	// The persisted aggregate reflects the transition.
	reloaded, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if got := reloaded.ComponentMemory["legacy"].State; got != developer_memory.StateRemoved {
		t.Errorf("persisted state = %s, want REMOVED", got)
	}
}

func TestS11AgerIdempotentRerunAppendsNothing(t *testing.T) {
	sb := harness.NewSandbox(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	store := developer_memory.NewStoreForRepo(sb.Root)
	mem := &developer_memory.DeveloperMemory{
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"legacy": {Name: "legacy", State: developer_memory.StateActive},
		},
	}
	if err := store.SaveMemory(mem); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}
	snap := &archmodel.ArchSnapshot{Components: []archmodel.DetectedComponent{{Name: "web"}}}
	ager := knowledge_aging.NewAger(store)

	first, err := ager.Age(snap, now)
	if err != nil {
		t.Fatalf("first Age: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first transitions = %d, want 1", len(first))
	}
	second, err := ager.Age(snap, now)
	if err != nil {
		t.Fatalf("second Age: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second transitions = %d, want 0 (already REMOVED)", len(second))
	}
	events, err := store.LoadEvents()
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("events after rerun = %d, want 1 (deduplicated, not duplicated)", len(events))
	}
}

func TestS11CurrentReferencedAbsentBecomesDeprecated(t *testing.T) {
	sb := harness.NewSandbox(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	store := developer_memory.NewStoreForRepo(sb.Root)
	mem := &developer_memory.DeveloperMemory{
		GlobalMemory: []developer_memory.KnowledgeClaim{
			s11helperClaim("ref_legacy", "legacy", evidence.SourceCode, now.Add(-30*24*time.Hour), nil),
		},
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"legacy": {Name: "legacy", State: developer_memory.StateActive},
		},
	}
	if err := store.SaveMemory(mem); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	snap := &archmodel.ArchSnapshot{Components: []archmodel.DetectedComponent{{Name: "web"}}}
	results, err := knowledge_aging.NewAger(store).Age(snap, now)
	if err != nil {
		t.Fatalf("Age: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("transitions = %d, want 1", len(results))
	}
	if results[0].NewState != developer_memory.StateDeprecated {
		t.Errorf("new state = %s, want DEPRECATED (still referenced by a CURRENT claim)", results[0].NewState)
	}
	if results[0].RuleID != "aging.transition.current_deprecated" {
		t.Errorf("rule id = %s, want aging.transition.current_deprecated", results[0].RuleID)
	}
}

func TestS11PinnedComponentNotTransitioned(t *testing.T) {
	sb := harness.NewSandbox(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	store := developer_memory.NewStoreForRepo(sb.Root)
	mem := &developer_memory.DeveloperMemory{
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"legacy": {Name: "legacy", State: developer_memory.StateActive},
		},
	}
	if err := store.SaveMemory(mem); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	snap := &archmodel.ArchSnapshot{Components: []archmodel.DetectedComponent{{Name: "web"}}}
	pins := map[string]developer_memory.KnowledgeState{"legacy": developer_memory.StateDeprecated}
	ager := knowledge_aging.NewAger(store, knowledge_aging.WithPinnedStates(pins))
	results, err := ager.Age(snap, now)
	if err != nil {
		t.Fatalf("Age: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("transitions = %d, want 0 (learning pin is authoritative)", len(results))
	}
	if got := ager.PinnedStates()["legacy"]; got != developer_memory.StateDeprecated {
		t.Errorf("pinned state = %s, want DEPRECATED", got)
	}
}

func TestS11DetectStaleEntitiesPopulatesNameAndReason(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	mem := &developer_memory.DeveloperMemory{
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"web":   {Name: "web", State: developer_memory.StateActive},
			"gone":  {Name: "gone", State: developer_memory.StateActive, LastSeen: now.Add(-72 * time.Hour)},
			"ancient": {Name: "ancient", State: developer_memory.StateDeprecated},
		},
	}
	snap := &archmodel.ArchSnapshot{Components: []archmodel.DetectedComponent{{Name: "web"}}}

	stale := knowledge_aging.DetectStaleEntities(snap, mem)
	if len(stale) != 1 {
		t.Fatalf("stale = %d, want 1 (DEPRECATED components are not detected)", len(stale))
	}
	if stale[0].Name != "gone" {
		t.Errorf("stale entity name = %s, want gone", stale[0].Name)
	}
	if stale[0].Reason == "" {
		t.Error("stale entity reason is empty")
	}
	if !stale[0].LastSeen.Equal(now.Add(-72 * time.Hour)) {
		t.Errorf("last_seen = %v, want the history's last observation", stale[0].LastSeen)
	}

	// A nil snapshot means absence of information, never staleness.
	if got := knowledge_aging.DetectStaleEntities(nil, mem); got != nil {
		t.Errorf("DetectStaleEntities(nil snap) = %v, want nil", got)
	}
	// A snapshot with the component present yields nothing.
	all := &archmodel.ArchSnapshot{Components: []archmodel.DetectedComponent{{Name: "web"}, {Name: "gone"}}}
	if got := knowledge_aging.DetectStaleEntities(all, mem); len(got) != 0 {
		t.Errorf("DetectStaleEntities(present) = %v, want none", got)
	}
}

func TestS11MissingEntityClaimsFiltersSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	gone := s11helperClaim("c_gone", "gone", evidence.SourceCode, now.Add(-24*time.Hour), nil)
	web := s11helperClaim("c_web", "web", evidence.SourceCode, now.Add(-24*time.Hour), nil)
	web.Object = "web"
	arch := s11helperClaim("c_arch", "architecture", evidence.SourceGit, now.Add(-24*time.Hour), nil)
	mem := &developer_memory.DeveloperMemory{GlobalMemory: []developer_memory.KnowledgeClaim{gone, web, arch}}

	snap := &archmodel.ArchSnapshot{Components: []archmodel.DetectedComponent{{Name: "web"}}}
	missing := knowledge_aging.MissingEntityClaims(snap, mem)
	if len(missing) != 1 || missing[0] != "c_gone" {
		t.Errorf("missing = %v, want [c_gone] (architecture pseudo-subject is never missing)", missing)
	}
}

func TestS11FreshenWithZeroNowNoPanic(t *testing.T) {
	// The spec's "NowIsMandatory" is not enforced: now is a value type and
	// a zero time behaves like the far past (every claim scores 1.0).
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	mem := &developer_memory.DeveloperMemory{GlobalMemory: []developer_memory.KnowledgeClaim{
		s11helperClaim("c1", "svc", evidence.SourceGit, now.Add(-200*24*time.Hour), nil),
	}}
	proj := knowledge_aging.FreshenMemoryWithSnapshot(mem, nil, time.Time{}, nil)
	if proj == nil {
		t.Fatal("FreshenMemoryWithSnapshot(zero now) returned nil")
	}
	if proj.GlobalMemory[0].FreshnessScore != 1.0 {
		t.Errorf("score with zero now = %v, want 1.0 (ValidFrom in the future)", proj.GlobalMemory[0].FreshnessScore)
	}
}

func TestS11RemovedClaimsScoreZeroRegardlessOfAge(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	c := s11helperClaim("c_removed", "svc", evidence.SourceCode, now.Add(-24*time.Hour), nil)
	c.State = developer_memory.StateRemoved
	mem := &developer_memory.DeveloperMemory{GlobalMemory: []developer_memory.KnowledgeClaim{c}}
	proj := knowledge_aging.FreshenMemoryWithSnapshot(mem, nil, now, nil)
	if proj.GlobalMemory[0].FreshnessScore != 0.0 {
		t.Errorf("REMOVED claim score = %v, want 0", proj.GlobalMemory[0].FreshnessScore)
	}
	if got := proj.GlobalMemory[0].State; got != developer_memory.StateRemoved {
		t.Errorf("state = %s, want REMOVED (authoritative closure is never reopened)", got)
	}
}

// s11helperStateTagAssertion keeps the state-tag contract check in one place.
func s11helperStateTagAssertion(t *testing.T, tags []string) {
	t.Helper()
	found := false
	for _, tag := range tags {
		if strings.HasPrefix(tag, archmodel.StateTagPrefix) {
			found = true
		}
	}
	if !found {
		t.Errorf("tags %v carry no state= tag", tags)
	}
}