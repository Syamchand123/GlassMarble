package commit_reasoning

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/evidence"
	"github.com/Syamchand123/GlassMarble/internal/git"
)

// Intent describes what a commit was trying to accomplish. These are
// architectural categories, not code-level ones — memory consumers (timeline,
// queries) group by them.
type Intent string

const (
	IntentAddFeature       Intent = "ADD_FEATURE"
	IntentFixBug           Intent = "FIX_BUG"
	IntentRefactor         Intent = "REFACTOR"
	IntentPerformance      Intent = "PERFORMANCE"
	IntentSecurity         Intent = "SECURITY"
	IntentTest             Intent = "TEST"
	IntentDocs             Intent = "DOCS"
	IntentInfrastructure   Intent = "INFRASTRUCTURE"
	IntentDependencyUpdate Intent = "DEPENDENCY_UPDATE"
	IntentUnknown          Intent = "UNKNOWN"
)

// IntentLevel ranks how much evidence backs an intent classification.
type IntentLevel int

const (
	// IntentLevelStructural marks intents derived from explicit facts —
	// build/CI/dependency files — that leave no room for interpretation.
	IntentLevelStructural IntentLevel = iota + 1
	// IntentLevelKeyword marks intents inferred from commit-message keywords.
	IntentLevelKeyword
	// IntentLevelLLM marks intents inferred by a language model.
	IntentLevelLLM
)

// IntentResult is a single intent classification with the evidence behind it.
type IntentResult struct {
	Intent     Intent
	Level      IntentLevel
	Source     evidence.Source
	Confidence float64
	Excerpt    string
}

// IntentLLMFunc is the optional Level-3 language-model backend. It receives
// the raw commit message and the PR description (when a PR is being reviewed)
// and must return a classification; an error falls back to the keyword level.
// Implementations must be safe for concurrent use and should cap prompt size
// themselves.
type IntentLLMFunc func(ctx context.Context, subject, body, prDescription string) (IntentResult, error)

// IntentExtractor classifies a commit's intent. Classification is
// deterministic and ordered: structural file evidence first (highest
// precision), then message keywords, then — only when an LLM backend is
// configured — a language model, and finally IntentUnknown. There is no map
// iteration anywhere in the decision path, so the winner is reproducible.
type IntentExtractor struct {
	llm    IntentLLMFunc
	logger *slog.Logger
}

// IntentExtractorOption configures an IntentExtractor.
type IntentExtractorOption func(*IntentExtractor)

// WithLLM enables Level-3 intent inference via the given backend.
func WithLLM(fn IntentLLMFunc) IntentExtractorOption {
	return func(e *IntentExtractor) { e.llm = fn }
}

// WithLogger attaches a logger used only for LLM failure diagnostics.
func WithLogger(l *slog.Logger) IntentExtractorOption {
	return func(e *IntentExtractor) { e.logger = l }
}

// NewIntentExtractor builds a keyword+structural extractor. An LLM backend
// can be attached later with WithLLM.
func NewIntentExtractor(opts ...IntentExtractorOption) *IntentExtractor {
	e := &IntentExtractor{logger: slog.Default()}
	for _, o := range opts {
		o(e)
	}
	return e
}

// confidenceIntentSource maps each level to its confidence and memory source.
// The source deliberately encodes the class (Constraint 8): structural
// evidence is EXPLICIT_REASON, keyword and LLM evidence are INFERENCE — a
// keyword guess must never masquerade as an explicit git fact.
var (
	// mergePrSubjectRegex matches the subject of a squash- or merge-committed
	// pull request ("Merge pull request #123 from acme/cache-fix"). The
	// subject is then just the branch name — an unreliable intent signal —
	// so keyword rules run against the body + PR description first and only
	// fall back to the full message when the body carries no signal.
	mergePrSubjectRegex = regexp.MustCompile(`(?i)^merge\s+(pull\s+request|pull-?request|branch)\b`)

	levelConfidence = map[IntentLevel]float64{
		IntentLevelStructural: 0.85,
		IntentLevelKeyword:    0.75,
		IntentLevelLLM:        0.60,
	}
	levelSource = map[IntentLevel]evidence.Source{
		IntentLevelStructural: evidence.SourceGit,
		IntentLevelKeyword:    evidence.SourceHeuristic,
		IntentLevelLLM:        evidence.SourceLLM,
	}
)

