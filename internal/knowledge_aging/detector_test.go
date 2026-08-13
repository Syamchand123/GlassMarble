package knowledge_aging

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

func snapWith(components ...string) *archmodel.ArchSnapshot {
	snap := &archmodel.ArchSnapshot{}
	for _, name := range components {
		snap.Components = append(snap.Components, archmodel.DetectedComponent{
			ID:   "comp_" + name,
			Name: name,
		})
	}
	return snap
}

func memoryWith(components map[string]developer_memory.KnowledgeState) *developer_memory.DeveloperMemory {
	mem := &developer_memory.DeveloperMemory{ComponentMemory: map[string]developer_memory.ComponentHistory{}}
	for name, state := range components {
		mem.ComponentMemory[name] = developer_memory.ComponentHistory{
			Name:     name,
			State:    state,
			LastSeen: baseNow.Add(-24 * time.Hour),
		}
	}
	return mem
}

// TestDetectStaleEntities_Components covers the presence matrix.
func TestDetectStaleEntities_Components(t *testing.T) {
	mem := memoryWith(map[string]developer_memory.KnowledgeState{
		"Present":        developer_memory.StateActive,
		"GoneActive":     developer_memory.StateActive,
		"GoneExp":        developer_memory.StateExperimental,
		"GoneDeprecated": developer_memory.StateDeprecated,
		"GoneRemoved":    developer_memory.StateRemoved,
	})

	stale := DetectStaleEntities(snapWith("Present"), mem)
	byName := map[string]StaleEntity{}
	for _, se := range stale {
		byName[se.Name] = se
	}

	if len(byName) != 2 {
		t.Fatalf("stale = %d entries, want 2 (GoneActive, GoneExp): %v", len(byName), stale)
	}
	for _, want := range []string{"GoneActive", "GoneExp"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("expected %s to be stale", want)
		}
		if byName[want].Reason == "" {
			t.Errorf("%s has no reason", want)
		}
	}
	for _, not := range []string{"Present", "GoneDeprecated", "GoneRemoved"} {
		if _, ok := byName[not]; ok {
			t.Errorf("%s must not be flagged (still present or terminal state)", not)
		}
	}
}

// TestDetectStaleEntities_CanonicalIDKeys pins that memory keyed by the
// canonical component ID (the Stage 5D/8 convention for every event kind)
// is matched against the snapshot's component IDs, not just its names.
func TestDetectStaleEntities_CanonicalIDKeys(t *testing.T) {
	snap := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{
			{ID: "comp_svc", Name: "internal/service"},
			{ID: "comp_db", Name: "internal/db"},
		},
	}
	mem := memoryWith(map[string]developer_memory.KnowledgeState{
		"comp_svc": developer_memory.StateActive,          // ID-keyed: present
		"internal/db": developer_memory.StateActive,       // name-keyed: present
		"comp_gone": developer_memory.StateActive,         // ID-keyed: missing
	})

	stale := DetectStaleEntities(snap, mem)
	if len(stale) != 1 {
		t.Fatalf("stale = %v, want exactly comp_gone", stale)
	}
	if stale[0].Name != "comp_gone" {
		t.Errorf("stale[0].Name = %q, want comp_gone", stale[0].Name)
	}
	if !snapshotHasComponent(snap, "comp_svc") {
		t.Errorf("snapshotHasComponent must resolve component IDs")
	}
}

// TestMissingEntityClaims_CanonicalIDKeys pins the claim-level presence
// check against ID-keyed claim subjects.
func TestMissingEntityClaims_CanonicalIDKeys(t *testing.T) {
	now := baseNow
	mem := &developer_memory.DeveloperMemory{
		GlobalMemory: []developer_memory.KnowledgeClaim{
			{ID: "c_present", Subject: "comp_svc", Predicate: "was_added", State: developer_memory.StateActive, ValidFrom: now},
			{ID: "c_missing", Subject: "comp_gone", Predicate: "was_added", State: developer_memory.StateActive, ValidFrom: now},
		},
	}
	snap := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{
			{ID: "comp_svc", Name: "internal/service"},
		},
	}

	missing := MissingEntityClaims(snap, mem)
	if len(missing) != 1 || missing[0] != "c_missing" {
		t.Errorf("missing = %v, want exactly [c_missing]", missing)
	}
}

