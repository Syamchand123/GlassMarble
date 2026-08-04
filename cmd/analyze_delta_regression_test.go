package cmd_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGitCmd executes git in dir and fatals on error, returning stdout.
func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v failed in %s: %v\n%s", args, dir, err, buf.String())
	}
	return strings.TrimSpace(buf.String())
}

// setupAnalyzeGitRepo builds a small repo where the latest commit touches only
// one of three Go files. This reproduces the exact scenario that used to make
// a bare `gmb analyze` after `gmb init` ingest only the last commit's files:
// the --commit flag defaulted to "HEAD" and CollectGitDiff runs
// `git diff-tree HEAD` (files changed IN the last commit) whenever a
// non-empty commit hash is supplied.
func setupAnalyzeGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available in PATH")
	}
	root := t.TempDir()
	runGitCmd(t, root, "init", "-q")
	runGitCmd(t, root, "config", "user.email", "test@example.com")
	runGitCmd(t, root, "config", "user.name", "Test User")
	runGitCmd(t, root, "config", "commit.gpgsign", "false")

	files := map[string]string{
		"a.go": "package main\n\nfunc A() {}\n",
		"b.go": "package main\n\nfunc B() {}\n",
		"c.go": "package main\n\nfunc C() {}\n",
	}
	for f, content := range files {
		if err := os.WriteFile(filepath.Join(root, f), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGitCmd(t, root, "add", ".")
	runGitCmd(t, root, "commit", "-q", "-m", "all files")

	// Latest commit touches only b.go -> `git diff-tree HEAD` reports 1 file.
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package main\n\nfunc Bee() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, root, "add", "b.go")
	runGitCmd(t, root, "commit", "-q", "-m", "touch b only")

	return root
}

// TestAnalyzeAfterInitFullScan reproduces the reported bug end-to-end: `gmb
// init` writes an EMPTY akg_state.ttl, and a bare `gmb analyze` must still
// ingest the whole repository (3 files), not just the files from the latest
// commit (1 file). Regression for the --commit "HEAD" default + empty-base
// delta path.
func TestAnalyzeAfterInitFullScan(t *testing.T) {
	root := setupAnalyzeGitRepo(t)

	if _, err := runGmbCommand(t, "init", "--dir", root); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	output, err := runGmbCommand(t, "analyze", "--dir", root, "--verbose")
	if err != nil {
		t.Fatalf("analyze failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Analyzed 3 files") {
		t.Fatalf("expected full scan of 3 files, got:\n%s", output)
	}
	if strings.Contains(output, "Analyzed 1 files") {
		t.Fatalf("analyze ingested only the latest commit's file:\n%s", output)
	}
}

// TestAnalyzeAfterInitDirtyTreeFullScan verifies the empty-base guard: with an
// empty akg_state.ttl (from init) even a dirty working tree must trigger a
// full scan, because a delta against an empty base would produce a partial
// graph with only the changed files.
func TestAnalyzeAfterInitDirtyTreeFullScan(t *testing.T) {
	root := setupAnalyzeGitRepo(t)

	if _, err := runGmbCommand(t, "init", "--dir", root); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Dirty the working tree (uncommitted change) AFTER init.
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package main\n\nfunc Alpha() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := runGmbCommand(t, "analyze", "--dir", root, "--verbose")
	if err != nil {
		t.Fatalf("analyze failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Analyzed 3 files") {
		t.Fatalf("expected full scan of 3 files after empty-base init, got:\n%s", output)
	}
}

// TestAnalyzeDeltaAfterRealBase verifies the incremental path still works when
// a real, non-empty base graph exists: after a full first run, modifying one
// file re-ingests only that file (delta mode preserved).
func TestAnalyzeDeltaAfterRealBase(t *testing.T) {
	root := setupAnalyzeGitRepo(t)

	// First run: no base -> full scan of all 3 files.
	output, err := runGmbCommand(t, "analyze", "--dir", root, "--verbose")
	if err != nil {
		t.Fatalf("analyze #1 failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Analyzed 3 files") {
		t.Fatalf("first analyze, want full scan of 3 files:\n%s", output)
	}

	// Modify a.go (uncommitted) and analyze: with a non-empty base graph the
	// delta path re-ingests only the 1 changed file.
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package main\n\nfunc Alpha() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output2, err := runGmbCommand(t, "analyze", "--dir", root, "--verbose")
	if err != nil {
		t.Fatalf("analyze #2 failed: %v\n%s", err, output2)
	}
	if !strings.Contains(output2, "Analyzed 1 files") {
		t.Fatalf("expected delta of 1 changed file, got:\n%s", output2)
	}
}
