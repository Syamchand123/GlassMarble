package arch_intelligence

import (
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// CommitMeta holds metadata about a git commit for event generation.
type CommitMeta struct {
	Hash      string
	Timestamp time.Time
}

// snapshotHasComponent checks if a component name exists in a snapshot.
func snapshotHasComponent(snap *archmodel.ArchSnapshot, name string) bool {
	if snap == nil {
		return false
	}
	for _, c := range snap.Components {
		if c.Name == name {
			return true
		}
	}
	return false
}

// GenerateEvents produces ArchEvents by comparing two snapshots.
// This is the core of "what changed architecturally?"
func GenerateEvents(
	base *archmodel.ArchSnapshot,
	head *archmodel.ArchSnapshot,
	graphDiff *akg.GraphDiff,
	commitMeta CommitMeta,
) []archmodel.ArchEvent {
	var events []archmodel.ArchEvent

	if head == nil {
		return events
	}

	// 1. Check for new components
	for _, comp := range head.Components {
		if !snapshotHasComponent(base, comp.Name) {
			events = append(events, archmodel.ArchEvent{
				ID:         "evt_" + commitMeta.Hash + "_" + comp.Name,
				Kind:       archmodel.EventServiceAdded,
				CommitHash: commitMeta.Hash,
				Timestamp:  commitMeta.Timestamp,
				Title:      "Component Added: " + comp.Name,
				Components: []string{comp.Name},
				Evidence:   comp.Evidence,
			})
		}
	}

	// Note: Further diffing for patterns, smells, and dead code would go here
	// by comparing base.Patterns vs head.Patterns, etc.

	return events
}
