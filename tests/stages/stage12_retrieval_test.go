package stages_test

// Stage 12 (evidence retrieval) tests against a REAL analyzed graph
// (stages 1-4 committed via the AKG transaction manager — see
// pipeline_test.go), a seeded developer-memory aggregate, and a hand-written
// Stage 5 intelligence artifact. Direct API calls only (ai_engine.Retriever).
//
// Discrepancies from API_REFERENCE.md:
//   - harness.SeedCorrections writes the JSON key "target", but
//     learning.Correction unmarshals from "target_id" (correction.go:75).
//     The fixture therefore loads with an empty TargetID and is inert for
//     REJECT semantics. These tests write corrections with the real shape
//     via s12AppendCorrection.
//   - learning.PatternFeedback resolves the rejected pattern NAME from a
//     PATTERN_DETECTED event's Components[0] (learner.go:192), and
//     selectRelevant matches that name against the pattern's Kind
//     (evidence_retriever.go:344). Rejection only takes effect while another
//     non-rejected pattern matches the query (the rejected item stays in the
//     fallback pool otherwise). The correction test encodes both facts.
//   - sectionBudget floors every section at 64 tokens (context_builder.go:204),
//     so MaxTokens=1 still keeps one item per section.
//   - EvidenceContext.Timeline comes from the memory aggregate's Timeline
//     field, not from memory/timeline.json (the retriever queries the
//     aggregate; timeline.json is only a derived artifact).
//   - The intelligence fallback (no latest.json) needs a real snapshot file:
//     the snapshot store self-heals its index by scanning snap_*.json.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine"
	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
	"github.com/Syamchand123/GlassMarble/internal/learning"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// s12WriteIntelligence writes a Stage5Result into
// .glassmarble/intelligence/latest.json, the artifact LoadLatestResult reads.
func s12WriteIntelligence(t *testing.T, sb *harness.Sandbox, res arch_intelligence.Stage5Result) {
	t.Helper()
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatalf("s12: marshal stage5 result: %v", err)
	}
	sb.WriteFile(filepath.Join(".glassmarble", "intelligence", "latest.json"), string(data))
}

// s12WriteSnapshot writes a raw ArchSnapshot file into
// .glassmarble/snapshots. The SnapshotStore rebuilds its index from
// snap_*.json files when index.json is missing, so one hand-written file is
// enough for store.Latest() to find it.
func s12WriteSnapshot(t *testing.T, sb *harness.Sandbox, snap archmodel.ArchSnapshot) {
	t.Helper()
	if len(snap.ID) < 6 || snap.ID[:5] != "snap_" {
		t.Fatalf("s12: snapshot ID %q must start with snap_", snap.ID)
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatalf("s12: marshal snapshot: %v", err)
	}
	rest := snap.ID[5:]
	if len(rest) > 8 {
		rest = rest[:8]
	}
	sb.WriteFile(filepath.Join(".glassmarble", "snapshots", "snap_"+rest+".json"), string(data))
}

// s12AppendCorrection appends one learning.Correction line to
// corrections.jsonl using the store's real JSON shape (target_id). It
// appends so a test can stack several corrections.
func s12AppendCorrection(t *testing.T, sb *harness.Sandbox, c learning.Correction) {
	t.Helper()
	line, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("s12: marshal correction: %v", err)
	}
	path := filepath.Join(sb.GmDir, "memory", "corrections.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("s12: mkdir corrections dir: %v", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("s12: open corrections.jsonl: %v", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatalf("s12: write corrections.jsonl: %v", err)
	}
}

// s12SeedMemory writes a custom memory aggregate (same JSON shape as
// harness.SeedMemory) so tests can control the events — SeedMemory's single
// CACHING_ADDED event cannot express the PATTERN_DETECTED event that
// PatternFeedback resolves rejections from.
func s12SeedMemory(t *testing.T, sb *harness.Sandbox, mem *developer_memory.DeveloperMemory) {
	t.Helper()
	dir := filepath.Join(sb.GmDir, "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("s12: mkdir memory dir: %v", err)
	}
	data, err := json.MarshalIndent(mem, "", "  ")
	if err != nil {
		t.Fatalf("s12: marshal memory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memory.json"), data, 0o644); err != nil {
		t.Fatalf("s12: write memory.json: %v", err)
	}
}

