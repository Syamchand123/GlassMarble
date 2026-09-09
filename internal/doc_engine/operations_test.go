package doc_engine

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/archfeatures"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
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

func TestOperations_Report(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	rep, err := Report(tempDir)
	if err != nil {
		t.Fatalf("Report returned error: %v", err)
	}

	if rep.TotalDocuments != 1 {
		t.Errorf("expected 1 document in report, got %d", rep.TotalDocuments)
	}
	if rep.TotalSections != 1 {
		t.Errorf("expected 1 section in report, got %d", rep.TotalSections)
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

func TestOperations_Suggest_And_Gaps(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	// Create an undocumented package
	pkgDir := filepath.Join(tempDir, "internal", "undoc")
	_ = os.MkdirAll(pkgDir, 0755)
	_ = os.WriteFile(filepath.Join(pkgDir, "undoc.go"), []byte("package undoc\nfunc Run() {}\n"), 0644)

	gaps, err := Gaps(tempDir)
	if err != nil {
		t.Fatalf("Gaps returned error: %v", err)
	}

	foundUndoc := false
	for _, g := range gaps.Gaps {
		if strings.Contains(g.Package, "undoc") {
			foundUndoc = true
			break
		}
	}
	if !foundUndoc {
		t.Logf("Gaps detected: %+v", gaps.Gaps)
	}

	suggestions, err := Suggest(tempDir)
	if err != nil {
		t.Fatalf("Suggest returned error: %v", err)
	}
	if len(suggestions) == 0 {
		t.Logf("No suggestions generated for gaps")
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

func TestOperations_Snapshot(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	snap, err := Snapshot(tempDir, "v1.2.0")
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	if snap.FilesCount != 1 {
		t.Errorf("expected 1 file in snapshot, got %d", snap.FilesCount)
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

	// 2. Evolution
	evo, err := GenerateEvolutionChapter(tempDir, "", "")
	if err != nil {
		t.Fatalf("GenerateEvolutionChapter failed: %v", err)
	}
	if !strings.Contains(evo, "System Evolution & Architectural History") {
		t.Errorf("unexpected evolution chapter content:\n%s", evo)
	}

	// 3. Glossary
	glossary, err := GenerateDomainGlossary(tempDir)
	if err != nil {
		t.Fatalf("GenerateDomainGlossary failed: %v", err)
	}
	if !strings.Contains(glossary, "Ubiquitous Language & Domain Glossary") {
		t.Errorf("unexpected glossary content:\n%s", glossary)
	}
}

func TestOperations_SRE(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	// Subsystem health
	health := ComputeSubsystemHealth(tempDir, "docs")
	_ = health

	// Error catalog
	errCat, err := GenerateErrorCatalog(tempDir)
	if err != nil {
		t.Fatalf("GenerateErrorCatalog failed: %v", err)
	}
	if !strings.Contains(errCat, "Living Error Code Catalog") {
		t.Errorf("unexpected error catalog:\n%s", errCat)
	}

	// Concurrency
	conc, err := GenerateConcurrencyContracts(tempDir)
	if err != nil {
		t.Fatalf("GenerateConcurrencyContracts failed: %v", err)
	}
	if !strings.Contains(conc, "Concurrency, Threading & Lock Contention Reference") {
		t.Errorf("unexpected concurrency contracts:\n%s", conc)
	}

	// Test topology
	topo, err := GenerateTestTopology(tempDir)
	if err != nil {
		t.Fatalf("GenerateTestTopology failed: %v", err)
	}
	if !strings.Contains(topo, "Test Topology & Verification Coverage Matrix") {
		t.Errorf("unexpected test topology:\n%s", topo)
	}
}

func TestOperations_Compliance(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	// Threat model
	threat, err := GenerateThreatModel(tempDir)
	if err != nil {
		t.Fatalf("GenerateThreatModel failed: %v", err)
	}
	if !strings.Contains(threat, "System Threat Model & Security Architecture") {
		t.Errorf("unexpected threat model:\n%s", threat)
	}

	// Config dict
	configDict, err := GenerateConfigDictionary(tempDir)
	if err != nil {
		t.Fatalf("GenerateConfigDictionary failed: %v", err)
	}
	if !strings.Contains(configDict, "Runtime Configuration & Environment Variable Dictionary") {
		t.Errorf("unexpected config dictionary:\n%s", configDict)
	}

	// SBOM
	goMod := "module example.com/app\ngo 1.22\nrequire github.com/spf13/cobra v1.8.0\n"
	_ = os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goMod), 0644)
	sbom, err := GenerateSBOM(tempDir)
	if err != nil {
		t.Fatalf("GenerateSBOM failed: %v", err)
	}
	if !strings.Contains(sbom, "Software Bill of Materials") {
		t.Errorf("unexpected SBOM:\n%s", sbom)
	}

	// API surface
	boundaries, err := CheckAPISurfaceBoundaries(tempDir)
	if err != nil {
		t.Fatalf("CheckAPISurfaceBoundaries failed: %v", err)
	}
	_ = boundaries
}

func TestOperations_DevExAndPublishing(t *testing.T) {
	tempDir := setupTestRepoWithDoc(t)

	// Snippet verification
	docWithSnippet := "# Doc\n<!-- gmb:snippet:example -->\n```go\npackage main\nfunc main() {}\n```\n"
	errs, err := VerifyCodeSnippets(docWithSnippet, nil)
	if err != nil {
		t.Fatalf("VerifyCodeSnippets failed: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("expected 0 snippet errors, got %d", len(errs))
	}

	// Platform formatting
	rawDoc := "> [!NOTE]\n> Notice message\n"
	vp := FormatForPlatform(rawDoc, "vitepress")
	if !strings.Contains(vp, "::: info") {
		t.Errorf("expected vitepress admonition, got:\n%s", vp)
	}

	// Federation manifest
	manifest, err := ExportFederationManifest(tempDir)
	if err != nil {
		t.Fatalf("ExportFederationManifest failed: %v", err)
	}
	if _, err := os.Stat(manifest); os.IsNotExist(err) {
		t.Errorf("manifest file missing at: %s", manifest)
	}

	// i18n inventory
	i18nDoc, err := GenerateI18nInventory(tempDir)
	if err != nil {
		t.Fatalf("GenerateI18nInventory failed: %v", err)
	}
	if !strings.Contains(i18nDoc, "Internationalization (i18n) & Localization Inventory") {
		t.Errorf("unexpected i18n inventory:\n%s", i18nDoc)
	}
}

