package commit_reasoning

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
	"github.com/Syamchand123/GlassMarble/internal/git"
)

// Reasoner turns one commit (or a range of commits) into architectural
// events with evidence, intent and impact. It is the Stage 8 entry point
// (v2_master_implementaion_plan.md §6).
type Reasoner struct {
	cfg            *config.IntelligenceConfig
	logger         *slog.Logger
	extractor      *IntentExtractor
	forbiddenPairs []config.ForbiddenDepRule
}

// ReasonerOption configures a Reasoner.
type ReasonerOption func(*Reasoner)

// WithConfig attaches intelligence thresholds (e.g. layer definitions for
// violation rules). Nil-safe: a nil config disables config-gated rules.
func WithConfig(cfg *config.IntelligenceConfig) ReasonerOption {
	return func(r *Reasoner) { r.cfg = cfg }
}

// WithSLogger attaches a logger for diagnostics.
func WithSLogger(l *slog.Logger) ReasonerOption {
	return func(r *Reasoner) { r.logger = l }
}

// WithIntentExtractor overrides the default keyword/structural extractor —
// e.g. one wired with an LLM backend in cmd.
func WithIntentExtractor(e *IntentExtractor) ReasonerOption {
	return func(r *Reasoner) { r.extractor = e }
}

// WithLayerForbidden injects drift-level forbidden dependency pairs used by
// layer-violation rules, mirroring the Stage 5 engine convention
// (arch_intelligence.WithLayerForbidden).
func WithLayerForbidden(rules []config.ForbiddenDepRule) ReasonerOption {
	return func(r *Reasoner) { r.forbiddenPairs = rules }
}

