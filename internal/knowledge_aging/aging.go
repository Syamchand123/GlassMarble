// Package knowledge_aging implements knowledge aging of the GlassMarble V2
// pipeline: Knowledge Aging (master plan §9).
//
// WHAT THIS IS:
//
//	Architecture knowledge has a shelf life. Something true 18 months ago
//	may be irrelevant or wrong today. This package assigns freshness scores
//	to every knowledge claim and moves memory items through temporal states
//	(CURRENT / DEPRECATED / REMOVED / HISTORICAL / EXPERIMENTAL / UNKNOWN)
//	deterministically, without ever deleting history.
//
// HOW IT WORKS:
//
//	- scorer.go      — FreshnessScore: a PURE function of the claim and
//	                   the clock. Exponential decay with a half-life that
//	                   depends on how the claim was established (code 365d,
//	                   docs 270d, git 180d, LLM 90d). 0.0 for REMOVED or
//	                   past-ValidUntil claims.
//	- detector.go    — DetectStaleEntities: memory entities absent from the
//	                   current architecture snapshot, and
//	                   MissingEntityClaims: claims whose subject/object
//	                   entity vanished from the snapshot (reuses
//	                   archmodel.StaleEntity).
//	- transitions.go — the deterministic state-transition rules. Presence
//	                   in the graph drives transitions; a configurable
//	                   stale-grace period guards against transient absence
//	                   (plan §9.3's "two consecutive snapshots", expressed
//	                   in time); a deprecated component that reappears in
//	                   the graph is restored to CURRENT. Freshness only
//	                   decays claim ranking. Every decision names its rule
//	                   and its concrete evidence.
//	- aging.go       — Ager: orchestrates one aging pass and persists it.
//	                   State transitions are appended to events.jsonl as
//	                   STATE_CHANGE events (with SourceRule evidence), so
//	                   a rebuild of memory from the WAL reproduces aging
//	                   states exactly. The persisted aggregate carries the
//	                   WAL-derived states plus a freshness stamp; the
//	                   temporal-state projection of claims is applied at
//	                   read time (FreshenMemory / FreshenMemoryWithSnapshot).
//
// DESIGN RULES:
//
//	- Never destroy knowledge: transitions only move states; nothing is
//	  deleted. REMOVED / HISTORICAL are terminal for aging — restoring a
//	  component is a user correction (convention learning) or a new SERVICE_ADDED
//	  event (developer memory), never a silent revert.
//	- Deterministic first: every rule is a pure function of the snapshot,
//	  the memory and the clock. No LLM anywhere in this package.
//	- Corrections win (convention learning integration): components whose state the
//	  developer pinned via a STATE correction are exempt from aging
//	  transitions (WithPinnedStates) — the user's state is authoritative
//	  until a compensating correction.
//	- Freshness is derived, never stored as truth: the pure scorer is the
//	  source; the persisted aggregate keeps a stamp of it for JSON
//	  consumers, and queries recompute it live via FreshenMemory.
//
// DEPENDENCY DIRECTION (strict, cycle-free):
//
//	knowledge_aging imports config, archmodel, developer_memory and
//	evidence. Nothing imports this package from the leaf packages; the CLI
//	(cmd/) wires it into the pipeline and into read-time projections, and
//	is responsible for passing convention learning's STATE corrections as pins.
package knowledge_aging

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// Ager applies freshness decay and knowledge-state transitions to the
// developer memory and persists the result. It is store-backed: transitions
// are appended to the memory WAL as STATE_CHANGE events (replayable), and
// the freshened aggregate is saved atomically.
//
// Pinned states: components whose state the developer explicitly corrected
// (convention learning STATE corrections) must be passed via WithPinnedStates. Aging
// never transitions a pinned component — the user's state is authoritative
// until a compensating correction. The CLI (composition root) is
// responsible for deriving the pins from the correction log, so this
// package stays decoupled from the learning layer.
type Ager struct {
	store *developer_memory.MemoryStore
	cfg   *config.AgingConfig
	pins  map[string]developer_memory.KnowledgeState
	logf  func(format string, args ...any)
}

