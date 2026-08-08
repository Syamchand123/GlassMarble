package commit_reasoning

import (
	"sort"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/git"
)

// maxCommitGap is the largest gap between consecutive commits that still
// counts as "the same working session" for clustering purposes.
const maxCommitGap = 24 * time.Hour

// CommitCluster groups commits that belong to one logical change: they are
// close in time and share either a touched file or a subject token. A PR is
// the canonical example (many commits, one feature).
type CommitCluster struct {
	// Commits are ordered oldest first.
	Commits []*git.CommitMeta
	Files   []string
	PRs     []string
}

// ClusterCommits groups a time-ordered commit list into deterministic
// clusters. Two adjacent commits join the same cluster when their author
// timestamps are within maxCommitGap AND they share at least one touched
// file or one significant subject token. Singleton clusters are kept — a
// lone commit is still a cluster.
func ClusterCommits(metas []*git.CommitMeta) []CommitCluster {
	if len(metas) == 0 {
		return nil
	}
	var clusters []CommitCluster
	for _, m := range metas {
		if len(clusters) == 0 {
			clusters = append(clusters, CommitCluster{Commits: []*git.CommitMeta{m}})
			continue
		}
		lastIdx := len(clusters) - 1
		prev := clusters[lastIdx].Commits[len(clusters[lastIdx].Commits)-1]
		if withinGap(prev.Timestamp, m.Timestamp) && shareEvidence(clusters[lastIdx], m) {
			// Index the slice element — a struct copy would lose the append.
			clusters[lastIdx].Commits = append(clusters[lastIdx].Commits, m)
		} else {
			clusters = append(clusters, CommitCluster{Commits: []*git.CommitMeta{m}})
		}
	}
	for i := range clusters {
		clusters[i].finalize()
	}
	return clusters
}

// withinGap reports whether two author timestamps are close enough to be the
// same session. Zero timestamps (unparsed) only join when identical.
func withinGap(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return a.Equal(b)
	}
	gap := b.Sub(a)
	if gap < 0 {
		gap = -gap
	}
	return gap <= maxCommitGap
}

// shareEvidence reports whether a cluster and a new commit overlap on
// touched files or significant subject tokens.
func shareEvidence(cluster CommitCluster, m *git.CommitMeta) bool {
	clusterFiles := make(map[string]bool)
	for _, c := range cluster.Commits {
		for _, f := range c.Files {
			clusterFiles[strings.ToLower(f)] = true
		}
	}
	for _, f := range m.Files {
		if clusterFiles[strings.ToLower(f)] {
			return true
		}
	}
	clusterTokens := make(map[string]bool)
	for _, c := range cluster.Commits {
		for _, tok := range significantTokens(c.Subject) {
			clusterTokens[tok] = true
		}
	}
	for _, tok := range significantTokens(m.Subject) {
		if clusterTokens[tok] {
			return true
		}
	}
	return false
}

// significantTokens extracts meaningful subject words: 4+ alphanumeric
// characters that are not common glue words. "fix", "add", "update" are
// deliberately excluded — they carry no cluster identity.
var stopTokens = map[string]bool{
	"with": true, "from": true, "into": true, "when": true, "this": true,
	"that": true, "then": true, "they": true, "them": true, "have": true,
	"will": true, "make": true, "makeit": true, "maked": true, "more": true,
	"also": true, "only": true, "just": true, "some": true, "very": true,
}

func significantTokens(s string) []string {
	var out []string
	for _, tok := range nameTokensOf(s) {
		if len(tok) < 4 || stopTokens[tok] {
			continue
		}
		out = append(out, tok)
	}
	sort.Strings(out)
	return out
}

// finalize computes the cluster's derived fields: sorted unique files and
// the PR numbers referenced across the cluster's commits.
func (c *CommitCluster) finalize() {
	fileSet := make(map[string]bool)
	prSet := make(map[string]bool)
	for _, m := range c.Commits {
		for _, f := range m.Files {
			fileSet[f] = true
		}
		for _, pr := range m.RelatedPRs {
			prSet[pr] = true
		}
	}
	c.Files = sortedKeysOf(fileSet)
	c.PRs = sortedKeysOf(prSet)
}

func sortedKeysOf(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
