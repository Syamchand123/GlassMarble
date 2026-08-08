package knowledge_fusion

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// testClaim builds a minimal doc-derived claim (subject = decision title).
func testClaim(id, subject, object string, ts time.Time) developer_memory.KnowledgeClaim {
	return developer_memory.KnowledgeClaim{
		ID:             id,
		Subject:        subject,
		Predicate:      "decided_to",
		Object:         object,
		ClaimKind:      developer_memory.ClaimExplicitReason,
		State:          developer_memory.StateActive,
		ValidFrom:      ts,
		Evidence:       evidenceFor(ts),
		FreshnessScore: 1.0,
	}
}

// testGraph builds a CodePropertyGraph with the given nodes (name → id) and
// a FileNodeIndex entry for file mapping to those node IDs.
func testGraph(t *testing.T, file string, names map[string]string) *akg.CodePropertyGraph {
	t.Helper()
	g := akg.NewCodePropertyGraph("test")
	nodeIDs := make(map[string]bool, len(names))
	for name, id := range names {
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{
			ID:       id,
			Kind:     "MODULE",
			Name:     name,
			FileSpec: stage4.LocationMeta{Path: file},
		})
		nodeIDs[id] = true
	}
	if file != "" {
		g.FileNodeIndex = g.FileNodeIndex.Set(file, nodeIDs)
	}
	return g
}

func TestLinkDocumentClaimsToAKG_TextPreserved(t *testing.T) {
	ts := time.Now().UTC()
	g := testGraph(t, "services/user-service/main.go", map[string]string{"UserService": "mod::user-service"})

	claim := testClaim("c1", "Use UserService", "UserService", ts)
	linked := LinkDocumentClaimsToAKG([]developer_memory.KnowledgeClaim{claim}, g)
	if len(linked) != 1 {
		t.Fatalf("got %d claims, want 1", len(linked))
	}
	got := linked[0]
	// Text is never rewritten by linking.
	if got.Subject != "Use UserService" || got.Object != "UserService" {
		t.Errorf("text mutated: subject=%q object=%q", got.Subject, got.Object)
	}
	// The object resolved against the graph (case-insensitive name match).
	if got.ObjectID != "mod::user-service" {
		t.Errorf("object_id = %q, want mod::user-service", got.ObjectID)
	}
}

func TestLinkDocumentClaimsToAKG_FileExpansion(t *testing.T) {
	ts := time.Now().UTC()
	g := testGraph(t, "services/user-service/main.go", map[string]string{
		"UserService": "mod::user-service",
		"UserCache":   "mod::user-cache",
	})

	// A PR claim whose subject is a file path expands to one claim per AKG
	// node defined in that file.
	claim := developer_memory.KnowledgeClaim{
		ID:        "pr-claim",
		Subject:   "services/user-service/main.go",
		Predicate: "was_modified_by_pr",
		Object:    "PR 42",
		ClaimKind: developer_memory.ClaimFact,
		State:     developer_memory.StateActive,
		ValidFrom: ts,
		Evidence:  evidenceFor(ts),
	}
	linked := LinkDocumentClaimsToAKG([]developer_memory.KnowledgeClaim{claim}, g)
	if len(linked) != 2 {
		t.Fatalf("got %d claims, want 2 expansions", len(linked))
	}

	ids := []string{linked[0].SubjectID, linked[1].SubjectID}
	if ids[0] == ids[1] {
		t.Errorf("expansions share a subject_id: %v", ids)
	}
	for _, c := range linked {
		// Subject text stays the file path (queryability contract).
		if c.Subject != "services/user-service/main.go" {
			t.Errorf("subject = %q, want file path preserved", c.Subject)
		}
		if c.ID == "pr-claim" {
			t.Error("expansion reused the source claim ID")
		}
		// Only the first (original) expansion is marked as an inference;
		// both carry the same evidence, none fabricated.
		if c.Evidence.IsEmpty() {
			t.Error("expansion lost its evidence")
		}
	}
}