// AgerOption customizes an Ager.
type AgerOption func(*Ager)

// WithConfig uses explicit aging configuration (defaults when nil).
func WithConfig(cfg *config.AgingConfig) AgerOption {
	return func(a *Ager) {
		a.cfg = cfg
	}
}

// WithPinnedStates marks component states as authoritative user corrections
// (derived from convention learning STATE corrections by the caller). Pinned components
// are exempt from every aging transition, so aging can never fight a
// developer's explicit state correction. The map value is informational
// (used for logging); presence in the map is what pins the component.
func WithPinnedStates(pins map[string]developer_memory.KnowledgeState) AgerOption {
	return func(a *Ager) {
		if pins != nil {
			a.pins = pins
		}
	}
}

// WithLogger attaches a warning sink for non-fatal conditions.
func WithLogger(logf func(format string, args ...any)) AgerOption {
	return func(a *Ager) {
		a.logf = logf
	}
}

// NewAger creates an Ager over the given memory store with default aging
// configuration.
func NewAger(store *developer_memory.MemoryStore, opts ...AgerOption) *Ager {
	a := &Ager{
		store: store,
		cfg:   config.DefaultAgingConfig(),
		pins:  map[string]developer_memory.KnowledgeState{},
		logf:  func(string, ...any) {},
	}
	for _, o := range opts {
		o(a)
	}
	if a.cfg == nil {
		a.cfg = config.DefaultAgingConfig()
	}
	a.cfg.ApplyDefaults()
	return a
}

// PinnedStates returns the currently pinned components (component name →
// pinned state). Read-only copy semantics: the caller must not modify the
// returned map if it intends to share it.
func (a *Ager) PinnedStates() map[string]developer_memory.KnowledgeState {
	out := make(map[string]developer_memory.KnowledgeState, len(a.pins))
	for k, v := range a.pins {
		out[k] = v
	}
	return out
}

// warn reports a non-fatal condition through the warning sink.
func (a *Ager) warn(format string, args ...any) {
	if a.logf != nil {
		a.logf(format, args...)
	}
}

// TransitionResult captures one knowledge-state transition decided by an
// aging pass. The canonical record is the appended STATE_CHANGE event; this
// struct is the in-run report (what changed, by which rule, and the event
// that records it).
type TransitionResult struct {
	Component string
	OldState  developer_memory.KnowledgeState
	NewState  developer_memory.KnowledgeState
	RuleID    string
	Reason    string
	Event     *archmodel.ArchEvent
}

