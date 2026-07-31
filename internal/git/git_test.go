package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
