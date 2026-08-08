// Package developer_memory implements Stage 6 of the GlassMarble V2 pipeline:
// the Developer Memory Engine.
//
// WHAT THIS IS:
//
//	Stage 5 (arch_intelligence) tells us what the system IS today. Stage 6
//	tells us what the system WAS, what changed, and — where real evidence
//	exists — WHY it changed. It converts ephemeral ArchEvents (produced by
//	Stage 5D event generation and, later, Stage 8 commit reasoning) into a
//	persistent, queryable, reproducible project memory.
//
// CONCEPTUAL PIPELINE:
//
//	Facts → Observations → Events → Evolution → Memory
//
// ARCHITECTURE:
//
//   - MemoryStore   — persistence. Append-only JSONL write-ahead logs
//     (events.jsonl, claims.jsonl) are the source of truth;
//     memory.json and timeline.json are derived aggregates
//     rebuilt atomically from the logs. Corrupt lines are
//     skipped with a warning, never fatal.
//   - MemoryBuilder — idempotent ingestion. ProcessEvents validates (every
//     event must carry evidence — an empty bundle is a bug),
//     appends only events not already in the WAL, then
//     rebuilds the aggregates. Re-running analysis never
//     duplicates memory.
//   - query.go      — deterministic retrieval: tokenize + stopword filter,
//     rank by match quality × confidence × freshness, top-k.
//
// EVIDENCE DISCIPLINE (the single most important rule of this package):
//
//	Never invent historical reasons. Every claim carries a ClaimKind that
//	states how it was established:
//
//	  FACT            — directly observed from deterministic analysis
//	                    (e.g. "the dependency was added", from the graph diff)
//	  EXPLICIT_REASON — stated by a human in a commit/PR/issue/docs
//	                    (from ArchEvent.IntentSrc being a human source)
//	  INFERENCE       — derived by GlassMarble from surrounding evidence
//	                    (LLM or heuristic intent extraction)
//	  SPECULATION     — low-confidence guess, never presented as fact
//
//	If a commit says "added Redis because latency was 600ms", that is stored
//	as an EXPLICIT_REASON with the commit as evidence. If the reason is only
//	inferred, it is an INFERENCE with correspondingly lower confidence. If
//	there is no reason, no reason claim is created at all.
//
// KNOWLEDGE STATES (temporal validity):
//
//	CURRENT / DEPRECATED / REMOVED / HISTORICAL / EXPERIMENTAL / UNKNOWN.
//	Stage 6 assigns states deterministically from events (added → CURRENT,
//	removed → REMOVED, with ValidUntil stamped). Stage 11 (knowledge_aging)
//	owns the decay curves and DEPRECATED/HISTORICAL transitions. Historical
//	knowledge is never deleted — memory only grows.
//
// DEPENDENCY DIRECTION (strict, cycle-free):
//
//	developer_memory imports only evidence, archmodel and stdlib. No stage
//	package imports this package's internals; Stage 12 reads it through the
//	store/query APIs.
package developer_memory

