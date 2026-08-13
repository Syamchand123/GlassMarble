package knowledge_aging

import (
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// StaleEntity is re-exported for callers that want the type without
// importing archmodel; the canonical definition lives in archmodel so all
// stages share one type (archmodel.StaleEntity).
type StaleEntity = archmodel.StaleEntity

// DetectStaleEntities finds memory that is no longer backed by the current
// architecture snapshot.
//
// "Stale" means: a component (or the entity a claim refers to) is tracked
// in developer memory but is absent from the current snapshot. Presence is
// decided against the snapshot's detected components and pattern members.
// This is the deterministic input to the Stage 11 state transitions —
// nothing here invents anything, every result names the snapshot it was
// checked against.
//
// A nil snapshot (no analysis has run yet, or snapshots are unavailable)
// yields no stale entities: absence of information is not absence of the
// component.
func DetectStaleEntities(
	currentSnap *archmodel.ArchSnapshot,
	memory *developer_memory.DeveloperMemory,
) []StaleEntity {
	if memory == nil || currentSnap == nil {
		return nil
	}

	present := indexPresentEntities(currentSnap)

	var stale []StaleEntity
	for compName, history := range memory.ComponentMemory {
		if history.State != developer_memory.StateActive && history.State != developer_memory.StateExperimental {
			continue
		}
		if !present.hasComponent(compName) {
			stale = append(stale, StaleEntity{
				Name:     compName,
				LastSeen: history.LastSeen,
				Reason:   "component no longer detected in current graph",
			})
		}
	}
	return stale
}

// MissingEntityClaims returns the IDs of claims whose subject or object
// entity is no longer present in the current snapshot. Such claims describe
// knowledge about entities that have disappeared from the graph; the aging
// projection marks them historical so they keep their provenance but stop
// ranking as current knowledge.
//
// A claim is tied to an entity by name or by node ID: the subject/object
// counts as present when it matches a detected component name or pattern
// member, or when its asserted node ID (SubjectID / ObjectID) appears in a
// detected component's node IDs. A claim naming an entity that matches
// neither is missing. Intent claims whose subject is the conventional
// "architecture" pseudo-entity are never flagged — their object is the
// intent text, not a named entity.
func MissingEntityClaims(
	currentSnap *archmodel.ArchSnapshot,
	memory *developer_memory.DeveloperMemory,
) []string {
	if memory == nil || currentSnap == nil {
		return nil
	}
	present := indexPresentEntities(currentSnap)

	var missing []string
	for _, claim := range memory.GlobalMemory {
		if claim.Subject != "" && entityMissing(present, claim.Subject, claim.SubjectID) {
			missing = append(missing, claim.ID)
			continue
		}
		if claim.Subject != nonEntitySubject && claim.Object != "" && entityMissing(present, claim.Object, claim.ObjectID) {
			missing = append(missing, claim.ID)
		}
	}
	return missing
}

// nonEntitySubject is the well-known pseudo-subject the memory builder uses
// for intent claims ("the architecture was changed because ..."). It is a
// statement about the whole system, not a named entity, so it can never be
// "missing".
const nonEntitySubject = "architecture"

// entityMissing decides whether a named entity with an optional node ID is
// absent from the graph: the name is not detected, and either no node ID is
// asserted (pure name reference) or the asserted node ID is also absent.
func entityMissing(present presentEntities, name, nodeID string) bool {
	if name == nonEntitySubject || present.hasComponent(name) {
		return false
	}
	return nodeID == "" || !present.hasNode(nodeID)
}

// presentEntities indexes the snapshot's component names and node IDs for
// O(1) presence checks.
type presentEntities struct {
	components map[string]struct{}
	nodes      map[string]struct{}
}

func indexPresentEntities(snap *archmodel.ArchSnapshot) presentEntities {
	p := presentEntities{
		components: make(map[string]struct{}),
		nodes:      make(map[string]struct{}),
	}
	if snap == nil {
		return p
	}
	for _, comp := range snap.Components {
		if comp.Name != "" {
			p.components[comp.Name] = struct{}{}
		}
		// Memory keys are canonical component IDs (Stage 5D/8 events use
		// them for every kind); index the ID too so ID-keyed memory is
		// never mis-flagged as stale or missing.
		if comp.ID != "" {
			p.components[comp.ID] = struct{}{}
		}
		for _, nid := range comp.NodeIDs {
			if nid != "" {
				p.nodes[nid] = struct{}{}
			}
		}
	}
	for _, pat := range snap.Patterns {
		for _, pc := range pat.Components {
			if pc != "" {
				p.components[pc] = struct{}{}
			}
		}
	}
	return p
}

func (p presentEntities) hasComponent(name string) bool {
	if name == "" {
		return false
	}
	_, ok := p.components[name]
	return ok
}

func (p presentEntities) hasNode(id string) bool {
	if id == "" {
		return true // no node ID asserted → not a resolvable reference
	}
	_, ok := p.nodes[id]
	return ok
}

// snapshotHasComponent is a convenience wrapper for callers that only need
// a boolean presence check (kept for the archmodel.StaleEntity contract
// used by the CLI reports).
func snapshotHasComponent(snap *archmodel.ArchSnapshot, compName string) bool {
	if snap == nil {
		return false
	}
	return indexPresentEntities(snap).hasComponent(compName)
}

// staleSince returns the reference timestamp for "when did the knowledge
// about this component stop being observed". Zero when unknown.
func staleSince(se StaleEntity) time.Time {
	return se.LastSeen
}