// structuralFileRules map a file that a commit touches to an explicit,
// file-level intent. A commit that only touches files matching one rule is
// classified at the structural level — the fact is in the tree, not the
// message.
var structuralFileRules = []struct {
	pattern *regexp.Regexp
	intent  Intent
}{
	// Dependency manifests.
	{
		regexp.MustCompile(`(?i)^.*(go\.mod|go\.sum|package\.json|package-lock\.json|yarn\.lock|pnpm-lock\.yaml|pom\.xml|build\.gradle|requirements.*\.txt|Cargo\.(toml|lock)|Gemfile(\.lock)?|composer\.(json|lock))$`),
		IntentDependencyUpdate,
	},
	// Build, CI, deployment and infrastructure configuration.
	{
		regexp.MustCompile(`(?i)^.*(\.github/workflows/|\.gitlab-ci\.yml|Jenkinsfile|Dockerfile|docker-compose(\.yml|\.yaml)?|Makefile|\.goreleaser\.yml|\.terraform/|terraform\.tf|k8s/|helm/|infra/|\.github/actions/)`),
		IntentInfrastructure,
	},
}

// keywordRules are the deterministic Level-2 intent keywords, ordered by
// decreasing specificity so the first match wins. All patterns carry (?i) and
// word boundaries; matching happens directly against the original message, so
// excerpts preserve the author's casing.
var keywordRules = []struct {
	pattern *regexp.Regexp
	intent  Intent
}{
	{regexp.MustCompile(`(?i)\b(fix(es|ed)?|bug( ?fix)?|correct(?:s|ed)?|resolve(?:s|d)?|hotfix)\b`), IntentFixBug},
	{regexp.MustCompile(`(?i)\b(test(?:s|ing)?|spec(?:s)?|coverage|unit ?test|integration ?test|e2e)\b`), IntentTest},
	{regexp.MustCompile(`(?i)\b(perf(?:ormance)?|optimiz(?:e|es|ed|ing)?|accelerat(?:e|es|ed)?|speed ?up|latency|throughput|slowdown)\b`), IntentPerformance},
	{regexp.MustCompile(`(?i)\b(secur(?:e|ity)|authentication|oauth|jwt|csrf|xss|vuln(?:erabilit)?|injection|sanitiz(?:e|es|ed)?|encrypt(?:s|ed|ion)?|decrypt(?:s|ed)?)\b`), IntentSecurity},
	{regexp.MustCompile(`(?i)\b(refactor(?:s|ed|ing)?|restructur(?:e|es|ed|ing)?|clean(?:up|s)?|reorgani[sz](?:e|es|ed|ing)?|simplif(?:y|ies|ied|ying)?|extract(?:s|ed|ing)?|consolidat(?:e|es|ed)?)\b`), IntentRefactor},
	{regexp.MustCompile(`(?i)\b(add(?:s|ed)?|feat(?:ure)?|new |introduce(?:s|d)?|implement(?:s|ed)?|support(?:s|ed)? for|extend(?:s|ed)?)\b`), IntentAddFeature},
	{regexp.MustCompile(`(?i)\b(doc(?:s|ument(?:s|ation)?)?\b|readme|changelog|comment(?:s|ing)?|wiki|adr )\b`), IntentDocs},
	{regexp.MustCompile(`(?i)\b(ci|cd|build|deploy(?:s|ed)?|release|infra(?:structure)?|pipeline|bump|upgrade|depend(?:enc|en)ies|tooling)\b`), IntentInfrastructure},
}

// Extract classifies the intent of a commit. meta supplies the message and
// touched files; prDescription optionally adds the PR context (merged PRs
// usually carry intent in the PR body, not the squashed commit).
//
// Levels, in order:
//  1. Structural — every touched file matches a structural rule.
//  2. Keyword    — first keyword rule matching subject+body+PR description.
//     A squash-merged PR whose subject is a bare "Merge pull request #N
//     from <branch>" marker runs the keyword rules against the body + PR
//     description first: the branch name is noise, the body is the author's
//     message.
//  3. LLM        — only when an IntentLLMFunc is configured; errors degrade
//     to Level 2 with a log line, never to silence.
//  4. Unknown    — nothing matched; still carries a source so the memory
//     builder can persist it.
func (e *IntentExtractor) Extract(ctx context.Context, meta *git.CommitMeta, prDescription string) IntentResult {
	if e == nil {
		return IntentResult{Intent: IntentUnknown, Level: IntentLevelKeyword, Source: evidence.SourceHeuristic, Confidence: levelConfidence[IntentLevelKeyword]}
	}
	if meta != nil {
		if r, ok := e.extractStructural(meta); ok {
			return r
		}
	}
	if meta != nil {
		if r, ok := e.keywordPriorityText(meta, prDescription); ok {
			return r
		}
	}
	if e.llm != nil && meta != nil {
		if r, err := e.llm(ctx, meta.Subject, meta.Body, prDescription); err == nil && r.Intent != IntentUnknown {
			r.Level = IntentLevelLLM
			r.Source = evidence.SourceLLM
			r.Confidence = levelConfidence[IntentLevelLLM]
			return r
		} else if err != nil && e.logger != nil {
			e.logger.Warn("commit_reasoning: LLM intent inference failed, falling back to keywords", "error", err)
		}
	}
	return IntentResult{
		Intent:     IntentUnknown,
		Level:      IntentLevelKeyword,
		Source:     evidence.SourceHeuristic,
		Confidence: levelConfidence[IntentLevelKeyword],
		Excerpt:    "no structural, keyword or LLM signal classified the intent",
	}
}

