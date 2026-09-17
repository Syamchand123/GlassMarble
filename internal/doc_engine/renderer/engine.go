// Package renderer — engine.go
// Orchestrator: Track A (LLM prose, mandatory by default) with the
// deterministic reference appendix folded in, or Track B alone (the
// complete deterministic document) only under the explicit --no-llm
// opt-out. See RenderSection's doc comment for the exact contract.
// Implements Stage 6, coordinates Stage 7 (Quality Firewall) and Stage 8 (Atomic MVCC Write).
package renderer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/grounding"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/invalidator"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/patcher"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/review"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/verifier"
)

// OrchestratorOptions configures the dual-track renderer orchestrator.
type OrchestratorOptions struct {
	NoLLM    bool
	Model    string
	Provider provider.Provider
	Verbose  bool
	Out      io.Writer
	// GlobalStyle is the DocsConfig.Style fallback merged under the
	// per-document style: per-doc Voice/JargonBlacklist win when non-empty.
	GlobalStyle config.StyleSpec
	// MaxOutputTokens overrides DefaultLLMActuatorConfig's hardcoded budget
	// (300) with the user's configured ai.yaml max_output_tokens when set
	// (>0). Without this, Track A always used 300 output tokens regardless
	// of what the user configured (ai.yaml commonly sets 8192+) — a budget
	// that small is often exhausted by a reasoning model's own chain-of-
	// thought before it ever emits the final answer, producing a response
	// truncated mid-thought that then gets shipped as if it were the
	// section's content (see LLMActuatorConfig.MaxOutputTokens).
	MaxOutputTokens int
	// Temperature overrides DefaultLLMActuatorConfig's hardcoded 0.0 with
	// the user's configured ai.yaml temperature when set.
	Temperature *float64
}

// Orchestrator coordinates dual-track rendering, quality gates, and MVCC writes.
type Orchestrator struct {
	opts     OrchestratorOptions
	det      *DeterministicRenderer
	actuator *LLMActuator

	// D5 observability: usage counters accumulated across ProcessDocument
	// calls (guarded for parallel section rendering).
	countsMu   sync.Mutex
	tracksUsed map[string]int
	repairs    int
	fallbacks  int
}

// SnapshotCounters returns D5 observability counters: per-track render
// counts, repair uses, and Track A→B fallbacks. Safe for concurrent use.
func (o *Orchestrator) SnapshotCounters() (tracks map[string]int, repairs, fallbacks int) {
	o.countsMu.Lock()
	defer o.countsMu.Unlock()
	tracks = make(map[string]int, len(o.tracksUsed))
	for k, v := range o.tracksUsed {
		tracks[k] = v
	}
	return tracks, o.repairs, o.fallbacks
}

func (o *Orchestrator) countTrack(track string) {
	o.countsMu.Lock()
	defer o.countsMu.Unlock()
	if o.tracksUsed == nil {
		o.tracksUsed = make(map[string]int)
	}
	o.tracksUsed[track]++
}

func (o *Orchestrator) countRepair() {
	o.countsMu.Lock()
	defer o.countsMu.Unlock()
	o.repairs++
}

func (o *Orchestrator) countFallback() {
	o.countsMu.Lock()
	defer o.countsMu.Unlock()
	o.fallbacks++
}

