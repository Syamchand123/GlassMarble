package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeP3Fixture(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	gmDir := filepath.Join(tempDir, ".glassmarble")
	if err := os.MkdirAll(gmDir, 0755); err != nil {
		t.Fatal(err)
	}
	docsYAML := "version: 1\ndocs_dir: docs\ndocuments:\n  - id: guide\n    target: docs/guide.md\n    title: Guide\n    purpose: Testing\n    audience: Engineers\n    scope:\n      paths:\n        - \"internal/**\"\n    sections:\n      - id: overview\n        title: Overview\n        instruction: Describe the subsystem.\n"
	if err := os.WriteFile(filepath.Join(gmDir, "docs.yaml"), []byte(docsYAML), 0644); err != nil {
		t.Fatal(err)
	}
	docsDir := filepath.Join(tempDir, "docs")
	_ = os.MkdirAll(docsDir, 0755)
	body := "# Guide\n\n<!-- gmb:begin:overview -->\nDescribe the subsystem interface completely.\n<!-- gmb:end:overview -->\n"
	if err := os.WriteFile(filepath.Join(docsDir, "guide.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return tempDir
}

func TestDocEvalJSON(t *testing.T) {
	tempDir := writeP3Fixture(t)
	out, err := runGmbCommand(t, "doc", "eval", "--dir", tempDir, "--json")
	if err != nil {
		t.Fatalf("doc eval --json failed: %v\n%s", err, out)
	}
	for _, want := range []string{`"global_score"`, `"samples"`, `"docs"`} {
		if !strings.Contains(out, want) {
			t.Errorf("doc eval --json output missing %s:\n%s", want, out)
		}
	}
}

func TestDocLedgerEmpty(t *testing.T) {
	tempDir := writeP3Fixture(t)
	out, err := runGmbCommand(t, "doc", "ledger", "--dir", tempDir, "--json")
	if err != nil {
		t.Fatalf("doc ledger --json failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"runs"`) {
		t.Errorf("doc ledger --json output missing 'runs':\n%s", out)
	}
}

func TestDocReviewFlow(t *testing.T) {
	tempDir := writeP3Fixture(t)
	out, err := runGmbCommand(t, "doc", "review", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc review failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no pending items") {
		t.Errorf("expected empty review queue, got:\n%s", out)
	}
	out, err = runGmbCommand(t, "doc", "review", "--stats", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc review --stats failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "0 pending") {
		t.Errorf("expected 0 pending, got:\n%s", out)
	}
	out, err = runGmbCommand(t, "doc", "review", "approve", "nope", "--dir", tempDir)
	if err == nil {
		t.Errorf("expected failure resolving unknown review id, got success:\n%s", out)
	}
}