// NewReasoner builds a Reasoner with the default intent extractor.
func NewReasoner(opts ...ReasonerOption) *Reasoner {
	r := &Reasoner{
		logger:    slog.Default(),
		extractor: NewIntentExtractor(),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// ReasonInput is the complete evidence a commit reasoning run can use.
// Only RepoDir and CommitHash are required; every snapshot/graph/diff field
// is optional and each classification pass skips the evidence it lacks.
type ReasonInput struct {
	RepoDir       string
	CommitHash    string
	PRDescription string
	BaseSnap      *archmodel.ArchSnapshot
	HeadSnap      *archmodel.ArchSnapshot
	GraphDiff     *akg.GraphDiff
	BaseGraph     *akg.CodePropertyGraph
	HeadGraph     *akg.CodePropertyGraph
}

// ReasonCommit analyzes one commit and produces its architectural events.
//
// Flow:
//  1. Read and resolve the commit metadata (internal/git, NUL-separated
//     parsing — safe for any body content).
//  2. Extract PR/issue references and the commit intent.
//  3. Classify the architectural changes (component, smell, graph, cycle
//     passes — see ClassifyChange).
//  4. Resolve the impact of the commit's files onto snapshot components.
//  5. Assemble deterministic events whose IDs match Stage 5D's scheme via
//     arch_intelligence.EventID, so the Stage 6 memory builder deduplicates
//     the two generators (Stage 8 events are enriched and appended first).
func (r *Reasoner) ReasonCommit(ctx context.Context, in ReasonInput) ([]archmodel.ArchEvent, error) {
	if in.RepoDir == "" || in.CommitHash == "" {
		return nil, fmt.Errorf("commit_reasoning: repo dir and commit hash are required")
	}
	meta, err := git.ReadCommit(in.RepoDir, in.CommitHash)
	if err != nil {
		return nil, err
	}
	ExtractRelatedRefs(meta)

	intent := r.extractor.Extract(ctx, meta, in.PRDescription)
	impact := ResolveImpact(meta.Files, in.HeadSnap)

	base := &ClassifyInput{
		Meta:      meta,
		BaseSnap:  in.BaseSnap,
		HeadSnap:  in.HeadSnap,
		Diff:      arch_timeline.Diff(in.BaseSnap, in.HeadSnap),
		GraphDiff: in.GraphDiff,
		BaseGraph: in.BaseGraph,
		HeadGraph: in.HeadGraph,
	}
	if r.cfg != nil {
		base.Layers = r.cfg.ArchLayers
	}
	base.Forbidden = r.forbiddenPairs
	changes := ClassifyChange(*base)

	events := make([]archmodel.ArchEvent, 0, len(changes))
	for _, c := range changes {
		events = append(events, r.buildEvent(meta, c, intent, impact))
	}
	return events, nil
}

// ReasonCommitRange analyzes every commit in from..to (oldest first) and
// returns all events. Snapshots are not available across a range, so only
// graph- and message-level passes run; the pipeline uses ReasonCommit with
// snapshots for its per-snapshot walk instead.
func (r *Reasoner) ReasonCommitRange(ctx context.Context, repoDir, from, to string) ([]archmodel.ArchEvent, error) {
	if repoDir == "" || from == "" || to == "" {
		return nil, fmt.Errorf("commit_reasoning: repo dir, from and to are required")
	}
	metas, err := git.ReadCommitRange(repoDir, from, to)
	if err != nil {
		return nil, err
	}
	var events []archmodel.ArchEvent
	for _, meta := range metas {
		ExtractRelatedRefs(meta)
		intent := r.extractor.Extract(ctx, meta, "")
		in := ClassifyInput{
			Meta:      meta,
			GraphDiff: nil,
			BaseSnap:  nil,
			HeadSnap:  nil,
		}
		for _, c := range ClassifyChange(in) {
			events = append(events, r.buildEvent(meta, c, intent, nil))
		}
	}
	return events, nil
}

// buildEvent assembles one ArchEvent from a classified change. The ID comes
// from the shared EventID contract, the evidence bundle mixes git facts,
// code facts and the intent claim, and the title is human-readable.
func (r *Reasoner) buildEvent(meta *git.CommitMeta, c ClassifiedChange, intent IntentResult, impact []string) archmodel.ArchEvent {
	b := evidence.Bundle{}
	if meta != nil && meta.Hash != "" {
		b.Add(evidence.EvidenceItem{
			Source:     evidence.SourceGit,
			Reference:  meta.Hash,
			Excerpt:    excerpt(meta.Subject, meta.Body),
			Confidence: 1.0,
			Timestamp:  meta.Timestamp,
		})
	}
	for _, it := range c.Evidence.Items {
		b.Add(it)
	}
	b.Add(evidence.EvidenceItem{
		Source:     intent.Source,
		Reference:  intentRef(meta),
		Excerpt:    intent.Excerpt,
		Confidence: intent.Confidence,
		Timestamp:  meta.Timestamp,
	})
	components := c.Names
	if len(components) == 0 {
		components = impact
	}
	ev := archmodel.ArchEvent{
		ID:            arch_intelligence.EventID(meta.Hash, c.Kind, c.AffectedIDs),
		Kind:          c.Kind,
		CommitHash:    meta.Hash,
		Timestamp:     meta.Timestamp,
		Title:         titleFor(c),
		Description:   c.Summary,
		AffectedIDs:   c.AffectedIDs,
		Components:    dedupeSorted(components),
		Evidence:      b,
		Intent:        string(intent.Intent),
		IntentSrc:     intent.Source,
		Tags:          dedupeSorted(c.Tags),
		RelatedPRs:    meta.RelatedPRs,
		RelatedIssues: meta.RelatedIssues,
		ValidFrom:     meta.Timestamp,
	}
	return ev
}

// titleFor renders a stable, human-readable title for a classified change.
func titleFor(c ClassifiedChange) string {
	kind := string(c.Kind)
	parts := strings.Split(strings.ToLower(kind), "_")
	words := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			words = append(words, strings.ToUpper(p[:1])+p[1:])
		}
	}
	head := strings.Join(words, " ")
	if len(c.Names) > 0 {
		return head + ": " + strings.Join(c.Names, ", ")
	}
	if len(c.AffectedIDs) > 0 {
		return head + ": " + strings.Join(c.AffectedIDs, ", ")
	}
	return head
}

// excerpt caps the commit message evidence at 512 characters.
func excerpt(subject, body string) string {
	msg := strings.TrimSpace(subject + "\n" + body)
	if len(msg) > 512 {
		msg = msg[:512]
	}
	return msg
}

func intentRef(meta *git.CommitMeta) string {
	if meta != nil && len(meta.RelatedPRs) > 0 {
		return "PR #" + strings.Join(meta.RelatedPRs, ", #")
	}
	if meta != nil {
		return meta.Hash
	}
	return ""
}

func dedupeSorted(in []string) []string {
	if len(in) < 2 {
		return in
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	uniq := out[:1]
	for _, s := range out[1:] {
		if s != uniq[len(uniq)-1] {
			uniq = append(uniq, s)
		}
	}
	return uniq
}
