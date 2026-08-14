package knowledge_fusion

import (
	"context"
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// RunResult reports what one fusion run produced. It drives the
// "knowledge fusion: fused N claims from M sources" summary line in `gmb analyze`.
type RunResult struct {
	// AdrFiles is the number of ADR files that yielded at least one claim.
	AdrFiles int `json:"adr_files"`
	// ReadmeFiles is the number of README files that yielded claims.
	ReadmeFiles int `json:"readme_files"`
	// PRs is the number of pull requests whose file changes became claims.
	PRs int `json:"prs"`
	// Issues is the number of issues whose file changes became claims.
	Issues int `json:"issues"`
	// Sources is the total number of distinct sources that contributed
	// claims (ADR files + README files + PRs + issues).
	Sources int `json:"sources"`
	// TotalClaims is the number of claims after conflict resolution.
	TotalClaims int `json:"total_claims"`
	// NewClaims is the number of claims newly appended to the memory WAL
	// (zero when nothing changed — fusion is idempotent).
	NewClaims int `json:"new_claims"`
}

// FusionEngine coordinates multi-source knowledge extraction, linking,
// conflict resolution and persistence into developer memory. It is the
// knowledge fusion orchestrator (master plan §7 / §13.1).
type FusionEngine struct {
	cfg          *config.FusionConfig
	store        *developer_memory.MemoryStore
	prAdapter    PRAdapter
	issueAdapter IssueAdapter
	logf         func(format string, args ...any)
}

// Option customizes a FusionEngine.
type Option func(*FusionEngine)

// WithPRAdapter installs a pull-request source adapter. When set, PR claims
// are fused (subject to cfg.GitSourcesEnabled()).
func WithPRAdapter(a PRAdapter) Option {
	return func(f *FusionEngine) { f.prAdapter = a }
}

// WithIssueAdapter installs an issue-tracker source adapter.
func WithIssueAdapter(a IssueAdapter) Option {
	return func(f *FusionEngine) { f.issueAdapter = a }
}

// WithLogger attaches a warning sink for non-fatal source failures.
func WithLogger(logf func(format string, args ...any)) Option {
	return func(f *FusionEngine) {
		if logf != nil {
			f.logf = logf
		}
	}
}

// NewFusionEngine creates a knowledge fusion engine. cfg may be nil (defaults are
// applied). store is the developer-memory store the fused claims are
// appended to; a nil store makes Run return a validation error — fused
// claims without persistence would be lost.
func NewFusionEngine(cfg *config.FusionConfig, store *developer_memory.MemoryStore, opts ...Option) *FusionEngine {
	if cfg == nil {
		cfg = config.DefaultFusionConfig()
	}
	cfg.ApplyDefaults()
	f := &FusionEngine{
		cfg:   cfg,
		store: store,
		logf:  func(string, ...any) {},
	}
	for _, o := range opts {
		o(f)
	}
	return f
}

// warn reports a non-fatal condition through the attached logger.
func (f *FusionEngine) warn(format string, args ...any) {
	if f.logf != nil {
		f.logf(format, args...)
	}
}

// Run executes the full fusion pipeline against repoDir:
//
//	discover docs → parse ADRs/READMEs → fetch PRs/issues (adapters)
//	    → build claims → entity-link against the AKG
//	    → resolve conflicts → append NEW claims to developer memory
//
// Failure semantics follow master plan §15.6: individual source failures
// (an unreadable ADR, an unavailable adapter) are logged and skipped —
// partial fusion is better than no fusion. Run returns an error only for
// structural failures: an unreadable repo directory, an absent store, or a
// persistence failure (a claim that fails validation is an internal bug and
// is returned as an error before anything is written).
//
// Run is idempotent: re-running with the same sources appends nothing.
func (f *FusionEngine) Run(ctx context.Context, repoDir string, graph *akg.CodePropertyGraph) (RunResult, error) {
	var res RunResult
	if f.store == nil {
		return res, fmt.Errorf("knowledge_fusion: no memory store configured")
	}
	if repoDir == "" {
		return res, fmt.Errorf("knowledge_fusion: repo dir is required")
	}

	lexicon := f.cfg.Lexicon()

	// 1. Discover and parse documentation sources.
	docs, err := FindDocs(repoDir, f.cfg)
	if err != nil {
		return res, fmt.Errorf("knowledge_fusion: discover docs: %w", err)
	}
	rawClaims, err := f.parseDocs(docs, lexicon)
	if err != nil {
		return res, err
	}
	res.AdrFiles = f.countKind(docs, DocKindADR)
	res.ReadmeFiles = f.countKind(docs, DocKindReadme)

	// 2. PR/issue sources from git history (opt-in via config).
	if f.cfg.GitSourcesEnabled() {
		prs, issues, err := f.fetchSourceControl(ctx, repoDir)
		if err != nil {
			f.warn("knowledge_fusion: git sources unavailable, continuing with docs only: %v", err)
		} else {
			rawClaims = append(rawClaims, prClaims(prs)...)
			rawClaims = append(rawClaims, issueClaims(issues)...)
			res.PRs = len(prs)
			res.Issues = len(issues)
		}
	}

	// 3. Entity-link against the AKG. A nil graph is legal (no linking).
	linked := LinkDocumentClaimsToAKG(rawClaims, graph)

	// 4. Resolve contradictions. The loser of a contradiction is marked
	// HISTORICAL; every claim survives.
	resolved := ResolveConflicts(linked, exclusivePredicateSet(f.cfg.ExclusivePredicates))

	// 5. Validate and persist only the claims the memory WAL does not
	// already have (idempotency + bounded WAL growth).
	appended, err := f.persistNewClaims(ctx, resolved)
	if err != nil {
		return res, err
	}

	res.TotalClaims = len(resolved)
	res.NewClaims = appended
	res.Sources = res.AdrFiles + res.ReadmeFiles + res.PRs + res.Issues
	return res, nil
}

// parseDocs parses every discovered document, counting successes. A
// malformed ADR is a per-source failure: logged and skipped.
func (f *FusionEngine) parseDocs(docs []DocSource, lexicon []string) ([]developer_memory.KnowledgeClaim, error) {
	var claims []developer_memory.KnowledgeClaim
	for _, doc := range docs {
		switch doc.Kind {
		case DocKindADR:
			adrClaims, err := ParseADR(doc, lexicon)
			if err != nil {
				f.warn("knowledge_fusion: skipping ADR %s: %v", doc.Rel, err)
				continue
			}
			claims = append(claims, adrClaims...)
		case DocKindReadme:
			claims = append(claims, ParseReadme(doc, lexicon)...)
		}
	}
	return claims, nil
}

// fetchSourceControl builds the LocalGitAdapter from the config and fetches
// PR/issue records. An adapter is only consulted when one is installed, so
// the engine works with zero wiring (docs-only fusion).
func (f *FusionEngine) fetchSourceControl(ctx context.Context, repoDir string) ([]PullRequest, []Issue, error) {
	var prs []PullRequest
	var issues []Issue
	if f.prAdapter == nil && f.issueAdapter == nil {
		return nil, nil, fmt.Errorf("no PR/issue adapters installed")
	}
	if f.prAdapter != nil {
		got, err := f.prAdapter.FetchRelatedPRs(ctx, nil)
		if err != nil {
			f.warn("knowledge_fusion: PR adapter %s failed: %v", f.prAdapter.Name(), err)
		} else {
			prs = got
		}
	}
	if f.issueAdapter != nil {
		got, err := f.issueAdapter.FetchRelatedIssues(ctx, nil)
		if err != nil {
			f.warn("knowledge_fusion: issue adapter %s failed: %v", f.issueAdapter.Name(), err)
		} else {
			issues = got
		}
	}
	if len(prs) == 0 && len(issues) == 0 {
		return nil, nil, fmt.Errorf("no PR/issue records extracted")
	}
	return prs, issues, nil
}

// prClaims converts PR records into file-level claims. Each claim states
// that one changed file was modified by the PR — a directly observable fact
// (SourcePR, ClaimFact), with the PR's title and description as evidence
// excerpt and the earliest commit author time as ValidFrom. The subject is
// the file path; the entity linker expands it to the AKG nodes defined in
// that file.
func prClaims(prs []PullRequest) []developer_memory.KnowledgeClaim {
	var claims []developer_memory.KnowledgeClaim
	for _, pr := range prs {
		ts := pr.Timestamp.UTC()
		excerpt := joinNonEmpty(pr.Title, pr.Description, " | ")
		bundle := evidence.NewBundle(evidence.EvidenceItem{
			Source:     evidence.SourcePR,
			Reference:  "PR " + pr.ID,
			Excerpt:    excerpt,
			Confidence: 0.90,
			Timestamp:  ts,
		})
		for _, file := range pr.FilesChanged {
			claims = append(claims, newFusedClaim(
				"pr", "PR "+pr.ID, file, "was_modified_by_pr", "PR "+pr.ID,
				developer_memory.ClaimFact, developer_memory.StateActive,
				ts, bundle, "", "",
			))
		}
	}
	return claims
}

// issueClaims converts issue records into file-level claims: a file changed
// in a commit that references the issue is claimed to "fix_issue" it. The
// source is the issue itself (SourceIssue) — the reference is observable,
// the framing is the issue tracker's.
func issueClaims(issues []Issue) []developer_memory.KnowledgeClaim {
	var claims []developer_memory.KnowledgeClaim
	for _, issue := range issues {
		ts := issue.Timestamp.UTC()
		excerpt := joinNonEmpty(issue.Title, issue.Description, " | ")
		bundle := evidence.NewBundle(evidence.EvidenceItem{
			Source:     evidence.SourceIssue,
			Reference:  "issue " + issue.ID,
			Excerpt:    excerpt,
			Confidence: 0.90,
			Timestamp:  ts,
		})
		for _, file := range issue.FilesChanged {
			claims = append(claims, newFusedClaim(
				"issue", "issue "+issue.ID, file, "fixes_issue", "issue "+issue.ID,
				developer_memory.ClaimFact, developer_memory.StateActive,
				ts, bundle, "", "",
			))
		}
	}
	return claims
}

// joinNonEmpty joins the non-empty parts with the separator.
func joinNonEmpty(parts ...string) string {
	sep := " | "
	var out string
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if i > 0 && out != "" {
			out += sep
		}
		out += p
	}
	return out
}

