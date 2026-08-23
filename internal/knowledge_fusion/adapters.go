package knowledge_fusion

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/commit_reasoning"
	"github.com/Syamchand123/GlassMarble/internal/git"
)

// PullRequest is the adapter-neutral representation of one pull request.
// Everything is a directly observable source-control fact — no
// interpretation. Timestamp is the earliest author time among the commits
// that mention the PR, the closest local-git proxy for when the PR's work
// was authored.
type PullRequest struct {
	ID           string
	Title        string
	Description  string
	Author       string
	Timestamp    time.Time
	Commits      []string
	FilesChanged []string
}

// Issue is the adapter-neutral representation of one issue. Timestamp is
// the earliest author time among the commits that reference the issue.
type Issue struct {
	ID           string
	Title        string
	Description  string
	Timestamp    time.Time
	Commits      []string
	FilesChanged []string
}

// PRAdapter is the interface for pull-request source integration. The
// built-in LocalGitAdapter needs no external API; future GitHub/GitLab
// adapters implement the same interface (master plan §7.4).
type PRAdapter interface {
	Name() string
	FetchRelatedPRs(ctx context.Context, refs []string) ([]PullRequest, error)
}

// IssueAdapter is the interface for issue-tracker source integration.
type IssueAdapter interface {
	Name() string
	FetchRelatedIssues(ctx context.Context, refs []string) ([]Issue, error)
}

// LocalGitAdapter implements both PRAdapter and IssueAdapter with zero API
// calls: it walks recent git history and classifies every commit's PR/issue
// references using commit_reasoning.ExtractRelatedRefs — the same
// extraction the commit reasoning reasoning engine uses, so the two phases can never
// disagree about what "PR #42" or "Fixes #42" means.
//
// Deterministic by construction: commits are walked newest-first (git
// order), grouped by reference ID, and results are sorted by ID.
type LocalGitAdapter struct {
	// RepoDir is the git repository root.
	RepoDir string
	// MaxCommits caps how many most-recent commits are scanned (0 = the
	// config default, applied by the engine before construction).
	MaxCommits int
	// Warnf receives non-fatal per-commit read failures (nil disables).
	Warnf func(format string, args ...any)
}

func (a *LocalGitAdapter) Name() string { return "LocalGitAdapter" }

// FetchRelatedPRs returns the PRs referenced by the scanned commit history.
// When refs is non-empty it acts as a filter: only PRs whose ID is listed
// are returned. Results are sorted by ID (string order).
func (a *LocalGitAdapter) FetchRelatedPRs(ctx context.Context, refs []string) ([]PullRequest, error) {
	commits, err := a.scanCommits(ctx)
	if err != nil {
		return nil, err
	}
	want := newRefFilter(refs)
	byID := make(map[string]*pullGroup)
	var order []string
	for _, meta := range commits {
		commit_reasoning.ExtractRelatedRefs(meta)
		for _, pr := range meta.RelatedPRs {
			if !want(pr) {
				continue
			}
			g, ok := byID[pr]
			if !ok {
				g = &pullGroup{id: pr}
				byID[pr] = g
				order = append(order, pr)
			}
			g.add(meta)
		}
	}

	sort.Strings(order)
	prs := make([]PullRequest, 0, len(order))
	for _, id := range order {
		g := byID[id]
		sort.Strings(g.files)
		prs = append(prs, PullRequest{
			ID:           g.id,
			Title:        g.title,
			Description:  g.description,
			Author:       g.author,
			Timestamp:    g.timestamp,
			Commits:      g.commits,
			FilesChanged: g.files,
		})
	}
	return prs, nil
}

