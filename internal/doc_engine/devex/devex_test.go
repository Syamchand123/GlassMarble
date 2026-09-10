package devex

import (
	"encoding/json"
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

func TestVerifySnippetsCRLF(t *testing.T) {
	tempDir := t.TempDir()
	pkgDir := filepath.Join(tempDir, "greet")
	_ = os.MkdirAll(pkgDir, 0755)
	code := "package greet\n\n// Greet greets.\nfunc Greet(name string) string { return name }\n"
	_ = os.WriteFile(filepath.Join(pkgDir, "greet.go"), []byte(code), 0644)

	// Windows checkout: every line ends with CRLF. Snippet extraction,
	// verification, and fix application must behave identically to LF.
	docLF := "# Guide\n<!-- gmb:snippet:example -->\n```go\nGreet()\n```\n"
	docCRLF := strings.ReplaceAll(docLF, "\n", "\r\n")

	errsLF, err := VerifySnippetsInRepo(tempDir, docLF, nil)
	if err != nil {
		t.Fatalf("LF verify failed: %v", err)
	}
	errsCRLF, err := VerifySnippetsInRepo(tempDir, docCRLF, nil)
	if err != nil {
		t.Fatalf("CRLF verify failed: %v", err)
	}
	if len(errsCRLF) == 0 {
		t.Fatalf("CRLF markdown produced zero snippet errors (extraction silently failed); LF produced %d", len(errsLF))
	}
	if len(errsCRLF) != len(errsLF) {
		t.Errorf("CRLF/LF parity: CRLF=%d errors, LF=%d errors", len(errsCRLF), len(errsLF))
	}
	fixed := ApplySnippetFixes(docCRLF, errsCRLF)
	if !strings.Contains(fixed, "Greet(name string)") {
		t.Errorf("CRLF fix did not apply signature:\n%s", fixed)
	}
}

func TestVerifySnippetsInRepoArity(t *testing.T) {
	tempDir := t.TempDir()
	pkgDir := filepath.Join(tempDir, "greet")
	_ = os.MkdirAll(pkgDir, 0755)
	code := `package greet

// Greet greets a person.
func Greet(name string, age int) string { return name }
`
	_ = os.WriteFile(filepath.Join(pkgDir, "greet.go"), []byte(code), 0644)

	doc := `
# Guide
<!-- gmb:snippet:example -->
` + "```go\n" + `package main

func main() {
	Greet("bob")
}
` + "```\n"

	errs, err := VerifySnippetsInRepo(tempDir, doc, nil)
	if err != nil {
		t.Fatalf("VerifySnippetsInRepo failed: %v", err)
	}
	found := false
	for _, e := range errs {
		if e.Symbol == "Greet" && strings.Contains(e.ErrorMessage, "argument") {
			found = true
			if e.SuggestedFix == "" {
				t.Errorf("expected SuggestedFix with correct signature, got empty")
			}
			if !strings.Contains(e.SuggestedFix, "Greet") {
				t.Errorf("expected signature fix mentioning Greet, got %q", e.SuggestedFix)
			}
		}
	}
	if !found {
		t.Fatalf("expected arity mismatch for Greet, got %+v", errs)
	}

	// Correct arity must not report.
	fixed := strings.Replace(doc, `Greet("bob")`, `Greet("bob", 3)`, 1)
	errs, _ = VerifySnippetsInRepo(tempDir, fixed, nil)
	for _, e := range errs {
		if e.Symbol == "Greet" && strings.Contains(e.ErrorMessage, "argument") {
			t.Errorf("unexpected arity error on correct call: %+v", e)
		}
	}
}

func TestApplySnippetFixes(t *testing.T) {
	md := "# Doc\n\nline two\n\tGreet(\"bob\")\nline four\n"
	errs := []SnippetError{
		{LineNumber: 4, Symbol: "Greet", SuggestedFix: `Greet("bob", 3)`},
		{LineNumber: 99, Symbol: "Nope", SuggestedFix: "x"},
		{LineNumber: 2, Symbol: "Skip"},
	}
	out := ApplySnippetFixes(md, errs)
	lines := strings.Split(out, "\n")
	if lines[3] != "\t"+`Greet("bob", 3)` {
		t.Errorf("expected indented fix on line 4, got %q", lines[3])
	}
	if lines[2] != "line two" {
		t.Errorf("untouched lines must be preserved, got %q", lines[2])
	}
}

func TestExportKnowledgeBaseChunkSymbols(t *testing.T) {
	tempDir := t.TempDir()

	cfgPath := config.DocsConfigPath(tempDir)
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0755)
	data := "version: 1\ndocs_dir: docs\ndocuments:\n  - id: auth\n    target: docs/auth.md\n    title: Auth Guide\n    scope:\n      paths: [\"internal/auth/**\"]\n    sections:\n      - id: intro\n        title: Intro\n        managed: true\n"
	_, _ = storage.AtomicWriteFile(cfgPath, []byte(data))

	targetFull := filepath.Join(tempDir, "docs", "auth.md")
	_ = os.MkdirAll(filepath.Dir(targetFull), 0755)
	md := "# Auth Guide\n\n<!-- gmb:begin:intro -->\nCall `Authenticate` then `ValidateToken` to log in.\n<!-- gmb:end:intro -->\n"
	_, _ = storage.AtomicWriteFile(targetFull, []byte(md))

	// Repo declares Authenticate so the chunk symbol resolves to file#line.
	authDir := filepath.Join(tempDir, "internal", "auth")
	_ = os.MkdirAll(authDir, 0755)
	_ = os.WriteFile(filepath.Join(authDir, "auth.go"), []byte("package auth\n\n// Authenticate logs in.\nfunc Authenticate(user string) error { return nil }\n"), 0644)

	summary, err := ExportKnowledgeBase(tempDir, "rag", ".glassmarble/rag")
	if err != nil {
		t.Fatalf("ExportKnowledgeBase failed: %v", err)
	}
	if summary.TotalChunks != 1 {
		t.Fatalf("expected 1 chunk, got %d", summary.TotalChunks)
	}
	chunkFile := filepath.Join(tempDir, ".glassmarble", "rag", "auth_intro.json")
	raw, err := os.ReadFile(chunkFile)
	if err != nil {
		t.Fatalf("reading chunk file: %v", err)
	}
	var chunk RAGChunk
	if err := json.Unmarshal(raw, &chunk); err != nil {
		t.Fatalf("parsing chunk JSON: %v", err)
	}
	hasAuth := false
	for _, s := range chunk.Symbols {
		if s == "Authenticate" {
			hasAuth = true
		}
	}
	if !hasAuth {
		t.Errorf("expected Authenticate in chunk symbols, got %v", chunk.Symbols)
	}
	if len(chunk.Symbols) > 50 {
		t.Errorf("expected symbols capped at 50, got %d", len(chunk.Symbols))
	}
	foundRef := false
	for _, r := range chunk.FileRefs {
		if strings.HasPrefix(r, "Authenticate@") && strings.Contains(r, "internal/auth/auth.go#") {
			foundRef = true
		}
	}
	if !foundRef {
		t.Errorf("expected Authenticate file#line ref, got %v", chunk.FileRefs)
	}
}