// lockedWriter serializes concurrent writes to the orchestrator's Out
// (phase-1 render goroutines share it for verbose fallback notes; the
// configured writers — bytes.Buffer in tests included — are not
// goroutine-safe on their own).
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// parallelSections reports whether concurrent section rendering is enabled.
// Env GMB_DOC_PARALLEL=0 (also "false"/"off"/"no") disables it and restores
// strictly serial rendering. Any other value, including unset, enables it.
// There is deliberately no CLI flag (cmd/ is frozen); the env gate is the
// documented opt-out.
func parallelSections() bool {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv("GMB_DOC_PARALLEL"))); v {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

// maxSectionWorkers caps the phase-1 worker pool at min(4, NumCPU, nJobs).
func maxSectionWorkers(nJobs int) int {
	n := nJobs
	if m := runtime.NumCPU(); m < n {
		n = m
	}
	if n > 4 {
		n = 4
	}
	if n < 1 {
		n = 1
	}
	return n
}

// NewOrchestrator creates a new Dual-Track Orchestrator.
func NewOrchestrator(opts OrchestratorOptions) *Orchestrator {
	if opts.Out == nil {
		opts.Out = io.Discard
	} else {
		opts.Out = &lockedWriter{w: opts.Out}
	}

	det := NewDeterministicRenderer()
	var actuator *LLMActuator

	if opts.Provider != nil && !opts.NoLLM {
		actCfg := DefaultLLMActuatorConfig(opts.Provider, opts.Model)
		if opts.MaxOutputTokens > 0 {
			actCfg.MaxOutputTokens = opts.MaxOutputTokens
		}
		if opts.Temperature != nil {
			actCfg.Temperature = *opts.Temperature
		}
		actuator = NewLLMActuator(actCfg)
	}

	return &Orchestrator{
		opts:     opts,
		det:      det,
		actuator: actuator,
	}
}

// RenderResult captures the outcome of rendering a single section.
type SectionRenderOutcome struct {
	Content      string
	RenderMode   string // "llm" | "deterministic"
	TokensUsed   int
	DurationMs   int64
	RepairUsed   bool
	FallbackUsed bool
	Warning      string
}

// RenderSection executes rendering with Quality Firewall gates and repair
// retries. The LLM is mandatory by default: real documentation is a real
// explanation, and the only thing making that possible is Track A. There
// is no automatic fallback to the deterministic renderer when Track A
// fails or isn't configured — that used to mean a raw fact-table dump
// could silently ship as if it were finished prose, which is exactly the
// failure mode this design closes. The deterministic renderer's full
// standalone output is reachable only via the explicit opts.NoLLM
// (`gmb doc --no-llm`) opt-out, which the caller (cmd/doc.go) treats as a
// deliberate, clearly-labeled choice, not a degrade path.
func (o *Orchestrator) RenderSection(ctx context.Context, fs *config.FactSheet, graph *akg.CodePropertyGraph) (SectionRenderOutcome, error) {
	if fs == nil {
		return SectionRenderOutcome{}, fmt.Errorf("doc_engine/orchestrator: nil FactSheet")
	}

	symIndex := buildSymbolIndex(fs, graph)

	if o.opts.NoLLM {
		outcome, err := o.renderTrackB(fs, symIndex)
		if err == nil {
			o.countTrack("deterministic")
		}
		return outcome, err
	}

	if o.actuator == nil {
		return SectionRenderOutcome{}, fmt.Errorf("doc_engine/orchestrator: no LLM provider configured — documentation generation requires a working LLM (run `gmb ai configure`, or pass --no-llm to explicitly generate a grounding-only reference document instead)")
	}

	outcome, err := o.renderTrackA(ctx, fs, symIndex)
	if err != nil {
		return SectionRenderOutcome{}, fmt.Errorf("LLM generation failed for section %q: %w", fs.SectionID, err)
	}
	o.countTrack("llm")
	if outcome.RepairUsed {
		o.countRepair()
	}
	return outcome, nil
}

// renderTrackA executes Track A with 1 repair retry on Gate 1/2/3 failure.
// The LLM writes prose only (see prompts.go's system prompt); the gates
// validate that prose alone (grounding, markdown/diagram syntax, secrets,
// and — against the PRIOR PROSE only, via stripReferenceAppendix — whether
// it's a cosmetic no-op worth discarding to avoid git churn). The
// deterministic reference appendix is then rendered fresh from the
// CURRENT fact sheet and appended unconditionally: it must reflect
// whatever actually changed in the code even on a run where the prose
// itself is an accurate, unchanged no-op, and combining it in only after
// the gates run means it never needs re-validating (it is provably
// grounded by construction, unlike anything the model wrote).
func (o *Orchestrator) renderTrackA(ctx context.Context, fs *config.FactSheet, symIndex verifier.AKGSymbolIndex) (SectionRenderOutcome, error) {
	resp, err := o.actuator.Render(ctx, fs)
	if err != nil {
		return SectionRenderOutcome{}, err
	}

	priorProse := stripReferenceAppendix(fs.PriorSectionMarkdown)
	candidate := humanizeCompoundIdentifiers(resp.Text)
	gateRes := verifier.RunGates(priorProse, candidate, symIndex)

	if gateRes.Pass {
		prose := candidate
		if gateRes.SemanticNoOp && priorProse != "" {
			// Zero-git-churn: discard candidate if only cosmetic diff
			prose = priorProse
		}
		return o.finishTrackA(fs, prose, resp.TotalTokens, resp.Duration, false, symIndex)
	}

	// Gate failure inspection
	if gateRes.FailedGate == 4 {
		// Gate 4: Hard fail on secret pattern — do not retry LLM
		return SectionRenderOutcome{}, fmt.Errorf("gate 4 hard failure: %w", gateRes.Error)
	}

	// Gate 1, 2, or 3: Attempt 1 repair retry
	repairResp, repairErr := o.actuator.Repair(ctx, fs, candidate, gateRes.Error)
	if repairErr != nil {
		return SectionRenderOutcome{}, fmt.Errorf("repair attempt failed: %w", repairErr)
	}

	repairedCandidate := humanizeCompoundIdentifiers(repairResp.Text)
	repairGateRes := verifier.RunGates(priorProse, repairedCandidate, symIndex)

	if repairGateRes.Pass {
		prose := repairedCandidate
		if repairGateRes.SemanticNoOp && priorProse != "" {
			prose = priorProse
		}
		return o.finishTrackA(fs, prose, resp.TotalTokens+repairResp.TotalTokens, resp.Duration+repairResp.Duration, true, symIndex)
	}

	return SectionRenderOutcome{}, fmt.Errorf("gate %d failed after repair: %w", repairGateRes.FailedGate, repairGateRes.Error)
}

// finishTrackA combines gate-validated prose with a freshly-rendered
// reference appendix into the final section content, then runs one last
// full-content gate pass (mainly Gate 4: the appendix is built from real
// source doc-comments/signatures, which could in principle still contain
// something secret-shaped) before shipping.
func (o *Orchestrator) finishTrackA(fs *config.FactSheet, prose string, tokens int, dur time.Duration, repaired bool, symIndex verifier.AKGSymbolIndex) (SectionRenderOutcome, error) {
	content := combineProseAndAppendix(prose, o.det.RenderReferenceAppendix(fs))

	if finalGate := verifier.RunGates(fs.PriorSectionMarkdown, content, symIndex); !finalGate.Pass {
		return SectionRenderOutcome{}, fmt.Errorf("gate %d failed on combined prose+reference content: %w", finalGate.FailedGate, finalGate.Error)
	}

	return SectionRenderOutcome{
		Content:    content,
		RenderMode: "llm",
		TokensUsed: tokens,
		DurationMs: dur.Milliseconds(),
		RepairUsed: repaired,
	}, nil
}

// renderTrackB executes deterministic generation directly.
func (o *Orchestrator) renderTrackB(fs *config.FactSheet, symIndex verifier.AKGSymbolIndex) (SectionRenderOutcome, error) {
	content, err := o.det.RenderSection(fs)
	if err != nil {
		return SectionRenderOutcome{}, fmt.Errorf("deterministic render: %w", err)
	}

	gateRes := verifier.RunGates(fs.PriorSectionMarkdown, content, symIndex)
	if !gateRes.Pass && gateRes.FailedGate == 4 {
		return SectionRenderOutcome{}, fmt.Errorf("deterministic output triggered gate 4 secret scan: %w", gateRes.Error)
	}

	outcome := SectionRenderOutcome{
		Content:    content,
		RenderMode: "deterministic",
		TokensUsed: 0,
		DurationMs: 0,
	}
	if !gateRes.Pass {
		// All 5 gates observe Track B output (plan Stage 7: no exceptions).
		// Deterministic output is grounded by construction, so a non-secret
		// gate failure is reported, not fatal: keep prior content when the
		// failure is churn-only, otherwise ship with a warning.
		if gateRes.SemanticNoOp && fs.PriorSectionMarkdown != "" {
			outcome.Content = fs.PriorSectionMarkdown
		}
		outcome.Warning = fmt.Sprintf("deterministic output failed gate %d (%v); shipped with warning", gateRes.FailedGate, gateRes.Error)
	}
	return outcome, nil
}

// enforceMaxWords verifies the SectionSpec.MaxWords cap post-render.
// Prompt text alone is not enforcement: on violation Track A gets one
// repair retry asking for brevity; otherwise (or on Track B) a warning is
// recorded and the content ships unchanged — truncation would corrupt
// markdown structure.
func (o *Orchestrator) enforceMaxWords(ctx context.Context, fs *config.FactSheet, sec *config.SectionSpec, graph *akg.CodePropertyGraph, outcome SectionRenderOutcome, warnings *[]string) string {
	if sec == nil || sec.MaxWords <= 0 {
		return outcome.Content
	}

	if outcome.RenderMode != "llm" {
		// Deterministic tables (the explicit --no-llm path) cannot be
		// shortened without discarding grounded facts.
		if wordCount(outcome.Content) > sec.MaxWords {
			*warnings = append(*warnings, fmt.Sprintf("section %s exceeds MaxWords cap (%d words > %d max)", sec.ID, wordCount(outcome.Content), sec.MaxWords))
		}
		return outcome.Content
	}

	// The word budget is about the PROSE the model wrote, never the
	// deterministic reference appendix folded in afterward: counting the
	// appendix against it would unfairly shrink the model's writing
	// budget on any well-documented package, and asking the model to
	// "shorten" the combined text risked it rewriting or dropping tables
	// it was never supposed to touch.
	prose, appendix := splitProseAndAppendix(outcome.Content)
	if wordCount(prose) <= sec.MaxWords {
		return outcome.Content
	}
	if o.actuator != nil && !outcome.RepairUsed {
		repairResp, repairErr := o.actuator.Repair(ctx, fs, prose,
			fmt.Errorf("your prose is %d words, exceeding the MaxWords cap of %d: shorten it without losing any factual statement", wordCount(prose), sec.MaxWords))
		if repairErr == nil {
			repairGateRes := verifier.RunGates(stripReferenceAppendix(fs.PriorSectionMarkdown), repairResp.Text, buildSymbolIndex(fs, graph))
			if repairGateRes.Pass && wordCount(repairResp.Text) <= sec.MaxWords {
				*warnings = append(*warnings, fmt.Sprintf("section %s shortened to MaxWords cap (%d words)", sec.ID, sec.MaxWords))
				o.countRepair()
				return combineProseAndAppendix(repairResp.Text, appendix)
			}
		}
	}
	*warnings = append(*warnings, fmt.Sprintf("section %s exceeds MaxWords cap (%d words > %d max)", sec.ID, wordCount(prose), sec.MaxWords))
	return outcome.Content
}

func wordCount(s string) int {
	return len(strings.Fields(s))
}

// queueConflictReview records a 3-way merge conflict as a D6 human-review
// item (kind "conflict"). Best-effort: any queue error is swallowed by the
// caller contract — review must never fail a doc run.
func queueConflictReview(repoRoot, docPath, sectionID, base, ours, theirs string) error {
	oursLine, theirsLine := firstDiffLine(base, ours), firstDiffLine(base, theirs)
	_, err := review.Queue(repoRoot, review.ReviewItem{
		Kind:      "conflict",
		DocPath:   docPath,
		SectionID: sectionID,
		Summary:   fmt.Sprintf("merge conflict in %s [%s]: human edits preserved, machine update appended as note", docPath, sectionID),
		Detail:    fmt.Sprintf("first human divergence at line %d; first machine divergence at line %d", oursLine, theirsLine),
	})
	return err
}

func firstDiffLine(base, other string) int {
	bl, ol := strings.Split(base, "\n"), strings.Split(other, "\n")
	for i := 0; i < len(bl) && i < len(ol); i++ {
		if bl[i] != ol[i] {
			return i + 1
		}
	}
	if len(bl) != len(ol) {
		if len(bl) < len(ol) {
			return len(bl) + 1
		}
		return len(ol) + 1
	}
	return 0
}

// ProcessDocument coordinates the full read -> ground -> render -> merge -> write pipeline for a single document.
func (o *Orchestrator) ProcessDocument(
	ctx context.Context,
	repoRoot string,
	doc *config.DocSpec,
	dirtySectionIDs []string,
	graph *akg.CodePropertyGraph,
	dossier *config.GlobalCommitDossier,
	sm *storage.StateManager,
	commitHash string,
) (docUpdated bool, tokensUsed int, warnings []string, failedSections int, err error) {
	return o.processDocument(ctx, repoRoot, doc, dirtySectionIDs, graph, dossier, sm, commitHash, nil)
}

// ProcessDocumentWithBudget is ProcessDocument plus a shared token budget:
// found via live testing against a real, large repository (internal/
// doc_engine itself, ~22k AKG nodes) — a single document scoped broadly
// enough spent 759,704 tokens in one run despite docs.yaml's own
// constraints.max_tokens_per_run: 50000, because doc_engine.Run's budget
// check (totalTokens >= maxTokens) only ever runs BETWEEN documents in
// its own outer loop — there is no such checkpoint between the SECTIONS
// of a single document, which is exactly where ProcessDocument spends
// tokens. A repo with one broadly-scoped document (or just one document
// selected via --doc/--tag) never hits the between-documents check at
// all. budget is shared across every section of this document (see
// sectionBudget) and, when the caller threads the SAME budget across
// multiple ProcessDocument calls in one Run(), across documents too —
// see doc_engine.go's caller for how tokensUsedSoFar seeds it.
func (o *Orchestrator) ProcessDocumentWithBudget(
	ctx context.Context,
	repoRoot string,
	doc *config.DocSpec,
	dirtySectionIDs []string,
	graph *akg.CodePropertyGraph,
	dossier *config.GlobalCommitDossier,
	sm *storage.StateManager,
	commitHash string,
	budget *SectionBudget,
) (docUpdated bool, tokensUsed int, warnings []string, failedSections int, err error) {
	return o.processDocument(ctx, repoRoot, doc, dirtySectionIDs, graph, dossier, sm, commitHash, budget)
}

// sectionBudget is a shared, concurrency-safe token ceiling checked before
// each section's LLM render is dispatched (see renderOneSection's caller
// in processDocument) — both across the sections of one document and,
// when the same instance is threaded through multiple ProcessDocumentWith
// Budget calls, across the documents of one Run(). tryReserve is checked
// BEFORE spending anything, so it bounds the worst-case overshoot to
// roughly one round of in-flight concurrent sections past the limit,
// rather than the unbounded overshoot a check-only-between-documents (or
// check-only-between-sections-serially, which parallel rendering
// sidesteps entirely) scheme allows.
type SectionBudget struct {
	mu        sync.Mutex
	remaining int
}

// newSectionBudget returns nil (no enforcement) when maxTokens <= 0 — the
// caller's own "unbounded" signal — so every call site can pass the
// result straight through without a separate nil-vs-disabled branch.
func NewSectionBudget(maxTokens, alreadyUsed int) *SectionBudget {
	if maxTokens <= 0 {
		return nil
	}
	return &SectionBudget{remaining: maxTokens - alreadyUsed}
}

// tryReserve reports whether there is still budget to attempt a render. A
// nil budget always allows (enforcement disabled).
func (b *SectionBudget) tryReserve() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.remaining > 0
}