// FetchRelatedIssues returns the issues referenced by the scanned commit
// history, filtered by refs when non-empty. Results are sorted by ID.
func (a *LocalGitAdapter) FetchRelatedIssues(ctx context.Context, refs []string) ([]Issue, error) {
	commits, err := a.scanCommits(ctx)
	if err != nil {
		return nil, err
	}
	want := newRefFilter(refs)
	byID := make(map[string]*pullGroup)
	var order []string
	for _, meta := range commits {
		commit_reasoning.ExtractRelatedRefs(meta)
		for _, issue := range meta.RelatedIssues {
			if !want(issue) {
				continue
			}
			g, ok := byID[issue]
			if !ok {
				g = &pullGroup{id: issue}
				byID[issue] = g
				order = append(order, issue)
			}
			g.add(meta)
		}
	}

	sort.Strings(order)
	issues := make([]Issue, 0, len(order))
	for _, id := range order {
		g := byID[id]
		sort.Strings(g.files)
		issues = append(issues, Issue{
			ID:           g.id,
			Title:        "Issue " + g.id,
			Description:  g.title + "\n" + g.description,
			Timestamp:    g.timestamp,
			Commits:      g.commits,
			FilesChanged: g.files,
		})
	}
	return issues, nil
}

// scanCommits reads the metadata of the MaxCommits most-recent commits,
// newest first (git log order). Per-commit read failures are non-fatal:
// the scan continues and the failure is reported through Warnf, because a
// single unreadable commit must not silently discard the rest of history.
func (a *LocalGitAdapter) scanCommits(ctx context.Context) ([]*git.CommitMeta, error) {
	max := a.MaxCommits
	if max <= 0 {
		max = 500
	}
	args := []string{"log", "--format=%H", "-n", fmt.Sprintf("%d", max)}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = a.RepoDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("knowledge_fusion: git log failed: %w", err)
	}

	hashes := splitLines(string(out))
	metas := make([]*git.CommitMeta, 0, len(hashes))
	for _, h := range hashes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if h == "" {
			continue
		}
		meta, err := git.ReadCommit(a.RepoDir, h)
		if err != nil {
			if a.Warnf != nil {
				a.Warnf("knowledge_fusion: skipping unreadable commit %s: %v", h, err)
			}
			continue
		}
		metas = append(metas, meta)
	}
	return metas, nil
}

// pullGroup accumulates the commits that reference one PR/issue ID into a
// deterministic aggregate: earliest author time, newest commit's subject as
// title (walked newest-first, first-seen wins), joined bodies as
// description, and the sorted, deduplicated union of changed files.
type pullGroup struct {
	id          string
	title       string
	description string
	author      string
	timestamp   time.Time
	commits     []string
	files       []string
	seenFiles   map[string]bool
}

// add folds one commit into the group.
func (g *pullGroup) add(meta *git.CommitMeta) {
	if g.seenFiles == nil {
		g.seenFiles = make(map[string]bool)
	}
	if g.timestamp.IsZero() || meta.Timestamp.Before(g.timestamp) {
		g.timestamp = meta.Timestamp
	}
	if g.title == "" {
		g.title = meta.Subject
		g.author = meta.Author
	}
	if g.description == "" {
		g.description = meta.Body
	} else if meta.Body != "" {
		g.description = g.description + "\n---\n" + meta.Body
	}
	g.commits = append(g.commits, meta.Hash)
	for _, f := range meta.Files {
		if !g.seenFiles[f] {
			g.seenFiles[f] = true
			g.files = append(g.files, f)
		}
	}
}

// newRefFilter returns a predicate that accepts any ref when the filter list
// is empty, and only listed refs otherwise.
func newRefFilter(refs []string) func(string) bool {
	if len(refs) == 0 {
		return func(string) bool { return true }
	}
	want := make(map[string]bool, len(refs))
	for _, r := range refs {
		want[r] = true
	}
	return func(ref string) bool { return want[ref] }
}

// splitLines splits a git command output into non-empty lines.
func splitLines(out string) []string {
	var lines []string
	start := 0
	for i := 0; i <= len(out); i++ {
		if i == len(out) || out[i] == '\n' {
			if line := out[start:i]; line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	return lines
}