// Age runs one aging pass at time now against the current architecture
// snapshot (nil when no snapshot exists yet):
//
//  1. refresh every claim's freshness score in memory,
//  2. detect entities that vanished from the graph,
//  3. apply the deterministic state-transition rules (skipping components
//     pinned by convention learning STATE corrections),
//  4. append STATE_CHANGE events to the memory WAL (deduplicated by ID),
//  5. rebuild the aggregate so the transitions are applied and save
//     memory.json + timeline.json atomically with a freshness stamp.
//
// The persisted aggregate keeps the WAL-derived states (component states
// come from the replayable events; claim states are never projected into
// the store). Temporal-state projection of claims happens at read time via
// FreshenMemory / FreshenMemoryWithSnapshot, so a persisted projection can
// never poison later re-derivation.
//
// Idempotent by construction: a transition only fires when the component's
// state actually changes, so re-running Age on unchanged memory appends
// nothing and rewrites the aggregate with the same values.
func (a *Ager) Age(snap *archmodel.ArchSnapshot, now time.Time) ([]TransitionResult, error) {
	if a.store == nil {
		return nil, fmt.Errorf("knowledge_aging: nil store")
	}
	if !a.cfg.AgingEnabled() {
		return nil, nil
	}

	mem, err := a.store.LoadMemory()
	if err != nil {
		return nil, fmt.Errorf("knowledge_aging: load memory: %w", err)
	}
	if mem == nil || (len(mem.ComponentMemory) == 0 && len(mem.GlobalMemory) == 0) {
		return nil, nil // nothing to age
	}

	staleList := DetectStaleEntities(snap, mem)
	staleMap := make(map[string]StaleEntity, len(staleList))
	for _, se := range staleList {
		staleMap[se.Name] = se
	}

	existing, err := a.store.LoadEvents()
	if err != nil {
		return nil, fmt.Errorf("knowledge_aging: load event WAL: %w", err)
	}
	have := make(map[string]bool, len(existing))
	for _, e := range existing {
		have[e.ID] = true
	}

	var results []TransitionResult
	var pending []archmodel.ArchEvent
	for compName, history := range mem.ComponentMemory {
		if _, pinned := a.pins[compName]; pinned {
			// convention learning STATE corrections win over aging: the developer
			// owns this component's temporal state.
			continue
		}
		dec := determineNextState(compName, history, staleMap, mem, snap, a.cfg, now)
		if dec.newState == "" || dec.newState == history.State {
			continue
		}
		ev := a.transitionEvent(compName, history.State, dec, now)
		results = append(results, TransitionResult{
			Component: compName,
			OldState:  history.State,
			NewState:  dec.newState,
			RuleID:    dec.ruleID,
			Reason:    dec.reason,
			Event:     &ev,
		})
		if !have[ev.ID] {
			pending = append(pending, ev)
			have[ev.ID] = true
		}
	}

	if len(pending) > 0 {
		for _, ev := range pending {
			if err := a.store.AppendEvent(ev); err != nil {
				a.warn("knowledge_aging: append transition event %q: %v", ev.ID, err)
			}
		}
		// Rebuild so the new states are folded into the aggregate.
		mem, err = a.store.Rebuild()
		if err != nil {
			return results, fmt.Errorf("knowledge_aging: rebuild after transitions: %w", err)
		}
	}

	stamped := stampFreshness(mem, now, a.cfg)
	if err := a.store.SaveMemoryAndTimeline(stamped); err != nil {
		return results, fmt.Errorf("knowledge_aging: persist freshened memory: %w", err)
	}
	return results, nil
}

// transitionEvent builds the STATE_CHANGE event that records one state
// transition. The ID is deterministic (component + old state + new state +
// timestamp), the new state is carried in the well-known "state=<STATE>"
// tag, and the evidence names the rule that decided it.
func (a *Ager) transitionEvent(comp string, old developer_memory.KnowledgeState, dec transitionDecision, now time.Time) archmodel.ArchEvent {
	sum := sha256.Sum256([]byte(comp + "\x00" + string(old) + "\x00" + string(dec.newState) + "\x00" + now.Format(time.RFC3339Nano)))
	ev := archmodel.ArchEvent{
		ID:        "aging_" + hex.EncodeToString(sum[:8]),
		Kind:      archmodel.EventStateChanged,
		Timestamp: now,
		Title:     fmt.Sprintf("State change: %s → %s", comp, dec.newState),
		Description: dec.reason,
		Components:  []string{comp},
		Tags:        []string{"aging", archmodel.StateTag(string(dec.newState))},
		Evidence: evidence.NewBundle(evidence.EvidenceItem{
			Source:     evidence.SourceRule,
			Reference:  dec.ruleID,
			Excerpt:    dec.reason,
			Confidence: 1.0,
			Timestamp:  now,
		}),
	}
	return ev
}

