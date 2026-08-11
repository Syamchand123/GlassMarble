package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

// IsRootCommit reports whether ref has no parent commit (i.e. it is the
// repository's initial commit). The diff of a root commit covers the entire
// tree, so callers must not treat it as an incremental delta.
func IsRootCommit(repoDir, ref string) (bool, error) {
	if repoDir == "" || ref == "" {
		return false, fmt.Errorf("repo dir and ref are required")
	}
	if _, err := runGitCommand(repoDir, "rev-parse", ref+"^"); err != nil {
		return true, nil
	}
	return false, nil
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

// CommitMeta carries the full metadata of one git commit, used by the
// Stage 8 commit-reasoning engine (v2_master_implementaion_plan.md §6.2).
// Everything here is a directly observable git fact — no interpretation.
type CommitMeta struct {
	Hash          string    `json:"hash"`
	Author        string    `json:"author"`
	AuthorEmail   string    `json:"author_email"`
	Timestamp     time.Time `json:"timestamp"`
	Parents       []string  `json:"parents,omitempty"`
	Subject       string    `json:"subject"`
	Body          string    `json:"body"`
	Files         []string  `json:"files,omitempty"`
	Insertions    int       `json:"insertions"`
	Deletions     int       `json:"deletions"`
	Tags          []string  `json:"tags,omitempty"`
	RelatedPRs    []string  `json:"related_prs,omitempty"`
	RelatedIssues []string  `json:"related_issues,omitempty"`
}

// commitMetaFormat is the git log format for ReadCommit. Fields are separated
// by NUL because commit bodies may contain any byte except NUL — git
// guarantees commit messages never contain NUL, so this parse is lossless
// (a "|"-separated parse would break on any pipe in the body).
const commitMetaFormat = "%H%x00%an%x00%ae%x00%aI%x00%P%x00%D%x00%s%x00%b"

// ReadCommit reads the full metadata for one commit, resolving short prefixes
// and refs via ResolveRef. The diff statistics (files, insertions, deletions)
// come from `git diff-tree --numstat` against the first parent; root commits
// have no parent and are read against the empty tree.
func ReadCommit(repoDir, ref string) (*CommitMeta, error) {
	if repoDir == "" || ref == "" {
		return nil, fmt.Errorf("repo dir and ref are required")
	}
	fullHash, err := ResolveRef(repoDir, ref)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve commit %q: %w", ref, err)
	}

	out, err := runGitCommand(repoDir, "log", "-1", "--format="+commitMetaFormat, fullHash)
	if err != nil {
		return nil, fmt.Errorf("git log for %q failed: %w", fullHash, err)
	}
	meta, err := parseCommitMeta(out, fullHash)
	if err != nil {
		return nil, err
	}

	// Diff statistics against the parent. The empty-tree hash is the
	// conventional base for root commits.
	base := "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	if len(meta.Parents) > 0 {
		base = meta.Parents[0]
	}
	statsOut, err := runGitCommand(repoDir, "diff-tree", "--no-commit-id", "--numstat", "-r", "-M", base, fullHash)
	if err != nil {
		// Missing statistics are non-fatal: the commit facts still hold.
		return meta, nil
	}
	applyNumstat(statsOut, meta)
	return meta, nil
}

// ReadCommitRange reads the metadata for every commit in from..to, oldest
// first, stopping at the first unreadable commit with an error.
func ReadCommitRange(repoDir, from, to string) ([]*CommitMeta, error) {
	if repoDir == "" || from == "" || to == "" {
		return nil, fmt.Errorf("repo dir, from and to are required")
	}
	out, err := runGitCommand(repoDir, "log", "--reverse", "--format=%H", from+".."+to)
	if err != nil {
		return nil, fmt.Errorf("git log range %s..%s failed: %w", from, to, err)
	}
	var hashes []string
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			hashes = append(hashes, line)
		}
	}
	metas := make([]*CommitMeta, 0, len(hashes))
	for _, h := range hashes {
		m, err := ReadCommit(repoDir, h)
		if err != nil {
			return nil, fmt.Errorf("read commit %q: %w", h, err)
		}
		metas = append(metas, m)
	}
	return metas, nil
}

// parseCommitMeta splits the NUL-separated git log output into a CommitMeta.
// A malformed output is an error — never a partially populated structure that
// would silently lose provenance.
func parseCommitMeta(out, resolvedHash string) (*CommitMeta, error) {
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	if len(fields) < 7 {
		return nil, fmt.Errorf("git log output has %d fields, want at least 7", len(fields))
	}
	meta := &CommitMeta{
		Hash:        fields[0],
		Author:      fields[1],
		AuthorEmail: fields[2],
		Subject:     fields[6],
	}
	if meta.Hash == "" {
		meta.Hash = resolvedHash
	}
	if len(fields) > 7 {
		meta.Body = strings.TrimSpace(fields[7])
	}
	if t, err := time.Parse(time.RFC3339, fields[3]); err == nil {
		meta.Timestamp = t.UTC()
	} else {
		return nil, fmt.Errorf("commit %q has unparsable author timestamp %q: %w", meta.Hash, fields[3], err)
	}
	if fields[4] != "" {
		meta.Parents = strings.Fields(fields[4])
	}
	meta.Tags = parseRefTags(fields[5])
	return meta, nil
}

// parseRefTags extracts tag names from the %D decoration field
// ("HEAD -> main, tag: v1.2.0, origin/main").
func parseRefTags(decoration string) []string {
	var tags []string
	for _, part := range strings.Split(decoration, ",") {
		part = strings.TrimSpace(part)
		if rest, ok := strings.CutPrefix(part, "tag: "); ok {
			tags = append(tags, rest)
		}
	}
	sort.Strings(tags)
	return tags
}

// applyNumstat folds `git diff-tree --numstat` output into meta. Lines have
// the form "<ins>\t<del>\t<path>" where either count may be "-" for binary
// files; rename lines may carry "{old => new}" paths. Paths may contain
// spaces, so only the first two fields are split on whitespace.
func applyNumstat(out string, meta *CommitMeta) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		meta.Insertions += atoiSafe(fields[0])
		meta.Deletions += atoiSafe(fields[1])
		path := strings.Join(fields[2:], " ")
		meta.Files = append(meta.Files, normalizeNumstatPath(path))
	}
	sort.Strings(meta.Files)
	meta.Files = dedupeStrings(meta.Files)
}

// normalizeNumstatPath strips rename arrows from diff-tree output:
// "{old => new}" or "old => new" both reduce to "new".
func normalizeNumstatPath(path string) string {
	if i := strings.Index(path, "=>"); i >= 0 {
		rest := strings.TrimSpace(path[i+2:])
		rest = strings.TrimSuffix(strings.TrimSuffix(rest, "}"), ")")
		return strings.TrimSpace(rest)
	}
	return path
}

func atoiSafe(s string) int {
	if s == "-" || s == "" {
		return 0
	}
	n, _ := strconv.Atoi(s)
	return n
}

func dedupeStrings(in []string) []string {
	if len(in) < 2 {
		return in
	}
	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}
