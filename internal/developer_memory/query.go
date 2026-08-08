package developer_memory

import (
	"sort"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// MemoryQueryResult holds the results of a memory query.
type MemoryQueryResult struct {
	Query      string
	Components []ComponentHistory
	Claims     []KnowledgeClaim
}

// QueryMemory answers questions like "what do we know about Redis?"
// This is a deterministic query — no LLM involved.
func QueryMemory(store *MemoryStore, query string) *MemoryQueryResult {
	mem, err := store.LoadMemory()
	if err != nil || mem == nil {
		return &MemoryQueryResult{Query: query}
	}

	queryTokens := strings.Fields(strings.ToLower(query))
	if len(queryTokens) == 0 {
		return &MemoryQueryResult{Query: query}
	}

	result := &MemoryQueryResult{
		Query: query,
	}

	// Search component history for matching components (case-insensitive)
	for name, history := range mem.ComponentMemory {
		nameLower := strings.ToLower(name)
		for _, token := range queryTokens {
			if strings.Contains(nameLower, token) {
				result.Components = append(result.Components, history)
				break
			}
		}
	}

	// Search KnowledgeClaims where subject or object matches
	for _, claim := range mem.GlobalMemory {
		subjLower := strings.ToLower(claim.Subject)
		objLower := strings.ToLower(claim.Object)
		for _, token := range queryTokens {
			if strings.Contains(subjLower, token) || strings.Contains(objLower, token) {
				result.Claims = append(result.Claims, claim)
				break
			}
		}
	}

	// Rank results by freshness score (descending)
	sort.Slice(result.Claims, func(i, j int) bool {
		return result.Claims[i].FreshnessScore > result.Claims[j].FreshnessScore
	})

	return result
}

// GetComponentTimeline returns all timeline entries for a component.
func GetComponentTimeline(store *MemoryStore, componentName string) []archmodel.TimelineEntry {
	mem, err := store.LoadMemory()
	if err != nil || mem == nil {
		return nil
	}

	var entries []archmodel.TimelineEntry
	compLower := strings.ToLower(componentName)

	for _, entry := range mem.Timeline {
		for _, comp := range entry.Components {
			if strings.ToLower(comp) == compLower {
				entries = append(entries, entry)
				break
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.Before(entries[j].Timestamp) // oldest first
	})

	return entries
}

// GetFullTimeline returns the complete architecture timeline within a time window.
func GetFullTimeline(store *MemoryStore, from, to time.Time) []archmodel.TimelineEntry {
	mem, err := store.LoadMemory()
	if err != nil || mem == nil {
		return nil
	}

	var entries []archmodel.TimelineEntry
	for _, entry := range mem.Timeline {
		if (entry.Timestamp.Equal(from) || entry.Timestamp.After(from)) &&
			(entry.Timestamp.Equal(to) || entry.Timestamp.Before(to)) {
			entries = append(entries, entry)
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})

	return entries
}