// FreshenMemory is the read-time temporal projection of the memory
// aggregate. It returns a copy in which:
//
//   - every claim's FreshnessScore is recomputed from the clock (pure),
//   - every ACTIVE / EXPERIMENTAL claim whose subject component is
//     DEPRECATED / REMOVED / HISTORICAL inherits that state, so knowledge
//     about entities that have aged out stops ranking as current without
//     losing its provenance.
//
// The source aggregate is never mutated. Input claim states are expected to
// be WAL-derived (memory.json now stores them unprojected), so this
// projection is the single place where claim states are derived and can
// never double-apply. Use FreshenMemoryWithSnapshot when the current
// architecture snapshot is available: it additionally marks claims about
// entities that vanished from the graph as HISTORICAL.
func FreshenMemory(mem *developer_memory.DeveloperMemory, now time.Time, cfg *config.AgingConfig) *developer_memory.DeveloperMemory {
	return freshenMemory(mem, now, cfg, nil)
}

// FreshenMemoryWithSnapshot is FreshenMemory plus the missing-entity
// projection: claims whose subject or object entity is no longer present in
// the snapshot are marked HISTORICAL (they keep their provenance but stop
// ranking as current knowledge). A nil snapshot behaves exactly like
// FreshenMemory — absence of information is not absence of the entity.
func FreshenMemoryWithSnapshot(mem *developer_memory.DeveloperMemory, snap *archmodel.ArchSnapshot, now time.Time, cfg *config.AgingConfig) *developer_memory.DeveloperMemory {
	if mem == nil {
		return nil
	}
	missing := make(map[string]bool)
	for _, id := range MissingEntityClaims(snap, mem) {
		missing[id] = true
	}
	return freshenMemory(mem, now, cfg, missing)
}

// stampFreshness returns a deep copy of the aggregate with every claim's
// FreshnessScore recomputed and NO state projection applied. This is what
// the aging pass persists: component states come from the replayable WAL
// events, claim states stay WAL-derived, and the freshness stamp serves JSON
// consumers. The temporal claim-state projection is applied at read time
// (FreshenMemory), never stored.
func stampFreshness(mem *developer_memory.DeveloperMemory, now time.Time, cfg *config.AgingConfig) *developer_memory.DeveloperMemory {
	if mem == nil {
		return nil
	}
	if cfg == nil {
		cfg = config.DefaultAgingConfig()
	}
	cfg.ApplyDefaults()

	proj := &developer_memory.DeveloperMemory{
		ProjectID:       mem.ProjectID,
		LastUpdated:     mem.LastUpdated,
		TotalEvents:     mem.TotalEvents,
		Timeline:        append([]archmodel.TimelineEntry(nil), mem.Timeline...),
		ComponentMemory: make(map[string]developer_memory.ComponentHistory, len(mem.ComponentMemory)),
		GlobalMemory:    make([]developer_memory.KnowledgeClaim, len(mem.GlobalMemory)),
		Events:          append([]archmodel.ArchEvent(nil), mem.Events...),
	}

	for i, claim := range mem.GlobalMemory {
		claim.FreshnessScore = FreshnessScoreWithConfig(claim, now, cfg)
		proj.GlobalMemory[i] = claim
	}

	for name, history := range mem.ComponentMemory {
		hc := history
		hc.Claims = make([]developer_memory.KnowledgeClaim, len(history.Claims))
		for i, claim := range history.Claims {
			claim.FreshnessScore = FreshnessScoreWithConfig(claim, now, cfg)
			hc.Claims[i] = claim
		}
		proj.ComponentMemory[name] = hc
	}
	return proj
}

