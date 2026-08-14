package ingest

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CollectGitDiff queries git in rootDir to produce a slice of FileTasks for changed files.
// If commitHash is provided, it diffs that commit against its parent.
// If commitHash is empty, it diffs working tree against HEAD.
func CollectGitDiff(rootDir string, commitHash string) ([]FileTask, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}

	var cmd *exec.Cmd
	if commitHash != "" {
		args := []string{"diff-tree", "-r", "--no-commit-id", "--name-status"}
		// A root commit has no parent, so diff-tree would report nothing;
		// --root makes it diff against the empty tree.
		if _, err := exec.Command("git", "rev-parse", commitHash+"^").Output(); err != nil {
			args = append(args, "--root")
		}
		args = append(args, commitHash)
		cmd = exec.Command("git", args...)
	} else {
		cmd = exec.Command("git", "diff", "--name-status", "HEAD")
	}
	cmd.Dir = absRoot

	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return collectGitStatus(absRoot)
	}

	reg := Registry()
	var tasks []FileTask
	lines := strings.Split(out.String(), "\n")
	now := time.Now()

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		status := parts[0]
		relPath := parts[len(parts)-1]
		fullPath := filepath.Join(absRoot, relPath)

		lang, _, ok := DetectLanguage(fullPath, reg)
		if !ok {
			continue
		}
		if deltaPathFiltered(relPath) {
			continue
		}

		var change ChangeKind
		switch {
		case strings.HasPrefix(status, "A"):
			change = ChangeAdded
		case strings.HasPrefix(status, "M"):
			change = ChangeModified
		case strings.HasPrefix(status, "D"):
			change = ChangeDeleted
		case strings.HasPrefix(status, "R"):
			change = ChangeRenamed
		default:
			change = ChangeModified
		}

		tasks = append(tasks, FileTask{
			FilePath: fullPath,
			RelPath:  relPath,
			Language: lang,
			Change:   change,
			Commit:   commitHash,
			Time:     now,
		})
	}

	return tasks, nil
}

// GitCommandOutput runs a git command in rootDir and returns its trimmed
// stdout. It is used by callers that need raw git output (e.g. the watch
// command's working-tree fingerprint) without parsing FileTasks.
func GitCommandOutput(rootDir string, args ...string) (string, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", err
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = absRoot
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ingest: git %s failed: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(out.String()), nil
}

// deltaPathFiltered reports whether a git-derived path must be excluded
// from delta ingestion: generated files, hidden paths, and skip-list
// directories (vendor, node_modules, ...). The full-scan walker applies the
// same filters, so delta and full ingestion must agree on what counts as
// analyzable source (a mismatch would re-add excluded files on re-analysis).
func deltaPathFiltered(relPath string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(relPath), "/") {
		if seg == "" {
			continue
		}
		if strings.HasPrefix(seg, ".") {
			return true
		}
		if _, skip := defaultSkipDirs[seg]; skip {
			return true
		}
	}
	return isGeneratedFile(filepath.Base(relPath))
}

func collectGitStatus(absRoot string) ([]FileTask, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = absRoot
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ingest: git status failed: %w", err)
	}

	reg := Registry()
	var tasks []FileTask
	now := time.Now()

	lines := strings.Split(out.String(), "\n")
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		status := line[:2]
		relPath := strings.TrimSpace(line[3:])
		fullPath := filepath.Join(absRoot, relPath)

		lang, _, ok := DetectLanguage(fullPath, reg)
		if !ok {
			continue
		}
		if deltaPathFiltered(relPath) {
			continue
		}

		var change ChangeKind
		switch {
		case strings.Contains(status, "A") || strings.Contains(status, "?"):
			change = ChangeAdded
		case strings.Contains(status, "D"):
			change = ChangeDeleted
		case strings.Contains(status, "R"):
			change = ChangeRenamed
		default:
			change = ChangeModified
		}

		tasks = append(tasks, FileTask{
			FilePath: fullPath,
			RelPath:  relPath,
			Language: lang,
			Change:   change,
			Time:     now,
		})
	}
	return tasks, nil
}
