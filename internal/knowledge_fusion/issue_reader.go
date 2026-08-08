package knowledge_fusion

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/git"
)

type Issue struct {
	ID           string
	Title        string
	Description  string
	FilesChanged []string
}

type IssueAdapter interface {
	Name() string
	FetchRelatedIssues(ctx context.Context, refs []string) ([]Issue, error)
}

// LocalGitAdapter implements both PRAdapter and IssueAdapter without API calls.
// It searches the git commit history for messages related to the issue/PR.
type LocalGitAdapter struct {
	RepoDir string
}

func (a *LocalGitAdapter) Name() string {
	return "LocalGitAdapter"
}

func (a *LocalGitAdapter) fetchFromGit(ctx context.Context, pattern string, refs []string) (map[string]*git.CommitMeta, error) {
	// If refs provided, build an OR regex for them, else just general pattern
	grepPattern := pattern
	if len(refs) > 0 {
		grepPattern = fmt.Sprintf("(?i)(%s) #(%s)", strings.ReplaceAll(pattern, "(?i)", ""), strings.Join(refs, "|"))
	}

	cmd := exec.CommandContext(ctx, "git", "log", "-E", "--grep="+grepPattern, "--format=%H")
	cmd.Dir = a.RepoDir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	commits := make(map[string]*git.CommitMeta)
	hashes := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, h := range hashes {
		if h == "" {
			continue
		}
		meta, err := git.ReadCommit(a.RepoDir, h)
		if err == nil {
			commits[h] = meta
		}
	}
	return commits, nil
}

func (a *LocalGitAdapter) FetchRelatedPRs(ctx context.Context, refs []string) ([]PullRequest, error) {
	commits, err := a.fetchFromGit(ctx, "(?i)(pull request|pr)", refs)
	if err != nil {
		return nil, err
	}

	prMap := make(map[string]*PullRequest)
	re := regexp.MustCompile(`(?i)(?:pull request|pr) #?(\d+)`)

	for _, meta := range commits {
		msg := meta.Subject + "\n" + meta.Body
		matches := re.FindAllStringSubmatch(msg, -1)
		for _, m := range matches {
			if len(m) > 1 {
				id := m[1]
				if _, exists := prMap[id]; !exists {
					prMap[id] = &PullRequest{
						ID:          id,
						Title:       meta.Subject,
						Description: meta.Body,
						Author:      meta.Author,
					}
				}
				// Append files changed to this PR
				prMap[id].FilesChanged = append(prMap[id].FilesChanged, meta.Files...)
			}
		}
	}

	var prs []PullRequest
	for _, pr := range prMap {
		prs = append(prs, *pr)
	}
	return prs, nil
}

func (a *LocalGitAdapter) FetchRelatedIssues(ctx context.Context, refs []string) ([]Issue, error) {
	commits, err := a.fetchFromGit(ctx, "(?i)(fixes|closes|resolves|issue)", refs)
	if err != nil {
		return nil, err
	}

	issueMap := make(map[string]*Issue)
	re := regexp.MustCompile(`(?i)(?:fixes|closes|resolves|issue) #?(\d+)`)

	for _, meta := range commits {
		msg := meta.Subject + "\n" + meta.Body
		matches := re.FindAllStringSubmatch(msg, -1)
		for _, m := range matches {
			if len(m) > 1 {
				id := m[1]
				if _, exists := issueMap[id]; !exists {
					issueMap[id] = &Issue{
						ID:          id,
						Title:       fmt.Sprintf("Issue %s", id),
						Description: meta.Subject + "\n" + meta.Body,
					}
				}
				// Append files changed to this Issue
				issueMap[id].FilesChanged = append(issueMap[id].FilesChanged, meta.Files...)
			}
		}
	}

	var issues []Issue
	for _, issue := range issueMap {
		issues = append(issues, *issue)
	}
	return issues, nil
}
