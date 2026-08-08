package developer_memory

import (
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

type KnowledgeState string

const (
	StateActive       KnowledgeState = "ACTIVE"
	StateRemoved      KnowledgeState = "REMOVED"
	StateDeprecated   KnowledgeState = "DEPRECATED"
	StateExperimental KnowledgeState = "EXPERIMENTAL"
	StateUnknown      KnowledgeState = "UNKNOWN"
)

// DeveloperMemory is the persistent architectural memory for a project.
// It answers the question: "What do we know about this project and why?"
type DeveloperMemory struct {
	ProjectID       string                      `json:"project_id"`
	LastUpdated     time.Time                   `json:"last_updated"`
	TotalEvents     int                         `json:"total_events"`
	Timeline        []archmodel.TimelineEntry   `json:"timeline"`
	ComponentMemory map[string]ComponentHistory `json:"component_memory"` // key: component name
	GlobalMemory    []KnowledgeClaim            `json:"global_memory"`
}

// ComponentHistory stores the longitudinal history of one architectural component.
type ComponentHistory struct {
	Name      string           `json:"name"`
	FirstSeen time.Time        `json:"first_seen"`
	LastSeen  time.Time        `json:"last_seen"`
	State     KnowledgeState   `json:"state"`
	Events    []string         `json:"event_ids"` // arch event IDs
	Claims    []KnowledgeClaim `json:"claims"`
}

// KnowledgeClaim is a factual or inferred assertion about the system.
// This is the atom of the memory system.
type KnowledgeClaim struct {
	ID             string          `json:"id"`
	Subject        string          `json:"subject"`   // component/node name
	Predicate      string          `json:"predicate"` // "uses", "was_added_because", "replaced"
	Object         string          `json:"object"`    // value or component name
	Evidence       evidence.Bundle `json:"evidence"`
	State          KnowledgeState  `json:"state"`
	ValidFrom      time.Time       `json:"valid_from"`
	ValidUntil     *time.Time      `json:"valid_until,omitempty"`
	FreshnessScore float64         `json:"freshness_score"` // 0.0–1.0 (decays over time)
}
