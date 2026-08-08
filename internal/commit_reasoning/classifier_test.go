package commit_reasoning

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

func TestClassifyChange(t *testing.T) {
	delta := &akg.GraphDiff{
		NodesAdded: []akg.DiffNode{
			{Kind: "MODULE", ID: "service-a"},
		},
		EdgesAdded: []akg.DiffEdge{
			{Type: "PUBLISHES"},
			{Type: "SUBSCRIBES"},
			{Type: "QUERIES", TargetID: "redis-cluster"},
		},
	}

	baseSnap := &archmodel.ArchSnapshot{
		Metrics: archmodel.ArchMetrics{
			CycleCount: 0,
			AvgFanIn:   1.0,
		},
	}
	headSnap := &archmodel.ArchSnapshot{
		Metrics: archmodel.ArchMetrics{
			CycleCount: 1, // Cycle introduced
			AvgFanIn:   1.5, // 50% increase (Coupling increased)
		},
	}

	meta := &CommitMeta{}

	changes := ClassifyChange(delta, meta, baseSnap, headSnap)

	expected := map[archmodel.EventKind]int{
		archmodel.EventServiceAdded:      1,
		archmodel.EventAsyncIntroduced:   2, // PUBLISHES, SUBSCRIBES
		archmodel.EventCachingAdded:      1,
		archmodel.EventDataStoreAdded:    1, // QUERIES -> redis-cluster (it's both cache and DB in this test)
		archmodel.EventCycleIntroduced:   1,
		archmodel.EventCouplingIncreased: 1,
	}

	actual := make(map[archmodel.EventKind]int)
	for _, c := range changes {
		actual[c.Kind]++
	}

	for kind, expCount := range expected {
		if actual[kind] != expCount {
			t.Errorf("For %s: expected %d, got %d", kind, expCount, actual[kind])
		}
	}
	for kind, actCount := range actual {
		if expected[kind] == 0 && actCount > 0 {
			t.Errorf("Unexpected event kind: %s", kind)
		}
	}
}