// spend records tokens actually used against the shared budget. A no-op
// on a nil budget.
func (b *SectionBudget) spend(tokens int) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.remaining -= tokens
}

// processDocument is ProcessDocument's real implementation, taking an
// optional shared token budget (nil disables enforcement — every existing
// caller of the exported ProcessDocument above, tests included, keeps
// working unbounded exactly as before). See sectionBudget's doc comment
// for why this exists.
func (o *Orchestrator) processDocument(
	ctx context.Context,
	repoRoot string,
	doc *config.DocSpec,
	dirtySectionIDs []string,
	graph *akg.CodePropertyGraph,
	dossier *config.GlobalCommitDossier,
	sm *storage.StateManager,
	commitHash string,
	budget *SectionBudget,
) (docUpdated bool, tokensUsed int, warnings []string, failedSections int, err error) {
	if doc == nil {
		return false, 0, nil, 0, fmt.Errorf("doc_engine/orchestrator: nil DocSpec")
	}

	// Apply archetype defaults if needed
	if err := ApplyArchetype(doc); err != nil {
		warnings = append(warnings, fmt.Sprintf("could not apply archetype %q: %v", doc.Archetype, err))
	}

	absTarget := filepath.Join(repoRoot, doc.TargetPath)

	// Read or scaffold target markdown
	rawBytes, readErr := os.ReadFile(absTarget)
	var rawContent string
	if readErr == nil {
		rawContent = string(rawBytes)
	} else {
		// Document file does not exist yet; scaffold initial skeleton
		rawContent = scaffoldDocSkeleton(doc)
	}

	parsedDoc := patcher.ParseMarkdown(rawContent)

	dirtySet := make(map[string]bool)
	for _, id := range dirtySectionIDs {
		dirtySet[id] = true
	}
	// If no specific dirty sections specified, consider all managed sections in doc
	if len(dirtySectionIDs) == 0 {
		for _, s := range doc.Sections {
			if s.Managed {
				dirtySet[s.ID] = true
			}
		}
	}

	// C1: learned project vocabulary, built ONCE per ProcessDocument call
	// (not per section). One capped WalkDir (2000 .go files, vendor /
	// node_modules / .git skipped); the map is read-only thereafter and
	// shared across the phase-1 render goroutines.
	vocab := verifier.LearnVocabulary(repoRoot, 1000)

	// Phase 0 (serial): snapshot one job per dirty managed section.
	// parsedDoc is only touched here and in phase 2 — never concurrently.
	var jobs []sectionJob
	for i := range doc.Sections {
		sec := &doc.Sections[i]
		if !sec.Managed || sec.Freeze {
			continue
		}
		if !dirtySet[sec.ID] {
			continue
		}

		// Check if zone exists in parsed doc
		zone := patcher.ManagedZone(parsedDoc, sec.ID)
		if zone != nil && zone.Kind == patcher.ZoneFrozen {
			continue
		}

		job := sectionJob{sec: *sec}
		if zone != nil {
			job.priorBody = patcher.ExtractBody(zone)
			job.directives = zone.Directives
			job.zoneContent = zone.Content
			job.hasZone = true
		}
		jobs = append(jobs, job)
	}

	// Phase 1 (concurrent render — gap C5): grounding+render+gates per
	// section, pure w.r.t. disk. A semaphore caps workers at
	// min(4, NumCPU); GMB_DOC_PARALLEL=0 restores serial rendering.
	// Per-section outcomes are collected by index; first-errors are NOT
	// fatal — a failed section records a warning and is skipped in the
	// merge phase. State-manager calls remain phase-2-only (serial).
	results := make([]sectionResult, len(jobs))
	renderJob := func(i int) sectionResult {
		if !budget.tryReserve() {
			return sectionResult{
				job:     jobs[i],
				skipped: true,
				secWarnings: []string{fmt.Sprintf("skipped rendering section %s/%s: token budget exhausted for this run",
					doc.ID, jobs[i].sec.ID)},
			}
		}
		res := o.renderOneSection(ctx, repoRoot, doc, jobs[i], graph, dossier, commitHash, vocab)
		budget.spend(res.secTokens)
		return res
	}
	if parallelSections() && len(jobs) > 1 {
		sem := make(chan struct{}, maxSectionWorkers(len(jobs)))
		var wg sync.WaitGroup
		for i := range jobs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				results[i] = renderJob(i)
			}(i)
		}
		wg.Wait()
	} else {
		for i := range jobs {
			results[i] = renderJob(i)
		}
	}

	docModified := false

	// Phase 2 (serial merge/write/state, in doc.Sections order): the
	// pre-existing merge path, fed from phase-1 outcomes. Warnings and
	// token counts fold in by index, so concurrent rendering stays
	// byte-identical to serial rendering (determinism gate).
	for i := range results {
		res := &results[i]
		secID := res.job.sec.ID

		// B2c ranked-map emission (Verbose only): top-5 files+scores for
		// this section, printed in section order for determinism.
		if o.opts.Verbose && len(res.rankedFiles) > 0 {
			top := res.rankedFiles
			if len(top) > 5 {
				top = top[:5]
			}
			fmt.Fprintf(o.opts.Out, "doc_engine: context map %s [%s]: %s\n",
				doc.ID, secID, strings.Join(top, ", "))
		}
		warnings = append(warnings, res.secWarnings...)
		tokensUsed += res.secTokens
		if res.skipped {
			// A section that reaches here failed to render at all (LLM
			// error, an unrecoverable quality-gate rejection, provider
			// outage) — real content is missing from the final document,
			// not just imperfect. RunResult.Err/Warnings are documented as
			// "never fatal" (doc_engine.go), so this can't ride on those
			// without changing that contract; callers that care whether a
			// run actually finished everything it set out to do need this
			// count instead of grepping warning strings.
			failedSections++
			continue
		}
		outcome := res.outcome
		priorBody := res.job.priorBody

		// Stage 8: 3-way merge. BASE is the last rendered body from state
		// (not priorBody): it records what the machine wrote last run, while
		// priorBody (OURS) holds current human edits. Falls back to priorBody
		// when no stored base exists (first run).
		base := priorBody
		if sm != nil {
			if st, lErr := sm.Load(); lErr == nil && st != nil {
				if ds, ok := st.Documents[doc.TargetPath]; ok && ds != nil {
					if ss, ok := ds.Sections[secID]; ok && ss != nil && ss.LastRenderedBody != "" {
						base = ss.LastRenderedBody
					}
				}
			}
		}
		mergeRes := patcher.MergeSection(base, priorBody, outcome.Content)
		if mergeRes.Conflicted {
			patcher.WarnConflict(o.opts.Out, doc.TargetPath, secID)
			// D6 review queue: conflicts are human-decision items, recorded
			// best-effort (never fail the run on queue errors).
			_ = queueConflictReview(repoRoot, doc.TargetPath, secID, base, priorBody, outcome.Content)
		}

		// Apply to in-memory parsed doc
		newFullText := patcher.ApplyToDoc(parsedDoc, secID, mergeRes.Content)
		parsedDoc = patcher.ParseMarkdown(newFullText)
		docModified = true

		// Update section hash in state manager. Recompute the exact Stage-3
		// hash (same inputs FindDirtySections compares) so the next run can
		// hit "clean → 0 tokens". Persisting "" here would defeat zero-churn.
		if sm != nil {
			astHash := invalidator.SectionHash(doc, &res.job.sec, graph)
			_ = storage.WriteSectionHash(sm, doc.TargetPath, secID, astHash, outcome.RenderMode, commitHash, outcome.TokensUsed, outcome.DurationMs)
			// Persist the merged body as the BASE for the next run's 3-way merge.
			_ = storage.SetLastRenderedBody(sm, doc.TargetPath, secID, mergeRes.Content)
		}
	}

	if !docModified {
		return false, tokensUsed, warnings, failedSections, nil
	}

	// Reconstruct and write file. absTarget is the filesystem path;
	// doc.TargetPath is the docs_state.json key (relative). They must not
	// be conflated or state lookups miss under a second document entry.
	finalMarkdown := patcher.Reconstruct(parsedDoc.Zones)
	writeRes, wErr := storage.WriteDoc(sm, absTarget, doc.TargetPath, []byte(finalMarkdown), doc.ID, "", commitHash, "managed")
	if wErr != nil {
		return false, tokensUsed, warnings, failedSections, fmt.Errorf("writing document %s: %w", doc.TargetPath, wErr)
	}

	return writeRes.Changed, tokensUsed, warnings, failedSections, nil
}

