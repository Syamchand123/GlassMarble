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

