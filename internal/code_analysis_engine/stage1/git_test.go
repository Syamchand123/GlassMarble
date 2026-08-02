package stage1

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitAvailable skips the whole test when the git binary is not present.
func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available in PATH")
	}
}

// runGit runs git in dir and fails the test on error, returning trimmed stdout.
func runGit(t *testing.T, dir string, args ...string) string {
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

// setupMockRepo inits a disposable git repo with a local identity so commits
// never touch the user's global config.
func setupMockRepo(t *testing.T) string {
	t.Helper()
	gitAvailable(t)
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "config", "commit.gpgsign", "false")
	return root
}

func TestCollectGitDiffModified(t *testing.T) {
	root := setupMockRepo(t)

	writeTestFile(t, filepath.Join(root, "main.go"), "package main\nfunc V1() {}\n")
	runGit(t, root, "add", "main.go")
	runGit(t, root, "commit", "-q", "-m", "v1")

	writeTestFile(t, filepath.Join(root, "main.go"), "package main\nfunc V2() {}\n")
	runGit(t, root, "add", "main.go")
	runGit(t, root, "commit", "-q", "-m", "v2")

	// Uncommitted worktree change vs HEAD exercises the
	// `git diff --name-status HEAD` path (commitHash == "").
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\nfunc V3() {}\n")

	tasks, err := CollectGitDiff(root, "")
	if err != nil {
		t.Fatalf("CollectGitDiff(root, ''): %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks len = %d, want 1", len(tasks))
	}
	if tasks[0].Change != ChangeModified {
		t.Errorf("Change = %s, want %s", tasks[0].Change, ChangeModified)
	}
	if got := filepath.Base(tasks[0].FilePath); got != "main.go" {
		t.Errorf("FilePath = %s, want main.go", got)
	}
}

func TestCollectGitDiffWithCommitHash(t *testing.T) {
	root := setupMockRepo(t)

	writeTestFile(t, filepath.Join(root, "f1.go"), "package main\nfunc F1() {}\n")
	runGit(t, root, "add", "f1.go")
	runGit(t, root, "commit", "-q", "-m", "commit A")

	writeTestFile(t, filepath.Join(root, "f2.go"), "package main\nfunc F2() {}\n")
	runGit(t, root, "add", "f2.go")
	runGit(t, root, "commit", "-q", "-m", "commit B")

	commitB := runGit(t, root, "rev-parse", "HEAD")

	tasks, err := CollectGitDiff(root, commitB)
	if err != nil {
		t.Fatalf("CollectGitDiff(root, %s): %v", commitB, err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks len = %d, want 1 (only f2.go added in commit B)", len(tasks))
	}
	if tasks[0].Change != ChangeAdded {
		t.Errorf("Change = %s, want %s", tasks[0].Change, ChangeAdded)
	}
	if got := filepath.Base(tasks[0].FilePath); got != "f2.go" {
		t.Errorf("FilePath = %s, want f2.go", got)
	}
}

func TestCollectGitDiffNonGitDir(t *testing.T) {
	gitAvailable(t)
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")

	// On a non-git dir `git diff` fails, CollectGitDiff falls back to
	// `git status --porcelain`, which also fails. It must return either an
	// error or an empty slice without panicking.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("CollectGitDiff panicked on non-git dir: %v", r)
			}
		}()
		tasks, err := CollectGitDiff(root, "")
		if err != nil && tasks != nil {
			t.Errorf("err=%v but tasks non-nil (%d)", err, len(tasks))
		}
		if err == nil && len(tasks) != 0 {
			t.Errorf("expected empty or error, got %d tasks", len(tasks))
		}
	}()
}

func TestGitCommandOutput(t *testing.T) {
	root := setupMockRepo(t)
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")
	runGit(t, root, "add", "main.go")
	runGit(t, root, "commit", "-q", "-m", "initial")

	out, err := GitCommandOutput(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("GitCommandOutput(rev-parse HEAD): %v", err)
	}
	if len(out) != 40 {
		t.Errorf("rev-parse HEAD = %q, want a 40-char hash (got %d chars)", out, len(out))
	}
}

func TestGitCommandOutputError(t *testing.T) {
	gitAvailable(t)
	root := t.TempDir()
	_, err := GitCommandOutput(root, "nonexistentcmd")
	if err == nil {
		t.Fatal("GitCommandOutput(nonexistentcmd) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("error = %v, want mention of failure", err)
	}
}

func TestCollectGitStatusFallback(t *testing.T) {
	root := setupMockRepo(t)
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")
	runGit(t, root, "add", "main.go")
	runGit(t, root, "commit", "-q", "-m", "initial")

	writeTestFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() { /* changed */ }\n")

	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := collectGitStatus(absRoot)
	if err != nil {
		t.Fatalf("collectGitStatus: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks len = %d, want 1", len(tasks))
	}
	if tasks[0].Change != ChangeModified {
		t.Errorf("Change = %s, want %s", tasks[0].Change, ChangeModified)
	}
}
