package ingest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func untrackedGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// newRepo creates a git repo with one committed Go file.
func newUntrackedRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	untrackedGit(t, dir, "init")
	untrackedGit(t, dir, "config", "user.email", "test@example.com")
	untrackedGit(t, dir, "config", "user.name", "test")

	if err := os.WriteFile(filepath.Join(dir, "existing.go"), []byte("package main\n\nfunc Existing() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	untrackedGit(t, dir, "add", ".")
	untrackedGit(t, dir, "commit", "-m", "initial")
	return dir
}

func untrackedRelPaths(tasks []FileTask) map[string]ChangeKind {
	out := make(map[string]ChangeKind, len(tasks))
	for _, t := range tasks {
		out[filepath.ToSlash(t.RelPath)] = t.Change
	}
	return out
}

// TestCollectGitDiff_IncludesUntrackedFiles pins the delta blind spot:
// `git diff --name-status HEAD` never lists untracked files, so a newly
// created source file was silently skipped by incremental analysis and never
// entered the graph until a full rescan happened to run.
func TestCollectGitDiff_IncludesUntrackedFiles(t *testing.T) {
	dir := newUntrackedRepo(t)

	// brand new, never staged
	if err := os.WriteFile(filepath.Join(dir, "brand_new.go"), []byte("package main\n\nfunc BrandNew() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// and a modification to a tracked file, which must still be reported
	if err := os.WriteFile(filepath.Join(dir, "existing.go"), []byte("package main\n\nfunc Existing() { _ = 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tasks, err := CollectGitDiff(dir, "")
	if err != nil {
		t.Fatalf("CollectGitDiff: %v", err)
	}
	got := untrackedRelPaths(tasks)

	if _, ok := got["brand_new.go"]; !ok {
		t.Errorf("untracked new file was not reported; got %v", got)
	} else if got["brand_new.go"] != ChangeAdded {
		t.Errorf("untracked new file should be ChangeAdded, got %v", got["brand_new.go"])
	}
	if _, ok := got["existing.go"]; !ok {
		t.Errorf("tracked modification missing from delta; got %v", got)
	}
}

// TestCollectGitDiff_RespectsGitignore ensures the untracked sweep does not
// drag in ignored build output.
func TestCollectGitDiff_RespectsGitignore(t *testing.T) {
	dir := newUntrackedRepo(t)

	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked_new.go"), []byte("package main\n\nfunc New() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tasks, err := CollectGitDiff(dir, "")
	if err != nil {
		t.Fatalf("CollectGitDiff: %v", err)
	}
	got := untrackedRelPaths(tasks)

	if _, ok := got["ignored.go"]; ok {
		t.Errorf("gitignored file must not be analyzed; got %v", got)
	}
	if _, ok := got["tracked_new.go"]; !ok {
		t.Errorf("untracked, non-ignored file should be analyzed; got %v", got)
	}
}

// TestCollectGitDiff_NoDuplicateTasks guards the union of the tracked diff and
// the untracked sweep.
func TestCollectGitDiff_NoDuplicateTasks(t *testing.T) {
	dir := newUntrackedRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "staged_new.go"), []byte("package main\n\nfunc S() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// staged but not committed: appears in `git diff HEAD` already
	untrackedGit(t, dir, "add", "staged_new.go")

	tasks, err := CollectGitDiff(dir, "")
	if err != nil {
		t.Fatalf("CollectGitDiff: %v", err)
	}
	counts := map[string]int{}
	for _, tk := range tasks {
		counts[filepath.ToSlash(tk.RelPath)]++
	}
	for path, n := range counts {
		if n > 1 {
			t.Errorf("file %q reported %d times, expected once", path, n)
		}
	}
	if counts["staged_new.go"] == 0 {
		t.Errorf("staged new file missing from delta; got %v", counts)
	}
}