import (
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// KnowledgeState describes the temporal status of a memory item.
// String values are persisted to JSON and must never change after first
// release — see the master implementation plan §1.5.
type KnowledgeState string

const (
	// StateActive marks knowledge that is currently true in the system.
	// Persisted as "CURRENT" (master plan §1.5).
	StateActive KnowledgeState = "CURRENT"

	// StateDeprecated marks knowledge that is no longer maintained but may
	// still be referenced elsewhere. Assigned by Stage 11 (knowledge aging).
	StateDeprecated KnowledgeState = "DEPRECATED"

	// StateRemoved marks a component/claim that no longer exists in the
	// graph. The historical record is preserved — nothing is deleted.
	StateRemoved KnowledgeState = "REMOVED"

	// StateHistorical marks knowledge that was true in the past and is
	// retained purely for context. Assigned by Stage 11.
	StateHistorical KnowledgeState = "HISTORICAL"

	// StateExperimental marks knowledge that is not yet confirmed.
	StateExperimental KnowledgeState = "EXPERIMENTAL"

	// StateUnknown is the fallback state when nothing has been observed.
	StateUnknown KnowledgeState = "UNKNOWN"
)

// ClaimKind classifies HOW a claim was established. This is the data-model
// expression of the "never invent historical reasons" rule: the reader can
// always tell a directly-observed fact from a human-stated reason from an
// inferred one, and can weight them accordingly.
type ClaimKind string

const (
	// ClaimFact marks a claim directly observed from deterministic analysis
	// (e.g. "PaymentService was_added", derived from the graph diff).
	ClaimFact ClaimKind = "FACT"

	// ClaimExplicitReason marks a reason explicitly stated by a human in a
	// commit message, PR description, issue, ADR or README. Highest-weight
	// reason kind; never second-guessed by later inference.
	ClaimExplicitReason ClaimKind = "EXPLICIT_REASON"

	// ClaimInference marks a reason derived by GlassMarble (LLM intent
	// extraction, name heuristics, rules). Lower confidence by construction.
	ClaimInference ClaimKind = "INFERENCE"

	// ClaimSpeculation marks a low-confidence guess. Never presented with
	// the weight of a fact or an explicit reason.
	ClaimSpeculation ClaimKind = "SPECULATION"
)

// DeveloperMemory is the persistent architectural memory for one project.
// It answers the question: "What do we know about this project, when did it
// change, and why?"
//
// This struct is the memory.json aggregate: it is derived from the
// append-only WALs (events.jsonl + claims.jsonl) by MemoryStore.Rebuild and
// can always be reconstructed from them.
type DeveloperMemory struct {
	// ProjectID is a stable identifier for the analyzed repository
	// (sha256 of the absolute repo directory). Set on first ingestion.
	ProjectID string `json:"project_id"`

	// LastUpdated is the timestamp of the most recent event in memory.
	LastUpdated time.Time `json:"last_updated"`

	// TotalEvents is the number of unique architectural events recorded.
	TotalEvents int `json:"total_events"`

	// Timeline holds the human-readable architectural evolution, oldest
	// first. Fully derivable from the event WAL.
	Timeline []archmodel.TimelineEntry `json:"timeline"`

	// ComponentMemory keys component names to their longitudinal history.
	ComponentMemory map[string]ComponentHistory `json:"component_memory"`

	// GlobalMemory holds every knowledge claim ever recorded (event-derived
	// facts, explicit reasons, and — in later stages — fused documentation
	// claims). Claims are never deleted, only state-marked.
	GlobalMemory []KnowledgeClaim `json:"global_memory"`

	// Events holds every unique event in memory, in WAL order. The events
	// WAL remains the source of truth; this slice is the aggregate view the
	// query layer (and Stage 12 evidence retrieval) reads from.
	Events []archmodel.ArchEvent `json:"events"`
}

// ComponentHistory stores the longitudinal history of one architectural
// component (service, module, bounded context, external dependency).
type ComponentHistory struct {
	Name string `json:"name"`

	// FirstSeen is the timestamp of the earliest event mentioning the
	// component.
	FirstSeen time.Time `json:"first_seen"`

	// LastSeen is the timestamp of the latest event mentioning the
	// component.
	LastSeen time.Time `json:"last_seen"`

	// State is the component's current temporal state (CURRENT, REMOVED, ...).
	State KnowledgeState `json:"state"`

	// Events lists the ArchEvent IDs that mention this component, in
	// ingestion order.
	Events []string `json:"event_ids"`

	// Claims lists the knowledge claims whose subject is this component.
	Claims []KnowledgeClaim `json:"claims"`
}

// KnowledgeClaim is the atom of the memory system: one factual or inferred
// assertion about the system. Every stored claim answers the WHAT / WHEN /
// WHY / SOURCE / CONFIDENCE / VALID FROM / VALID UNTIL / EVIDENCE questions
// that the master plan §5 (Core Data Model) requires.
type KnowledgeClaim struct {
	// ID is deterministic: sha256(eventID + subject + predicate + object).
	// Stable across rebuilds, which is what makes re-processing idempotent.
	ID string `json:"id"`

	// Subject is the component/node the claim is about. Always kept
	// human-readable — entity resolution results live in SubjectID.
	Subject string `json:"subject"`

	// SubjectID is the AKG node ID the subject resolved to (set by the
	// Stage 9 entity linker). Empty when the subject is not a graph
	// entity (e.g. a file that no longer maps to nodes, or the global
	// "architecture" subject). Additive field — persisted claims without
	// it remain valid.
	SubjectID string `json:"subject_id,omitempty"`

	// Predicate is the relationship/assertion ("was_added", "depends_on",
	// "was_added_because", "involves", ...).
	Predicate string `json:"predicate"`

	// Object is the value or counterpart ("PaymentService", "redis",
	// "payment latency exceeded 600ms", ...). Always kept human-readable;
	// the resolved node ID lives in ObjectID.
	Object string `json:"object"`

	// ObjectID is the AKG node ID the object resolved to (Stage 9 entity
	// linking). Empty when the object is not a graph entity.
	ObjectID string `json:"object_id,omitempty"`

	// ClaimKind classifies how this claim was established (FACT /
	// EXPLICIT_REASON / INFERENCE / SPECULATION). Never empty for
	// event-derived claims.
	ClaimKind ClaimKind `json:"claim_kind"`

	// Evidence is the full provenance chain backing this claim. Non-empty
	// for every claim this package produces.
	Evidence evidence.Bundle `json:"evidence"`

	// State is the temporal status (CURRENT / REMOVED / ...).
	State KnowledgeState `json:"state"`

	// ValidFrom is when the claim became true.
	ValidFrom time.Time `json:"valid_from"`

	// ValidUntil is when the claim stopped being true (e.g. the component
	// was removed); nil means still valid.
	ValidUntil *time.Time `json:"valid_until,omitempty"`

	// FreshnessScore is 0.0–1.0 and decays over time; Stage 11 owns the
	// decay curves. Initialized to 1.0 at creation.
	FreshnessScore float64 `json:"freshness_score"`
}
