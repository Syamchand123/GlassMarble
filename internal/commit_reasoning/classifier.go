package commit_reasoning

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

type ClassifiedChange struct {
	Kind        archmodel.EventKind
	AffectedIDs []string
}

func ClassifyChange(
	delta *akg.GraphDiff,
	meta *CommitMeta,
	baseSnap, headSnap *archmodel.ArchSnapshot,
) []ClassifiedChange {
	var changes []ClassifiedChange

	// 1. New MODULE node appeared → EventServiceAdded
	for _, n := range delta.NodesAdded {
		if n.Kind == "MODULE" {
			changes = append(changes, ClassifiedChange{
				Kind:        archmodel.EventServiceAdded,
				AffectedIDs: []string{n.ID},
			})
		}
	}

	// 2. MODULE node disappeared → EventServiceRemoved
	for _, n := range delta.NodesRemoved {
		if n.Kind == "MODULE" {
			changes = append(changes, ClassifiedChange{
				Kind:        archmodel.EventServiceRemoved,
				AffectedIDs: []string{n.ID},
			})
		}
	}

	// 3. New EdgeQueriesDB from an existing service → EventDataStoreAdded
	// 4. New EdgePublishes + EdgeSubscribes pattern → EventAsyncIntroduced
	// 5. New edges to "redis", "memcached", "cache" nodes → EventCachingAdded
	for _, e := range delta.EdgesAdded {
		targetLower := strings.ToLower(e.TargetID)

		if e.Type == "QUERIES_DB" || e.Type == "QUERIES" {
			changes = append(changes, ClassifiedChange{
				Kind:        archmodel.EventDataStoreAdded,
				AffectedIDs: []string{e.SourceID, e.TargetID},
			})
		}

		if e.Type == "PUBLISHES" || e.Type == "SUBSCRIBES" {
			changes = append(changes, ClassifiedChange{
				Kind:        archmodel.EventAsyncIntroduced,
				AffectedIDs: []string{e.SourceID, e.TargetID},
			})
		}

		if strings.Contains(targetLower, "redis") ||
			strings.Contains(targetLower, "memcached") ||
			strings.Contains(targetLower, "cache") {
			changes = append(changes, ClassifiedChange{
				Kind:        archmodel.EventCachingAdded,
				AffectedIDs: []string{e.SourceID, e.TargetID},
			})
		}
	}

	// 6. New cycle appeared in SCC → EventCycleIntroduced
	// 7. Cycle disappeared → EventCycleResolved
	if headSnap.Metrics.CycleCount > baseSnap.Metrics.CycleCount {
		changes = append(changes, ClassifiedChange{
			Kind: archmodel.EventCycleIntroduced,
		})
	} else if headSnap.Metrics.CycleCount < baseSnap.Metrics.CycleCount {
		changes = append(changes, ClassifiedChange{
			Kind: archmodel.EventCycleResolved,
		})
	}

	// 8. Layer violation appeared → EventLayerViolation
	if headSnap.Metrics.LayerViolationCount > baseSnap.Metrics.LayerViolationCount {
		changes = append(changes, ClassifiedChange{
			Kind: archmodel.EventLayerViolation,
		})
	}

	// 9. Coupling metrics increased >20% → EventCouplingIncreased
	if baseSnap.Metrics.AvgFanIn > 0 {
		increase := (headSnap.Metrics.AvgFanIn - baseSnap.Metrics.AvgFanIn) / baseSnap.Metrics.AvgFanIn
		if increase > 0.20 {
			changes = append(changes, ClassifiedChange{
				Kind: archmodel.EventCouplingIncreased,
			})
		} else if increase < -0.20 {
			changes = append(changes, ClassifiedChange{
				Kind: archmodel.EventCouplingDecreased,
			})
		}
	}

	// 10. Dead code appeared → EventDeadCodeDetected
	if headSnap.Metrics.DeadCodeNodeCount > baseSnap.Metrics.DeadCodeNodeCount {
		changes = append(changes, ClassifiedChange{
			Kind: archmodel.EventDeadCodeDetected,
		})
	}

	return changes
}
