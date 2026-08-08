package developer_memory

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// MemoryBuilder processes ArchEvents and updates DeveloperMemory.
type MemoryBuilder struct {
	store *MemoryStore
}

// NewMemoryBuilder creates a MemoryBuilder.
func NewMemoryBuilder(store *MemoryStore) *MemoryBuilder {
	return &MemoryBuilder{store: store}
}

// ProcessEvents ingests new ArchEvents into the memory.
func (b *MemoryBuilder) ProcessEvents(events []archmodel.ArchEvent) error {
	mem, err := b.store.LoadMemory()
	if err != nil {
		return err
	}

	for _, event := range events {
		// 1. Append event to events.jsonl
		if err := b.store.AppendEvent(event); err != nil {
			return err
		}

		mem.TotalEvents++
		if event.Timestamp.After(mem.LastUpdated) {
			mem.LastUpdated = event.Timestamp
		}

		// 2. Update component history
		for _, comp := range event.Components {
			history, exists := mem.ComponentMemory[comp]
			if !exists {
				history = ComponentHistory{
					Name:      comp,
					FirstSeen: event.Timestamp,
					State:     StateActive,
				}
			}

			history.Events = append(history.Events, event.ID)
			history.LastSeen = event.Timestamp

			if event.Kind == archmodel.EventServiceAdded {
				history.FirstSeen = event.Timestamp
				history.State = StateActive
			} else if event.Kind == archmodel.EventServiceRemoved {
				history.State = StateRemoved
			}

			mem.ComponentMemory[comp] = history
		}

		// 3. Extract and store KnowledgeClaims from event
		claims := b.claimsFromEvent(event)
		for _, claim := range claims {
			if err := b.store.AppendClaim(claim); err != nil {
				return err
			}
			mem.GlobalMemory = append(mem.GlobalMemory, claim)
		}

		// 4. Update timeline
		entry := archmodel.TimelineEntry{
			Timestamp:   event.Timestamp,
			CommitHash:  event.CommitHash,
			Title:       event.Title,
			Description: event.Description,
			EventKind:   event.Kind,
			Components:  event.Components,
			Intent:      event.Intent,
			Tags:        event.Tags,
		}
		if err := b.store.AppendTimelineEntry(entry); err != nil {
			return err
		}
		mem.Timeline = append(mem.Timeline, entry)
	}

	return b.store.SaveMemory(mem)
}

func (b *MemoryBuilder) claimsFromEvent(event archmodel.ArchEvent) []KnowledgeClaim {
	var claims []KnowledgeClaim
	if len(event.Components) == 0 {
		return claims
	}

	for i, comp := range event.Components {
		claim := KnowledgeClaim{
			ID:             fmt.Sprintf("claim-%s-%d", event.ID, i),
			Subject:        comp,
			Predicate:      "involved_in_event",
			Object:         string(event.Kind),
			Evidence:       event.Evidence,
			State:          StateActive,
			ValidFrom:      event.Timestamp,
			FreshnessScore: 1.0,
		}

		// Optionally enhance predicate based on EventKind
		if strings.Contains(string(event.Kind), "ADDED") {
			claim.Predicate = "was_added"
		} else if strings.Contains(string(event.Kind), "REMOVED") {
			claim.Predicate = "was_removed"
			claim.State = StateRemoved
		}

		claims = append(claims, claim)
	}

	return claims
}