// sectionJob is one dirty section's phase-1 input snapshot. Captured
// serially (phase 0) so the concurrent render phase never touches parsedDoc.
type sectionJob struct {
	sec         config.SectionSpec // value copy; never aliases doc.Sections
	priorBody   string
	directives  map[string]string // read-only in phase 1 (zone directives)
	zoneContent string            // read-only in phase 1 (todo-marker scan)
	hasZone     bool
}

// sectionResult is one section's phase-1 render outcome. Merged serially in
// job order (phase 2), so warnings, token counts, and file bytes stay
// deterministic regardless of goroutine completion order.
type sectionResult struct {
	job         sectionJob
	skipped     bool // true: nothing to merge (fact-sheet nil / render failed)
	outcome     SectionRenderOutcome
	secTokens   int
	secWarnings []string
	rankedFiles []string // B2c: TakeRankedMap stash for verbose emission
}

// renderOneSection runs the phase-1 pipeline for a single section:
// grounding → render + firewall → post-render gates. Pure w.r.t. disk:
// no parsedDoc mutation, no state-manager calls (those are phase-2-only
// and serial). Safe for concurrent use across sections of one document:
// the only shared mutable state is the orchestrator's mutex-guarded
// counters and the locked Out writer, plus read-only graph/dossier/doc
// access. vocab (C1) is built once per ProcessDocument and read-only here.
func (o *Orchestrator) renderOneSection(
	ctx context.Context,
	repoRoot string,
	doc *config.DocSpec,
	job sectionJob,
	graph *akg.CodePropertyGraph,
	dossier *config.GlobalCommitDossier,
	commitHash string,
	vocab map[string]bool,
) sectionResult {
	sec := &job.sec
	var res sectionResult
	res.job = job

	// Stage 4: Grounding
	fs := grounding.AssembleFactSheet(doc, sec, graph, dossier, job.priorBody, repoRoot)
	if fs == nil {
		res.skipped = true
		return res
	}
	// B2c: stash the ranked file map immediately (single-flight take from
	// the keyed store); the serial merge phase prints it when Verbose.
	if ranked, ok := grounding.TakeRankedMap(doc.TargetPath, sec.ID); ok {
		res.rankedFiles = ranked
	}
	fs.CommitHash = commitHash
	fs.DocPurpose = doc.Purpose
	fs.DocAudience = doc.Audience
	// Global style is the fallback; per-doc style wins on non-empty fields.
	fs.Style = o.opts.GlobalStyle
	if doc.Style != nil {
		if doc.Style.Standard != "" {
			fs.Style.Standard = doc.Style.Standard
		}
		if doc.Style.Voice != "" {
			fs.Style.Voice = doc.Style.Voice
		}
		if doc.Style.Tone != "" {
			fs.Style.Tone = doc.Style.Tone
		}
		if doc.Style.CodeBlockFormat != "" {
			fs.Style.CodeBlockFormat = doc.Style.CodeBlockFormat
		}
		if len(doc.Style.JargonBlacklist) > 0 {
			fs.Style.JargonBlacklist = doc.Style.JargonBlacklist
		}
	}
	// Thread the section's MaxWords cap through to the system prompt.
	fs.MaxWords = sec.MaxWords
	if dossier != nil {
		fs.CommitIntent = dossier.CommitIntent
		fs.CommitReason = dossier.CommitReason
	}
	if len(job.directives) > 0 {
		if inst, ok := job.directives["instruction"]; ok && inst != "" {
			fs.SectionInstruction = inst
		}
	}
	// D2: Diátaxis quadrant guidance. The archetype's quadrant shapes
	// the prose contract (reference/how-to/tutorial/explanation) without
	// touching user content — prompt payload only, via PromptGuidance
	// (never SectionInstruction, which Track B renders verbatim to the
	// reader — see PromptGuidance's doc comment for why that distinction
	// matters).
	if q := ArchetypeQuadrant(doc.Archetype); q != "" {
		if qp := QuadrantPrompt(q); qp != "" {
			fs.PromptGuidance = "Documentation quadrant (" + q + "): " + qp
		}
	}

	// Stage 6 & 7: Render + Quality Firewall. Default RenderMode from the
	// orchestrator decision so the contract field is never decorative:
	// deterministic when no LLM is available, llm otherwise.
	if fs.RenderMode == "" {
		if o.actuator != nil && !o.opts.NoLLM {
			fs.RenderMode = "llm"
		} else {
			fs.RenderMode = "deterministic"
		}
	}
	outcome, rErr := o.RenderSection(ctx, fs, graph)
	if rErr != nil {
		res.skipped = true
		res.secWarnings = append(res.secWarnings, fmt.Sprintf("failed rendering section %s/%s: %v", doc.ID, sec.ID, rErr))
		return res
	}
	res.secTokens += outcome.TokensUsed
	if outcome.Warning != "" {
		res.secWarnings = append(res.secWarnings, outcome.Warning)
	}
	outcome.Content = o.enforceMaxWords(ctx, fs, sec, graph, outcome, &res.secWarnings)

	// P9: living diagram directive — best-effort. After a managed
	// section renders, if the zone directives contain `diagram`
	// (parsed via patcher.ProcessDirectives), generate the diagram
	// markdown via the grounding diagram path and inject it into the
	// section body before merge. On error, warning only.
	if job.hasZone && len(job.directives) > 0 {
		pd := patcher.ProcessDirectives(job.directives)
		if pd.DiagramType != "" {
			ref := config.DiagramRef{Type: pd.DiagramType, Scope: pd.DiagramScope}
			if (strings.EqualFold(ref.Type, "callgraph") || strings.EqualFold(ref.Type, "sequence")) && ref.Entry == "" && len(doc.Scope.EntryPoints) > 0 {
				ref.Entry = doc.Scope.EntryPoints[0]
			}
			if diag, dErr := grounding.GenerateDiagram(ref, graph); dErr != nil {
				res.secWarnings = append(res.secWarnings, fmt.Sprintf("diagram directive %q for section %s/%s failed: %v", pd.DiagramType, doc.ID, sec.ID, dErr))
			} else if strings.TrimSpace(diag) != "" {
				outcome.Content = strings.TrimRight(outcome.Content, "\n") + "\n\n" + strings.TrimSpace(diag) + "\n"
			}
		}
	}

	// P9: todo population — best-effort. When the managed zone contains
	// a `gmb:todo:` marker, append a deterministic draft line derived
	// from grounding via patcher.PopulateTodos.
	if job.hasZone && strings.Contains(job.zoneContent, "gmb:todo:") {
		outcome.Content = patcher.PopulateTodos(outcome.Content, exportedShortNames(fs))
	}

	// Gate 6: prose quality (plan C1, both tracks, with learned project
	// vocabulary so project terms never flag the typo rule). Strict mode
	// fails; non-strict only reports. On strict LLM failure: one repair
	// retry, then deterministic fallback (mirrors the Stage 7 ladder).
	// On strict deterministic failure: warn + ship (tables cannot be
	// re-voiced; failing would discard grounded facts).
	prosePass, proseFailures := verifier.CheckProseGateWithVocab(outcome.Content, fs.Style, fs.Style.StrictProse, vocab)
	if !prosePass {
		proseErr := fmt.Errorf("prose quality gate failed: %s", strings.Join(proseFailures, "; "))
		if outcome.RenderMode == "llm" && o.actuator != nil && !outcome.RepairUsed {
			repairResp, repairErr := o.actuator.Repair(ctx, fs, outcome.Content, proseErr)
			if repairErr == nil {
				symIndex := buildSymbolIndex(fs, graph)
				if rg := verifier.RunGates(fs.PriorSectionMarkdown, repairResp.Text, symIndex); rg.Pass {
					if rp, rf := verifier.CheckProseGateWithVocab(repairResp.Text, fs.Style, true, vocab); rp {
						outcome.Content = repairResp.Text
						outcome.RepairUsed = true
						outcome.TokensUsed += repairResp.TotalTokens
						res.secTokens += repairResp.TotalTokens
						o.countRepair()
						res.secWarnings = append(res.secWarnings, fmt.Sprintf("section %s/%s prose repaired to strict style", doc.ID, sec.ID))
						goto gate7
					} else {
						_ = rf
					}
				}
			}
			detOutcome, detErr := o.renderTrackB(fs, buildSymbolIndex(fs, graph))
			if detErr != nil {
				res.skipped = true
				res.secWarnings = append(res.secWarnings, fmt.Sprintf("section %s/%s strict prose failed (%v); skipping update", doc.ID, sec.ID, proseErr))
				return res
			}
			detOutcome.FallbackUsed = true
			o.countFallback()
			detOutcome.Warning = fmt.Sprintf("strict prose failed on LLM output; deterministic fallback used (%v)", proseErr)
			outcome = detOutcome
			res.secTokens += outcome.TokensUsed
		} else {
			res.secWarnings = append(res.secWarnings, fmt.Sprintf("section %s/%s prose quality: %s", doc.ID, sec.ID, strings.Join(proseFailures, "; ")))
		}
	} else if len(proseFailures) > 0 {
		res.secWarnings = append(res.secWarnings, fmt.Sprintf("section %s/%s prose suggestions: %s", doc.ID, sec.ID, strings.Join(proseFailures, "; ")))
	}

gate7:
	// Gate 7: reference integrity (plan C2). Engine-side it reports
	// warnings only — hard failures belong to `doc check` (CI), where
	// missing files and bad anchors fail the gate without blocking
	// generation here.
	{
		var symFn func(string) bool
		if symIndex := buildSymbolIndex(fs, graph); symIndex != nil {
			symFn = func(s string) bool { return symIndex.HasSymbol(s) }
		}
		refRep := verifier.CheckReferences(repoRoot, doc.TargetPath, outcome.Content, symFn, false)
		for _, br := range refRep.Broken {
			res.secWarnings = append(res.secWarnings, fmt.Sprintf("section %s/%s reference integrity: %s (line %d: %s)", doc.ID, sec.ID, br.Reason, br.Line, br.Target))
		}
	}

	res.outcome = outcome
	return res
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

func scaffoldDocSkeleton(doc *config.DocSpec) string {
	var sb strings.Builder
	title := doc.Title
	if title == "" {
		title = doc.ID
	}
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))
	if doc.Purpose != "" {
		sb.WriteString(fmt.Sprintf("%s\n\n", doc.Purpose))
	}

	for _, sec := range doc.Sections {
		sb.WriteString(fmt.Sprintf("## %s\n\n", sec.Title))
		if sec.Managed {
			sb.WriteString(patcher.BuildBeginMarker(sec.ID) + "\n")
			sb.WriteString(patcher.BuildEndMarker(sec.ID) + "\n\n")
		}
	}
	return sb.String()
}

