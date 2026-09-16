package doc_engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/archfeatures"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/grounding"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/ledger"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/renderer"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
)

func setupTestRepoWithDoc(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()

	// 1. Initialize docs.yaml
	cfgPath := config.DocsConfigPath(tempDir)
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0755)

	doc := config.DocSpec{
		ID:         "testdoc",
		TargetPath: "docs/testdoc.md",
		Title:      "Test Documentation",
		Archetype:  "module",
		Audience:   "Developers",
		Scope: config.ScopeRule{
			Paths: []string{"internal/testdoc/**"},
		},
		Sections: []config.SectionSpec{
			{
				ID:          "overview",
				Title:       "Overview",
				Managed:     true,
				Instruction: "Explain module overview",
			},
		},
	}

	_ = updateDocsYAML(tempDir, doc)

	// 2. Write initial markdown file
	targetFull := filepath.Join(tempDir, doc.TargetPath)
	_ = os.MkdirAll(filepath.Dir(targetFull), 0755)
	mdContent := "# Test Documentation\n\n<!-- gmb:begin:overview -->\nOld content\n<!-- gmb:end:overview -->\n"
	_ = os.WriteFile(targetFull, []byte(mdContent), 0644)

	// 3. Initialize state manager
	sm := storage.NewStateManager(config.StorageDirPath(tempDir))
	state, _ := sm.Load()
	docState := storage.GetOrCreateDocState(state, doc.TargetPath)
	secState := storage.GetOrCreateSectionState(docState, "overview")
	secState.ASTSubgraphHash = fmt.Sprintf("%x", sha256.Sum256([]byte("Old content")))
	_ = sm.Save(state)

	return tempDir
}

func TestOperations_Diff(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	diffRes, err := Diff(tempDir, RunOptions{})
	if err != nil {
		t.Fatalf("Diff returned error: %v", err)
	}

	// Should execute without panic and report section diffs
	if len(diffRes.Sections) == 0 {
		t.Logf("No sections dirty or detected")
	}
}

