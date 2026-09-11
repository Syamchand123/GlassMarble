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

func TestDocExportState(t *testing.T) {
	tempDir := writeP3Fixture(t)
	out, err := runGmbCommand(t, "doc", "export", "--format", "state", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc export --format state failed: %v\n%s", err, out)
	}
	for _, want := range []string{`"schema_version"`, `"documents"`} {
		if !strings.Contains(out, want) {
			t.Errorf("doc export --format state output missing %s:\n%s", want, out)
		}
	}
}

func TestDocExportStateOutFile(t *testing.T) {
	tempDir := writeP3Fixture(t)
	target := filepath.Join(tempDir, "state.json")
	out, err := runGmbCommand(t, "doc", "export", "--format", "state", "--out", target, "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc export --format state --out failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("state file not written: %v\n%s", err, out)
	}
	for _, want := range []string{`"schema_version"`, `"documents"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("state file missing %s:\n%s", want, data)
		}
	}
}

func TestDocLangmatrix(t *testing.T) {
	tempDir := writeP3Fixture(t)
	out, err := runGmbCommand(t, "doc", "langmatrix", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc langmatrix failed: %v\n%s", err, out)
	}
	for _, want := range []string{"language", "signatures", "go"} {
		if !strings.Contains(out, want) {
			t.Errorf("doc langmatrix output missing %q:\n%s", want, out)
		}
	}
	out, err = runGmbCommand(t, "doc", "langmatrix", "--json", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc langmatrix --json failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"go"`) {
		t.Errorf("doc langmatrix --json output missing known language \"go\":\n%s", out)
	}
}

func TestDocHelpListsLangmatrix(t *testing.T) {
	tempDir := t.TempDir()
	out, err := runGmbCommand(t, "doc", "--help", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc --help failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "langmatrix") {
		t.Errorf("doc --help must list langmatrix:\n%s", out)
	}
}

func TestDocReviewRecordRevert(t *testing.T) {
	tempDir := writeP3Fixture(t)
	// Missing --reason must fail.
	if out, err := runGmbCommand(t, "doc", "review", "record-revert", "--dir", tempDir); err == nil {
		t.Errorf("expected failure without --reason, got success:\n%s", out)
	}
	out, err := runGmbCommand(t, "doc", "review", "record-revert",
		"--doc", "docs/guide.md", "--section", "overview", "--reason", "human rewrote the table",
		"--dir", tempDir)
	if err != nil {
		t.Fatalf("doc review record-revert failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "revert recorded") {
		t.Errorf("expected revert confirmation, got:\n%s", out)
	}
	// The revert is observed, never pending.
	out, err = runGmbCommand(t, "doc", "review", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc review failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no pending items") {
		t.Errorf("revert must not be pending, got:\n%s", out)
	}
	out, err = runGmbCommand(t, "doc", "review", "--stats", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc review --stats failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "1 observed") {
		t.Errorf("expected 1 observed, got:\n%s", out)
	}
}

func TestDocReviewTuning(t *testing.T) {
	tempDir := writeP3Fixture(t)
	out, err := runGmbCommand(t, "doc", "review", "--tuning", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc review --tuning failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no tuning suggestions") {
		t.Errorf("expected empty tuning message, got:\n%s", out)
	}
	// After a recorded revert, tuning must surface a suggestion.
	if _, err := runGmbCommand(t, "doc", "review", "record-revert",
		"--doc", "docs/guide.md", "--section", "overview", "--reason", "human rewrote the table",
		"--dir", tempDir); err != nil {
		t.Fatalf("record-revert failed: %v", err)
	}
	out, err = runGmbCommand(t, "doc", "review", "--tuning", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doc review --tuning failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "tuning suggestion") {
		t.Errorf("expected tuning suggestion after revert, got:\n%s", out)
	}
}