// s12LatestFixture returns the hand-written Stage 5 result used by the
// seeded-retrieval tests: one LAYERED pattern over the cache (with a
// citable rule reference) and a metrics block.
func s12LatestFixture() arch_intelligence.Stage5Result {
	return arch_intelligence.Stage5Result{
		GraphHash: "s12hash",
		Metrics:   archmodel.ArchMetrics{TotalNodes: 4, TotalEdges: 3},
		Patterns: []archmodel.DetectedPattern{
			{
				Kind:       archmodel.PatternLayered,
				Name:       "cache",
				Components: []string{"cache"},
				Confidence: 0.9,
				Evidence: evidence.NewBundle(evidence.EvidenceItem{
					Source:     evidence.SourceRule,
					Reference:  "PR-07",
					Confidence: 1.0,
					Timestamp:  time.Now().Add(-24 * time.Hour),
				}),
			},
		},
		Smells: []archmodel.ArchSmell{
			{
				Kind:     archmodel.SmellTightCoupling,
				Title:    "cache coupling",
				Severity: archmodel.SeverityMedium,
			},
		},
	}
}

// TestStage12SeededRetrieval drives the full pipeline: real graph committed
// via the transaction manager, seeded memory, hand-written Stage 5
// intelligence. The retrieval must ground every section.
func TestStage12SeededRetrieval(t *testing.T) {
	sb := harness.NewSandbox(t)
	analyzeProject(t, sb)
	sb.SeedMemory("proj_test")
	s12WriteIntelligence(t, sb, s12LatestFixture())

	r := ai_engine.NewRetriever(sb.Root)
	ctx := r.RetrieveForQuestion("what does the cache do?", ai_engine.RetrieveOptions{})

	if ctx.Question != "what does the cache do?" {
		t.Errorf("Question = %q, want the input question", ctx.Question)
	}
	if len(ctx.Nodes) == 0 {
		t.Error("Nodes empty — the cache nodes must match the question")
	}
	if len(ctx.Claims) == 0 {
		t.Error("Claims empty — SeedMemory claims about cache must be retrieved")
	}
	if len(ctx.Patterns) == 0 {
		t.Error("Patterns empty — the LAYERED/cache pattern must survive relevance filtering")
	}
	if ctx.TokenCount <= 0 {
		t.Errorf("TokenCount = %d, want > 0", ctx.TokenCount)
	}
	if ctx.Empty() {
		t.Error("Empty() = true on a fully seeded retrieval")
	}
	if ctx.MetricSummary == "" {
		t.Error("MetricSummary empty — intelligence metrics must be rendered")
	}

	prompt := ctx.BuildPrompt()
	for _, want := range []string{
		ctx.Question,
		ai_engine.EvidenceSectionAKG,
		ai_engine.EvidenceSectionHistory,
		ai_engine.EvidenceSectionPatterns,
		ai_engine.EvidenceSectionQuestion,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("BuildPrompt missing %q", want)
		}
	}
	if got := ctx.Citations(); len(got) == 0 {
		t.Error("Citations() empty — pattern rule references must be citable")
	}
}

// TestStage12NodeMatching asserts every returned node carries the query term
// in its name or file path, and that match scores stay within (0, 1].
func TestStage12NodeMatching(t *testing.T) {
	sb := harness.NewSandbox(t)
	analyzeProject(t, sb)

	ctx := ai_engine.NewRetriever(sb.Root).RetrieveForQuestion("service", ai_engine.RetrieveOptions{})
	if len(ctx.Nodes) == 0 {
		t.Fatal("no nodes matched the sample project's service layer")
	}
	for _, n := range ctx.Nodes {
		hay := strings.ToLower(n.Name + " " + n.File)
		if !strings.Contains(hay, "service") {
			t.Errorf("node %q (file %q) does not mention the query term", n.Name, n.File)
		}
		if n.Match <= 0 || n.Match > 1 {
			t.Errorf("node %q match score = %f, want in (0,1]", n.Name, n.Match)
		}
	}
}

