package knowledge_aging

import (
	"github.com/Syamchand123/GlassMarble/internal/evidence"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// StaleEntity is re-exported for callers that want the type without
// importing archmodel; the canonical definition lives in archmodel so all
// phases share one type (archmodel.StaleEntity).
type StaleEntity = archmodel.StaleEntity

// DetectStaleEntities finds memory that is no longer backed by the current
// architecture snapshot.
//
// "Stale" means: a component (or the entity a claim refers to) is tracked
// in developer memory but is absent from the current snapshot. Presence is
// decided against the snapshot's detected components and pattern members.
// This is the deterministic input to the knowledge aging state transitions —
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
		if claim.Subject != "" && claimSubjectMissing(present, claim) {
			missing = append(missing, claim.ID)
			continue
		}
		// The object side gets the same treatment: a document claim's object
		// ("redis", "600ms latency") is prose too.
		if claim.Subject != nonEntitySubject && claim.Object != "" &&
			!(claim.ObjectID == "" && isDocumentSourced(claim)) &&
			entityMissing(present, claim.Object, claim.ObjectID) {
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

// claimSubjectMissing answers the same question for a claim, but only when the
// graph can actually answer it.
//
// A document-derived claim's subject is prose - an ADR decision title such as
// "Use Redis for session cache", or a PR summary - which is neither a
// component name nor a node ID, so its SubjectID is empty by construction.
// Treating that as "absent from the graph" projected every fused ADR, README,
// PR and issue claim to HISTORICAL the moment it was created, permanently
// ranking all fused knowledge as stale. The graph simply has no opinion about
// such a subject, so it cannot be evidence of removal.
func claimSubjectMissing(present presentEntities, claim developer_memory.KnowledgeClaim) bool {
	if claim.SubjectID == "" && isDocumentSourced(claim) {
		return false
	}
	return entityMissing(present, claim.Subject, claim.SubjectID)
}

// isDocumentSourced reports whether a claim came from prose rather than from
// the code graph.
func isDocumentSourced(claim developer_memory.KnowledgeClaim) bool {
	switch claim.Evidence.PrimarySource {
	case evidence.SourceDocs, evidence.SourcePR, evidence.SourceIssue:
		return true
	}
	for _, it := range claim.Evidence.Items {
		switch it.Source {
		case evidence.SourceDocs, evidence.SourcePR, evidence.SourceIssue:
			return true
		}
	}
	return false
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
		// Memory keys are canonical component IDs (component inference/commit reasoning events use
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
		return false
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
