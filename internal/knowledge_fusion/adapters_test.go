package knowledge_fusion

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupGitRepo creates an initialized temp git repository with a known
// commit history exercising PR and issue references.
//
//	commit 1 (oldest): feat: add parser (Fixes #12, PR #100)
//	commit 2:          refactor: split modules (#100)
//	commit 3 (newest): fix: handle edge case (PR #200)
func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "user.email", "test@glassmarble.org")
	runGit(t, dir, "config", "commit.gpgsign", "false")

	commit := func(file, msg string, authorTime string) {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", file)
		// Pin author dates so timestamps are deterministic.
		env := []string{"GIT_AUTHOR_DATE=" + authorTime, "GIT_COMMITTER_DATE=" + authorTime}
		cmd := exec.Command("git", "commit", "-q", "-m", msg)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit %q: %v\n%s", msg, err, out)
		}
	}

	commit("a.go", "feat: add parser (Fixes #12)", "2024-05-01T09:00:00Z")
	commit("b.go", "refactor: split modules (#100)", "2024-05-02T09:00:00Z")
	commit("c.go", "fix: handle edge case (#100)", "2024-05-03T09:00:00Z")
	commit("d.go", "docs: PR #200 updates", "2024-05-04T09:00:00Z")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestLocalGitAdapter_FetchRelatedPRs(t *testing.T) {
	dir := setupGitRepo(t)
	adapter := &LocalGitAdapter{RepoDir: dir, MaxCommits: 50}

	prs, err := adapter.FetchRelatedPRs(context.Background(), nil)
	if err != nil {
		t.Fatalf("FetchRelatedPRs: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("got %d PRs, want 2 (100, 200)", len(prs))
	}

	// Sorted by ID.
	if prs[0].ID != "100" || prs[1].ID != "200" {
		t.Errorf("PR IDs = %q, %q; want 100, 200 (sorted)", prs[0].ID, prs[1].ID)
	}

	// PR 100 aggregates two commits: earliest author time, subject of the
	// newest commit (history is walked newest-first) as title, union of
	// changed files (sorted, deduplicated).
	pr := prs[0]
	if pr.Title != "fix: handle edge case (#100)" {
		t.Errorf("PR 100 title = %q, want newest commit subject", pr.Title)
	}
	wantTS := time.Date(2024, 5, 2, 9, 0, 0, 0, time.UTC)
	if !pr.Timestamp.Equal(wantTS) {
		t.Errorf("PR 100 timestamp = %v, want %v (earliest author time)", pr.Timestamp, wantTS)
	}
	if strings.Join(pr.FilesChanged, ",") != "b.go,c.go" {
		t.Errorf("PR 100 files = %v, want [b.go c.go]", pr.FilesChanged)
	}
	if len(pr.Commits) != 2 {
		t.Errorf("PR 100 commits = %d, want 2", len(pr.Commits))
	}
}

func TestLocalGitAdapter_FetchRelatedIssues(t *testing.T) {
	dir := setupGitRepo(t)
	adapter := &LocalGitAdapter{RepoDir: dir, MaxCommits: 50}

	issues, err := adapter.FetchRelatedIssues(context.Background(), nil)
	if err != nil {
		t.Fatalf("FetchRelatedIssues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1 (12)", len(issues))
	}
	if issues[0].ID != "12" {
		t.Errorf("issue ID = %q, want 12", issues[0].ID)
	}
	if issues[0].FilesChanged[0] != "a.go" {
		t.Errorf("issue files = %v, want [a.go]", issues[0].FilesChanged)
	}
	// Description carries the commit subject/body so the claim excerpt is
	// human-readable.
	if !strings.Contains(issues[0].Description, "add parser") {
		t.Errorf("issue description = %q, want commit subject", issues[0].Description)
	}
}

func TestLocalGitAdapter_RefFilter(t *testing.T) {
	dir := setupGitRepo(t)
	adapter := &LocalGitAdapter{RepoDir: dir, MaxCommits: 50}

	prs, err := adapter.FetchRelatedPRs(context.Background(), []string{"100"})
	if err != nil {
		t.Fatalf("FetchRelatedPRs: %v", err)
	}
	if len(prs) != 1 || prs[0].ID != "100" {
		t.Errorf("filtered PRs = %+v, want only 100", prs)
	}

	issues, err := adapter.FetchRelatedIssues(context.Background(), []string{"999"})
	if err != nil {
		t.Fatalf("FetchRelatedIssues: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("filtered issues = %+v, want none", issues)
	}
}

func TestLocalGitAdapter_NotARepo(t *testing.T) {
	adapter := &LocalGitAdapter{RepoDir: t.TempDir(), MaxCommits: 10}
	if _, err := adapter.FetchRelatedPRs(context.Background(), nil); err == nil {
		t.Fatal("expected error for a directory without git history")
	}
}

func TestLocalGitAdapter_DeterministicAcrossRuns(t *testing.T) {
	dir := setupGitRepo(t)
	adapter := &LocalGitAdapter{RepoDir: dir, MaxCommits: 50}

	first, err := adapter.FetchRelatedPRs(context.Background(), nil)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	second, err := adapter.FetchRelatedPRs(context.Background(), nil)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("run sizes differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		a, b := first[i], second[i]
		if a.ID != b.ID || a.Title != b.Title || !a.Timestamp.Equal(b.Timestamp) {
			t.Errorf("run %d differs: %+v vs %+v", i, a, b)
		}
	}
}
