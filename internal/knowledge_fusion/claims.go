package knowledge_fusion

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// fusedClaimID derives the deterministic, stable ID for a fused claim.
//
// The ID is a content hash over everything that makes the claim distinct:
// the source kind and reference (so the same statement from two different
// ADRs gets two different claims), the subject/predicate/object text, and
// the resolved entity IDs. The resolved IDs are part of the hash so that
// entity-linker expansions (one claim per AKG node in a file) get distinct
// IDs — otherwise the memory store's ID dedup would silently drop all but
// the first expansion.
//
// Deterministic across runs and machines: same sources, same graph → same
// claim IDs → re-running fusion appends nothing.
func fusedClaimID(kind, reference, subject, predicate, object, subjectID, objectID string) string {
	sum := sha256.Sum256([]byte(strings.Join(
		[]string{"fused", kind, reference, subject, predicate, object, subjectID, objectID},
		"\x00",
	)))
	return "claim_" + hex.EncodeToString(sum[:16])
}

// newFusedClaim builds a knowledge claim with full provenance discipline:
//
//   - deterministic ID (see fusedClaimID),
//   - ClaimKind and State passed by the caller,
//   - ValidFrom taken from the source's real timestamp (doc mtime, commit
//     author time, ...) — never time.Now(),
//   - FreshnessScore initialized to 1.0 (knowledge aging owns decay),
//   - SubjectID/ObjectID populated when the entity linker resolved them.
//
// subjectID and objectID are optional ("" when unresolved); the subject and
// object TEXT are always preserved human-readable.
func newFusedClaim(kind, reference, subject, predicate, object string,
	claimKind developer_memory.ClaimKind, state developer_memory.KnowledgeState,
	validFrom time.Time, bundle evidence.Bundle,
	subjectID, objectID string) developer_memory.KnowledgeClaim {
	return developer_memory.KnowledgeClaim{
		ID:             fusedClaimID(kind, reference, subject, predicate, object, subjectID, objectID),
		Subject:        subject,
		SubjectID:      subjectID,
		Predicate:      predicate,
		Object:         object,
		ObjectID:       objectID,
		ClaimKind:      claimKind,
		Evidence:       bundle,
		State:          state,
		ValidFrom:      validFrom,
		FreshnessScore: 1.0,
	}
}

// evidenceReference returns the first evidence item's reference. Claim IDs
// incorporate it so expansions derived from different source documents stay
// distinct.
func evidenceReference(b evidence.Bundle) string {
	if len(b.Items) == 0 {
		return ""
	}
	return b.Items[0].Reference
}

// validateFusedClaim enforces the same evidence discipline the developer memory
// memory builder applies to event-derived claims. A claim without an ID,
// without evidence, or without a timestamp cannot be persisted.
func validateFusedClaim(c developer_memory.KnowledgeClaim) error {
	if c.ID == "" {
		return fmt.Errorf("claim has empty ID (cannot deduplicate)")
	}
	if c.Subject == "" {
		return fmt.Errorf("claim %q has an empty subject", c.ID)
	}
	if c.Predicate == "" {
		return fmt.Errorf("claim %q has an empty predicate", c.ID)
	}
	if c.Evidence.IsEmpty() {
		return fmt.Errorf("claim %q has an empty evidence bundle (violates the evidence rule)", c.ID)
	}
	if c.ValidFrom.IsZero() {
		return fmt.Errorf("claim %q has a zero valid_from (provenance must carry a real source timestamp)", c.ID)
	}
	if c.ClaimKind == "" {
		return fmt.Errorf("claim %q has an empty claim kind", c.ID)
	}
	if c.State == "" {
		return fmt.Errorf("claim %q has an empty knowledge state", c.ID)
	}
	return nil
}
