package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Helper executing a git command within a specific working directory.
func runGitCommand(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git command failed: %w (stderr: %s)", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// GetHEADCommitHash retrieves the current HEAD commit hash of the git repository.
func GetHEADCommitHash(repoDir string) (string, error) {
	return runGitCommand(repoDir, "rev-parse", "HEAD")
}

// GetChangedFiles lists the files that have changed between two commit hashes.
func GetChangedFiles(repoDir string, oldCommit, newCommit string) ([]string, error) {
	if oldCommit == "" || newCommit == "" {
		return nil, nil // Triggers full scan
	}

	// Resolve tags/branches if any are passed
	oldRes, err := runGitCommand(repoDir, "rev-parse", oldCommit)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve old commit hash: %w", err)
	}

	newRes, err := runGitCommand(repoDir, "rev-parse", newCommit)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve new commit hash: %w", err)
	}

	if oldRes == newRes {
		return []string{}, nil // No changes
	}

	output, err := runGitCommand(repoDir, "diff", "--name-only", oldRes, newRes)
	if err != nil {
		return nil, err
	}

	if output == "" {
		return []string{}, nil
	}

	lines := strings.Split(output, "\n")
	var files []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			files = append(files, trimmed)
		}
	}

	return files, nil
}

// EnsureGitIgnore guarantees that the .glassmarble/ directory is added to .gitignore.
func EnsureGitIgnore(repoDir string) error {
	ignorePath := filepath.Join(repoDir, ".gitignore")
	targetEntry := ".glassmarble/"

	// Read existing content
	data, err := os.ReadFile(ignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Write fresh gitignore
			content := fmt.Sprintf("# GlassMarble Local Cache\n%s\n", targetEntry)
			return os.WriteFile(ignorePath, []byte(content), 0644)
		}
		return err
	}

	contentStr := string(data)
	lines := strings.Split(contentStr, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == targetEntry {
			return nil // Entry already exists
		}
	}

	// Append entry safely
	var newContent string
	if len(contentStr) > 0 && !strings.HasSuffix(contentStr, "\n") {
		newContent = contentStr + "\n" + targetEntry + "\n"
	} else {
		newContent = contentStr + targetEntry + "\n"
	}

	return os.WriteFile(ignorePath, []byte(newContent), 0644)
}
