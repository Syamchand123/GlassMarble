package knowledge_aging

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

func docSourcedClaim(id, subject string, src evidence.Source) developer_memory.KnowledgeClaim {
	return developer_memory.KnowledgeClaim{
		ID:        id,
		Subject:   subject,
		Predicate: "was_decided_because",
		Object:    "lower session latency",
		State:     developer_memory.StateActive,
		Evidence: evidence.NewBundle(evidence.EvidenceItem{
			Source:     src,
			Reference:  "docs/adr/0014-redis.md",
			Confidence: 0.9,
		}),
	}
}

func snapWithComponents(names ...string) *archmodel.ArchSnapshot {
	comps := make([]archmodel.DetectedComponent, 0, len(names))
	for _, n := range names {
		comps = append(comps, archmodel.DetectedComponent{ID: n, Name: n, NodeIDs: []string{n + "::x"}})
	}
	return &archmodel.ArchSnapshot{Components: comps}
}

// TestDocumentClaimsAreNotMissingJustForBeingAbsentFromTheGraph pins the fix.
//
// An ADR claim's subject is a decision title ("Use Redis for session cache"),
// which is neither a component name nor a node ID, so its SubjectID is empty
// by construction. entityMissing treated an empty SubjectID as "absent from
// the graph", so every fused ADR, README, PR and issue claim was reported
// missing the moment it was created and projected to HISTORICAL — permanently
// ranking all fused knowledge as stale.
func TestDocumentClaimsAreNotMissingJustForBeingAbsentFromTheGraph(t *testing.T) {
	mem := &developer_memory.DeveloperMemory{
		GlobalMemory: []developer_memory.KnowledgeClaim{
			docSourcedClaim("adr-1", "Use Redis for session cache", evidence.SourceDocs),
			docSourcedClaim("pr-1", "Split billing into core and web", evidence.SourcePR),
			docSourcedClaim("issue-1", "Checkout times out under load", evidence.SourceIssue),
		},
	}
	missing := MissingEntityClaims(snapWithComponents("PaymentService"), mem)
	if len(missing) != 0 {
		t.Errorf("document-derived claims must not be reported missing for absence from the graph, got %v", missing)
	}
}

// TestCodeClaimsStillReportedMissing guards against over-correcting: a claim
// about a component that really did disappear must still be detected.
func TestCodeClaimsStillReportedMissing(t *testing.T) {
	mem := &developer_memory.DeveloperMemory{
		GlobalMemory: []developer_memory.KnowledgeClaim{
			{
				ID:        "code-1",
				Subject:   "RemovedService",
				Predicate: "depends_on",
				Object:    "PaymentService",
				State:     developer_memory.StateActive,
				Evidence: evidence.NewBundle(evidence.EvidenceItem{
					Source: evidence.SourceCode, Reference: "svc.go", Confidence: 0.9,
				}),
			},
		},
	}
	missing := MissingEntityClaims(snapWithComponents("PaymentService"), mem)
	if len(missing) != 1 {
		t.Errorf("a code claim about a vanished component should still be missing, got %v", missing)
	}
}

// TestDocumentClaimWithResolvedSubjectStillChecked: once knowledge fusion has
// resolved a document claim to a real node, the graph *can* answer the
// question, so a vanished node must still age it out.
func TestDocumentClaimWithResolvedSubjectStillChecked(t *testing.T) {
	c := docSourcedClaim("adr-2", "GoneService", evidence.SourceDocs)
	c.SubjectID = "gone::id"
	mem := &developer_memory.DeveloperMemory{GlobalMemory: []developer_memory.KnowledgeClaim{c}}

	missing := MissingEntityClaims(snapWithComponents("PaymentService"), mem)
	if len(missing) != 1 {
		t.Errorf("a document claim resolved to a missing node should still be reported, got %v", missing)
	}
}