type memorySymbolIndex struct {
	symbols map[string]bool
}

func (m *memorySymbolIndex) HasSymbol(id string) bool {
	if m.symbols[id] {
		return true
	}
	// Also test short name after "::" or "."
	parts := strings.Split(id, "::")
	if len(parts) > 1 && m.symbols[parts[len(parts)-1]] {
		return true
	}
	subparts := strings.Split(id, ".")
	if len(subparts) > 1 && m.symbols[subparts[len(subparts)-1]] {
		return true
	}
	return false
}

// exportedShortNames returns the sorted, deduplicated short names of exported
// symbols known to the FactSheet (used for P9 todo population).
func exportedShortNames(fs *config.FactSheet) []string {
	if fs == nil {
		return nil
	}
	seen := make(map[string]bool)
	var names []string
	add := func(fqn string) {
		short := fqn
		if idx := strings.LastIndex(short, "::"); idx >= 0 {
			short = short[idx+2:]
		} else if idx := strings.LastIndex(short, "."); idx >= 0 {
			short = short[idx+1:]
		}
		short = strings.TrimSpace(short)
		if short == "" || seen[short] {
			return
		}
		if r := []rune(short); len(r) == 0 || !unicode.IsUpper(r[0]) {
			return
		}
		seen[short] = true
		names = append(names, short)
	}
	for _, s := range fs.GroundTruth.Symbols {
		add(s.FQN)
	}
	for _, s := range fs.GroundTruth.AddedSymbols {
		add(s.FQN)
	}
	sort.Strings(names)
	return names
}