// freshenMemory applies the full read-time projection: freshness recompute,
// subject-state inheritance and (when supplied) missing-entity marking.
func freshenMemory(mem *developer_memory.DeveloperMemory, now time.Time, cfg *config.AgingConfig, missing map[string]bool) *developer_memory.DeveloperMemory {
	if mem == nil {
		return nil
	}
	if cfg == nil {
		cfg = config.DefaultAgingConfig()
	}
	cfg.ApplyDefaults()

	proj := &developer_memory.DeveloperMemory{
		ProjectID:       mem.ProjectID,
		LastUpdated:     mem.LastUpdated,
		TotalEvents:     mem.TotalEvents,
		Timeline:        append([]archmodel.TimelineEntry(nil), mem.Timeline...),
		ComponentMemory: make(map[string]developer_memory.ComponentHistory, len(mem.ComponentMemory)),
		GlobalMemory:    make([]developer_memory.KnowledgeClaim, len(mem.GlobalMemory)),
		Events:          append([]archmodel.ArchEvent(nil), mem.Events...),
	}

	for i, claim := range mem.GlobalMemory {
		proj.GlobalMemory[i] = freshenClaim(claim, componentState(mem, claim.Subject), missing[claim.ID], now, cfg)
	}

	for name, history := range mem.ComponentMemory {
		hc := history
		hc.Claims = make([]developer_memory.KnowledgeClaim, len(history.Claims))
		for i, claim := range history.Claims {
			hc.Claims[i] = freshenClaim(claim, history.State, missing[claim.ID], now, cfg)
		}
		proj.ComponentMemory[name] = hc
	}
	return proj
}

// freshenClaim recomputes a claim's freshness and applies the temporal-state
// projection. The state is projected FIRST so the score is consistent with
// it: knowledge about an aged-out entity is both marked and scored as not
// fresh. See projectedClaimState for the exact state derivation rules.
func freshenClaim(claim developer_memory.KnowledgeClaim, subjectState developer_memory.KnowledgeState, missing bool, now time.Time, cfg *config.AgingConfig) developer_memory.KnowledgeClaim {
	claim.State = projectedClaimState(claim, subjectState, missing)
	claim.FreshnessScore = FreshnessScoreWithConfig(claim, now, cfg)
	return claim
}

// projectedClaimState derives the display state of one claim from its
// persisted (WAL-derived) state, the temporal state of its subject
// component and whether its entity is absent from the current snapshot.
//
// Rules:
//
//   - Claims that were authoritatively closed — REMOVED, or carrying a
//     ValidUntil (removal events stamp it) — keep their state: aging never
//     reopens explicit closure.
//   - ACTIVE / EXPERIMENTAL claims about a vanished entity are projected
//     HISTORICAL (missing-entity marking) — they keep their provenance but
//     stop ranking as current.
//   - ACTIVE / EXPERIMENTAL claims inherit the subject component's
//     temporal state (DEPRECATED / REMOVED / HISTORICAL), so knowledge
//     about aged-out components stops ranking as current.
//   - Every other persisted state (DEPRECATED from ADR status,
//     HISTORICAL from knowledge fusion conflict resolution, UNKNOWN) is displayed
//     as stored — those states carry their own meaning and are never
//     overwritten by the projection.
//
// The projection is downgrade-only by design: claim states close
// monotonically (aging, removal, supersession) and are reopened only by
// new events or user corrections, never by a projection.
func projectedClaimState(claim developer_memory.KnowledgeClaim, subjectState developer_memory.KnowledgeState, missing bool) developer_memory.KnowledgeState {
	if claim.ValidUntil != nil || claim.State == developer_memory.StateRemoved {
		return claim.State
	}
	switch claim.State {
	case developer_memory.StateActive, developer_memory.StateExperimental:
		if missing {
			return developer_memory.StateHistorical
		}
		switch subjectState {
		case developer_memory.StateDeprecated, developer_memory.StateRemoved, developer_memory.StateHistorical:
			return subjectState
		}
	}
	return claim.State
}

// componentState returns the temporal state of the named component in the
// aggregate ("" when unknown — the claim keeps its own state).
func componentState(mem *developer_memory.DeveloperMemory, name string) developer_memory.KnowledgeState {
	if h, ok := mem.ComponentMemory[name]; ok {
		return h.State
	}
	return ""
}