// TestOperations_Diff_NoSpuriousChangeWhenContentMatchesRealRender guards
// against the regression where Diff() built an empty GroundTruthPayload
// instead of calling grounding.AssembleFactSheet: with no symbols,
// diagrams, or sentinels to work with, the freshly rendered candidate was
// drastically different from real prior content no matter what, so
// hasChange was true for every populated section, always — `gmb doc diff`
// could never report "up to date." This seeds the doc's on-disk content
// with the actual output of a real grounded render, then asserts Diff
// reports it unchanged.
func TestOperations_Diff_NoSpuriousChangeWhenContentMatchesRealRender(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	cfg, err := config.LoadDocsConfig(tempDir)
	if err != nil {
		t.Fatalf("LoadDocsConfig: %v", err)
	}
	doc := &cfg.Documents[0]
	sec := &doc.Sections[0]

	fs := grounding.AssembleFactSheet(doc, sec, nil, nil, "", tempDir)
	if fs == nil {
		t.Fatal("AssembleFactSheet returned nil")
	}
	orch := renderer.NewOrchestrator(renderer.OrchestratorOptions{NoLLM: true})
	outcome, err := orch.RenderSection(context.Background(), fs, nil)
	if err != nil {
		t.Fatalf("RenderSection: %v", err)
	}

	targetFull := filepath.Join(tempDir, doc.TargetPath)
	mdContent := fmt.Sprintf("# Test Documentation\n\n<!-- gmb:begin:overview -->\n%s\n<!-- gmb:end:overview -->\n", outcome.Content)
	if err := os.WriteFile(targetFull, []byte(mdContent), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	diffRes, err := Diff(tempDir, RunOptions{})
	if err != nil {
		t.Fatalf("Diff returned error: %v", err)
	}
	for _, s := range diffRes.Sections {
		if s.SectionID == "overview" && s.HasChange {
			t.Errorf("expected no change for section already matching real render, got diff:\n%s", s.DiffPreview)
		}
	}
	if diffRes.HasChanges {
		t.Errorf("expected DiffResult.HasChanges=false, got true")
	}
}

// TestOperations_Diff_DetectsRealChange guards against the opposite failure
// mode: Diff() must still detect an actual change when the section's
// instruction (and therefore its rendered prose) changes.
func TestOperations_Diff_DetectsRealChange(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	cfg, err := config.LoadDocsConfig(tempDir)
	if err != nil {
		t.Fatalf("LoadDocsConfig: %v", err)
	}
	doc := &cfg.Documents[0]
	sec := &doc.Sections[0]

	fs := grounding.AssembleFactSheet(doc, sec, nil, nil, "", tempDir)
	if fs == nil {
		t.Fatal("AssembleFactSheet returned nil")
	}
	orch := renderer.NewOrchestrator(renderer.OrchestratorOptions{NoLLM: true})
	outcome, err := orch.RenderSection(context.Background(), fs, nil)
	if err != nil {
		t.Fatalf("RenderSection: %v", err)
	}

	targetFull := filepath.Join(tempDir, doc.TargetPath)
	mdContent := fmt.Sprintf("# Test Documentation\n\n<!-- gmb:begin:overview -->\n%s\n<!-- gmb:end:overview -->\n", outcome.Content)
	if err := os.WriteFile(targetFull, []byte(mdContent), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	// Mutate the section's instruction — a real spec change that must cause
	// the next render to differ from the seeded prior content.
	doc.Sections[0].Instruction = "Explain module overview, focusing on concurrency safety"
	if err := updateDocsYAML(tempDir, *doc); err != nil {
		t.Fatalf("updateDocsYAML: %v", err)
	}

	diffRes, err := Diff(tempDir, RunOptions{})
	if err != nil {
		t.Fatalf("Diff returned error: %v", err)
	}
	found := false
	for _, s := range diffRes.Sections {
		if s.SectionID == "overview" {
			found = true
			if !s.HasChange {
				t.Errorf("expected change detected after instruction edit, got none")
			}
		}
	}
	if !found {
		t.Fatal("overview section not present in diff result")
	}
	if !diffRes.HasChanges {
		t.Errorf("expected DiffResult.HasChanges=true, got false")
	}
}

// TestOperations_Diff_HybridSection_ComparesAppendixOnlyNotProse guards
// against a real bug found via live testing (re-verified original audit
// Finding C): once the mandatory-LLM overhaul made real LLM prose the
// default rendered content (folded above a deterministic reference
// appendix — see renderer/deterministic.go's RenderReferenceAppendix),
// Diff() kept comparing a real on-disk section (prose + appendix)
// against a NoLLM-rendered candidate that structurally has no prose at
// all. That reported hasChange=true for essentially every populated
// LLM-authored section, always, regardless of whether the underlying
// grounded facts had actually changed — `gmb doc diff` became noise. The
// fix compares only the appendix half for a hybrid (prose + appendix)
// section, since prose can never be predicted without an actual LLM
// call.
func TestOperations_Diff_HybridSection_ComparesAppendixOnlyNotProse(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	// setupTestRepoWithDoc's scope ("internal/testdoc/**") matches no real
	// files, so a fresh grounded render's reference appendix would always
	// come back empty — seed one real exported symbol so the appendix
	// actually has content, and this test can tell "appendix compared and
	// found unchanged" apart from "there was never anything to compare."
	srcDir := filepath.Join(tempDir, "internal", "testdoc")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	src := "package testdoc\n\n// Run does the thing.\nfunc Run() error { return nil }\n"
	if err := os.WriteFile(filepath.Join(srcDir, "run.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadDocsConfig(tempDir)
	if err != nil {
		t.Fatalf("LoadDocsConfig: %v", err)
	}
	doc := &cfg.Documents[0]
	sec := &doc.Sections[0]

	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("internal/testdoc/run.go::Run", &link.ResolvedNode{
		ID:   "internal/testdoc/run.go::Run",
		Name: "Run",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      "internal/testdoc/run.go",
			LineStart: 3,
			LineEnd:   3,
		},
		Properties: map[string]string{"signature": "func Run() error"},
	})

	fs := grounding.AssembleFactSheet(doc, sec, graph, nil, "", tempDir)
	if fs == nil {
		t.Fatal("AssembleFactSheet returned nil")
	}
	det := renderer.NewDeterministicRenderer()
	appendix := det.RenderReferenceAppendix(fs)
	if strings.TrimSpace(appendix) == "" {
		t.Fatal("expected a non-empty reference appendix from the seeded Run() symbol — test setup is broken")
	}

	// Real LLM prose that a deterministic (NoLLM) render could never
	// reproduce — if Diff() ever compares full content again, this alone
	// guarantees a spurious "changed" report.
	prose := "This module does something genuinely different from any deterministic table, in the LLM's own words, across several sentences of real explanatory prose."
	hybridBody := prose + "\n\n" + appendix

	targetFull := filepath.Join(tempDir, doc.TargetPath)
	mdContent := fmt.Sprintf("# Test Documentation\n\n<!-- gmb:begin:overview -->\n%s\n<!-- gmb:end:overview -->\n", hybridBody)
	if err := os.WriteFile(targetFull, []byte(mdContent), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	diffRes, err := Diff(tempDir, RunOptions{HeadGraph: graph})
	if err != nil {
		t.Fatalf("Diff returned error: %v", err)
	}
	for _, s := range diffRes.Sections {
		if s.SectionID == "overview" && s.HasChange {
			t.Errorf("expected no change for a hybrid section whose appendix matches a fresh render (only prose text differs from any NoLLM candidate), got diff:\n%s", s.DiffPreview)
		}
	}
	if diffRes.HasChanges {
		t.Errorf("expected DiffResult.HasChanges=false for an up-to-date hybrid section, got true")
	}

	// Now corrupt just the appendix (simulating a real grounded-data
	// change) while leaving the prose untouched — this MUST be detected.
	corruptedBody := prose + "\n\n" + appendix + "\n\nThis table row was never generated by any renderer."
	mdContent = fmt.Sprintf("# Test Documentation\n\n<!-- gmb:begin:overview -->\n%s\n<!-- gmb:end:overview -->\n", corruptedBody)
	if err := os.WriteFile(targetFull, []byte(mdContent), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	diffRes, err = Diff(tempDir, RunOptions{HeadGraph: graph})
	if err != nil {
		t.Fatalf("Diff returned error: %v", err)
	}
	found := false
	for _, s := range diffRes.Sections {
		if s.SectionID == "overview" {
			found = true
			if !s.HasChange {
				t.Errorf("expected a change detected when the appendix content itself differs from a fresh render")
			}
		}
	}
	if !found {
		t.Fatal("overview section not present in diff result")
	}
}

// TestOperations_Diff_NeverRenderedSectionAlwaysReportsChange guards
// against a real edge case found while fixing the hybrid-appendix
// comparison above: a section with no managed zone content at all yet
// (freshly scaffolded, never rendered) whose grounded data happens to be
// empty — e.g. its scope matches no real files yet — must still report a
// pending change. Comparing an empty prior ("" from SplitProseAndAppendix
// on an empty string) against an empty fresh appendix (RenderReference
// Appendix legitimately returns "" when there's nothing to show) would
// otherwise agree they "match," silently reporting a section that has
// never been rendered as already up to date.
func TestOperations_Diff_NeverRenderedSectionAlwaysReportsChange(t *testing.T) {
	tempDir := t.TempDir()
	doc := config.DocSpec{
		ID:         "guide",
		TargetPath: "docs/guide.md",
		Title:      "Guide",
		Scope:      config.ScopeRule{Paths: []string{"internal/**"}},
		Sections: []config.SectionSpec{
			{ID: "overview", Title: "Overview", Managed: true, Instruction: "Describe the subsystem."},
		},
	}
	if err := updateDocsYAML(tempDir, doc); err != nil {
		t.Fatalf("updateDocsYAML: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tempDir, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	// No <!-- gmb:begin:overview --> zone at all: never rendered.
	if err := os.WriteFile(filepath.Join(tempDir, "docs/guide.md"), []byte("# Guide\n"), 0644); err != nil {
		t.Fatal(err)
	}

	diffRes, err := Diff(tempDir, RunOptions{})
	if err != nil {
		t.Fatalf("Diff returned error: %v", err)
	}
	found := false
	for _, s := range diffRes.Sections {
		if s.SectionID == "overview" {
			found = true
			if !s.HasChange {
				t.Error("expected a never-rendered section to always report a pending change")
			}
		}
	}
	if !found {
		t.Fatal("overview section not present in diff result")
	}
	if !diffRes.HasChanges {
		t.Error("expected DiffResult.HasChanges=true for a never-rendered section")
	}
}

func TestOperations_Export_JSONL_And_RAG(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	// Test RAG JSON export
	ragRes, err := Export(tempDir, "rag", ".glassmarble/rag")
	if err != nil {
		t.Fatalf("Export RAG failed: %v", err)
	}
	if ragRes.ChunksCount != 1 {
		t.Errorf("expected 1 exported chunk, got %d", ragRes.ChunksCount)
	}

	// Test JSONL export
	jsonlRes, err := Export(tempDir, "jsonl", ".glassmarble/rag")
	if err != nil {
		t.Fatalf("Export JSONL failed: %v", err)
	}
	if jsonlRes.ChunksCount != 1 {
		t.Errorf("expected 1 exported chunk in JSONL, got %d", jsonlRes.ChunksCount)
	}

	jsonlFile := filepath.Join(tempDir, ".glassmarble", "rag", "knowledge_base.jsonl")
	data, err := os.ReadFile(jsonlFile)
	if err != nil {
		t.Fatalf("failed to read knowledge_base.jsonl: %v", err)
	}

	var chunk RAGChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		t.Fatalf("invalid jsonl entry: %v", err)
	}
	if chunk.DocID != "testdoc" || chunk.SectionID != "overview" {
		t.Errorf("unexpected chunk content: %+v", chunk)
	}
}

func TestOperations_Release(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	guide, err := Release(tempDir, "HEAD~1", "HEAD", "")
	if err != nil {
		t.Fatalf("Release returned error: %v", err)
	}
	if !strings.Contains(guide, "# Migration Guide: HEAD~1 → HEAD") {
		t.Errorf("unexpected migration guide content:\n%s", guide)
	}
}

func TestOperations_ArchFeatures(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	// 1. ADR
	adrPath, err := GenerateADR(tempDir, archfeatures.ADREvent{
		Type:     "COMPONENT_SPLIT",
		Title:    "Decouple Architecture Features",
		Decision: "Extracted Phase 6 into subpackages",
	})
	if err != nil {
		t.Fatalf("GenerateADR failed: %v", err)
	}
	if !strings.Contains(adrPath, "0001-decouple-architecture-features.md") {
		t.Errorf("unexpected adr path: %s", adrPath)
	}
}

func TestOperations_SRE(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	// Error catalog
	errCat, err := GenerateErrorCatalog(tempDir)
	if err != nil {
		t.Fatalf("GenerateErrorCatalog failed: %v", err)
	}
	if !strings.Contains(errCat, "Living Error Code Catalog") {
		t.Errorf("unexpected error catalog:\n%s", errCat)
	}

	// CPG-aware wrapper with nil graph delegates to the ident-scan catalog.
	errCatG, err := GenerateErrorCatalogWithGraph(tempDir, nil)
	if err != nil {
		t.Fatalf("GenerateErrorCatalogWithGraph failed: %v", err)
	}
	if !strings.Contains(errCatG, "Living Error Code Catalog") {
		t.Errorf("unexpected graph error catalog:\n%s", errCatG)
	}
}

func TestOperations_EvalFaithfulnessLedgerAndRelevance(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	result, err := EvalFaithfulness(tempDir, 0, nil)
	if err != nil {
		t.Fatalf("EvalFaithfulness failed: %v", err)
	}
	if result.Samples != 1 {
		t.Fatalf("Samples = %d, want 1 (one non-empty managed section)", result.Samples)
	}
	if len(result.Docs) != 1 {
		t.Fatalf("Docs = %d, want 1", len(result.Docs))
	}
	de := result.Docs[0]
	if de.Relevance < 0 || de.Relevance > 1 {
		t.Errorf("per-doc Relevance out of range: %v", de.Relevance)
	}
	// Fixture body "Old content" shares no content token with instruction
	// "Explain module overview" → relevance 0 deterministically.
	if de.Relevance != 0 {
		t.Errorf("Relevance = %v, want 0 for unrelated body/instruction", de.Relevance)
	}
	if result.Relevance != de.Relevance {
		t.Errorf("global Relevance = %v, want per-doc %v (single doc)", result.Relevance, de.Relevance)
	}

	// D1 trend record appended: Kind eval, score/samples mirror result.
	data, err := os.ReadFile(filepath.Join(tempDir, ".glassmarble", "runs", "runs.jsonl"))
	if err != nil {
		t.Fatalf("eval must append a ledger record: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var rec ledger.RunRecord
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &rec); err != nil {
		t.Fatalf("ledger record is not JSON: %v", err)
	}
	if rec.Kind != "eval" {
		t.Errorf("ledger Kind = %q, want eval", rec.Kind)
	}
	if rec.EvalScore != result.GlobalScore || rec.EvalSamples != result.Samples {
		t.Errorf("ledger eval fields %+v must mirror result %+v", rec, result)
	}
	if rec.Commit != "" {
		t.Errorf("eval Commit must be empty (working-tree scoring), got %q", rec.Commit)
	}
}

// TestOperations_EvalFaithfulness_UsesRealGraphWhenProvided guards against
// the regression where EvalFaithfulness always called AssembleFactSheet
// with a nil graph, so Collector.CollectSectionFacts short-circuited to an
// empty GroundTruthPayload — a 100%-correct section scored as almost
// entirely unfaithful because the "ground truth" it was compared against
// was empty, not because anything in the section was actually wrong. With
// a real graph carrying the referenced symbol in scope, the same prose
// must score as supported.
func TestOperations_EvalFaithfulness_UsesRealGraphWhenProvided(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	targetFull := filepath.Join(tempDir, "docs", "testdoc.md")
	mdContent := "# Test Documentation\n\n<!-- gmb:begin:overview -->\n" +
		"The `Foo` function handles the module's core behavior.\n" +
		"<!-- gmb:end:overview -->\n"
	if err := os.WriteFile(targetFull, []byte(mdContent), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	fqn := "internal/testdoc/pkg.go::Foo"
	graph := akg.NewCodePropertyGraph("eval-test")
	graph.Nodes = graph.Nodes.Set(fqn, &link.ResolvedNode{
		ID:   fqn,
		Name: "Foo",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      "internal/testdoc/pkg.go",
			LineStart: 10,
			LineEnd:   12,
		},
		Properties: map[string]string{
			"signature":   "func Foo() error",
			"doc_comment": "Foo handles the module's core behavior.",
		},
	})

	without, err := EvalFaithfulness(tempDir, 0, nil)
	if err != nil {
		t.Fatalf("EvalFaithfulness(nil graph) failed: %v", err)
	}
	with, err := EvalFaithfulness(tempDir, 0, graph)
	if err != nil {
		t.Fatalf("EvalFaithfulness(real graph) failed: %v", err)
	}

	if without.GlobalScore >= with.GlobalScore {
		t.Errorf("expected score with a real graph (%v) to exceed score with nil graph (%v)",
			with.GlobalScore, without.GlobalScore)
	}
	if with.GlobalScore != 1.0 {
		t.Errorf("expected fully-grounded prose to score 1.0 with the real graph, got %v (unsupported: %v)",
			with.GlobalScore, with.Docs[0].Unsupported)
	}
}

func TestOperations_EvalArchetypeScores(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	// Second doc with a different archetype: per-archetype means must
	// carry both keys (module from fixture, runbook here).
	second := config.DocSpec{
		ID:         "rundoc",
		TargetPath: "docs/rundoc.md",
		Title:      "Runbook",
		Archetype:  "runbook",
		Audience:   "Operators",
		Scope:      config.ScopeRule{Paths: []string{"internal/testdoc/**"}},
		Sections: []config.SectionSpec{
			{ID: "steps", Title: "Steps", Managed: true, Instruction: "List restart steps"},
		},
	}
	if err := updateDocsYAML(tempDir, second); err != nil {
		t.Fatalf("adding second doc: %v", err)
	}
	targetFull := filepath.Join(tempDir, second.TargetPath)
	_ = os.MkdirAll(filepath.Dir(targetFull), 0755)
	_ = os.WriteFile(targetFull, []byte("# Runbook\n\n<!-- gmb:begin:steps -->\nRestart steps\n<!-- gmb:end:steps -->\n"), 0644)

	result, err := EvalFaithfulness(tempDir, 0, nil)
	if err != nil {
		t.Fatalf("EvalFaithfulness failed: %v", err)
	}
	if len(result.ArchetypeScores) != 2 {
		t.Fatalf("ArchetypeScores = %v, want module + runbook keys", result.ArchetypeScores)
	}
	for _, key := range []string{"module", "runbook"} {
		s, ok := result.ArchetypeScores[key]
		if !ok {
			t.Errorf("ArchetypeScores missing %q: %v", key, result.ArchetypeScores)
			continue
		}
		if s < 0 || s > 1 {
			t.Errorf("archetype %q score out of range: %v", key, s)
		}
	}
	// Docs without archetype group under "custom".
	if _, ok := result.ArchetypeScores["custom"]; ok {
		t.Errorf("no custom-archetype docs present, want no custom key: %v", result.ArchetypeScores)
	}

	// Ledger eval record mirrors the archetype map for trend tracking.
	data, err := os.ReadFile(filepath.Join(tempDir, ".glassmarble", "runs", "runs.jsonl"))
	if err != nil {
		t.Fatalf("eval must append a ledger record: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var rec ledger.RunRecord
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &rec); err != nil {
		t.Fatalf("ledger record is not JSON: %v", err)
	}
	if len(rec.EvalArchetypes) != 2 {
		t.Errorf("ledger EvalArchetypes = %v, want module + runbook", rec.EvalArchetypes)
	}
}

func TestOperations_Compliance(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	// Config dict
	configDict, err := GenerateConfigDictionary(tempDir)
	if err != nil {
		t.Fatalf("GenerateConfigDictionary failed: %v", err)
	}
	if !strings.Contains(configDict, "Runtime Configuration & Environment Variable Dictionary") {
		t.Errorf("unexpected config dictionary:\n%s", configDict)
	}
}

func TestOperations_DevExAndPublishing(t *testing.T) {
	// Snippet verification
	docWithSnippet := "# Doc\n<!-- gmb:snippet:example -->\n```go\npackage main\nfunc main() {}\n```\n"
	errs, err := VerifyCodeSnippets(docWithSnippet, nil)
	if err != nil {
		t.Fatalf("VerifyCodeSnippets failed: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("expected 0 snippet errors, got %d", len(errs))
	}
}

func TestComputeFreshnessScore_ArchEventsDecay(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	writeScope := func(content string) {
		t.Helper()
		full := filepath.Join(repo, "scope", "a.txt")
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	runGit("init", "-q")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test")
	writeScope("v1")
	runGit("add", ".")
	runGit("commit", "-qm", "feat: add thing")
	hashA := runGit("rev-parse", "HEAD")
	writeScope("v2")
	runGit("add", ".")
	runGit("commit", "-qm", "feat: add second thing")

	doc := config.DocSpec{
		ID:         "fresh",
		TargetPath: "docs/fresh.md",
		Scope:      config.ScopeRule{Paths: []string{"scope/**"}},
	}

	// One in-scope commit behind HEAD either way.
	plainScore, plainBehind := ComputeFreshnessScore(repo, doc, hashA)
	eventScore, eventBehind := ComputeFreshnessScoreWithArchEvents(repo, doc, hashA, []string{"LAYER_VIOLATION"})
	if plainBehind != 1 || eventBehind != 1 {
		t.Fatalf("expected 1 commit behind, got plain=%d events=%d", plainBehind, eventBehind)
	}
	// B3: dossier arch events raise the decay base (1.0 vs 0.5), so the
	// events-aware score must decay harder than the event-less score.
	if eventScore >= plainScore {
		t.Errorf("expected events-aware score < plain score, got events=%d plain=%d", eventScore, plainScore)
	}
	// Empty events behave like the dossier-less entry point.
	emptyScore, _ := ComputeFreshnessScoreWithArchEvents(repo, doc, hashA, nil)
	if emptyScore != plainScore {
		t.Errorf("expected empty events to match plain score (%d), got %d", plainScore, emptyScore)
	}
	// Fully synced HEAD scores 100 regardless of events.
	head := runGit("rev-parse", "HEAD")
	fullScore, fullBehind := ComputeFreshnessScoreWithArchEvents(repo, doc, head, []string{"CYCLE_INTRODUCED"})
	if fullScore != 100 || fullBehind != 0 {
		t.Errorf("expected (100, 0) when synced, got (%d, %d)", fullScore, fullBehind)
	}
}