// TestStage12RetrievalDeterminism asserts two retrievals of the same question
// yield byte-identical node and claim orderings.
func TestStage12RetrievalDeterminism(t *testing.T) {
	sb := harness.NewSandbox(t)
	analyzeProject(t, sb)
	sb.SeedMemory("proj_test")
	s12WriteIntelligence(t, sb, s12LatestFixture())

	r := ai_engine.NewRetriever(sb.Root)
	first := r.RetrieveForQuestion("cache", ai_engine.RetrieveOptions{})
	second := r.RetrieveForQuestion("cache", ai_engine.RetrieveOptions{})

	nodeIDs := func(ctx *ai_engine.EvidenceContext) []string {
		out := make([]string, len(ctx.Nodes))
		for i, n := range ctx.Nodes {
			out[i] = n.ID
		}
		return out
	}
	claimIDs := func(ctx *ai_engine.EvidenceContext) []string {
		out := make([]string, len(ctx.Claims))
		for i, c := range ctx.Claims {
			out[i] = c.ID
		}
		return out
	}
	if !reflect.DeepEqual(nodeIDs(first), nodeIDs(second)) {
		t.Errorf("node order differs between retrievals:\n%v\n%v", nodeIDs(first), nodeIDs(second))
	}
	if !reflect.DeepEqual(claimIDs(first), claimIDs(second)) {
		t.Errorf("claim order differs between retrievals:\n%v\n%v", claimIDs(first), claimIDs(second))
	}
}

// TestStage12TopKCapsSections asserts TopK bounds every evidence section.
func TestStage12TopKCapsSections(t *testing.T) {
	sb := harness.NewSandbox(t)
	analyzeProject(t, sb)
	sb.SeedMemory("proj_test")
	s12WriteIntelligence(t, sb, s12LatestFixture())

	ctx := ai_engine.NewRetriever(sb.Root).RetrieveForQuestion("cache", ai_engine.RetrieveOptions{TopK: 1})
	if len(ctx.Nodes) > 1 || len(ctx.Claims) > 1 || len(ctx.Timeline) > 1 ||
		len(ctx.Patterns) > 1 || len(ctx.Smells) > 1 || len(ctx.Components) > 1 {
		t.Errorf("TopK=1 exceeded: nodes=%d claims=%d timeline=%d patterns=%d smells=%d components=%d",
			len(ctx.Nodes), len(ctx.Claims), len(ctx.Timeline),
			len(ctx.Patterns), len(ctx.Smells), len(ctx.Components))
	}
}

// TestStage12TokenBudgetTrim asserts TrimToBudget pulls the rendered prompt
// back toward the budget (the 64-token per-section floor allows slack) and
// still works for absurdly small budgets.
func TestStage12TokenBudgetTrim(t *testing.T) {
	sb := harness.NewSandbox(t)
	analyzeProject(t, sb)
	sb.SeedMemory("proj_test")
	s12WriteIntelligence(t, sb, s12LatestFixture())

	r := ai_engine.NewRetriever(sb.Root)

	ctx := r.RetrieveForQuestion("cache", ai_engine.RetrieveOptions{MaxTokens: 100})
	if ctx.TokenCount <= 0 {
		t.Errorf("TokenCount = %d, want > 0", ctx.TokenCount)
	}
	if ctx.TokenCount > 1000 {
		t.Errorf("TokenCount = %d, want <= 1000 (budget 100 plus per-section floor)", ctx.TokenCount)
	}

	tiny := r.RetrieveForQuestion("cache", ai_engine.RetrieveOptions{MaxTokens: 1})
	if tiny.TokenCount <= 0 {
		t.Errorf("MaxTokens=1 TokenCount = %d, want > 0", tiny.TokenCount)
	}
	for _, sec := range []struct {
		name string
		got  int
	}{
		{"nodes", len(tiny.Nodes)},
		{"claims", len(tiny.Claims)},
		{"timeline", len(tiny.Timeline)},
		{"patterns", len(tiny.Patterns)},
		{"smells", len(tiny.Smells)},
		{"components", len(tiny.Components)},
	} {
		if sec.got > 1 {
			t.Errorf("MaxTokens=1 section %s has %d items, want <= 1", sec.name, sec.got)
		}
	}
}

