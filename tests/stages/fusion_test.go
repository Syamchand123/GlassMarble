// knowledge fusion (knowledge fusion) pipeline tests.
//
// Discrepancies vs the phase-9 spec: there is no FusionStore/FusedClaim/
// Fuse/Put/Store/Missing/Persist/PurgeStale and no fusion.json. The
// delivered API is FusionEngine (NewFusionEngine + Run), which persists
// claims into developer_memory's claims.jsonl WAL + memory.json aggregate
// (approved deviation documented in internal/knowledge_fusion/doc.go).
// "Dedup by signature" is realized by ResolveConflicts (identical claims
// merged with combined evidence) plus WAL-ID dedup on re-run. Missing-ID
// checks are run against the store's claim WAL (LoadClaims), not a store
// method. ctx cancellation surfaces as an error from Run before any claim
// is appended.
package stages_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
	"github.com/Syamchand123/GlassMarble/internal/knowledge_fusion"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// s9helperConfig returns the fusion config with git sources disabled so
// runs are hermetic (mirrors the package-internal test configuration).
func s9helperConfig() *config.FusionConfig {
	gitOff := false
	cfg := config.DefaultFusionConfig()
	cfg.IncludeGitSources = &gitOff
	return cfg
}

// s9helperClaim builds a validated-shape KnowledgeClaim with one evidence
// item, for direct conflict-resolution tests.
func s9helperClaim(id, subject, predicate, object string, src evidence.Source, conf float64, ref string) developer_memory.KnowledgeClaim {
	return developer_memory.KnowledgeClaim{
		ID:        id,
		Subject:   subject,
		Predicate: predicate,
		Object:    object,
		ClaimKind: developer_memory.ClaimExplicitReason,
		State:     developer_memory.StateActive,
		ValidFrom: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		Evidence: evidence.NewBundle(evidence.EvidenceItem{
			Source:     src,
			Reference:  ref,
			Excerpt:    "fixture",
			Confidence: conf,
			Timestamp:  time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		}),
	}
}

func TestS9FusionEngineRequiresStoreAndRepo(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteFile("docs/adr/0001-use-redis.md", "# Use Redis\n\n## Status\n\nAccepted\n\n## Decision\n\nUse Redis for sessions.\n")

	// No store: structural failure, not a partial-fusion warning.
	engine := knowledge_fusion.NewFusionEngine(s9helperConfig(), nil)
	if _, err := engine.Run(context.Background(), sb.Root, nil); err == nil {
		t.Fatal("Run with nil store: expected error, got nil")
	}

	store := developer_memory.NewStoreForRepo(sb.Root)
	engine = knowledge_fusion.NewFusionEngine(s9helperConfig(), store)
	if _, err := engine.Run(context.Background(), "", nil); err == nil {
		t.Fatal("Run with empty repo dir: expected error, got nil")
	}
	// A non-existent repo dir is a discovery failure.
	if _, err := engine.Run(context.Background(), sb.Path("missing-dir"), nil); err == nil {
		t.Fatal("Run with missing repo dir: expected error, got nil")
	}
}

