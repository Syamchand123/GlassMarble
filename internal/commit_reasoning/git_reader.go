package commit_reasoning

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type CommitMeta struct {
	Hash          string
	Author        string
	AuthorEmail   string
	Timestamp     time.Time
	Subject       string
	Body          string
	Files         []string
	Insertions    int
	Deletions     int
	Tags          []string
	RelatedPRs    []string
	RelatedIssues []string
}

var prRegex = regexp.MustCompile(`(?i)(?:pull request|pr) #?(\d+)`)
var issueRegex = regexp.MustCompile(`(?i)(?:fixes|closes|resolves|issue) #?(\d+)`)

func parseGitShow(out string) (*CommitMeta, error) {
	parts := strings.SplitN(strings.TrimSpace(out), "|", 6)
	if len(parts) < 5 {
		return nil, fmt.Errorf("invalid git output format")
	}

	meta := &CommitMeta{
		Hash:        parts[0],
		Author:      parts[1],
		AuthorEmail: parts[2],
		Subject:     parts[4],
	}

	if t, err := time.Parse(time.RFC3339, parts[3]); err == nil {
		meta.Timestamp = t
	}
	if len(parts) == 6 {
		meta.Body = strings.TrimSpace(parts[5])
	}

	fullMessage := meta.Subject + "\n" + meta.Body
	for _, m := range prRegex.FindAllStringSubmatch(fullMessage, -1) {
		if len(m) > 1 {
			meta.RelatedPRs = append(meta.RelatedPRs, m[1])
		}
	}
	for _, m := range issueRegex.FindAllStringSubmatch(fullMessage, -1) {
		if len(m) > 1 {
			meta.RelatedIssues = append(meta.RelatedIssues, m[1])
		}
	}

	return meta, nil
}

func ReadCommit(repoDir, hash string) (*CommitMeta, error) {
	cmd := exec.Command("git", "show", "-s", "--format=%H|%an|%ae|%aI|%s|%b", hash)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git show failed: %w", err)
	}

	meta, err := parseGitShow(string(out))
	if err != nil {
		return nil, err
	}

	// Use --numstat for precise insertions/deletions and file list
	cmdStats := exec.Command("git", "show", "--numstat", "--format=", hash)
	cmdStats.Dir = repoDir
	statsOut, err := cmdStats.Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(statsOut)), "\n")
		for _, line := range lines {
			parts := strings.Fields(line)
			if len(parts) == 3 {
				ins, _ := strconv.Atoi(parts[0])
				del, _ := strconv.Atoi(parts[1])
				meta.Insertions += ins
				meta.Deletions += del
				meta.Files = append(meta.Files, parts[2])
			}
		}
	}

	return meta, nil
}

func ReadCommitRange(repoDir, from, to string) ([]*CommitMeta, error) {
	cmd := exec.Command("git", "log", fmt.Sprintf("%s..%s", from, to), "--format=%H")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	var commits []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			commits = append(commits, line)
		}
	}

	var results []*CommitMeta
	for _, c := range commits {
		meta, err := ReadCommit(repoDir, c)
		if err != nil {
			return nil, err
		}
		results = append(results, meta)
	}

	return results, nil
}