func buildSymbolIndex(fs *config.FactSheet, graph *akg.CodePropertyGraph) verifier.AKGSymbolIndex {
	m := make(map[string]bool)

	addSym := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		m[s] = true
		parts := strings.Split(s, "::")
		if len(parts) > 1 {
			m[parts[len(parts)-1]] = true
		}
		subparts := strings.Split(s, ".")
		if len(subparts) > 1 {
			m[subparts[len(subparts)-1]] = true
		}
	}

	if fs != nil {
		for _, s := range fs.GroundTruth.Symbols {
			addSym(s.FQN)
		}
		for _, s := range fs.GroundTruth.AllSymbols {
			addSym(s.FQN)
		}
		for _, s := range fs.GroundTruth.AddedSymbols {
			addSym(s.FQN)
		}
		for _, s := range fs.GroundTruth.ModifiedSymbols {
			addSym(s.FQN)
		}
		for _, s := range fs.GroundTruth.Sentinels {
			addSym(s.FQN)
		}
		for _, s := range fs.GroundTruth.AddedSentinels {
			addSym(s.FQN)
		}
		for _, s := range fs.GroundTruth.ConfigVars {
			addSym(s.Name)
		}
		for _, cf := range fs.GroundTruth.CallFlow {
			addSym(cf)
		}
		for _, c := range fs.GroundTruth.Callers {
			addSym(c)
		}
	}

	if graph != nil && graph.Nodes != nil {
		graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
			addSym(id)
			if n != nil && n.Name != "" {
				addSym(n.Name)
			}
		})
	}

	return &memorySymbolIndex{symbols: m}
}