func TestS9FusionPipelinePersistsClaimsAndIsIdempotent(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteFile("docs/adr/0001-use-redis.md", "# Use Redis\n\n## Status\n\nAccepted\n\n## Decision\n\nUse Redis for sessions.\n")
	sb.WriteFile("docs/adr/0002-adopt-kafka.md", "# Adopt Kafka\n\n## Status\n\nProposed\n\n## Decision\n\nAdopt Kafka for event streaming.\n")
	sb.WriteFile("README.md", "# Repo\n\nUses Redis and PostgreSQL.\n")

	store := developer_memory.NewStoreForRepo(sb.Root)
	res, err := knowledge_fusion.NewFusionEngine(s9helperConfig(), store).Run(context.Background(), sb.Root, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.AdrFiles != 2 || res.ReadmeFiles != 1 {
		t.Errorf("adr/readme files = %d/%d, want 2/1", res.AdrFiles, res.ReadmeFiles)
	}
	// 2 decision claims + 2 ADR tech claims + 2 README mentions.
	if res.TotalClaims != 6 {
		t.Errorf("total claims = %d, want 6", res.TotalClaims)
	}
	if res.NewClaims != 6 {
		t.Errorf("new claims = %d, want 6 on first run", res.NewClaims)
	}
	if res.Sources != 3 {
		t.Errorf("sources = %d, want 3", res.Sources)
	}

	claims, err := store.LoadClaims()
	if err != nil {
		t.Fatalf("LoadClaims: %v", err)
	}
	if len(claims) != 6 {
		t.Fatalf("stored claims = %d, want 6", len(claims))
	}
	var sawRedisDecision, sawKafkaState bool
	for _, c := range claims {
		if c.Evidence.IsEmpty() || c.ValidFrom.IsZero() {
			t.Errorf("claim %s violates evidence/provenance discipline", c.ID)
		}
		if c.Predicate == "decided_to" && c.Object == "Use Redis for sessions." {
			sawRedisDecision = true
		}
		if c.Predicate == "decided_to_use" && c.Object == "kafka" {
			sawKafkaState = c.State == developer_memory.StateExperimental
		}
	}
	if !sawRedisDecision {
		t.Error("no decided_to claim persisted for the Redis ADR")
	}
	if !sawKafkaState {
		t.Error("kafka decided_to_use claim missing or not EXPERIMENTAL (status: Proposed)")
	}

	// WAL-ID dedup makes a second run append nothing.
	res2, err := knowledge_fusion.NewFusionEngine(s9helperConfig(), store).Run(context.Background(), sb.Root, nil)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res2.NewClaims != 0 {
		t.Errorf("new claims on rerun = %d, want 0 (idempotent)", res2.NewClaims)
	}
	if res2.TotalClaims != 6 {
		t.Errorf("total claims on rerun = %d, want 6 (stable)", res2.TotalClaims)
	}
}

func TestS9FusionCancelledCtxAbortsBeforePersist(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteFile("docs/adr/0001-use-redis.md", "# Use Redis\n\n## Status\n\nAccepted\n\n## Decision\n\nUse Redis for sessions.\n")

	store := developer_memory.NewStoreForRepo(sb.Root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := knowledge_fusion.NewFusionEngine(s9helperConfig(), store).Run(ctx, sb.Root, nil)
	if err == nil {
		t.Fatal("Run with cancelled ctx: expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	claims, err := store.LoadClaims()
	if err != nil {
		t.Fatalf("LoadClaims: %v", err)
	}
	if len(claims) != 0 {
		t.Errorf("claims persisted despite cancellation = %d, want 0", len(claims))
	}
}

func TestS9FusionResolveConflictsMergesIdenticalClaims(t *testing.T) {
	// Same fact stated by two sources: one claim survives with both
	// evidence items (sources merged, nothing overwritten).
	a := s9helperClaim("claim_a", "api", "state", "running", evidence.SourceDocs, 0.9, "docs/adr/0001.md")
	b := s9helperClaim("claim_b", "api", "state", "running", evidence.SourcePR, 0.9, "PR 42")

	resolved := knowledge_fusion.ResolveConflicts([]developer_memory.KnowledgeClaim{a, b}, nil)
	if len(resolved) != 1 {
		t.Fatalf("resolved = %d claims, want 1 merged", len(resolved))
	}
	got := resolved[0]
	if got.ID != "claim_a" {
		t.Errorf("primary claim ID = %s, want claim_a (source docs outranks pr)", got.ID)
	}
	if len(got.Evidence.Items) != 2 {
		t.Errorf("evidence items = %d, want 2 (sources merged)", len(got.Evidence.Items))
	}
	if got.State != developer_memory.StateActive {
		t.Errorf("merged claim state = %s, want CURRENT", got.State)
	}
}

func TestS9FusionResolveConflictsExclusiveContradictionLosesRetained(t *testing.T) {
	// "state" is exclusive (single-valued per subject): the higher
	// confidence claim stays CURRENT; the loser becomes HISTORICAL with a
	// ValidUntil — never deleted.
	a := s9helperClaim("claim_a", "api", "state", "running", evidence.SourceDocs, 0.95, "docs/adr/0001.md")
	b := s9helperClaim("claim_b", "api", "state", "stopped", evidence.SourceDocs, 0.8, "docs/adr/0002.md")

	resolved := knowledge_fusion.ResolveConflicts(
		[]developer_memory.KnowledgeClaim{a, b},
		map[string]bool{"state": true},
	)
	if len(resolved) != 2 {
		t.Fatalf("resolved = %d claims, want 2 (loser retained, never deleted)", len(resolved))
	}
	var winner, loser *developer_memory.KnowledgeClaim
	for i := range resolved {
		if resolved[i].State == developer_memory.StateActive {
			winner = &resolved[i]
		} else if resolved[i].State == developer_memory.StateHistorical {
			loser = &resolved[i]
		}
	}
	if winner == nil || loser == nil {
		t.Fatalf("want one CURRENT winner and one HISTORICAL loser, got %+v", resolved)
	}
	if winner.ID != "claim_a" {
		t.Errorf("winner = %s, want claim_a (higher confidence)", winner.ID)
	}
	if loser.ValidUntil == nil || !loser.ValidUntil.Equal(winner.ValidFrom) {
		t.Errorf("loser valid_until = %v, want winner valid_from %v", loser.ValidUntil, winner.ValidFrom)
	}
}

func TestS9FusionResolveConflictsMultiValuedCoexists(t *testing.T) {
	// uses_technology is multi-valued: different objects coexist untouched.
	a := s9helperClaim("claim_a", "architecture", "uses_technology", "redis", evidence.SourceDocs, 0.7, "README.md")
	b := s9helperClaim("claim_b", "architecture", "uses_technology", "kafka", evidence.SourceDocs, 0.7, "README.md")

	resolved := knowledge_fusion.ResolveConflicts([]developer_memory.KnowledgeClaim{a, b}, map[string]bool{"state": true})
	if len(resolved) != 2 {
		t.Fatalf("resolved = %d claims, want 2 (multi-valued predicate)", len(resolved))
	}
	for _, c := range resolved {
		if c.State != developer_memory.StateActive {
			t.Errorf("claim %s state = %s, want CURRENT (no contradiction)", c.ID, c.State)
		}
	}
}

func TestS9FusionFindDocsRespectsGlobsAndSizeCap(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteFile("docs/adr/0001-use-redis.md", "# Use Redis\n\n## Status\n\nAccepted\n\n## Decision\n\nUse Redis.\n")
	sb.WriteFile("docs/decisions/0002-adopt-kafka.md", "# Adopt Kafka\n\n## Status\n\nAccepted\n\n## Decision\n\nAdopt Kafka.\n")
	sb.WriteFile("README.md", "# Repo\n")
	sb.WriteFile("vendor/README.md", "# vendored\n")
	sb.WriteFile("docs/adr/0003-big.md", string(make([]byte, 128))) // oversized

	cfg := s9helperConfig()
	cfg.DocMaxSizeBytes = 64
	docs, err := knowledge_fusion.FindDocs(sb.Root, cfg)
	if err != nil {
		t.Fatalf("FindDocs: %v", err)
	}
	if len(docs) != 3 {
		t.Fatalf("docs = %d, want 3 (README + 2 ADRs; vendor and oversized skipped)", len(docs))
	}
	want := []string{"README.md", "docs/adr/0001-use-redis.md", "docs/decisions/0002-adopt-kafka.md"}
	for i, d := range docs {
		if d.Rel != want[i] {
			t.Errorf("docs[%d] = %s, want %s (deterministic sorted order)", i, d.Rel, want[i])
		}
	}

	// nil cfg falls back to defaults and discovers the same ADR globs.
	docs, err = knowledge_fusion.FindDocs(sb.Root, nil)
	if err != nil {
		t.Fatalf("FindDocs(nil cfg): %v", err)
	}
	if len(docs) != 4 {
		t.Errorf("docs with nil cfg = %d, want 4 (defaults, incl. the oversized file)", len(docs))
	}
}

func TestS9FusionParseADRDirect(t *testing.T) {
	sb := harness.NewSandbox(t)
	rel := "docs/adr/0001-use-redis.md"
	sb.WriteFile(rel, "# Use Redis\n\n## Status\n\nAccepted\n\n## Context\n\nSessions are slow.\n\n## Decision\n\nUse Redis for sessions.\n")

	doc := knowledge_fusion.DocSource{
		Path:    sb.Path(rel),
		Rel:     rel,
		Kind:    knowledge_fusion.DocKindADR,
		ModTime: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
	}
	claims, err := knowledge_fusion.ParseADR(doc, s9helperConfig().Lexicon())
	if err != nil {
		t.Fatalf("ParseADR: %v", err)
	}
	// 1 decision claim + 1 decided_to_use redis claim.
	if len(claims) != 2 {
		t.Fatalf("claims = %d, want 2", len(claims))
	}
	var sawDecision bool
	for _, c := range claims {
		if c.Subject != "Use Redis" {
			t.Errorf("claim subject = %q, want %q", c.Subject, "Use Redis")
		}
		if c.State != developer_memory.StateActive {
			t.Errorf("claim %s state = %s, want CURRENT (status: Accepted)", c.ID, c.State)
		}
		if c.Predicate == "decided_to" {
			sawDecision = true
		}
		if c.Evidence.IsEmpty() || c.Evidence.Items[0].Reference != rel {
			t.Errorf("claim %s evidence reference = %v, want %s", c.ID, c.Evidence.Items, rel)
		}
	}
	if !sawDecision {
		t.Error("no decided_to claim")
	}

	// A malformed ADR (no decision section) is a hard parse error.
	sb.WriteFile("docs/adr/0002-broken.md", "# Broken only\n")
	badDoc := knowledge_fusion.DocSource{Path: sb.Path("docs/adr/0002-broken.md"), Rel: "docs/adr/0002-broken.md", Kind: knowledge_fusion.DocKindADR}
	if _, err := knowledge_fusion.ParseADR(badDoc, nil); err == nil {
		t.Errorf("ParseADR on malformed %s: expected error, got nil", badDoc.Rel)
	}
}

func TestS9FusionParseReadmeDirect(t *testing.T) {
	sb := harness.NewSandbox(t)
	rel := "README.md"
	sb.WriteFile(rel, "# Repo\n\nUses Redis and PostgreSQL; not rediscount.\n")

	doc := knowledge_fusion.DocSource{
		Path:    sb.Path(rel),
		Rel:     rel,
		Kind:    knowledge_fusion.DocKindReadme,
		ModTime: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
	}
	claims := knowledge_fusion.ParseReadme(doc, s9helperConfig().Lexicon())
	if len(claims) != 2 {
		t.Fatalf("claims = %d, want 2 (redis + postgresql, rediscount must not match)", len(claims))
	}
	for _, c := range claims {
		if c.Subject != "architecture" || c.Predicate != "uses_technology" || c.State != developer_memory.StateActive {
			t.Errorf("readme claim = %s %s (state %s), want architecture uses_technology CURRENT", c.Subject, c.Predicate, c.State)
		}
		if c.Object != "postgresql" && c.Object != "redis" {
			t.Errorf("unexpected readme claim object %q", c.Object)
		}
	}
}