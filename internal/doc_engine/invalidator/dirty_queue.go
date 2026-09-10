// Package invalidator — dirty_queue.go
// Prioritized dirty-section queue with budget enforcement.
//
// The ordering primitives live in catalog/cascade.go (SortDirtySections,
// EnforceBudget). This file provides the explicit queue type required by the
// master plan §14 package architecture, including security-first priority
// assignment (Stage 3: security > arch-events > API surface > internal > config).
package invalidator

import (
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/catalog"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// DirtyQueue is a prioritized queue of sections awaiting render.
// Lower Priority value means earlier processing.
type DirtyQueue struct {
	items []config.DirtySectionRef
}

// NewDirtyQueue builds a queue from raw dirty refs.
func NewDirtyQueue(refs []config.DirtySectionRef) *DirtyQueue {
	items := make([]config.DirtySectionRef, len(refs))
	copy(items, refs)
	return &DirtyQueue{items: items}
}

// AssignPriorities applies Stage 3 priority ordering:
// 1=security, 2=arch-event, 3=API surface, 4=internal, 5=config.
func (q *DirtyQueue) AssignPriorities() {
	for i := range q.items {
		q.items[i].Priority = PriorityForSection(q.items[i])
	}
}

// PriorityForSection computes the Stage 3 priority for a single ref.
func PriorityForSection(ref config.DirtySectionRef) int {
	id := strings.ToLower(ref.DocID + " " + ref.SectionID)
	switch {
	case strings.Contains(id, "secur") || strings.Contains(id, "threat") || strings.Contains(id, "auth"):
		return 1
	case strings.Contains(id, "architect") || strings.Contains(id, "evolution") || strings.Contains(id, "adr"):
		return 2
	case strings.Contains(id, "interface") || strings.Contains(id, "api") || strings.Contains(id, "export"):
		return 3
	case strings.Contains(id, "config") || strings.Contains(id, "env"):
		return 5
	default:
		return 4
	}
}

// Sort orders items by priority (stable, then by doc/section ID).
func (q *DirtyQueue) Sort() {
	q.items = catalog.SortDirtySections(q.items)
}

// EnforceBudget truncates the queue to maxUpdates, keeping highest priority.
func (q *DirtyQueue) EnforceBudget(maxUpdates int) {
	q.items = catalog.EnforceBudget(q.items, maxUpdates)
}

// Items returns the queued refs in current order.
func (q *DirtyQueue) Items() []config.DirtySectionRef {
	out := make([]config.DirtySectionRef, len(q.items))
	copy(out, q.items)
	return out
}

// Len returns the queue depth.
func (q *DirtyQueue) Len() int { return len(q.items) }

// SortedCopy returns a sorted copy without mutating the queue.
func SortedCopy(refs []config.DirtySectionRef) []config.DirtySectionRef {
	cp := make([]config.DirtySectionRef, len(refs))
	copy(cp, refs)
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i].Priority != cp[j].Priority {
			return cp[i].Priority < cp[j].Priority
		}
		return cp[i].DocID < cp[j].DocID
	})
	return cp
}
