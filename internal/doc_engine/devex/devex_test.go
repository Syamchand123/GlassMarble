package devex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
)

func TestVerifyCodeSnippets(t *testing.T) {
	// Valid snippet with known symbol
	validDoc := `
# Guide
<!-- gmb:snippet:example -->
` + "```go\n" + `package main
import "fmt"

func main() {
	fmt.Println("Hello world")
}
` + "```\n"

	errs, err := VerifyCodeSnippets(validDoc, map[string]bool{"Println": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("expected 0 snippet errors, got %d: %+v", len(errs), errs)
	}

	// Broken syntax snippet
	brokenDoc := `
# Broken
<!-- gmb:snippet:example -->
` + "```go\n" + `func main( { fmt.Println("syntax error") }` + "\n```\n"

	errs, _ = VerifyCodeSnippets(brokenDoc, nil)
	if len(errs) == 0 {
		t.Errorf("expected syntax error on broken snippet")
	}
}

func TestRecordAndLoadQueryGaps(t *testing.T) {
	tempDir := t.TempDir()

	err := RecordQueryGap(tempDir, "how to configure auth", "auth", "docs/auth.md")
	if err != nil {
		t.Fatalf("RecordQueryGap failed: %v", err)
	}

	// Record again to test count increment
	err = RecordQueryGap(tempDir, "how to configure auth", "auth", "docs/auth.md")
	if err != nil {
		t.Fatalf("RecordQueryGap second time failed: %v", err)
	}

	gaps, err := LoadQueryGaps(tempDir)
	if err != nil {
		t.Fatalf("LoadQueryGaps failed: %v", err)
	}
	if len(gaps) != 1 {
		t.Fatalf("expected 1 gap, got %d", len(gaps))
	}
	if gaps[0].Count != 2 {
		t.Errorf("expected count 2, got %d", gaps[0].Count)
	}
	if gaps[0].MissingFrom != "docs/auth.md" {
		t.Errorf("expected missing_from 'docs/auth.md', got %q", gaps[0].MissingFrom)
	}
}

func TestGenerateMigrationGuide(t *testing.T) {
	tempDir := t.TempDir()

	guide, err := GenerateMigrationGuide(tempDir, "v1.0.0", "v1.1.0", "")
	if err != nil {
		t.Fatalf("GenerateMigrationGuide failed: %v", err)
	}

	if !strings.Contains(guide, "# Migration Guide: v1.0.0 → v1.1.0") {
		t.Errorf("missing migration title in output:\n%s", guide)
	}
	if !strings.Contains(guide, "Executive Summary") {
		t.Errorf("missing Executive Summary in output:\n%s", guide)
	}
}

func TestExportKnowledgeBase(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Create docs.yaml
	cfgPath := config.DocsConfigPath(tempDir)
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0755)

	// Write docs.yaml
	data := "version: 1\ndocs_dir: docs\ndocuments:\n  - id: auth\n    target: docs/auth.md\n    title: Auth Guide\n    scope:\n      paths: [\"internal/auth/**\"]\n    sections:\n      - id: intro\n        title: Intro\n        managed: true\n"
	_, _ = storage.AtomicWriteFile(cfgPath, []byte(data))

	// 2. Write markdown with managed zone
	targetFull := filepath.Join(tempDir, "docs", "auth.md")
	_ = os.MkdirAll(filepath.Dir(targetFull), 0755)
	md := "# Auth Guide\n\n<!-- gmb:begin:intro -->\nAuth module handles PKCE.\n<!-- gmb:end:intro -->\n"
	_, _ = storage.AtomicWriteFile(targetFull, []byte(md))

	// 3. Export as RAG
	summary, err := ExportKnowledgeBase(tempDir, "rag", ".glassmarble/rag")
	if err != nil {
		t.Fatalf("ExportKnowledgeBase failed: %v", err)
	}
	if summary.TotalChunks != 1 {
		t.Errorf("expected 1 exported chunk, got %d", summary.TotalChunks)
	}

	// 4. Export as JSONL
	jsonlSummary, err := ExportKnowledgeBase(tempDir, "jsonl", ".glassmarble/rag")
	if err != nil {
		t.Fatalf("ExportKnowledgeBase JSONL failed: %v", err)
	}
	if jsonlSummary.TotalChunks != 1 {
		t.Errorf("expected 1 exported chunk in JSONL, got %d", jsonlSummary.TotalChunks)
	}
}
