package knowledge_aging

import (
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// StaleEntity represents a component or memory that is no longer found in the codebase.
type StaleEntity struct {
	Name     string    `json:"name"`
	LastSeen time.Time `json:"last_seen"`
	Reason   string    `json:"reason"`
}

// DetectStaleEntities finds components/claims that may be outdated.
// "Stale" means: was present in AKG history but not in the current AKG snapshot.
func DetectStaleEntities(
	currentSnap *archmodel.ArchSnapshot,
	memory *developer_memory.DeveloperMemory,
) []StaleEntity {
	var stale []StaleEntity
	
	if memory == nil || currentSnap == nil {
		return stale
	}

	for compName, history := range memory.ComponentMemory {
		if history.State == developer_memory.StateActive || history.State == developer_memory.StateExperimental {
			// Check if this component still exists in the current snapshot
			if !snapshotHasComponent(currentSnap, compName) {
				stale = append(stale, StaleEntity{
					Name:     compName,
					LastSeen: history.LastSeen,
					Reason:   "Component no longer detected in current graph",
				})
			}
		}
	}
	return stale
}

// snapshotHasComponent is a helper to check if a component is in the snapshot
func snapshotHasComponent(snap *archmodel.ArchSnapshot, compName string) bool {
	for _, comp := range snap.Components {
		if comp.Name == compName {
			return true
		}
	}
	return false
}
