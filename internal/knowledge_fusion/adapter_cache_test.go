package knowledge_fusion

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func fusionGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestLocalGitAdapterScansHistoryOnce pins the memoisation.
//
// FetchRelatedPRs and FetchRelatedIssues each called scanCommits, which runs
// `git log` and then forks one `git` process per commit. At the default
// MaxCommits of 500 that walked the same history twice — roughly a thousand
// subprocesses per fusion run — even though cmd/fusion.go's comment claimed a
// shared adapter already avoided the second walk.
func TestLocalGitAdapterScansHistoryOnce(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	fusionGit(t, dir, "init")
	fusionGit(t, dir, "config", "user.email", "t@example.com")
	fusionGit(t, dir, "config", "user.name", "test")
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte{byte('a' + i)}, 0o644); err != nil {
			t.Fatal(err)
		}
		fusionGit(t, dir, "add", ".")
		fusionGit(t, dir, "commit", "-m", "change (#12) fixes #34")
	}

	a := &LocalGitAdapter{RepoDir: dir, MaxCommits: 10}
	ctx := context.Background()

	first, err := a.scanCommits(ctx)
	if err != nil {
		t.Fatalf("scanCommits: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("expected commits to be scanned")
	}

	// A second call must reuse the first walk, not redo it.
	second, err := a.scanCommits(ctx)
	if err != nil {
		t.Fatalf("scanCommits again: %v", err)
	}
	if len(second) != len(first) {
		t.Fatalf("second scan returned %d commits, first returned %d", len(second), len(first))
	}
	if &second[0] != &first[0] {
		t.Error("second scan re-walked the history instead of reusing the cached result")
	}

	// And the two public entry points must share it.
	if _, err := a.FetchRelatedPRs(ctx, nil); err != nil {
		t.Fatalf("FetchRelatedPRs: %v", err)
	}
	if _, err := a.FetchRelatedIssues(ctx, nil); err != nil {
		t.Fatalf("FetchRelatedIssues: %v", err)
	}
	third, _ := a.scanCommits(ctx)
	if &third[0] != &first[0] {
		t.Error("FetchRelatedPRs/Issues did not share the cached walk")
	}
}
