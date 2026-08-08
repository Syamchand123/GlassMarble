package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/git"
)

// Helper to set up a clean, initialized temporary Git repository for testing
func setupMockRepo(t *testing.T) string {
	tempDir, err := os.MkdirTemp("", "mock_git_repo_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Initialize git repo
	runCmd(t, tempDir, "git", "init")
	// Set test-local git user/email configuration
	runCmd(t, tempDir, "git", "config", "user.name", "Test User")
	runCmd(t, tempDir, "git", "config", "user.email", "test@glassmarble.org")
	runCmd(t, tempDir, "git", "config", "commit.gpgsign", "false")

	return tempDir
}

func runCmd(t *testing.T, dir string, name string, args ...string) string {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Command failed in %s: %s %v -> error: %v, output: %s", dir, name, args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func TestGitHelpers(t *testing.T) {
	repoDir := setupMockRepo(t)
	defer os.RemoveAll(repoDir)

	// 1. Verify GetHEADCommitHash fails on empty repo (no commits)
	_, err := git.GetHEADCommitHash(repoDir)
	if err == nil {
		t.Error("Expected error calling rev-parse on repo with no commits")
	}

	// 2. Commit first file
	file1 := filepath.Join(repoDir, "file1.txt")
	if err := os.WriteFile(file1, []byte("version 1"), 0644); err != nil {
		t.Fatalf("Failed to write file1: %v", err)
	}
	runCmd(t, repoDir, "git", "add", "file1.txt")
	runCmd(t, repoDir, "git", "commit", "-m", "first commit")

	commit1, err := git.GetHEADCommitHash(repoDir)
	if err != nil {
		t.Fatalf("Expected valid HEAD hash: %v", err)
	}
	if len(commit1) != 40 {
		t.Errorf("Expected 40-char hash, got %s", commit1)
	}

	// 3. Commit second file
	file2 := filepath.Join(repoDir, "file2.txt")
	if err := os.WriteFile(file2, []byte("version 2"), 0644); err != nil {
		t.Fatalf("Failed to write file2: %v", err)
	}
	runCmd(t, repoDir, "git", "add", "file2.txt")
	runCmd(t, repoDir, "git", "commit", "-m", "second commit")

	commit2, err := git.GetHEADCommitHash(repoDir)
	if err != nil {
		t.Fatalf("Expected valid HEAD hash: %v", err)
	}

	// 4. Verify GetChangedFiles between commit1 and commit2 contains file2.txt
	changed, err := git.GetChangedFiles(repoDir, commit1, commit2)
	if err != nil {
		t.Fatalf("GetChangedFiles failed: %v", err)
	}
	if len(changed) != 1 || changed[0] != "file2.txt" {
		t.Errorf("Expected only [file2.txt], got %v", changed)
	}

	// 5. Test EnsureGitIgnore
	gitignore := filepath.Join(repoDir, ".gitignore")
	if _, err := os.Stat(gitignore); !os.IsNotExist(err) {
		t.Fatal("Expected .gitignore to not exist initially")
	}

	if err := git.EnsureGitIgnore(repoDir); err != nil {
		t.Fatalf("EnsureGitIgnore failed: %v", err)
	}

	data, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}
	if !strings.Contains(string(data), ".glassmarble/") {
		t.Errorf("Expected .gitignore to contain '.glassmarble/', got:\n%s", string(data))
	}

	// Ensure calling it again doesn't duplicate the entry
	if err := git.EnsureGitIgnore(repoDir); err != nil {
		t.Fatalf("EnsureGitIgnore failed on second call: %v", err)
	}
	data2, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}
	count := strings.Count(string(data2), ".glassmarble/")
	if count != 1 {
		t.Errorf("Expected exactly one occurrence of '.glassmarble/', got %d", count)
	}
}

func TestGetCommitTimestamp(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available in PATH")
	}
	repoDir := setupMockRepo(t)
	defer os.RemoveAll(repoDir)

	// No commits yet → ref cannot be resolved.
	if _, err := git.GetCommitTimestamp(repoDir, "HEAD"); err == nil {
		t.Error("expected error resolving HEAD on a repo with no commits")
	}

	file := filepath.Join(repoDir, "f.txt")
	if err := os.WriteFile(file, []byte("v1"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runCmd(t, repoDir, "git", "add", "f.txt")
	runCmd(t, repoDir, "git", "commit", "-m", "first", "--date", "@1700000000")
	head, err := git.GetHEADCommitHash(repoDir)
	if err != nil {
		t.Fatalf("GetHEADCommitHash: %v", err)
	}

	// Full hash, short prefix, and the HEAD ref must all resolve, and to the
	// same instant (author timestamp, UTC).
	full, err := git.GetCommitTimestamp(repoDir, head)
	if err != nil {
		t.Fatalf("GetCommitTimestamp(full): %v", err)
	}
	short, err := git.GetCommitTimestamp(repoDir, head[:8])
	if err != nil {
		t.Fatalf("GetCommitTimestamp(prefix): %v", err)
	}
	byHead, err := git.GetCommitTimestamp(repoDir, "HEAD")
	if err != nil {
		t.Fatalf("GetCommitTimestamp(HEAD): %v", err)
	}
	if !full.Equal(short) || !full.Equal(byHead) {
		t.Errorf("timestamps disagree: full=%v prefix=%v HEAD=%v", full, short, byHead)
	}
	if !full.Equal(time.Unix(1700000000, 0).UTC()) {
		t.Errorf("expected the authored time 1700000000, got %v", full)
	}
	if full.Location() != time.UTC {
		t.Errorf("expected UTC, got %v", full.Location())
	}

	// A second commit authored later must resolve later.
	runCmd(t, repoDir, "git", "commit", "--allow-empty", "-m", "second", "--date", "@1700001000")
	second, err := git.GetCommitTimestamp(repoDir, "HEAD")
	if err != nil {
		t.Fatalf("GetCommitTimestamp(second): %v", err)
	}
	if !second.Equal(time.Unix(1700001000, 0).UTC()) {
		t.Errorf("second commit timestamp = %v, want 1700001000", second)
	}
	if !second.After(full) {
		t.Errorf("second commit timestamp %v should be after first %v", second, full)
	}

	// Bogus refs must error.
	if _, err := git.GetCommitTimestamp(repoDir, "no-such-ref-xyz"); err == nil {
		t.Error("expected error for an unresolvable ref")
	}
	// Empty inputs must error, not run git.
	if _, err := git.GetCommitTimestamp("", "HEAD"); err == nil {
		t.Error("expected error for an empty repo dir")
	}
	if _, err := git.GetCommitTimestamp(repoDir, ""); err == nil {
		t.Error("expected error for an empty ref")
	}
}