// TestDetectStaleEntities_PatternMembers pins that pattern members count as
// present even when not detected as components.
func TestDetectStaleEntities_PatternMembers(t *testing.T) {
	mem := memoryWith(map[string]developer_memory.KnowledgeState{
		"PaymentService": developer_memory.StateActive,
	})
	snap := &archmodel.ArchSnapshot{
		Patterns: []archmodel.DetectedPattern{
			{Kind: archmodel.PatternCQRS, Components: []string{"PaymentService"}},
		},
	}
	if stale := DetectStaleEntities(snap, mem); len(stale) != 0 {
		t.Errorf("pattern member flagged as stale: %v", stale)
	}
}

// TestDetectStaleEntities_NodeIDs pins that components resolved via node IDs
// in claims are recognized as present.
func TestDetectStaleEntities_NodeIDs(t *testing.T) {
	snap := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{
			{Name: "OrderService", NodeIDs: []string{"n_ord_1", "n_ord_2"}},
		},
	}
	mem := memoryWith(map[string]developer_memory.KnowledgeState{
		"OrderService": developer_memory.StateActive,
	})

	if stale := DetectStaleEntities(snap, mem); len(stale) != 0 {
		t.Errorf("component with node IDs flagged as stale: %v", stale)
	}
}

// TestDetectStaleEntities_EdgeCases pins nil-safety.
func TestDetectStaleEntities_EdgeCases(t *testing.T) {
	mem := memoryWith(map[string]developer_memory.KnowledgeState{"X": developer_memory.StateActive})

	if stale := DetectStaleEntities(nil, mem); len(stale) != 0 {
		t.Errorf("nil snapshot must yield no stale entities, got %v", stale)
	}
	if stale := DetectStaleEntities(snapWith("X"), nil); len(stale) != 0 {
		t.Errorf("nil memory must yield no stale entities, got %v", stale)
	}
	if stale := DetectStaleEntities(nil, nil); len(stale) != 0 {
		t.Errorf("nil/nil must yield no stale entities, got %v", stale)
	}
	if !snapshotHasComponent(snapWith("X"), "X") {
		t.Errorf("snapshotHasComponent must find X")
	}
	if snapshotHasComponent(snapWith("X"), "Y") || snapshotHasComponent(nil, "X") {
		t.Errorf("snapshotHasComponent must miss Y and nil snapshots")
	}
}

// TestMissingEntityClaims covers the claim-level staleness signal.
func TestMissingEntityClaims(t *testing.T) {
	now := baseNow
	mem := &developer_memory.DeveloperMemory{
		GlobalMemory: []developer_memory.KnowledgeClaim{
			{ID: "c_gone_subject", Subject: "DeadService", Predicate: "was_added", State: developer_memory.StateActive, ValidFrom: now},
			{ID: "c_gone_object", Subject: "PaymentService", Predicate: "depends_on", Object: "DeadService", State: developer_memory.StateActive, ValidFrom: now},
			{ID: "c_present", Subject: "PaymentService", Predicate: "was_added", State: developer_memory.StateActive, ValidFrom: now},
			{ID: "c_architecture", Subject: "architecture", Predicate: "was_changed_because", Object: "latency", State: developer_memory.StateActive, ValidFrom: now},
			{ID: "c_nodeid", Subject: "RedisCache", SubjectID: "n_redis_1", Predicate: "was_added", State: developer_memory.StateActive, ValidFrom: now},
		},
	}
	snap := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{
			{Name: "PaymentService", NodeIDs: []string{"n_redis_1"}},
		},
	}

	missing := MissingEntityClaims(snap, mem)
	want := map[string]bool{"c_gone_subject": true, "c_gone_object": true}
	if len(missing) != len(want) {
		t.Fatalf("missing = %v, want exactly %v", missing, want)
	}
	for _, id := range missing {
		if !want[id] {
			t.Errorf("unexpected missing claim %q", id)
		}
	}

	if got := MissingEntityClaims(nil, mem); got != nil {
		t.Errorf("nil snapshot must yield no missing claims, got %v", got)
	}
}
