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
	for _, want := range []string{"Documentation Intelligence Engine", "check", "diff", "init", "status", "release", "export", "view"} {
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

// TestDocInitCLI_CommaSeparatedScopeAndEntryPoints guards against a
// regression where --scope/--entry-points (Cobra StringArray flags, meant
// to be repeated for multiple values) silently stored a single comma-
// separated value as ONE literal glob/FQN containing a comma — which
// matches nothing, with no error or warning, so the resulting document
// would never ground any content. A natural single
// --scope "a/**,b/**" is a reasonable thing to type instead of repeating
// the flag, so it must split into separate values too.
func TestDocInitCLI_CommaSeparatedScopeAndEntryPoints(t *testing.T) {
	tempDir := t.TempDir()
	out, err := runGmbCommand(t, "doc", "init", "docs/architecture.md",
		"--dir", tempDir,
		"--scope", "pkg/**,cmd/**",
		"--archetype", "architecture",
		"--title", "Architecture",
		"--entry-points", "pkg/a.go::Foo,pkg/b.go::Bar",
	)
	if err != nil {
		t.Fatalf("doc init failed: %v\n%s", err, out)
	}

	data, err := os.ReadFile(filepath.Join(tempDir, ".glassmarble", "docs.yaml"))
	if err != nil {
		t.Fatalf("reading docs.yaml: %v", err)
	}
	content := string(data)
	for _, want := range []string{"pkg/**", "cmd/**", "pkg/a.go::Foo", "pkg/b.go::Bar"} {
		if !strings.Contains(content, want) {
			t.Errorf("docs.yaml missing split value %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "pkg/**,cmd/**") || strings.Contains(content, "pkg/a.go::Foo,pkg/b.go::Bar") {
		t.Errorf("docs.yaml still has an unsplit comma-joined value:\n%s", content)
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

func TestDocCheck_VerifySnippets(t *testing.T) {
	tempDir := t.TempDir()
	out, err := runGmbCommand(t, "doc", "check", "--verify-snippets", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc check --verify-snippets failed: %v\n%s", err, out)
	}

	// Broken snippet
	gmDir := filepath.Join(tempDir, ".glassmarble")
	_ = os.MkdirAll(gmDir, 0755)
	docsDir := filepath.Join(tempDir, "docs")
	_ = os.MkdirAll(docsDir, 0755)
	brokenFile := filepath.Join(docsDir, "broken.md")
	_ = os.WriteFile(brokenFile, []byte("# Broken\n<!-- gmb:snippet:example -->\n```go\nfunc broken( {\n```\n"), 0644)
	docsYAML := "version: 1\ndocs_dir: docs\ndocuments:\n  - id: broken\n    target: docs/broken.md\n    title: Broken doc\n    scope:\n      paths:\n        - \"docs/**\"\n    sections:\n      - id: main\n        title: Main\n        instruction: Document the state.\n"
	_ = os.WriteFile(filepath.Join(gmDir, "docs.yaml"), []byte(docsYAML), 0644)

	out, err = runGmbCommand(t, "doc", "check", "--verify-snippets", "--dir", tempDir)
	if err == nil {
		t.Fatalf("expected failure for broken snippet, got success:\n%s", out)
	}
}