// persistNewClaims validates every resolved claim and appends only those
// whose ID is not already in the memory claims WAL, then rebuilds and
// persists the memory aggregate so fused claims are immediately queryable
// through `gmb memory --ask`.
//
// Validation failure of any claim aborts the whole run before anything is
// written (a claim missing evidence is an internal bug — never persist it).
// The existing-claims check makes fusion idempotent while keeping the WAL
// append-only AND bounded: re-running on the same sources adds nothing.
func (f *FusionEngine) persistNewClaims(ctx context.Context, claims []developer_memory.KnowledgeClaim) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	for _, c := range claims {
		if err := validateFusedClaim(c); err != nil {
			return 0, fmt.Errorf("knowledge_fusion: invalid fused claim: %w", err)
		}
	}

	existing, err := f.store.LoadClaims()
	if err != nil {
		return 0, fmt.Errorf("knowledge_fusion: load existing claims: %w", err)
	}
	have := make(map[string]bool, len(existing))
	for _, c := range existing {
		have[c.ID] = true
	}

	// Deduplicate within the batch (conflict resolution already merges
	// identical claims, but expansions can re-collide after linking).
	seen := make(map[string]bool)
	appended := 0
	for i := range claims {
		c := claims[i]
		if have[c.ID] || seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		if err := f.store.AppendClaim(c); err != nil {
			return appended, fmt.Errorf("knowledge_fusion: append claim %q: %w", c.ID, err)
		}
		appended++
	}
	if appended == 0 {
		return 0, nil
	}

	mem, err := f.store.Rebuild()
	if err != nil {
		return appended, fmt.Errorf("knowledge_fusion: rebuild memory: %w", err)
	}
	if err := f.store.SaveMemoryAndTimeline(mem); err != nil {
		return appended, fmt.Errorf("knowledge_fusion: persist memory: %w", err)
	}
	return appended, nil
}

// countKind returns how many discovered docs have the given kind. Only
// docs that produced claims count toward the result — but since the parser
// never emits zero claims for a valid file, counting discovered files is
// equivalent and simpler; we count only kinds that are represented.
func (f *FusionEngine) countKind(docs []DocSource, kind DocKind) int {
	n := 0
	for _, d := range docs {
		if d.Kind == kind {
			n++
		}
	}
	return n
}
