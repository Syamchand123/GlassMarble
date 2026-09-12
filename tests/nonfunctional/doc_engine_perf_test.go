package nonfunctional_test

// Performance tripwires for the docs-engine feature (order-of-magnitude
// regression guards only — never perf tuning).
//
// Convention (mirrors internal/doc_engine/perf_budgets_test.go): every budget
// carries a GENEROUS margin (10x-100x over the expected millisecond-scale
// reality) so slow CI hardware never flakes. Only upper bounds are asserted;
// there are no exact-ms assertions, no sleeps, no network (--no-llm), and the
// 2000-file corpus is generated stubs. Heavy variants (50k files, nightly
// benchmarks) are OUT — see the report exclusions.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/catalog"
	docconfig "github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/grounding"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/invalidator"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/renderer"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/verifier"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// docEngPerfDocs is a one-document catalog scoped to the synthetic corpus.
func docEngPerfDocs() []docconfig.DocSpec {
	return []docconfig.DocSpec{
		{
			ID:         "guide",
			TargetPath: "docs/guide.md",
			Title:      "Guide",
			Purpose:    "Perf fixture.",
			Audience:   "Developers",
			Scope:      docconfig.ScopeRule{Paths: []string{"bench/**"}},
			Sections: []docconfig.SectionSpec{
				{ID: "overview", Title: "Overview", Instruction: "Document the current state.", Managed: true},
			},
		},
	}
}

// docEngPerfRepo builds a git repo with n generated stub files plus the perf
// docs.yaml, returning the sandbox and HEAD hash.
func docEngPerfRepo(t *testing.T, n int) (*harness.Sandbox, string) {
	t.Helper()
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.GitInit()
	files := make(map[string]string, n)
	for i := 0; i < n; i++ {
		dir := fmt.Sprintf("bench/d%02d", i/100)
		files[fmt.Sprintf("%s/f%04d.go", dir, i)] = fmt.Sprintf(
			"package d%02d\n\n// F%04d does work.\nfunc F%04d() string { return \"ok\" }\n",
			i/100, i, i)
	}
	head := sb.GitCommitFiles("perf: generated stubs", files)
	sb.WriteFile(".glassmarble/docs.yaml", `version: 1
docs_dir: docs
documents:
  - id: guide
    target: docs/guide.md
    title: Guide
    purpose: Perf fixture.
    audience: Developers
    scope:
      paths: ["bench/**"]
    sections:
      - id: overview
        title: Overview
        instruction: Document the current state.
`)
	return sb, head
}

// docEngPerfGraph builds a synthetic 2000-node CodePropertyGraph for the
// grounding stage (no repo I/O involved).
func docEngPerfGraph(n int) *akg.CodePropertyGraph {
	g := akg.NewCodePropertyGraph("commit-perf-test")
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("bench/file%04d.go::Func%04d", i, i)
		g.Nodes = g.Nodes.Set(id, &link.ResolvedNode{
			ID:   id,
			Name: fmt.Sprintf("Func%04d", i),
			Kind: "FUNCTION",
			FileSpec: link.LocationMeta{
				Path:      fmt.Sprintf("bench/file%04d.go", i),
				LineStart: 10,
				LineEnd:   20,
			},
			Properties: map[string]string{
				"signature":   fmt.Sprintf("func Func%04d() error", i),
				"doc_comment": fmt.Sprintf("Func%04d does work.", i),
			},
		})
	}
	return g
}

// TestDocEnginePerfFastBail: the already-processed fast-bail path on a
// 2000-file repo must bail almost instantly (budget 30s ≈ 2000x the <15ms
// plan target — a pure tripwire).
func TestDocEnginePerfFastBail(t *testing.T) {
	sb, head := docEngPerfRepo(t, 2000)
	cat := catalog.New(docEngPerfDocs())
	state := &storage.DocEngineState{LastCommit: head}

	start := time.Now()
	bail, _, reason, err := invalidator.FastBail(sb.Root, head, cat, state, false)
	elapsed := time.Since(start)
	t.Logf("fast-bail on 2000-file repo: %s (reason: %s)", elapsed, reason)
	if err != nil {
		t.Fatalf("FastBail: %v", err)
	}
	if !bail {
		t.Errorf("expected fast-bail on already-processed commit")
	}
	if elapsed > 30*time.Second {
		t.Errorf("FastBail took %s, budgeted at 30s (tripwire only)", elapsed)
	}
}

// TestDocEnginePerfInvalidation: dossier assembly + dirty-section discovery
// over a 2000-file repo (budget 60s — orders above the ms-scale expectation).
func TestDocEnginePerfInvalidation(t *testing.T) {
	sb, head := docEngPerfRepo(t, 2000)
	cat := catalog.New(docEngPerfDocs())
	state := &storage.DocEngineState{}

	start := time.Now()
	dossier, err := invalidator.BuildDossier(sb.Root, head, nil, nil)
	if err != nil {
		t.Fatalf("BuildDossier: %v", err)
	}
	inv := invalidator.New(cat)
	constraints := &docconfig.GlobalConstraints{}
	dirty, err := inv.FindDirtySections(dossier, state, nil, constraints)
	elapsed := time.Since(start)
	t.Logf("invalidation on 2000-file repo: %s (%d dirty sections)", elapsed, len(dirty))
	if err != nil {
		t.Fatalf("FindDirtySections: %v", err)
	}
	if len(dirty) == 0 {
		t.Errorf("expected dirty sections on initial sync")
	}
	if elapsed > 60*time.Second {
		t.Errorf("invalidation took %s, budgeted at 60s (tripwire only)", elapsed)
	}
}