func TestLinkDocumentClaimsToAKG_UnknownFileKept(t *testing.T) {
	ts := time.Now().UTC()
	g := testGraph(t, "services/user-service/main.go", map[string]string{"UserService": "mod::user-service"})

	claim := testClaim("c1", "deleted/old.go", "PR 7", ts)
	linked := LinkDocumentClaimsToAKG([]developer_memory.KnowledgeClaim{claim}, g)
	if len(linked) != 1 {
		t.Fatalf("got %d claims, want 1 (unknown file never dropped)", len(linked))
	}
	if linked[0].ID != "c1" {
		t.Errorf("claim ID changed to %q, want c1", linked[0].ID)
	}
	if linked[0].SubjectID != "" {
		t.Errorf("subject_id = %q, want empty for unknown file", linked[0].SubjectID)
	}
}

func TestLinkDocumentClaimsToAKG_GlobalSubjectNotExpanded(t *testing.T) {
	ts := time.Now().UTC()
	// The graph deliberately contains an "architecture" node AND a file
	// named architecture — the global subject must still never expand.
	g := testGraph(t, "architecture", map[string]string{"architecture": "mod::architecture"})

	claim := testClaim("c1", "architecture", "Redis", ts)
	linked := LinkDocumentClaimsToAKG([]developer_memory.KnowledgeClaim{claim}, g)
	if len(linked) != 1 {
		t.Fatalf("got %d claims, want 1 (architecture never expands)", len(linked))
	}
	if linked[0].SubjectID != "mod::architecture" {
		t.Errorf("subject_id = %q, want the name-resolved node", linked[0].SubjectID)
	}
	if linked[0].ID != "c1" {
		t.Error("global-subject claim was rewritten")
	}
}

func TestLinkDocumentClaimsToAKG_NilGraphPassthrough(t *testing.T) {
	ts := time.Now().UTC()
	claim := testClaim("c1", "Use Redis", "Redis", ts)
	linked := LinkDocumentClaimsToAKG([]developer_memory.KnowledgeClaim{claim}, nil)
	if len(linked) != 1 {
		t.Fatalf("got %d claims, want 1 (nil graph is a no-op)", len(linked))
	}
	if linked[0].ID != "c1" || linked[0].SubjectID != "" || linked[0].ObjectID != "" {
		t.Errorf("nil-graph linking mutated the claim: %+v", linked[0])
	}
}

func TestLinkDocumentClaimsToAKG_CaseInsensitiveNameCollisionLowestID(t *testing.T) {
	ts := time.Now().UTC()
	g := akg.NewCodePropertyGraph("test")
	// Two nodes share the name "Redis": the collision must resolve to the
	// lowest node ID deterministically, regardless of insertion order.
	g.Nodes = g.Nodes.Set("mod::redis-b", &stage4.ResolvedNode{ID: "mod::redis-b", Kind: "MODULE", Name: "Redis"})
	g.Nodes = g.Nodes.Set("mod::redis-a", &stage4.ResolvedNode{ID: "mod::redis-a", Kind: "MODULE", Name: "Redis"})

	claim := testClaim("c1", "Use Redis", "Redis", ts)
	linked := LinkDocumentClaimsToAKG([]developer_memory.KnowledgeClaim{claim}, g)
	if len(linked) != 1 {
		t.Fatalf("got %d claims, want 1", len(linked))
	}
	if linked[0].ObjectID != "mod::redis-a" {
		t.Errorf("object_id = %q, want mod::redis-a (lowest node ID wins)", linked[0].ObjectID)
	}
}

func evidenceFor(ts time.Time) evidence.Bundle {
	return evidence.NewBundle(evidence.EvidenceItem{
		Source:     evidence.SourceDocs,
		Reference:  "docs/adr/0001.md",
		Excerpt:    "excerpt",
		Confidence: 0.95,
		Timestamp:  ts,
	})
}
