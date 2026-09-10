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
	"unicode"

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

// AssignPriorities applies Stage 3 priority ordering using real dossier
// signals and section specs (see PriorityForSection):
// 1=sentinel-affected, 2=arch-event, 3=API surface, 4=internal, 5=config-only.
//
// specs maps SpecKey(ref.DocID, ref.SectionID) to the section's SectionSpec.
// A nil dossier or missing spec degrades gracefully to the default priority.
func (q *DirtyQueue) AssignPriorities(dossier *config.GlobalCommitDossier, specs map[string]*config.SectionSpec) {
	for i := range q.items {
		q.items[i].Priority = PriorityForSection(q.items[i], dossier, specs[SpecKey(q.items[i].DocID, q.items[i].SectionID)])
	}
}

// SpecKey builds the lookup key for the specs map used by AssignPriorities.
func SpecKey(docID, sectionID string) string {
	return docID + "\x00" + sectionID
}

// PriorityForSection computes the Stage 3 priority for a single ref from real
// signals — the dossier's changed symbols/sentinels/arch-events and the
// section's ground_with directives — instead of name substrings.
func PriorityForSection(ref config.DirtySectionRef, dossier *config.GlobalCommitDossier, sectionSpec *config.SectionSpec) int {
	// 5: config-only sections (ground_with is exactly [config_vars]) are
	// always lowest priority, regardless of dossier signals.
	if sectionSpec != nil && isConfigOnlySection(sectionSpec) {
		return 5
	}
	if dossier != nil {
		// 1: section grounds on sentinels/error_returns AND the dossier
		// carries sentinel changes.
		if groundsOnSentinels(sectionSpec) &&
			len(dossier.AddedSentinels)+len(dossier.ModifiedSentinels) > 0 {
			return 1
		}
		// 2: architectural events affect aggregate-level sections.
		if len(dossier.ArchEvents) > 0 {
			return 2
		}
		// 3: exported API surface changed.
		if hasExportedSymbolChanges(dossier) {
			return 3
		}
	}
	// 4: internal implementation default.
	return 4
}

// isConfigOnlySection reports whether the section grounds exclusively on
// config_vars.
func isConfigOnlySection(sec *config.SectionSpec) bool {
	if sec == nil || len(sec.GroundWith) == 0 {
		return false
	}
	for _, d := range sec.GroundWith {
		if strings.ToLower(strings.TrimSpace(d)) != "config_vars" {
			return false
		}
	}
	return true
}

// groundsOnSentinels reports whether the section pulls sentinel/error facts.
// A nil spec is treated as generic grounding (not sentinel-specific).
func groundsOnSentinels(sec *config.SectionSpec) bool {
	if sec == nil {
		return false
	}
	for _, d := range sec.GroundWith {
		switch strings.ToLower(strings.TrimSpace(d)) {
		case "sentinels", "error_returns":
			return true
		}
	}
	return false
}

// hasExportedSymbolChanges reports whether the dossier added or modified any
// exported (public API surface) symbol.
func hasExportedSymbolChanges(dossier *config.GlobalCommitDossier) bool {
	for _, s := range dossier.AddedSymbols {
		if isExportedFQN(s.FQN) {
			return true
		}
	}
	for _, s := range dossier.ModifiedSymbols {
		if isExportedFQN(s.FQN) {
			return true
		}
	}
	return false
}

// isExportedFQN reports whether an FQN's short symbol name is exported
// (starts with an uppercase letter). Handles "path::Name", "pkg.Name",
// and bare "Name" forms.
func isExportedFQN(fqn string) bool {
	name := fqn
	if idx := strings.LastIndex(name, "::"); idx >= 0 {
		name = name[idx+2:]
	}
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if name == "" {
		return false
	}
	return unicode.IsUpper([]rune(name)[0])
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
