package developer_memory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// MemoryBuilder processes ArchEvents and updates the persistent developer
// memory. It is the developer memory ingestion entry point, called after component inference
// event generation (and, later, commit reasoning).
//
// IDEMPOTENCY: event IDs are deterministic (sha256 of commit + kind +
// affected ids, computed by the event producers). ProcessEvents appends an
// event to the WAL only if its ID is not already present, and Rebuild
// deduplicates by ID as a second line of defense. Re-running analysis on the
// same commit therefore never duplicates events, claims or timeline entries.
//
// VALIDATION: every event must carry a non-empty evidence bundle (empty
// evidence is a bug — master plan §3.5) and a non-zero timestamp. Violations
// abort the whole batch before anything is written, so partial application
// is impossible.
type MemoryBuilder struct {
	store     *MemoryStore
	projectID string
}

// BuilderOption customizes a MemoryBuilder.
type BuilderOption func(*MemoryBuilder)

// WithProjectID stamps the repository identifier onto the memory aggregate
// on first ingestion (sha256 of the absolute repo directory — master plan
// §1.5). The ID is only set when the aggregate has none, so it survives
// rebuilds and is never overwritten. ProjectID is also persisted durably in
// the sidecar file .glassmarble/memory/project.id so future Rebuilds can
// derive it without requiring WithProjectID; passing WithProjectID remains
// optional once the sidecar exists.
func WithProjectID(id string) BuilderOption {
	return func(b *MemoryBuilder) {
		b.projectID = id
	}
}

// NewMemoryBuilder creates a MemoryBuilder with default options.
func NewMemoryBuilder(store *MemoryStore) *MemoryBuilder {
	return NewMemoryBuilderWithOptions(store)
}

// NewMemoryBuilderWithOptions creates a MemoryBuilder with explicit options.
func NewMemoryBuilderWithOptions(store *MemoryStore, opts ...BuilderOption) *MemoryBuilder {
	b := &MemoryBuilder{store: store}
	for _, o := range opts {
		o(b)
	}
	return b
}

// ProcessEvents ingests new ArchEvents into the memory.
//
// The processing pipeline:
//
//  1. Validate every event (evidence, timestamp, id) — fail fast, apply nothing.
//  2. Append only the events whose ID is not already in the WAL.
//  3. Rebuild the memory aggregate from the WALs and persist memory.json +
//     timeline.json atomically.
//
// Returns the number of newly-appended events.
func (b *MemoryBuilder) ProcessEvents(events []archmodel.ArchEvent) (int, error) {
	if b.store == nil {
		return 0, fmt.Errorf("developer_memory: nil store")
	}
	for i := range events {
		if err := validateEvent(events[i]); err != nil {
			return 0, err
		}
	}

	existing, err := b.store.LoadEvents()
	if err != nil {
		return 0, fmt.Errorf("developer_memory: load event WAL: %w", err)
	}
	have := make(map[string]bool, len(existing))
	for _, e := range existing {
		have[e.ID] = true
	}

	appended := 0
	for i := range events {
		if have[events[i].ID] {
			continue
		}
		if err := b.store.AppendEvent(events[i]); err != nil {
			return appended, fmt.Errorf("developer_memory: append event %q: %w", events[i].ID, err)
		}
		have[events[i].ID] = true
		appended++
	}

	mem, err := b.store.Rebuild()
	if err != nil {
		return appended, fmt.Errorf("developer_memory: rebuild memory: %w", err)
	}
	if mem.ProjectID == "" && b.projectID != "" {
		mem.ProjectID = b.projectID
	}
	if err := b.store.SaveMemoryAndTimeline(mem); err != nil {
		return appended, fmt.Errorf("developer_memory: persist memory: %w", err)
	}
	return appended, nil
}

// validateEvent enforces the developer memory evidence discipline. An event without
// evidence, without an ID, or without a timestamp is rejected before any
// write happens.
func validateEvent(e archmodel.ArchEvent) error {
	if e.ID == "" {
		return fmt.Errorf("developer_memory: event has empty ID (cannot deduplicate)")
	}
	if e.Evidence.IsEmpty() {
		return fmt.Errorf("developer_memory: event %q has an empty evidence bundle (violates the evidence rule)", e.ID)
	}
	if e.Timestamp.IsZero() {
		return fmt.Errorf("developer_memory: event %q has a zero timestamp", e.ID)
	}
	return nil
}