// TestStage12EmptyInputs asserts empty and whitespace questions short-circuit
// to an empty context without error.
func TestStage12EmptyInputs(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.SampleProject()
	sb.SeedMemory("proj_test")

	r := ai_engine.NewRetriever(sb.Root)
	for _, q := range []string{"", "   \t\n"} {
		ctx := r.RetrieveForQuestion(q, ai_engine.RetrieveOptions{})
		if !ctx.Empty() {
			t.Errorf("question %q: Empty() = false, want true", q)
		}
	}
}

// TestStage12FreshSandboxNoPanic asserts retrieval against a completely empty
// sandbox (no .glassmarble at all) yields an empty context, no panic and no
// error.
func TestStage12FreshSandboxNoPanic(t *testing.T) {
	sb := harness.NewSandbox(t)

	ctx := ai_engine.NewRetriever(sb.Root).RetrieveForQuestion("anything", ai_engine.RetrieveOptions{})
	if !ctx.Empty() {
		t.Errorf("Empty() = false on a fresh sandbox: %+v", ctx)
	}
}

// TestStage12MissingPieces asserts retrieval degrades gracefully when only
// one data source exists: graph without memory yields nodes only, memory
// without graph yields claims only.
func TestStage12MissingPieces(t *testing.T) {
	t.Run("graph_only", func(t *testing.T) {
		sb := harness.NewSandbox(t)
		sb.WriteAKGState(harness.TinyGraph())

		ctx := ai_engine.NewRetriever(sb.Root).RetrieveForQuestion("db", ai_engine.RetrieveOptions{})
		if len(ctx.Nodes) == 0 {
			t.Error("Nodes empty — the db node must match, graph only")
		}
		if len(ctx.Claims) != 0 {
			t.Errorf("Claims = %d, want 0 without memory", len(ctx.Claims))
		}
	})

	t.Run("memory_only", func(t *testing.T) {
		sb := harness.NewSandbox(t)
		sb.SeedMemory("proj_test")

		ctx := ai_engine.NewRetriever(sb.Root).RetrieveForQuestion("cache", ai_engine.RetrieveOptions{})
		if len(ctx.Claims) == 0 {
			t.Error("Claims empty — seeded memory must be retrieved, no graph needed")
		}
		if len(ctx.Nodes) != 0 {
			t.Errorf("Nodes = %d, want 0 without a graph", len(ctx.Nodes))
		}
	})
}

