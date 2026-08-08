package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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

// GetCommitTimestamp resolves ref (a commit hash, prefix, tag, branch or HEAD)
// to its author timestamp in UTC. Used by the snapshot engine so that
// snapshots and timeline entries are ordered by when commits were actually
// authored, not when analysis happened to run. Author time is used because it
// survives rebases and cherry-picks (committer time changes on every rewrite).
func GetCommitTimestamp(repoDir, ref string) (time.Time, error) {
	if repoDir == "" || ref == "" {
		return time.Time{}, fmt.Errorf("repo dir and ref are required")
	}
	out, err := runGitCommand(repoDir, "log", "-1", "--format=%at", ref)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot resolve ref %q: %w", ref, err)
	}
	secs, err := strconv.ParseInt(out, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("ref %q has unparsable author timestamp %q: %w", ref, out, err)
	}
	return time.Unix(secs, 0).UTC(), nil
}

// ResolveRef resolves any ref (full hash, prefix, tag, branch, HEAD, HEAD~n)
// to its full commit hash.
func ResolveRef(repoDir, ref string) (string, error) {
	if repoDir == "" || ref == "" {
		return "", fmt.Errorf("repo dir and ref are required")
	}
	return runGitCommand(repoDir, "rev-parse", "--verify", ref+"^{commit}")
}

// GetCommitOrder returns the number of commits reachable from ref
// (git rev-list --count). On a linear history this is the commit's position
// from the root, which strictly orders commits even when several were
// authored within the same second — the tie-breaker the snapshot index needs
// to tell which snapshot is the newest.
func GetCommitOrder(repoDir, ref string) (int64, error) {
	if repoDir == "" || ref == "" {
		return 0, fmt.Errorf("repo dir and ref are required")
	}
	out, err := runGitCommand(repoDir, "rev-list", "--count", ref)
	if err != nil {
		return 0, fmt.Errorf("cannot count commits for ref %q: %w", ref, err)
	}
	n, err := strconv.ParseInt(out, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("ref %q has unparsable commit count %q: %w", ref, out, err)
	}
	return n, nil
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