// extractStructural fires only when EVERY touched file matches one rule —
// a mixed change is a feature change, not a dependency update.
func (e *IntentExtractor) extractStructural(meta *git.CommitMeta) (IntentResult, bool) {
	files := meta.Files
	if len(files) == 0 {
		return IntentResult{}, false
	}
	for _, f := range files {
		if ruleForFile(f) == IntentUnknown {
			return IntentResult{}, false
		}
	}
	intent := ruleForFile(files[0])
	if intent == IntentUnknown {
		return IntentResult{}, false
	}
	excerpt := "only touched: " + strings.Join(files, ", ")
	if len(excerpt) > 512 {
		excerpt = excerpt[:512]
	}
	return IntentResult{
		Intent:     intent,
		Level:      IntentLevelStructural,
		Source:     evidence.SourceGit,
		Confidence: levelConfidence[IntentLevelStructural],
		Excerpt:    excerpt,
	}, true
}

func ruleForFile(file string) Intent {
	for _, rule := range structuralFileRules {
		if rule.pattern.MatchString(file) {
			return rule.intent
		}
	}
	return IntentUnknown
}

// keywordPriorityText runs the keyword rules against the best available
// text for the commit: for a merge-PR subject (a bare branch-name marker)
// the body + PR description is tried first, and only when it carries no
// signal does the full message win.
func (e *IntentExtractor) keywordPriorityText(meta *git.CommitMeta, prDescription string) (IntentResult, bool) {
	body := strings.TrimSpace(meta.Body)
	if prDescription != "" {
		if body != "" {
			body += "\n"
		}
		body += prDescription
	}
	if mergePrSubjectRegex.MatchString(meta.Subject) && body != "" {
		if r, ok := matchKeywordText(body); ok {
			return r, true
		}
	}
	return e.extractKeyword(meta, prDescription)
}

// extractKeyword runs the ordered keyword rules against the raw message.
// The excerpt is the original (case-preserved) line containing the match.
func (e *IntentExtractor) extractKeyword(meta *git.CommitMeta, prDescription string) (IntentResult, bool) {
	msg := strings.TrimSpace(meta.Subject + "\n" + meta.Body)
	if prDescription != "" {
		msg += "\n" + prDescription
	}
	return matchKeywordText(msg)
}

// matchKeywordText applies the ordered keyword rules to one text blob,
// returning the first (most specific) match.
func matchKeywordText(msg string) (IntentResult, bool) {
	for _, rule := range keywordRules {
		loc := rule.pattern.FindStringIndex(msg)
		if loc == nil {
			continue
		}
		return IntentResult{
			Intent:     rule.intent,
			Level:      IntentLevelKeyword,
			Source:     evidence.SourceHeuristic,
			Confidence: levelConfidence[IntentLevelKeyword],
			Excerpt:    excerptLine(msg, loc[0]),
		}, true
	}
	return IntentResult{}, false
}

// excerptLine returns the trimmed line containing index i, capped at 512.
func excerptLine(msg string, i int) string {
	start := strings.LastIndex(msg[:i], "\n") + 1
	endRel := strings.IndexByte(msg[i:], '\n')
	end := len(msg)
	if endRel >= 0 {
		end = i + endRel
	}
	line := strings.TrimSpace(msg[start:end])
	if len(line) > 512 {
		line = line[:512]
	}
	return line
}

// ClassifyIntent is a package-level convenience wrapping the default
// extractor — used by tests and callers that need no customization.
func ClassifyIntent(meta *git.CommitMeta, prDescription string) IntentResult {
	return NewIntentExtractor().Extract(context.Background(), meta, prDescription)
}
