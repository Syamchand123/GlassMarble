package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocCommandHelp(t *testing.T) {
	tempDir := t.TempDir()
	out, err := runGmbCommand(t, "doc", "--help", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc --help failed: %v\n%s", err, out)
	}
	for _, want := range []string{"Documentation Intelligence Engine", "check", "diff", "init", "status", "gaps", "release", "report", "export", "view"} {
		if !strings.Contains(out, want) {
			t.Errorf("doc --help output missing %q:\n%s", want, out)
		}
	}
}

func TestDocCommandNoConfig(t *testing.T) {
	tempDir := t.TempDir()
	out, err := runGmbCommand(t, "doc", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc failed on empty dir: %v\n%s", err, out)
	}
}

func TestDocCommandJSON(t *testing.T) {
	tempDir := t.TempDir()
	out, err := runGmbCommand(t, "doc", "--dir", tempDir, "--json")
	if err != nil {
		t.Fatalf("doc --json failed: %v\n%s", err, out)
	}
	for _, want := range []string{`"docs_updated"`, `"sections_processed"`, `"tokens_used"`} {
		if !strings.Contains(out, want) {
			t.Errorf("doc --json output missing %s:\n%s", want, out)
		}
	}
}

func TestDocCheckNoConfig(t *testing.T) {
	tempDir := t.TempDir()
	out, err := runGmbCommand(t, "doc", "check", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc check failed on empty dir: %v\n%s", err, out)
	}
	if !strings.Contains(out, "all 0 document(s) fresh") {
		t.Errorf("expected 0 documents fresh, got:\n%s", out)
	}
}

func TestDocCheckWithMissingTarget(t *testing.T) {
	tempDir := t.TempDir()
	gmDir := filepath.Join(tempDir, ".glassmarble")
	if err := os.MkdirAll(gmDir, 0755); err != nil {
		t.Fatal(err)
	}
	docsYAML := `version: 1
docs_dir: "docs"
documents:
  - id: "test-doc"
    target: "docs/test.md"
    title: "Test"
    purpose: "Testing"
    audience: "Developers"
    scope:
      paths: ["cmd/**"]
`
	if err := os.WriteFile(filepath.Join(gmDir, "docs.yaml"), []byte(docsYAML), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := runGmbCommand(t, "doc", "check", "--dir", tempDir)
	if err == nil {
		t.Fatalf("expected doc check to fail on missing file, got success:\n%s", out)
	}
	if !strings.Contains(err.Error(), "documentation drift detected") {
		t.Errorf("expected drift error, got: %v", err)
	}
	if !strings.Contains(out, "docs/test.md: file missing") {
		t.Errorf("expected missing file output, got:\n%s", out)
	}
}

func TestDocStatusJSON(t *testing.T) {
	tempDir := t.TempDir()
	out, err := runGmbCommand(t, "doc", "status", "--dir", tempDir, "--json")
	if err != nil {
		t.Fatalf("doc status --json failed: %v\n%s", err, out)
	}
	for _, want := range []string{`"AllFresh"`, `"Documents"`} {
		if !strings.Contains(out, want) {
			t.Errorf("doc status --json output missing %s:\n%s", want, out)
		}
	}
}

func TestDocDiffNoConfig(t *testing.T) {
	tempDir := t.TempDir()
	out, err := runGmbCommand(t, "doc", "diff", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc diff failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "0 document(s) configured") {
		t.Errorf("unexpected doc diff output:\n%s", out)
	}
}

func TestDocInitCLI(t *testing.T) {
	tempDir := t.TempDir()
	out, err := runGmbCommand(t, "doc", "init", "docs/auth.md",
		"--dir", tempDir,
		"--scope", "internal/auth/**",
		"--archetype", "architecture",
		"--title", "Auth Specs",
	)
	if err != nil {
		t.Fatalf("doc init failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Scaffolded docs/auth.md") {
		t.Errorf("doc init output missing success message:\n%s", out)
	}

	target := filepath.Join(tempDir, "docs", "auth.md")
	if _, err := os.Stat(target); os.IsNotExist(err) {
		t.Errorf("doc init did not create target file at %s", target)
	}
}

func TestDocGapsJSON(t *testing.T) {
	tempDir := t.TempDir()
	out, err := runGmbCommand(t, "doc", "gaps", "--dir", tempDir, "--json")
	if err != nil {
		t.Fatalf("doc gaps --json failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"gaps"`) {
		t.Errorf("doc gaps --json output missing 'gaps':\n%s", out)
	}
}

func TestDocSuggestJSON(t *testing.T) {
	tempDir := t.TempDir()
	out, err := runGmbCommand(t, "doc", "suggest", "--dir", tempDir, "--json")
	if err != nil {
		t.Fatalf("doc suggest --json failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "[") {
		t.Errorf("doc suggest --json expected json array:\n%s", out)
	}
}

func TestDocReportJSON(t *testing.T) {
	tempDir := t.TempDir()
	out, err := runGmbCommand(t, "doc", "report", "--dir", tempDir, "--json")
	if err != nil {
		t.Fatalf("doc report --json failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"total_documents"`) {
		t.Errorf("doc report --json output missing 'total_documents':\n%s", out)
	}
}

func TestDocExportJSON(t *testing.T) {
	tempDir := t.TempDir()
	// First init a doc so export has content
	_, _ = runGmbCommand(t, "doc", "init", "docs/test.md",
		"--dir", tempDir,
		"--scope", "cmd/**",
		"--archetype", "module",
	)

	out, err := runGmbCommand(t, "doc", "export", "--dir", tempDir, "--json")
	if err != nil {
		t.Fatalf("doc export --json failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"chunks_count"`) {
		t.Errorf("doc export --json output missing 'chunks_count':\n%s", out)
	}
}

func TestDocRelease(t *testing.T) {
	tempDir := t.TempDir()
	out, err := runGmbCommand(t, "doc", "release", "v1.0.0..v1.1.0", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc release failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "# Migration Guide: v1.0.0 → v1.1.0") {
		t.Errorf("doc release missing migration header:\n%s", out)
	}
}