// applyEvent folds one event into the memory aggregate. It is the single
// place where events become memory — Rebuild calls it for every event in the
// WAL, so this function is also what makes the aggregate reproducible.
func applyEvent(mem *DeveloperMemory, ev archmodel.ArchEvent) {
	if ev.Timestamp.After(mem.LastUpdated) {
		mem.LastUpdated = ev.Timestamp
	}
	mem.TotalEvents++

	// Claims are derived once and indexed by subject so each component's
	// history can carry its own claims. ComponentHistory.Claims is documented
	// as "claims whose subject is this component" but was never populated:
	// claims only ever reached mem.GlobalMemory, leaving every component with
	// an empty list. That silently emptied `gmb memory --component`, the
	// aging pass's claim projection and knowledge_aging's referencedBy scan.
	claims := claimsFromEvent(ev)
	claimsBySubject := make(map[string][]KnowledgeClaim, len(claims))
	for _, c := range claims {
		claimsBySubject[c.Subject] = append(claimsBySubject[c.Subject], c)
	}

	for _, comp := range ev.Components {
		history, ok := mem.ComponentMemory[comp]
		if !ok {
			history = ComponentHistory{Name: comp, State: StateUnknown}
		}
		if !containsString(history.Events, ev.ID) {
			history.Events = append(history.Events, ev.ID)
		}
		if history.FirstSeen.IsZero() || ev.Timestamp.Before(history.FirstSeen) {
			history.FirstSeen = ev.Timestamp
		}
		if ev.Timestamp.After(history.LastSeen) {
			history.LastSeen = ev.Timestamp
		}
		switch ev.Kind {
		case archmodel.EventServiceAdded:
			history.State = StateActive
		case archmodel.EventServiceRemoved:
			history.State = StateRemoved
		case archmodel.EventStateChanged:
			// knowledge aging aging transitions are replayable: the new state is
			// carried in the well-known "state=<STATE>" tag, so rebuilding
			// memory from the WAL reproduces aging states exactly.
			if s := archmodel.StateFromTags(ev.Tags); s != "" {
				history.State = KnowledgeState(s)
			}
		}
		for _, c := range claimsBySubject[comp] {
			if !containsClaimID(history.Claims, c.ID) {
				history.Claims = append(history.Claims, c)
			}
		}
		mem.ComponentMemory[comp] = history
	}

	for _, claim := range claims {
		mem.GlobalMemory = append(mem.GlobalMemory, claim)
	}

	mem.Timeline = append(mem.Timeline, timelineEntryFromEvent(ev))
}

// timelineEntryFromEvent derives the human-readable timeline row for an event.
func timelineEntryFromEvent(ev archmodel.ArchEvent) archmodel.TimelineEntry {
	return archmodel.TimelineEntry{
		Timestamp:   ev.Timestamp,
		CommitHash:  ev.CommitHash,
		Title:       ev.Title,
		Description: ev.Description,
		EventKind:   ev.Kind,
		Components:  ev.Components,
		Intent:      ev.Intent,
		Tags:        ev.Tags,
	}
}

// claimsFromEvent derives the knowledge claims for one event:
//
//   - one FACT claim per mentioned component, with a predicate derived from
//     the event kind (was_added / was_removed / depends_on / ...), and
//   - one reason claim when the event carries intent, classified by the
//     intent source: EXPLICIT_REASON for human-stated sources (git, pr,
//     docs, issue, user), INFERENCE for LLM/heuristic/rule extraction,
//     SPECULATION for unknown sources. If the event has no intent, no
//     reason claim is created — the memory never invents one.
func claimsFromEvent(ev archmodel.ArchEvent) []KnowledgeClaim {
	var claims []KnowledgeClaim

	predicate, removed := kindPredicate(ev.Kind)
	for _, comp := range ev.Components {
		object := claimObject(ev, comp)
		// STATE_CHANGE events assert "X state_changed_to <new state>": the
		// counterpart is the state carried in the well-known tag, not a
		// second component.
		if ev.Kind == archmodel.EventStateChanged {
			object = archmodel.StateFromTags(ev.Tags)
		}
		claim := newClaim(ev, comp, predicate, object, ClaimFact, removed)
		claims = append(claims, claim)
	}

	if ev.Intent != "" {
		subject := "architecture"
		if len(ev.Components) > 0 {
			subject = ev.Components[0]
		}
		kind := claimKindForSource(ev.IntentSrc)
		claim := newClaim(ev, subject, "was_changed_because", ev.Intent, kind, false)
		claims = append(claims, claim)
	}
	return claims
}

