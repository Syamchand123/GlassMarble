package arch_intelligence

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

func TestGenerateEvents(t *testing.T) {
	base := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{},
	}
	head := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{
			{Name: "c1"},
		},
	}
	diff := &akg.GraphDiff{}
	meta := CommitMeta{Hash: "abcdef", Timestamp: time.Now()}

	events := GenerateEvents(base, head, diff, meta)

	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}

	if len(events) > 0 && events[0].Kind != archmodel.EventServiceAdded {
		t.Errorf("Expected EventServiceAdded, got %v", events[0].Kind)
	}
}

// TestGenerateEvents_CanonicalComponentKeys locks the memory key contract:
// every event's Components must be the component's stable ID (never its raw
// directory name), so the memory builder never splits one component across
// two keys. Dependency, coupling, added and removed events all reference the
// same key space.
func TestGenerateEvents_CanonicalComponentKeys(t *testing.T) {
	base := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{
			{ID: "comp_svc", Name: "internal/service", Instability: 0.1, Ca: 1, Ce: 9},
			{ID: "comp_db", Name: "internal/db"},
		},
	}
	head := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{
			{ID: "comp_svc", Name: "internal/service", Dependencies: []string{"comp_db"}, Instability: 0.6, Ca: 1, Ce: 9},
			{ID: "comp_db", Name: "internal/db"},
		},
	}

	events := GenerateEvents(base, head, &akg.GraphDiff{}, testMeta())

	kinds := map[archmodel.EventKind]bool{}
	for _, e := range events {
		kinds[e.Kind] = true
		for _, comp := range e.Components {
			if comp != "comp_svc" && comp != "comp_db" {
				t.Errorf("event %s carries non-canonical component key %q (want comp_* ID)", e.Kind, comp)
			}
		}
	}
	if !kinds[archmodel.EventDependencyAdded] {
		t.Errorf("expected a dependency event, got kinds %v", kinds)
	}
	if !kinds[archmodel.EventCouplingIncreased] {
		t.Errorf("expected a coupling event, got kinds %v", kinds)
	}
}