// TestDocEnginePerfGrounding: fact collection over a synthetic 2000-node
// graph (budget 60s).
func TestDocEnginePerfGrounding(t *testing.T) {
	g := docEngPerfGraph(2000)
	collector := grounding.NewCollector(g)
	sec := &docconfig.SectionSpec{
		ID:          "overview",
		Title:       "Overview",
		Instruction: "Document the current state.",
		GroundWith:  []string{"signatures", "exported_symbols", "comments", "sentinels", "config_vars"},
		Managed:     true,
	}
	scope := &docconfig.ScopeRule{Paths: []string{"bench/**"}}

	start := time.Now()
	payload, err := collector.CollectSectionFacts(sec, scope)
	elapsed := time.Since(start)
	t.Logf("grounding over 2000-node graph: %s", elapsed)
	if err != nil {
		t.Fatalf("CollectSectionFacts: %v", err)
	}
	if len(payload.Symbols) == 0 {
		t.Errorf("expected symbols from 2000-node graph")
	}
	if elapsed > 60*time.Second {
		t.Errorf("grounding took %s, budgeted at 60s (tripwire only)", elapsed)
	}
}

// TestDocEnginePerfGates: the 5-gate firewall over ~1MB of markdown
// (budget 30s).
func TestDocEnginePerfGates(t *testing.T) {
	body := "# Title\n\n" + strings.Repeat("The service handles requests reliably. ", 25000)
	old := body + " Extra sentence one."
	now := body + " Extra sentence two with fresh facts."

	start := time.Now()
	res := verifier.RunGates(old, now, nil)
	elapsed := time.Since(start)
	t.Logf("gates over ~1MB markdown: %s (pass=%v)", elapsed, res.Pass)
	if !res.Pass {
		t.Errorf("gates must pass on clean prose (gate %d: %v)", res.FailedGate, res.Error)
	}
	if elapsed > 30*time.Second {
		t.Errorf("gates took %s, budgeted at 30s (tripwire only)", elapsed)
	}
}

// TestDocEnginePerfWrite: atomic MVCC write of ~1MB (budget 30s; covers the
// tmp → fsync → rename → verify contract including fsync costs on Windows).
func TestDocEnginePerfWrite(t *testing.T) {
	dir := t.TempDir()
	target := dir + "/perf.md"
	content := []byte("# Perf\n\n" + strings.Repeat("Lorem ipsum dolor sit amet. ", 35000))

	start := time.Now()
	changed, err := storage.AtomicWriteFile(target, content)
	elapsed := time.Since(start)
	t.Logf("atomic write of ~1MB: %s (changed=%v)", elapsed, changed)
	if err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}
	if !changed {
		t.Errorf("first write must report changed=true")
	}
	if elapsed > 30*time.Second {
		t.Errorf("write took %s, budgeted at 30s (tripwire only)", elapsed)
	}
}

// TestDocEnginePerfRender: deterministic render of a 500-symbol FactSheet
// (budget 30s).
func TestDocEnginePerfRender(t *testing.T) {
	var symbols []docconfig.SymbolFact
	for i := 0; i < 500; i++ {
		symbols = append(symbols, docconfig.SymbolFact{
			FQN:       fmt.Sprintf("bench/pkg%02d.go::Service%04d", i%10, i),
			Kind:      "func",
			Signature: fmt.Sprintf("func Service%04d() error", i),
			Doc:       fmt.Sprintf("Service%04d handles requests.", i),
			File:      fmt.Sprintf("bench/pkg%02d.go", i%10),
			Line:      10 + i,
		})
	}
	orch := renderer.NewOrchestrator(renderer.OrchestratorOptions{NoLLM: true})
	fs := &docconfig.FactSheet{
		DocID:     "guide",
		SectionID: "overview",
		GroundTruth: docconfig.GroundTruthPayload{
			Symbols: symbols,
		},
	}

	start := time.Now()
	outcome, err := orch.RenderSection(context.Background(), fs, nil)
	elapsed := time.Since(start)
	t.Logf("deterministic render of 500 symbols: %s (%d bytes)", elapsed, len(outcome.Content))
	if err != nil {
		t.Fatalf("RenderSection: %v", err)
	}
	if outcome.Content == "" {
		t.Errorf("render produced empty content")
	}
	if elapsed > 30*time.Second {
		t.Errorf("render took %s, budgeted at 30s (tripwire only)", elapsed)
	}
}

// TestDocEnginePerfFullRun: end-to-end `doc` run over a 2000-file repo
// (budget 240s — the suite's dominant cost, still well under the combined
// 5-minute envelope together with the other fast stages).
func TestDocEnginePerfFullRun(t *testing.T) {
	sb, head := docEngPerfRepo(t, 2000)

	start := time.Now()
	out, err := harness.RunGmb(t, sb, "doc", "--no-llm", "--force", "--branch-policy", "any", "--commit", head)
	elapsed := time.Since(start)
	t.Logf("full doc run on 2000-file repo: %s", elapsed)
	if err != nil {
		t.Fatalf("doc run failed: %v\n%s", err, out)
	}
	if !sb.Exists("docs/guide.md") {
		t.Errorf("expected docs/guide.md after full run")
	}
	if elapsed > 240*time.Second {
		t.Errorf("full doc run took %s, budgeted at 240s (tripwire only)", elapsed)
	}
}