// kindPredicate maps an EventKind to the claim predicate it produces.
// Events whose kind has no meaningful predicate still produce a FACT claim
// ("involved_in_event") so the change is never lost from memory.
func kindPredicate(kind archmodel.EventKind) (predicate string, removed bool) {
	switch kind {
	case archmodel.EventServiceAdded:
		return "was_added", false
	case archmodel.EventServiceRemoved:
		return "was_removed", true
	case archmodel.EventServiceSplit:
		return "was_split", false
	case archmodel.EventServiceMerged:
		return "was_merged", false
	case archmodel.EventDependencyAdded:
		return "depends_on", false
	case archmodel.EventDependencyRemoved:
		return "no_longer_depends_on", true
	case archmodel.EventPatternDetected:
		return "pattern_detected", false
	case archmodel.EventPatternLost:
		return "pattern_lost", true
	case archmodel.EventSmellDetected:
		return "smell_introduced", false
	case archmodel.EventSmellResolved:
		return "smell_resolved", true
	case archmodel.EventCycleIntroduced:
		return "cycle_introduced", false
	case archmodel.EventCycleResolved:
		return "cycle_resolved", true
	case archmodel.EventCouplingIncreased:
		return "coupling_increased", false
	case archmodel.EventCouplingDecreased:
		return "coupling_decreased", false
	case archmodel.EventLayerViolation:
		return "layer_violation", false
	case archmodel.EventDeadCodeDetected:
		return "dead_code_detected", false
	case archmodel.EventCachingAdded:
		return "caching_added", false
	case archmodel.EventAsyncIntroduced:
		return "async_introduced", false
	case archmodel.EventDataStoreAdded:
		return "datastore_added", false
	case archmodel.EventAPIAdded:
		return "api_added", false
	case archmodel.EventSecurityAdded:
		return "security_added", false
	case archmodel.EventBoundaryCreated:
		return "boundary_created", false
	case archmodel.EventStateChanged:
		return "state_changed_to", false
	default:
		return "involved_in_event", false
	}
}

// claimObject picks the claim object for a component fact claim. Dependency
// events carry [source, target] components, so the object is the counterpart.
func claimObject(ev archmodel.ArchEvent, comp string) string {
	if len(ev.Components) > 1 {
		if ev.Components[0] == comp {
			return ev.Components[1]
		}
		if ev.Components[1] == comp {
			return ev.Components[0]
		}
	}
	if len(ev.AffectedIDs) > 0 {
		return ev.AffectedIDs[0]
	}
	return ""
}

// newClaim builds a claim with a deterministic ID, full provenance, and the
// event's evidence bundle. Removal-derived claims are stamped with
// ValidUntil = event time so the temporal window is explicit.
func newClaim(ev archmodel.ArchEvent, subject, predicate, object string, kind ClaimKind, removed bool) KnowledgeClaim {
	claim := KnowledgeClaim{
		ID:             claimID(ev.ID, subject, predicate, object),
		Subject:        subject,
		Predicate:      predicate,
		Object:         object,
		ClaimKind:      kind,
		Evidence:       ev.Evidence,
		State:          StateActive,
		ValidFrom:      ev.Timestamp,
		FreshnessScore: 1.0,
	}
	if removed {
		claim.State = StateRemoved
		until := ev.Timestamp
		claim.ValidUntil = &until
	}
	return claim
}

// claimKindForSource classifies a reason claim by where the intent came from.
// Human-stated sources are EXPLICIT_REASON; derived sources are INFERENCE;
// unknown sources are SPECULATION (never presented with the weight of fact).
func claimKindForSource(src evidence.Source) ClaimKind {
	switch src {
	case evidence.SourceGit, evidence.SourcePR, evidence.SourceDocs, evidence.SourceIssue, evidence.SourceUser:
		return ClaimExplicitReason
	case evidence.SourceLLM, evidence.SourceHeuristic, evidence.SourceRule:
		return ClaimInference
	default:
		return ClaimSpeculation
	}
}

// claimID derives a deterministic, stable claim ID so re-processing the same
// event can never create a duplicate claim.
func claimID(eventID, subject, predicate, object string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{eventID, subject, predicate, object}, "\x00")))
	return "claim_" + hex.EncodeToString(sum[:8])
}

// containsString reports whether v is present in the slice.
// containsClaimID reports whether a claim with the given ID is already
// attached, so replaying the WAL cannot duplicate a component's claims.
func containsClaimID(claims []KnowledgeClaim, id string) bool {
	for _, c := range claims {
		if c.ID == id {
			return true
		}
	}
	return false
}

func containsString(v []string, s string) bool {
	for _, item := range v {
		if item == s {
			return true
		}
	}
	return false
}

// sortTimeline sorts timeline entries oldest-first, tie-breaking on commit
// hash for full determinism.
func sortTimeline(entries []archmodel.TimelineEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if !entries[i].Timestamp.Equal(entries[j].Timestamp) {
			return entries[i].Timestamp.Before(entries[j].Timestamp)
		}
		return entries[i].CommitHash < entries[j].CommitHash
	})
}
