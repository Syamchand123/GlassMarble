package doc_engine

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/archfeatures"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/ledger"
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

	result, err := EvalFaithfulness(tempDir, 0)
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

	result, err := EvalFaithfulness(tempDir, 0)
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
