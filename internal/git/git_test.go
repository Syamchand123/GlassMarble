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

// commitRepo builds a small git repository with deterministic content:
//   - root commit "initial" adding a.txt
//   - tagged commit "add payment service | with pipe" adding b.txt (subject
//     contains a pipe to prove NUL parsing survives it)
//   - final commit with a multi-line body mentioning "Fixes #42" and
//     "PR #12" and a rename of b.txt → c.txt plus a binary file
func commitRepo(t *testing.T) string {
	t.Helper()
	repoDir := setupMockRepo(t)
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	write("a.txt", "a1\n")
	runCmd(t, repoDir, "git", "add", "a.txt")
	runCmd(t, repoDir, "git", "commit", "-m", "initial", "--date", "@1700000000")

	write("b.txt", "b1\nb2\nb3\n")
	runCmd(t, repoDir, "git", "add", "b.txt")
	runCmd(t, repoDir, "git", "commit", "-m", "add payment service | with pipe", "--date", "@1700000100")
	runCmd(t, repoDir, "git", "tag", "v1.0.0")

	write("c.txt", "c1\n")
	runCmd(t, repoDir, "git", "add", "c.txt")
	runCmd(t, repoDir, "git", "rm", "-q", "b.txt")
	binary := make([]byte, 64)
	for i := range binary {
		binary[i] = byte(i)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "blob.bin"), binary, 0644); err != nil {
		t.Fatalf("write blob.bin: %v", err)
	}
	runCmd(t, repoDir, "git", "add", "blob.bin")
	runCmd(t, repoDir, "git", "commit", "-m", "wire up payment\r\n\r\nfixes #42 and references PR #12 because the DB was slow", "--date", "@1700000200")
	return repoDir
}

func TestReadCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available in PATH")
	}
	repoDir := commitRepo(t)
	defer os.RemoveAll(repoDir)

	head, err := git.GetHEADCommitHash(repoDir)
	if err != nil {
		t.Fatalf("GetHEADCommitHash: %v", err)
	}

	// Short prefix must resolve to the same commit as the full hash.
	meta, err := git.ReadCommit(repoDir, head[:8])
	if err != nil {
		t.Fatalf("ReadCommit(prefix): %v", err)
	}
	if meta.Hash != head {
		t.Errorf("Hash = %q, want %q", meta.Hash, head)
	}
	if meta.Subject != "wire up payment" {
		t.Errorf("Subject = %q, want %q", meta.Subject, "wire up payment")
	}
	if !strings.Contains(meta.Body, "fixes #42") {
		t.Errorf("Body should contain the full multi-line message, got %q", meta.Body)
	}
	if !meta.Timestamp.Equal(time.Unix(1700000200, 0).UTC()) {
		t.Errorf("Timestamp = %v, want 1700000200", meta.Timestamp)
	}
	if len(meta.Parents) != 1 {
		t.Errorf("Parents = %v, want exactly 1 parent", meta.Parents)
	}
	// The commit renames b.txt→c.txt, adds c.txt/blob.bin, deletes b.txt.
	found := map[string]bool{"c.txt": false, "blob.bin": false}
	for _, f := range meta.Files {
		if _, ok := found[f]; ok {
			found[f] = true
		}
	}
	if !found["c.txt"] {
		t.Errorf("Files should contain c.txt, got %v", meta.Files)
	}
	if !found["blob.bin"] {
		t.Errorf("Files should contain blob.bin (binary), got %v", meta.Files)
	}
	if meta.Insertions <= 0 {
		t.Errorf("Insertions = %d, want > 0", meta.Insertions)
	}
	if meta.Deletions <= 0 {
		t.Errorf("Deletions = %d, want > 0 (b.txt removed)", meta.Deletions)
	}
}

func TestReadCommit_TagsAndBodyPipes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available in PATH")
	}
	repoDir := commitRepo(t)
	defer os.RemoveAll(repoDir)

	// The middle commit's subject contains a pipe — a "|"-split parse would
	// truncate it. NUL-separated parsing must survive.
	byTag, err := git.ResolveRef(repoDir, "v1.0.0")
	if err != nil {
		t.Fatalf("ResolveRef(v1.0.0): %v", err)
	}
	meta, err := git.ReadCommit(repoDir, byTag)
	if err != nil {
		t.Fatalf("ReadCommit(tagged): %v", err)
	}
	if meta.Subject != "add payment service | with pipe" {
		t.Errorf("Subject with pipe = %q", meta.Subject)
	}
	if len(meta.Tags) != 1 || meta.Tags[0] != "v1.0.0" {
		t.Errorf("Tags = %v, want [v1.0.0]", meta.Tags)
	}
	if !meta.Timestamp.Equal(time.Unix(1700000100, 0).UTC()) {
		t.Errorf("Timestamp = %v, want 1700000100", meta.Timestamp)
	}
	// Root commit: no parents, no tags.
	rootMeta, err := git.ReadCommit(repoDir, "HEAD~2")
	if err != nil {
		t.Fatalf("ReadCommit(root): %v", err)
	}
	if len(rootMeta.Parents) != 0 {
		t.Errorf("Root commit Parents = %v, want none", rootMeta.Parents)
	}
	if len(rootMeta.Tags) != 0 {
		t.Errorf("Root commit Tags = %v, want none", rootMeta.Tags)
	}
}

func TestReadCommit_Errors(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available in PATH")
	}
	repoDir := setupMockRepo(t)
	defer os.RemoveAll(repoDir)

	if _, err := git.ReadCommit(repoDir, "no-such-commit"); err == nil {
		t.Error("expected error for an unresolvable commit")
	}
	if _, err := git.ReadCommit("", "HEAD"); err == nil {
		t.Error("expected error for an empty repo dir")
	}
	if _, err := git.ReadCommit(repoDir, ""); err == nil {
		t.Error("expected error for an empty ref")
	}
}

func TestReadCommitRange(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available in PATH")
	}
	repoDir := commitRepo(t)
	defer os.RemoveAll(repoDir)

	head, err := git.GetHEADCommitHash(repoDir)
	if err != nil {
		t.Fatalf("GetHEADCommitHash: %v", err)
	}
	root, err := git.ResolveRef(repoDir, "HEAD~2")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}

	metas, err := git.ReadCommitRange(repoDir, root, head)
	if err != nil {
		t.Fatalf("ReadCommitRange: %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("got %d commits in range, want 2", len(metas))
	}
	// Oldest first.
	if metas[0].Subject != "add payment service | with pipe" {
		t.Errorf("metas[0].Subject = %q, want the middle commit first", metas[0].Subject)
	}
	if metas[1].Subject != "wire up payment" {
		t.Errorf("metas[1].Subject = %q, want the head commit last", metas[1].Subject)
	}
	if !metas[0].Timestamp.Before(metas[1].Timestamp) {
		t.Error("range commits must be ordered oldest first")
	}
}
