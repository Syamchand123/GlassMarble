// Package catalog — cascade.go
// Implements multi-level invalidation thresholds (leaf, aggregate, root)
// and budget enforcement for the Documentation Intelligence Engine.
package catalog

import (
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// DocLevel represents the structural hierarchy tier of a managed document.
type DocLevel string

const (
	// DocLevelLeaf documents cover a concrete package or subsystem (e.g. docs/components/ai_engine.md).
	// Invalidated on direct symbol additions/modifications/deletions in their scope.
	DocLevelLeaf DocLevel = "leaf"

	// DocLevelAggregate documents cover cross-cutting system design (e.g. docs/architecture.md).
	// Invalidated only when changes trigger architectural milestone events:
	// SERVICE_ADDED, COMPONENT_SPLIT, CYCLE_INTRODUCED, LAYER_VIOLATION, etc.
	DocLevelAggregate DocLevel = "aggregate"

	// DocLevelRoot documents cover top-level orientation (e.g. README.md, getting started).
	// Invalidated only when public CLI entry points or repository-wide conventions change.
	DocLevelRoot DocLevel = "root"
)

// ClassifyDocLevel determines the hierarchy level of a document based on
// its target path, archetype, and scope breadth.
func ClassifyDocLevel(doc *config.DocSpec) DocLevel {
	if doc == nil {
		return DocLevelLeaf
	}

	target := strings.ToLower(normalizePath(doc.TargetPath))

	// Root documents
	if target == "readme.md" || target == "docs/index.md" || target == "docs/readme.md" {
		return DocLevelRoot
	}

	// Architecture and system-wide overviews are aggregate
	if doc.Archetype == "architecture" || doc.Archetype == "system" {
		return DocLevelAggregate
	}
	if strings.Contains(target, "architecture.md") || strings.Contains(target, "overview.md") {
		return DocLevelAggregate
	}

	// If scope covers global root directories (e.g. "internal/**", "cmd/**")
	for _, p := range doc.Scope.Paths {
		normP := normalizePath(p)
		if normP == "**" || normP == "internal/**" || normP == "cmd/**" {
			return DocLevelAggregate
		}
	}

	return DocLevelLeaf
}

// CriticalArchEvents are architectural event names that justify invalidating
// aggregate-level architecture documents.
var CriticalArchEvents = map[string]bool{
	"SERVICE_ADDED":     true,
	"SERVICE_REMOVED":   true,
	"COMPONENT_SPLIT":   true,
	"COMPONENT_MERGED":  true,
	"CYCLE_INTRODUCED":  true,
	"CYCLE_RESOLVED":    true,
	"LAYER_VIOLATION":   true,
	"INTERFACE_CHANGED": true,
	"NEW_DATABASE_LAYER": true,
}

// ShouldCascadeToAggregate reports whether the observed architectural events
// and commit intent justify invalidating aggregate-level documents.
// This prevents private helper changes in leaf modules from burning tokens
// on system architecture docs.
func ShouldCascadeToAggregate(archEvents []string, commitIntent string) bool {
	// 1. Check if any critical architectural event occurred
	for _, ev := range archEvents {
		evUpper := strings.ToUpper(strings.TrimSpace(ev))
		if CriticalArchEvents[evUpper] {
			return true
		}
	}

	// 2. Certain high-impact commit intents trigger aggregate checks
	intentUpper := strings.ToUpper(strings.TrimSpace(commitIntent))
	if intentUpper == "REFACTOR" || intentUpper == "SECURITY" {
		// Only cascade if accompanied by at least one architectural event
		return len(archEvents) > 0
	}

	return false
}

// SortDirtySections sorts dirty section references by ascending priority
// (priority 1 first, priority 5 last).
func SortDirtySections(sections []config.DirtySectionRef) []config.DirtySectionRef {
	sorted := make([]config.DirtySectionRef, len(sections))
	copy(sorted, sections)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority < sorted[j].Priority
		}
		if sorted[i].DocID != sorted[j].DocID {
			return sorted[i].DocID < sorted[j].DocID
		}
		return sorted[i].SectionID < sorted[j].SectionID
	})
	return sorted
}

// EnforceBudget caps the number of dirty sections according to the
// MaxDocUpdatesPerCommit constraint. Sections are prioritized before truncation
// so that security and public API changes are never dropped in favor of
// low-priority internal sections.
func EnforceBudget(sections []config.DirtySectionRef, maxUpdates int) []config.DirtySectionRef {
	if maxUpdates <= 0 || len(sections) <= maxUpdates {
		return SortDirtySections(sections)
	}

	sorted := SortDirtySections(sections)
	return sorted[:maxUpdates]
}