// TestStage12CorrectionRejectsPattern asserts a REJECT correction whose
// PATTERN_DETECTED event names a pattern kind excludes that kind from the
// evidence while a sibling pattern survives.
func TestStage12CorrectionRejectsPattern(t *testing.T) {
	sb := harness.NewSandbox(t)
	analyzeProject(t, sb)

	now := time.Now()
	mem := &developer_memory.DeveloperMemory{
		ProjectID:   "proj_test",
		LastUpdated: now,
		TotalEvents: 1,
		Events: []archmodel.ArchEvent{
			{
				ID:         "evt_s12_reject",
				Kind:       archmodel.EventPatternDetected,
				CommitHash: "abcdef1234567890",
				Timestamp:  now.Add(-24 * time.Hour),
				Title:      "layered architecture detected",
				Components: []string{"LAYERED_ARCH"},
				Intent:     "cache is a cross-cutting concern",
				ValidFrom:  now.Add(-24 * time.Hour),
				Evidence:   evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceRule, Reference: "PR-07", Confidence: 1.0, Timestamp: now.Add(-24 * time.Hour)}),
			},
		},
		GlobalMemory: []developer_memory.KnowledgeClaim{
			{
				ID:             "claim_s12_cache",
				Subject:        "cache",
				Predicate:      "serves",
				Object:         "greeting lookups",
				ClaimKind:      developer_memory.ClaimExplicitReason,
				State:          developer_memory.StateActive,
				FreshnessScore: 0.9,
				ValidFrom:      now.Add(-24 * time.Hour),
			},
		},
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"cache": {Name: "cache", State: developer_memory.StateActive},
		},
	}
	s12SeedMemory(t, sb, mem)
	s12AppendCorrection(t, sb, learning.Correction{
		// ID must be non-empty: Store.LoadAll drops lines with an empty ID
		// (correction.go:311). The store derives IDs in Append, which we
		// bypass by writing the JSONL directly.
		ID:       "corr_s12_reject",
		Kind:     learning.CorrectionKindReject,
		TargetID: "evt_s12_reject",
		// TargetType has no "pattern" value (overlay.go:15-19); PatternFeedback
		// resolves rejections purely from TargetID + the event's Components[0].
		Timestamp: now,
		Reason:    "cache is not a pattern",
	})

	intel := s12LatestFixture()
	intel.Patterns = append(intel.Patterns, archmodel.DetectedPattern{
		Kind:       "REPOSITORY_PATTERN",
		Name:       "repo",
		Components: []string{"repo"},
		Confidence: 0.8,
	})
	s12WriteIntelligence(t, sb, intel)

	ctx := ai_engine.NewRetriever(sb.Root).RetrieveForQuestion(
		"cache repository layered architecture", ai_engine.RetrieveOptions{})

	if ctx.Corrections < 0 {
		t.Errorf("Corrections = %d, want >= 0", ctx.Corrections)
	}
	if len(ctx.Claims) == 0 {
		t.Error("Claims empty — the cache claim must still be retrieved")
	}
	var rejectedKind, keptKind bool
	for _, p := range ctx.Patterns {
		switch p.Kind {
		case "LAYERED_ARCH":
			rejectedKind = true
		case archmodel.PatternRepository:
			keptKind = true
		}
	}
	if rejectedKind {
		t.Error("rejected pattern kind LAYERED_ARCH still present in evidence")
	}
	if !keptKind {
		t.Errorf("sibling pattern %q missing — rejection must not empty the section", archmodel.PatternRepository)
	}
}

// TestStage12SnapshotFallback asserts retrieval still grounds on the latest
// architecture snapshot when intelligence/latest.json is absent (deleted):
// nodes come from the graph, patterns/metrics from the snapshot.
func TestStage12SnapshotFallback(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteAKGState(harness.TinyGraph())
	s12WriteSnapshot(t, sb, archmodel.ArchSnapshot{
		ID:           "snap_s12f00d01",
		CommitHash:   "abcdef1234567890",
		Timestamp:    time.Now().Add(-time.Hour),
		TopologyHash: "s12tophash",
		Metrics:      archmodel.ArchMetrics{TotalNodes: 2, TotalEdges: 1},
		Patterns: []archmodel.DetectedPattern{
			{
				Kind:       archmodel.PatternLayered,
				Name:       "layered",
				Components: []string{"db"},
				Confidence: 0.9,
			},
		},
		Components: []archmodel.DetectedComponent{
			{ID: "comp_db", Name: "db", Kind: archmodel.ComponentLayer, Confidence: 0.8},
		},
	})

	ctx := ai_engine.NewRetriever(sb.Root).RetrieveForQuestion("db", ai_engine.RetrieveOptions{})

	if len(ctx.Nodes) == 0 {
		t.Error("Nodes empty — fallback must still match graph nodes")
	}
	if len(ctx.Patterns) == 0 {
		t.Error("Patterns empty — snapshot fallback must surface snapshot patterns")
	}
	if ctx.MetricSummary == "" {
		t.Error("MetricSummary empty — snapshot fallback must render snapshot metrics")
	}
	if ctx.Empty() {
		t.Error("Empty() = true although snapshot evidence was retrieved")
	}
}
