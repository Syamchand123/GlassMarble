// Package doc_engine is the public facade for the GlassMarble Documentation
// Intelligence Engine (v1.2.0).
//
// Architecture: 95% deterministic Go core, 5% LLM prose actuator.
// The engine is non-fatal by design — all errors are logged as warnings,
// and gmb analyze / git commit is NEVER blocked by documentation failures.
//
// Pipeline (8 stages):
//
//	Trigger → AKG Diff → Invalidation → Grounding → MD Parse → Render → Quality Firewall → MVCC Write
//
// Usage:
//
//	// After committing the AKG in runAnalysis:
//	docResult := doc_engine.Run(storageDir, tm, commitHash, verbose)
//	if docResult.Err != nil {
//	    log.Printf("warning: doc engine: %v", docResult.Err)
//	}
package doc_engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/aiconfig"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/commit_reasoning"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/archfeatures"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/catalog"
	docconfig "github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/grounding"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/invalidator"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/ledger"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/patcher"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/renderer"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/review"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/verifier"
	"github.com/Syamchand123/GlassMarble/internal/git"
)

// ────────────────────────────────────────────────────────────────────────────
// RunOptions — controls a single Run() execution
// ────────────────────────────────────────────────────────────────────────────

// RunOptions configures a single documentation engine run.
type RunOptions struct {
	// CommitHash is the HEAD commit hash. Required.
	CommitHash string

	// Verbose enables detailed per-section output.
	Verbose bool

	// NoLLM forces the deterministic renderer (Track B) for all sections.
	// Useful for offline / air-gapped environments.
	NoLLM bool

	// Provider is the optional LLM provider for Track A prose rendering.
	Provider provider.Provider

	// Model is the model name for Track A. If empty, falls back to AI config.
	Model string

	// MaxOutputTokens caps Track A's completion length. If <= 0, falls back
	// to AI config (ai.yaml's max_output_tokens), then to
	// DefaultLLMActuatorConfig's built-in default (300) if that's also unset.
	MaxOutputTokens int

	// Temperature sets Track A's sampling temperature. If nil, falls back
	// to AI config, then to DefaultLLMActuatorConfig's built-in default (0.0).
	Temperature *float64

	// Force bypasses the fast-bail check and reprocesses all documents.
	Force bool

	// DocID, if non-empty, restricts processing to the document with this ID.
	DocID string

	// Tag, if non-empty, restricts processing to documents tagged with this value.
	Tag string

	// BranchPolicy controls when the engine writes output on non-main branches.
	// "main-only" (default), "any", "tag-only"
	BranchPolicy string

	// ForceWrite overrides BranchPolicy (except on draft/wip branches, which
	// are always compute-only). Set via `gmb doc --write` (Pillar 5 / F9).
	ForceWrite bool

	// HeadGraph is the active CodePropertyGraph. If nil, graph is loaded if needed.
	HeadGraph *akg.CodePropertyGraph

	// BaseGraph is the previous commit's CodePropertyGraph for diffing.
	BaseGraph *akg.CodePropertyGraph

	// Out is the writer for human-readable progress output. Defaults to os.Stderr.
	Out io.Writer

	// Ctx, when non-nil, governs cancellation of this run: it is passed to
	// the LLM actuator (so an in-flight completion call aborts promptly on
	// cancellation) and checked between documents/sections so a cancelled
	// run stops picking up new work rather than running to completion.
	// Defaults to context.Background() (never cancelled) when nil, so
	// existing callers that don't set it keep today's behavior exactly.
	// The daemon (cmd/docserve.go) sets this from its own shutdown context
	// so a `gmb docserve` SIGINT/SIGTERM can actually interrupt an in-flight
	// run instead of blocking shutdown until it finishes on its own.
	Ctx context.Context
}

// RunResult is the aggregate outcome of a Run() execution.
type RunResult struct {
	// Commit is the HEAD commit hash that was processed.
	Commit string

	// DocsUpdated is the count of document files that were changed on disk.
	DocsUpdated int

	// SectionsProcessed is the total sections examined (including skipped).
	SectionsProcessed int

	// SectionsUpdated is the count of sections that were actually re-rendered.
	SectionsUpdated int

	// DirtySections contains the prioritized dirty sections identified during invalidation.
	DirtySections []docconfig.DirtySectionRef

	// TokensUsed is the total LLM token count for this run.
	TokensUsed int

	// Duration is the total wall-clock time for the run.
	Duration time.Duration

	// Err is the first critical error, if any. Non-nil does NOT mean the
	// caller should return an error — the engine is non-fatal.
	Err error

	// Warnings are non-fatal issues encountered during the run.
	Warnings []string
}

// ────────────────────────────────────────────────────────────────────────────
// CheckOptions — controls a Check() CI audit execution
// ────────────────────────────────────────────────────────────────────────────

// CheckOptions configures a non-modifying freshness / drift audit.
type CheckOptions struct {
	// Verbose enables detailed per-section output.
	Verbose bool

	// JSON emits machine-readable JSON output.
	JSON bool

	// DocID restricts the check to a single document.
	DocID string

	// Tag restricts the check to documents with this tag.
	Tag string

	// HeadGraph is the active CodePropertyGraph used to evaluate gmb:assert
	// doc-lint rules. If nil, assert evaluation is skipped (never failed
	// blindly). Optional.
	HeadGraph *akg.CodePropertyGraph

	// Out is the writer for output. Defaults to os.Stdout.
	Out io.Writer
}

// CheckResult is the outcome of a Check() audit.
type CheckResult struct {
	// AllFresh is true if all documents are at or above their freshness thresholds.
	AllFresh bool

	// Documents contains per-document freshness results.
	Documents []DocumentCheckResult

	// GlobalFreshness is the weighted average freshness across all documents (0-100).
	GlobalFreshness int

	// Warnings contains documents at or below MinFreshnessThreshold.
	Warnings []string

	// Failures contains documents at or below MinFreshnessFail.
	Failures []string
}

// DocumentCheckResult is the freshness audit result for a single document.
type DocumentCheckResult struct {
	ID           string `json:"id"`
	TargetPath   string `json:"target_path"`
	Freshness    int    `json:"freshness"`
	LastUpdated  string `json:"last_updated"`
	CommitsBehind int   `json:"commits_behind"`
	Status       string `json:"status"` // "fresh" | "warn" | "stale" | "missing"
}

// ────────────────────────────────────────────────────────────────────────────
// InitOptions — controls a gmb doc init scaffold run
// ────────────────────────────────────────────────────────────────────────────

// InitOptions configures a gmb doc init scaffold operation.
type InitOptions struct {
	// TargetPath is the .md file to create (relative to repoRoot).
	TargetPath string

	// Archetype is the built-in document template to use.
	Archetype string

	// ScopePaths are the glob patterns the document will track.
	ScopePaths []string

	// EntryPoints are FQNs ("path/to/file.go::Symbol") that seed call-graph
	// and sequence diagrams and PageRank-based context selection. Without
	// at least one, every callgraph/sequence diagram configured by the
	// chosen archetype renders as an empty "No call graph edges detected"
	// placeholder forever — there is no other way to set this after
	// scaffolding except hand-editing docs.yaml's entry_points key.
	EntryPoints []string

	// Title is the document title.
	Title string

	// Purpose is a one-sentence description of the document.
	Purpose string

	// Audience is the intended reader.
	Audience string

	// DocID is the stable identifier. If empty, derived from TargetPath.
	DocID string

	// Interactive enables the Charm Huh interactive questionnaire.
	// When false, all required fields must be provided in InitOptions.
	Interactive bool

	// Out is the writer for scaffold output. Defaults to os.Stdout.
	Out io.Writer
}

// ────────────────────────────────────────────────────────────────────────────
// Public API
// ────────────────────────────────────────────────────────────────────────────

// Run executes the documentation update pipeline for the given repository.
// It is non-fatal: all errors are captured in RunResult.Err and the caller
// should log them as warnings rather than propagating them.
//
// Integration point: call this after runLearning() and before runAging()
// in cmd/analyze.go — following the same non-fatal pattern as runMemoryPipeline.
func Run(repoRoot string, opts RunOptions) RunResult {
	start := time.Now()
	out := opts.Out
	if out == nil {
		out = os.Stderr
	}

	result := RunResult{Commit: opts.CommitHash}

	// Hook/daemon callers often invoke without --commit (notably git hooks,
	// where GIT_DIR is set and no commit flag is passed). Resolve HEAD so
	// the run processes a real commit instead of "commit (empty)".
	if opts.CommitHash == "" {
		if head, err := gitHeadCommit(repoRoot); err == nil && head != "" {
			opts.CommitHash = head
			result.Commit = head
		} else if opts.Verbose {
			fmt.Fprintln(out, "doc_engine: could not resolve HEAD commit, proceeding without commit context")
		}
	}

	// Load docs.yaml.
	cfg, err := docconfig.LoadDocsConfig(repoRoot)
	if err != nil {
		result.Err = fmt.Errorf("doc_engine: %w", err)
		return result
	}

	if len(cfg.Documents) == 0 {
		// No documents configured — nothing to do.
		if opts.Verbose {
			fmt.Fprintln(out, "doc_engine: no documents configured in .glassmarble/docs.yaml")
		}
		result.Duration = time.Since(start)
		return result
	}

	// P5 branch policy (master plan Pillar 5 + F9): decide up-front whether
	// this run may write files and state.
	canWrite, _, branchWarnings := resolveWritePermission(repoRoot, opts.BranchPolicy, opts.ForceWrite)
	result.Warnings = append(result.Warnings, branchWarnings...)

	// Load current state.
	storageDir := docconfig.StorageDirPath(repoRoot)
	sm := storage.NewStateManager(storageDir)
	state, err := sm.Load()
	if err != nil {
		result.Err = fmt.Errorf("doc_engine: loading state: %w", err)
		return result
	}

	// Filter documents by --doc and --tag flags. This validation must run
	// BEFORE the fast-bail check below: an invalid --doc/--tag filter is a
	// user input error that must surface every time, not just on the first
	// call for a given commit — otherwise a bad --doc id silently succeeds
	// (fast-bails) whenever HEAD happens to already be state.LastCommit.
	docs := filterDocuments(cfg.Documents, opts.DocID, opts.Tag)
	if len(docs) == 0 {
		if opts.DocID != "" {
			result.Err = fmt.Errorf("doc_engine: no document with id %q found in docs.yaml", opts.DocID)
		}
		result.Duration = time.Since(start)
		return result
	}

	// Fast-bail: if we've already processed this commit and --force is not set,
	// skip the entire run.
	if !opts.Force && state.LastCommit == opts.CommitHash && opts.CommitHash != "" {
		if opts.Verbose {
			fmt.Fprintf(out, "doc_engine: already processed commit %s, skipping (use --force to override)\n", opts.CommitHash[:8])
		}
		result.Duration = time.Since(start)
		return result
	}

	// ── Phase 1: Catalog & Invalidation Engine ────────────────────────────
	// In-memory catalog cache (gap C5, honest scope): the catalog rebuild
	// from docs.yaml is cheap, but reusing the index across in-process Run
	// calls helps daemon/serve + tests. Keyed by docs.yaml content hash +
	// doc/tag filter; any config change misses and rebuilds. The expensive
	// part (AKG load) is outside doc_engine control — cross-process AKG
	// caching belongs to the analyze/daemon layers, not here.
	cat := cachedCatalogForDocs(docs, docsConfigCacheKey(repoRoot, opts.DocID, opts.Tag))

	// Stage 1: Fast-bail evaluator (< 15ms latency target)
	bail, changedFiles, bailReason, _ := invalidator.FastBail(repoRoot, opts.CommitHash, cat, state, opts.Force)
	if bail {
		if opts.Verbose {
			fmt.Fprintf(out, "doc_engine: fast-bail: %s (< 15ms exit)\n", bailReason)
		}
		result.Duration = time.Since(start)
		return result
	}

	// P7 sweep: heal stale permalinks across ALL managed zones (non-fatal).
	// Skipped in compute-only mode — healing writes files.
	if canWrite {
		if healed, healErr := grounding.HealAllManagedDocs(repoRoot, docs); healErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("permalink heal sweep failed: %v", healErr))
		} else if opts.Verbose && healed > 0 {
			fmt.Fprintf(out, "doc_engine: healed %d stale permalink(s)\n", healed)
		}
	}

	if opts.Verbose || !isQuiet(out) {
		fmt.Fprintf(out, "doc_engine: processing %d document(s) for commit %s (%d files changed)\n",
			len(docs), shortHash(opts.CommitHash), len(changedFiles))
	}

	// Stage 2: Build GlobalCommitDossier
	dossier, err := invalidator.BuildDossier(repoRoot, opts.CommitHash, opts.BaseGraph, opts.HeadGraph)
	if err != nil && opts.Verbose {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not build commit dossier: %v", err))
	}

	// Stage 3: Invalidation & Dirty Section Discovery
	inv := invalidator.New(cat)
	dirtySections, err := inv.FindDirtySections(dossier, state, opts.HeadGraph, &cfg.Constraints)
	if err != nil && opts.Verbose {
		result.Warnings = append(result.Warnings, fmt.Sprintf("invalidation error: %v", err))
	}

	result.DirtySections = dirtySections
	result.SectionsProcessed = countTotalSections(docs)
	result.SectionsUpdated = len(dirtySections)

	// The dossier is the single shared source: record the prioritized
	// dirty list on it so downstream stages draw from one dossier.
	if dossier != nil {
		dossier.DirtySections = dirtySections
	}

	// Comment-only / whitespace commits carry zero symbol, config, sentinel,
	// or architectural changes. The ground truth is empty, so there is
	// nothing to render — mark processed and exit without LLM calls. This
	// wins over a non-empty dirtySections from FindDirtySections's coarse
	// "no candidates -> treat every section as a candidate" fallback path
	// (see TestExtraRunCommentOnlyBail): such sections are picked up on the
	// next commit that carries a real dossier change instead.
	if dossier != nil && !opts.Force && dossierChangeCount(dossier) == 0 {
		if opts.Verbose || !isQuiet(out) {
			fmt.Fprintln(out, "doc_engine: no code changes in dossier (comments/whitespace only), skipping render")
		}
		// Never persist an empty hash here either: it would clobber a good
		// LastCommit and defeat the already-processed fast-bail for every
		// future run (same hazard the later save in this function guards).
		// Also never persist it for a --doc/--tag filtered run: LastCommit
		// gates the top-level "whole commit already processed" fast-bail,
		// and this run only looked at a subset of documents — recording the
		// commit here would make a later unfiltered run for the same commit
		// fast-bail and silently skip every document it didn't touch.
		//
		// Uses sm.Update rather than mutating the `state` loaded at the top
		// of Run() and saving it directly: Update re-loads a fresh snapshot
		// under the shared state-transaction lock immediately before saving,
		// so a concurrent process's WriteDoc/WriteSectionHash committed
		// between our earlier Load and now can never be silently clobbered
		// by a save of our now-stale in-memory copy.
		if opts.CommitHash != "" && opts.DocID == "" && opts.Tag == "" {
			if saveErr := sm.Update(func(fresh *storage.DocEngineState) error {
				fresh.LastCommit = opts.CommitHash
				return nil
			}); saveErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("could not save docs_state.json: %v", saveErr))
			}
		} else if opts.CommitHash == "" {
			result.Warnings = append(result.Warnings, "doc_engine: skipping state commit-hash update (unknown commit)")
		}
		result.Duration = time.Since(start)
		return result
	}

	if opts.Verbose || !isQuiet(out) {
		fmt.Fprintf(out, "doc_engine: found %d dirty section(s) across %d document(s)\n",
			len(dirtySections), len(docs))
	}

	// P5 compute-only mode: dossier + invalidation already ran above, so
	// report what WOULD change without touching files or state.
	if !canWrite {
		for _, ds := range dirtySections {
			fmt.Fprintf(out, "doc_engine: would update %s [%s]: %s\n", ds.DocPath, ds.SectionID, ds.Reason)
		}
		if len(dirtySections) == 0 && (opts.Verbose || !isQuiet(out)) {
			fmt.Fprintln(out, "doc_engine: compute-only mode: no sections would change")
		}
		result.Duration = time.Since(start)
		return result
	}

	// ── Stages 4-8: Grounding, Render, Firewall, Write (Phases 2-4) ───────
	// Resolve AI provider if not forced to deterministic mode
	if opts.Provider == nil && !opts.NoLLM {
		if aiCfg, aiErr := aiconfig.LoadForDir(repoRoot, aiconfig.Config{}); aiErr == nil && aiCfg != nil {
			if eng, eErr := ai_engine.New(aiCfg, repoRoot); eErr == nil && eng != nil {
				opts.Provider = eng.Provider
				if opts.Model == "" {
					opts.Model = aiCfg.Model
				}
				// Without this, Track A always rendered with
				// DefaultLLMActuatorConfig's hardcoded 300-token budget and
				// 0.0 temperature, silently ignoring whatever the user
				// configured in ai.yaml (commonly 8192+ tokens) — a budget
				// that small is routinely exhausted by a reasoning model's
				// own chain-of-thought before it ever reaches the final
				// answer, so Track A shipped truncated reasoning transcripts
				// as if they were the section's content.
				if opts.MaxOutputTokens <= 0 {
					opts.MaxOutputTokens = aiCfg.MaxOutputTokens
				}
				if opts.Temperature == nil {
					opts.Temperature = aiconfig.EffectiveTemperature(aiCfg)
				}
			}
		}
	}

	orch := renderer.NewOrchestrator(renderer.OrchestratorOptions{
		NoLLM:           opts.NoLLM,
		Model:           opts.Model,
		Provider:        opts.Provider,
		Verbose:         opts.Verbose,
		Out:             out,
		GlobalStyle:     cfg.Style,
		MaxOutputTokens: opts.MaxOutputTokens,
		Temperature:     opts.Temperature,
	})

	// Index dirty sections by document ID
	dirtyByDoc := make(map[string][]string)
	for _, ds := range dirtySections {
		dirtyByDoc[ds.DocID] = append(dirtyByDoc[ds.DocID], ds.SectionID)
	}

	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	docsUpdatedCount := 0
	totalTokens := 0

	// F6/F10 token budget: hard ceiling on total LLM tokens for one run.
	maxTokens := cfg.Constraints.MaxTokensPerRun
	if maxTokens <= 0 {
		maxTokens = 100000
	}

	for i := range docs {
		d := &docs[i]
		secIDs := dirtyByDoc[d.ID]
		// A document configured in docs.yaml whose target file was never
		// scaffolded (deleted by hand, or a docs.yaml entry added without
		// ever running `gmb doc init`) has nothing for the invalidator to
		// diff against, so it never appears in dirtySections either — this
		// document was silently skipped below with no warning at all
		// (Force doesn't help either: ProcessDocument gets an empty
		// secIDs and has nothing to touch), unlike `gmb doc check` (which
		// reports "FAIL: file missing" clearly). A user watching only
		// `gmb doc`'s own output had no way to tell "nothing changed"
		// apart from "this document was never scaffolded."
		if _, statErr := os.Stat(filepath.Join(repoRoot, d.TargetPath)); os.IsNotExist(statErr) {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("doc_engine: %s: target file does not exist — run `gmb doc init %s` (or restore the file) before it can be generated; skipping", d.TargetPath, d.TargetPath))
		}
		if opts.Force || len(secIDs) > 0 {
			// Cancellation checkpoint: stop picking up NEW documents once the
			// caller cancels (e.g. `gmb docserve` shutting down). An
			// in-flight ProcessDocument call for the CURRENT document still
			// gets to observe ctx.Done() on its own — this only stops the
			// loop from starting another one after it.
			if ctx.Err() != nil {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("doc_engine: run cancelled, skipping remaining document(s): %v", ctx.Err()))
				break
			}
			// F6/F10: once the budget is exhausted, skip ALL remaining docs.
			if totalTokens >= maxTokens {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("token budget exhausted (%d/%d tokens used): skipping remaining document(s)", totalTokens, maxTokens))
				break
			}
			changed, tokens, docWarns, pErr := orch.ProcessDocument(
				ctx,
				repoRoot,
				d,
				secIDs,
				opts.HeadGraph,
				dossier,
				sm,
				opts.CommitHash,
			)
			if pErr != nil && opts.Verbose {
				result.Warnings = append(result.Warnings, fmt.Sprintf("doc %s error: %v", d.ID, pErr))
			}
			result.Warnings = append(result.Warnings, docWarns...)
			totalTokens += tokens
			if changed {
				docsUpdatedCount++
			}
		}
	}

	result.DocsUpdated = docsUpdatedCount
	result.TokensUsed = totalTokens

	// P33 living error catalog: regenerate with CPG caller resolution when
	// a real graph is available (analyze path). Parser-only runs skip it to
	// avoid noisy heuristic output. Non-fatal by design.
	if canWrite && opts.HeadGraph != nil {
		if _, errCat := GenerateErrorCatalogWithGraph(repoRoot, opts.HeadGraph); errCat != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("error catalog regeneration failed: %v", errCat))
		} else if opts.Verbose {
			fmt.Fprintln(out, "doc_engine: regenerated living error catalog (docs/errors.md)")
		}
	}
	freshSum := 0

	// P8 freshness (master-plan Appendix B): refresh per-document scores.
	// Reload state first — ProcessDocument performs its own Load/Save cycles,
	// so the in-memory snapshot from the top of Run() is stale.
	//
	// Phase 1 (unlocked): compute each document's freshness score. This runs
	// git subprocesses per document (ComputeFreshnessScoreWithArchEvents), so
	// it deliberately happens OUTSIDE the state-transaction lock acquired
	// below — holding that lock across potentially many git calls would
	// serialize every other concurrent gmb doc/daemon/CI process's state
	// updates behind this run's freshness computation for no correctness
	// benefit (LastUpdatedCommit, the only input read from state here, is
	// not expected to change concurrently for these documents at this point
	// in Run()).
	freshState, loadErr := sm.Load()
	if loadErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not reload state for freshness update: %v", loadErr))
		freshState = state
	}
	type freshnessResult struct {
		targetPath     string
		score, behind int
	}
	freshResults := make([]freshnessResult, 0, len(docs))
	for i := range docs {
		ds := storage.GetOrCreateDocState(freshState, docs[i].TargetPath)
		// B3: thread the run dossier's structural arch events into decay.
		var runArchEvents []string
		if dossier != nil {
			runArchEvents = dossier.ArchEvents
		}
		freshScore, behind := ComputeFreshnessScoreWithArchEvents(repoRoot, docs[i], ds.LastUpdatedCommit, runArchEvents)
		freshSum += freshScore
		freshResults = append(freshResults, freshnessResult{docs[i].TargetPath, freshScore, behind})
	}

	// Phase 2 (locked): apply the computed scores, plus the commit-hash
	// update, atomically against whatever is CURRENTLY persisted — not the
	// freshState snapshot read above, which a concurrent WriteDoc/
	// WriteSectionHash/SetLastRenderedBody could have already moved past by
	// now. sm.Update re-loads fresh state under the shared state-transaction
	// lock immediately before saving, so this can never silently clobber
	// another process's update the way saving a stale in-memory copy could.
	//
	// Never persist an empty commit hash: it would clobber a good LastCommit
	// and defeat the already-processed fast-bail for every future run. Also
	// never persist it for a --doc/--tag filtered run (see matching comment
	// above): LastCommit gates the top-level "whole commit already
	// processed" fast-bail, and a filtered run never looked at every
	// document, so recording the commit here would make a later unfiltered
	// run for the same commit skip documents this run never touched.
	if opts.CommitHash == "" {
		result.Warnings = append(result.Warnings, "doc_engine: skipping state commit-hash update (unknown commit)")
	}
	updateErr := sm.Update(func(fresh *storage.DocEngineState) error {
		for _, r := range freshResults {
			ds := storage.GetOrCreateDocState(fresh, r.targetPath)
			ds.FreshnessScore = r.score
			ds.CommitsBehind = r.behind
		}
		if opts.CommitHash != "" && opts.DocID == "" && opts.Tag == "" {
			fresh.LastCommit = opts.CommitHash
		}
		return nil
	})
	if updateErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not save docs_state.json: %v", updateErr))
	}

	// P16 auto-trigger: autonomous ADR generation (non-fatal, warnings only).
	// B3: structural dossier events feed generation alongside commit-message
	// keywords, so AKG-detected changes (splits, cycles, layer violations)
	// scaffold ADRs even when the commit message is terse.
	commitSubject := gitCommitSubject(repoRoot, opts.CommitHash)
	var dossierEvents []archfeatures.ADREvent
	if dossier != nil {
		dossierEvents = archfeatures.EventsFromDossier(dossier.ArchEvents, opts.CommitHash)
	}
	if created, adrErr := archfeatures.AutoGenerateADRsWithDossier(repoRoot, opts.CommitHash, commitSubject, dossierEvents); adrErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("auto ADR generation failed: %v", adrErr))
	} else {
		if opts.Verbose {
			for _, p := range created {
				fmt.Fprintf(out, "doc_engine: auto-generated ADR %s\n", p)
			}
		}
		// D4 lifecycle: new ADRs join the review queue as drafts and the
		// ADR index regenerates (both best-effort, never fail the run).
		for _, p := range created {
			_, _ = review.Queue(repoRoot, review.ReviewItem{
				Kind:      "adr-draft",
				DocPath:   p,
				Summary:   fmt.Sprintf("review auto-generated ADR %s", p),
				Detail:    fmt.Sprintf("generated from commit %s", shortHash(opts.CommitHash)),
			})
		}
		if len(created) > 0 {
			if idxErr := archfeatures.RegenerateIndex(repoRoot); idxErr != nil && opts.Verbose {
				fmt.Fprintf(out, "doc_engine: ADR index regeneration failed: %v\n", idxErr)
			}
		}
	}

	// D5 observability ledger: one JSONL record per run (best-effort).
	// Prune keeps the ledger bounded.
	tracks, repairs, fallbacks := orch.SnapshotCounters()
	avgFresh := 0
	if len(docs) > 0 {
		avgFresh = freshSum / len(docs)
	}
	_ = ledger.Append(repoRoot, ledger.RunRecord{
		Commit:            opts.CommitHash,
		BranchPolicy:      opts.BranchPolicy,
		DocsUpdated:       result.DocsUpdated,
		SectionsUpdated:   result.SectionsUpdated,
		SectionsProcessed: result.SectionsProcessed,
		TokensUsed:        result.TokensUsed,
		DurationMs:        time.Since(start).Milliseconds(),
		TracksUsed:        tracks,
		Repairs:           repairs,
		Fallbacks:         fallbacks,
		GlobalFreshness:   avgFresh,
		Warnings:          len(result.Warnings),
	})
	_ = ledger.Prune(repoRoot, 500)

	result.Duration = time.Since(start)
	return result
}

// dossierChangeCount totals every factual change in the dossier: symbols,
// config vars, sentinels, and arch events. Zero means the commit carried
// no code changes (comments/whitespace/docs only).
func dossierChangeCount(d *docconfig.GlobalCommitDossier) int {
	if d == nil {
		return 0
	}
	return len(d.AddedSymbols) + len(d.ModifiedSymbols) + len(d.RemovedSymbols) +
		len(d.AddedConfigVars) + len(d.RemovedConfigVars) +
		len(d.AddedSentinels) + len(d.ModifiedSentinels) +
		len(d.ArchEvents)
}

func countTotalSections(docs []docconfig.DocSpec) int {
	total := 0
	for _, d := range docs {
		if len(d.Sections) == 0 {
			total++
		} else {
			total += len(d.Sections)
		}
	}
	return total
}

// Check performs a non-modifying freshness and drift audit of all managed
// documents. Returns a CheckResult with per-document freshness scores.
// The caller should use CheckResult.Failures to set a non-zero exit code.
func Check(repoRoot string, opts CheckOptions) (CheckResult, error) {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}

	cfg, err := docconfig.LoadDocsConfig(repoRoot)
	if err != nil {
		return CheckResult{}, fmt.Errorf("doc_engine: %w", err)
	}

	storageDir := docconfig.StorageDirPath(repoRoot)
	sm := storage.NewStateManager(storageDir)
	state, err := sm.Load()
	if err != nil {
		return CheckResult{}, fmt.Errorf("doc_engine: loading state: %w", err)
	}

	docs := filterDocuments(cfg.Documents, opts.DocID, opts.Tag)
	result := CheckResult{AllFresh: true}

	// P9: symbol-exists closure for gmb:assert evaluation. Backed by the AKG
	// head graph when available, else by a lightweight Go AST scan of the
	// repo (exported idents). Never nil — asserts always evaluate.
	assertSymbolExists := buildAssertSymbolExists(opts.HeadGraph, repoRoot)

	// P8: live freshness needs a git work tree; without one (e.g. temp dirs
	// in tests) fall back to the stored scores.
	useLiveFreshness := gitWorkTreeAvailable(repoRoot)

	for _, doc := range docs {
		docResult := DocumentCheckResult{
			ID:         doc.ID,
			TargetPath: doc.TargetPath,
		}

		// Check if file exists.
		absPath := filepath.Join(repoRoot, doc.TargetPath)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			docResult.Status = "missing"
			docResult.Freshness = 0
			result.AllFresh = false
			result.Failures = append(result.Failures, fmt.Sprintf("%s: file missing", doc.TargetPath))
		} else {
			// P8 freshness (Appendix B): compute live from git history
			// (never saved here). Fall back to the stored score when git
			// is unavailable (e.g. temp dirs in tests).
			var lastSync string
			if ds, ok := state.Documents[doc.TargetPath]; ok && ds != nil {
				lastSync = ds.LastUpdatedCommit
				if !ds.LastUpdatedAt.IsZero() {
					docResult.LastUpdated = ds.LastUpdatedAt.Format("2006-01-02T15:04:05Z")
				}
				if !useLiveFreshness {
					docResult.Freshness = ds.FreshnessScore
					docResult.CommitsBehind = ds.CommitsBehind
				}
			}
			if useLiveFreshness {
				liveScore, behind := ComputeFreshnessScore(repoRoot, doc, lastSync)
				docResult.Freshness = liveScore
				docResult.CommitsBehind = behind
			} else if _, ok := state.Documents[doc.TargetPath]; !ok {
				// No state entry means never generated → freshness unknown.
				docResult.Freshness = 0
				docResult.Status = "stale"
			}

			threshold := cfg.Constraints.MinFreshnessThreshold
			if doc.MinFreshness > 0 {
				threshold = doc.MinFreshness
			}
			failThreshold := cfg.Constraints.MinFreshnessFail

			if docResult.Freshness < failThreshold {
				docResult.Status = "stale"
				result.AllFresh = false
				result.Failures = append(result.Failures,
					fmt.Sprintf("%s: freshness %d%% below fail threshold %d%%",
						doc.TargetPath, docResult.Freshness, failThreshold))
			} else if docResult.Freshness < threshold {
				docResult.Status = "warn"
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("%s: freshness %d%% below warn threshold %d%%",
						doc.TargetPath, docResult.Freshness, threshold))
			} else {
				docResult.Status = "fresh"
			}

			// P9: doc-lint asserts from gmb:assert directives. The closure
			// is AKG-backed when a graph is available, else repo-scan
			// backed — asserts always evaluate.
			if content, readErr := os.ReadFile(absPath); readErr == nil {
				for _, msg := range patcher.EvaluateAsserts(string(content), assertSymbolExists) {
					result.AllFresh = false
					// A failed gmb:assert is a real per-document correctness
					// problem, not merely a freshness-score dip — without
					// this, docResult.Status (the dashboard's per-row
					// STATUS column) would stay whatever the freshness
					// check above set it to, e.g. "fresh", while this exact
					// document also lands in result.Failures. `gmb doc
					// status` would then show a 100% FRESH row for a
					// document that just failed CI.
					docResult.Status = "stale"
					result.Failures = append(result.Failures,
						fmt.Sprintf("%s: %s", doc.TargetPath, msg))
				}
			}

			// Gate 7: reference integrity at check time (plan C2). Unlike
			// generation (warnings only), CI fails on broken references:
			// missing files, bad anchors, out-of-range permalink lines.
			// Frontmatter and TOC issues are warnings (advisory).
			if content, readErr := os.ReadFile(absPath); readErr == nil {
				refRep := verifier.CheckReferences(repoRoot, doc.TargetPath, string(content), assertSymbolExists, false)
				for _, br := range refRep.Broken {
					result.AllFresh = false
					docResult.Status = "stale" // see gmb:assert comment above
					result.Failures = append(result.Failures,
						fmt.Sprintf("%s: broken reference (line %d): %s — %s", doc.TargetPath, br.Line, br.Target, br.Reason))
				}
				for _, msg := range verifier.ValidateFrontmatter(string(content), cfg.TargetPlatform) {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("%s: frontmatter: %s", doc.TargetPath, msg))
				}
				for _, msg := range verifier.CheckTOC(string(content)) {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("%s: toc: %s", doc.TargetPath, msg))
				}
			}

			// D2 Diátaxis compass + reference completeness. Quadrant comes
			// from the doc's archetype; unknown archetypes skip silently.
			if quadrant := renderer.ArchetypeQuadrant(doc.Archetype); quadrant != "" {
				for _, sec := range doc.Sections {
					for _, msg := range renderer.CompassCheck(sec.Title, sec.Instruction, quadrant) {
						result.Warnings = append(result.Warnings,
							fmt.Sprintf("%s [%s]: diataxis: %s", doc.TargetPath, sec.ID, msg))
					}
				}
				if quadrant == "reference" && opts.HeadGraph != nil && opts.HeadGraph.Nodes != nil {
					var scopeSyms []string
					opts.HeadGraph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
						if n == nil || n.FileSpec.Path == "" {
							return
						}
						if !catalog.MatchesScope(&doc.Scope, n.FileSpec.Path) {
							return
						}
						name := n.Name
						if idx := strings.LastIndex(name, "."); idx >= 0 {
							name = name[idx+1:]
						}
						if r := []rune(name); len(r) > 0 && r[0] >= 'A' && r[0] <= 'Z' {
							scopeSyms = append(scopeSyms, name)
						}
					})
					if content, readErr := os.ReadFile(absPath); readErr == nil && len(scopeSyms) > 0 {
						missing := renderer.ReferenceCompleteness(scopeSyms, string(content))
						for i, msg := range missing {
							if i >= 10 {
								result.Warnings = append(result.Warnings,
									fmt.Sprintf("%s: reference completeness: ... and %d more undocumented symbols", doc.TargetPath, len(missing)-10))
								break
							}
							result.Warnings = append(result.Warnings,
								fmt.Sprintf("%s: reference completeness: %s", doc.TargetPath, msg))
						}
					}
				}
				// D2 tutorial-exec linkage (gap D2, non-breaking): a
				// tutorial-quadrant doc whose rendered markdown contains
				// fenced code blocks without exec/no_run tags earns a
				// WARNING suggesting gmb:snippet:exec linkage so tutorial
				// code stays executable. Warnings only, never failures.
				if quadrant == "tutorial" {
					if content, readErr := os.ReadFile(absPath); readErr == nil {
						if hasNonExecutableCodeBlocks(string(content)) {
							result.Warnings = append(result.Warnings,
								fmt.Sprintf("%s: tutorial section has non-executable code blocks; consider gmb:snippet:exec", doc.TargetPath))
						}
					}
				}
			}
		}

		result.Documents = append(result.Documents, docResult)
	}

	// D4 ADR lifecycle governance: unknown statuses and dangling
	// supersessions fail the gate (opt-in: clean when docs/adr absent).
	for _, msg := range archfeatures.ValidateADRLifecycle(repoRoot) {
		result.AllFresh = false
		result.Failures = append(result.Failures, fmt.Sprintf("adr lifecycle: %s", msg))
	}

	// Compute global freshness.
	if len(result.Documents) > 0 {
		total := 0
		for _, d := range result.Documents {
			total += d.Freshness
		}
		result.GlobalFreshness = total / len(result.Documents)
	}

	if opts.HeadGraph == nil && opts.Verbose {
		fmt.Fprintln(out, "doc_engine: assert evaluation uses repo symbol scan (no AKG graph)")
	}

	if opts.Verbose {
		_ = out
		// Phase 5 will add styled output here.
	}

	return result, nil
}

// ────────────────────────────────────────────────────────────────────────────
// In-memory catalog cache (gap C5, honest scope)
// ────────────────────────────────────────────────────────────────────────────

// catalogCache reuses the catalog index across in-process Run calls
// (daemon/serve + tests). The cached Catalog is treated as read-only by
// all consumers (invalidator, FastBail); nothing mutates through it.
var catalogCache struct {
	sync.Mutex
	key string
	cat *catalog.Catalog
}

// cachedCatalogForDocs returns a catalog for docs, reusing the cached index
// when key matches the last build. Any docs.yaml/filter change rebuilds.
func cachedCatalogForDocs(docs []docconfig.DocSpec, key string) *catalog.Catalog {
	catalogCache.Lock()
	defer catalogCache.Unlock()
	if catalogCache.cat != nil && catalogCache.key == key {
		return catalogCache.cat
	}
	cat := catalog.New(docs)
	catalogCache.key = key
	catalogCache.cat = cat
	return cat
}

// docsConfigCacheKey hashes the docs.yaml bytes plus the doc/tag filter so
// the catalog cache invalidates on any config or filter change. It probes
// .glassmarble/docs.yaml first, then the repo-root fallback (mirroring
// LoadDocsConfig); missing files hash as empty (same as an empty config).
func docsConfigCacheKey(repoRoot, docID, tag string) string {
	var data []byte
	for _, p := range []string{docconfig.DocsConfigPath(repoRoot), filepath.Join(repoRoot, docconfig.DocsConfigFilename)} {
		if b, err := os.ReadFile(p); err == nil {
			data = b
			break
		}
	}
	h := sha256.New()
	h.Write(data)
	h.Write([]byte("\x00" + docID + "\x00" + tag))
	return hex.EncodeToString(h.Sum(nil))
}

// ────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ────────────────────────────────────────────────────────────────────────────

// filterDocuments returns the subset of docs matching the given ID and/or tag.
// If both are empty, all documents are returned.
func filterDocuments(docs []docconfig.DocSpec, id, tag string) []docconfig.DocSpec {
	if id == "" && tag == "" {
		out := make([]docconfig.DocSpec, len(docs))
		copy(out, docs)
		return out
	}

	var result []docconfig.DocSpec
	for _, doc := range docs {
		if id != "" && doc.ID != id {
			continue
		}
		if tag != "" && !hasTag(doc.Tags, tag) {
			continue
		}
		result = append(result, doc)
	}
	return result
}

func hasTag(tags []string, target string) bool {
	for _, t := range tags {
		if t == target {
			return true
		}
	}
	return false
}

func shortHash(hash string) string {
	if len(hash) >= 8 {
		return hash[:8]
	}
	return hash
}

func isQuiet(out io.Writer) bool {
	return out == io.Discard
}

// ────────────────────────────────────────────────────────────────────────────
// P5 branch policy (master plan Pillar 5 + F9)
// ────────────────────────────────────────────────────────────────────────────

// resolveWritePermission decides whether a Run() may write files and state.
// Policies: "main-only" (default), "any", "tag-only".
//
//   - main-only: write only on main/master (or detached HEAD with a warning).
//     Any other branch without ForceWrite is compute-only; an unknown branch
//     (git lookup failed) is strictly compute-only with a warning.
//   - any: always write.
//   - tag-only: write only when HEAD is an exact tag match.
//   - A branch name containing "draft" or "wip" (case-insensitive) is always
//     compute-only, even with ForceWrite.
func resolveWritePermission(repoRoot, policy string, forceWrite bool) (canWrite bool, branch string, warnings []string) {
	branch = currentGitBranch(repoRoot)

	// Draft/WIP branches are always compute-only.
	if lower := strings.ToLower(branch); strings.Contains(lower, "draft") || strings.Contains(lower, "wip") {
		return false, branch, []string{
			fmt.Sprintf("draft branch %q: compute-only mode (no files or state will be written)", displayBranch(branch)),
		}
	}

	if forceWrite {
		// Warn only when the override actually changes the outcome;
		// on main/master (or policy "any") --write is a no-op confirmation.
		if branch != "" && branch != "main" && branch != "master" && branch != "HEAD" && normalizeBranchPolicy(policy) != "any" {
			return true, branch, []string{
				fmt.Sprintf("ForceWrite override (--write): writing on branch %q despite %q policy", displayBranch(branch), normalizeBranchPolicy(policy)),
			}
		}
		return true, branch, nil
	}

	switch normalizeBranchPolicy(policy) {
	case "any":
		return true, branch, nil
	case "tag-only":
		if isExactTagHead(repoRoot) {
			return true, branch, nil
		}
		return false, branch, []string{
			"tag-only branch policy: HEAD is not an exact tag match — compute-only mode (no files or state will be written)",
		}
	default: // "main-only"
		if policy != "" && policy != "main-only" {
			warnings = append(warnings, fmt.Sprintf("unknown branch-policy %q: falling back to main-only", policy))
		}
		switch branch {
		case "main", "master":
			return true, branch, warnings
		case "HEAD":
			// Detached HEAD (common in CI checkouts): allow with a warning.
			warnings = append(warnings, "detached HEAD: writing with unknown branch (main-only policy)")
			return true, branch, warnings
		case "":
			warnings = append(warnings, "unknown branch (git rev-parse failed): compute-only mode (no files or state will be written)")
			return false, branch, warnings
		default:
			warnings = append(warnings, fmt.Sprintf("branch %q is not main/master: compute-only mode (no files or state will be written; use --write to override)", branch))
			return false, branch, warnings
		}
	}
}

// normalizeBranchPolicy maps "" to the default "main-only".
func normalizeBranchPolicy(policy string) string {
	if policy == "" {
		return "main-only"
	}
	return policy
}

func displayBranch(branch string) string {
	if branch == "" {
		return "unknown"
	}
	return branch
}

// ────────────────────────────────────────────────────────────────────────────
// P8 freshness (master-plan Appendix B)
// ────────────────────────────────────────────────────────────────────────────

// ComputeFreshnessScoreWithArchEvents scores how in-sync a document is with
// HEAD (0-100) and counts the in-scope commits since lastSyncCommit,
// decaying by the current dossier's structural arch events.
//
// It runs `git rev-list --count` and `git log --format=%H|%s|%ct
// <lastSync>..HEAD -- <scope paths>` (scope globs pass through verbatim;
// entry_points are ignored for history). Each commit (capped at 100) is
// weighted by intent — via commit_reasoning, falling back to keyword weights
// — and decayed by recency: contribution = weight * base / sqrt(days+1),
// where base is 1.0 iff len(archEvents) > 0 (the dossier carries structural
// arch events), else 0.5.
// score = max(0, 100 - round(sum)). Any git failure yields (0, 0).
func ComputeFreshnessScoreWithArchEvents(repoRoot string, doc docconfig.DocSpec, lastSyncCommit string, archEvents []string) (score int, commitsBehind int) {
	return computeFreshnessScore(repoRoot, doc, lastSyncCommit, archEvents)
}

// ComputeFreshnessScore is the dossier-less entry point: identical scoring
// with no arch events (base 0.5). Prefer ComputeFreshnessScoreWithArchEvents
// when the run dossier is available.
func ComputeFreshnessScore(repoRoot string, doc docconfig.DocSpec, lastSyncCommit string) (score int, commitsBehind int) {
	return computeFreshnessScore(repoRoot, doc, lastSyncCommit, nil)
}

// computeFreshnessScore implements both exported entry points above.
func computeFreshnessScore(repoRoot string, doc docconfig.DocSpec, lastSyncCommit string, archEvents []string) (score int, commitsBehind int) {
	paths := append([]string{}, doc.Scope.Paths...)

	rangeSpec := "HEAD"
	if strings.TrimSpace(lastSyncCommit) != "" {
		rangeSpec = strings.TrimSpace(lastSyncCommit) + "..HEAD"
	}

	countArgs := []string{"rev-list", "--count", rangeSpec}
	if len(paths) > 0 {
		countArgs = append(countArgs, "--")
		countArgs = append(countArgs, paths...)
	}
	countOut, err := runGitOutput(repoRoot, countArgs...)
	if err != nil {
		return 0, 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(countOut))
	if err != nil || n < 0 {
		return 0, 0
	}
	commitsBehind = n
	if n == 0 {
		return 100, 0
	}

	logArgs := []string{"log", "--format=%H|%s|%ct", rangeSpec}
	if len(paths) > 0 {
		logArgs = append(logArgs, "--")
		logArgs = append(logArgs, paths...)
	}
	logOut, err := runGitOutput(repoRoot, logArgs...)
	if err != nil {
		return 0, commitsBehind
	}
	lines := strings.Split(strings.TrimSpace(logOut), "\n")
	if len(lines) > 100 {
		lines = lines[:100]
	}

	type scoredCommit struct {
		subject string
		ts      int64
		weight  float64
	}
	commits := make([]scoredCommit, 0, len(lines))
	for _, ln := range lines {
		parts := strings.SplitN(ln, "|", 3)
		if len(parts) != 3 {
			continue
		}
		ts, _ := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
		commits = append(commits, scoredCommit{
			subject: parts[1],
			ts:      ts,
			weight:  float64(weightForCommitSubject(parts[1])),
		})
	}
	if len(commits) == 0 {
		return 100, commitsBehind
	}

	// B3: the recency base keys off the dossier's structural arch events —
	// base is 1.0 iff the dossier carries any arch event, else 0.5.
	base := 0.5
	if len(archEvents) > 0 {
		base = 1.0
	}

	now := time.Now().Unix()
	var sum float64
	for _, c := range commits {
		var days int64
		if c.ts > 0 && now > c.ts {
			days = (now - c.ts) / 86400
		}
		sum += c.weight * base / math.Sqrt(float64(days+1))
	}
	score = 100 - int(math.Round(sum))
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score, commitsBehind
}

// weightForCommitSubject weights one commit by intent: deterministic
// commit_reasoning classification first, keyword fallback for UNKNOWN.
func weightForCommitSubject(subject string) int {
	if w, ok := intentWeightForSubject(subject); ok {
		return w
	}
	return keywordWeightForSubject(subject)
}

// intentWeightForSubject maps a commit_reasoning intent to an Appendix B
// weight. ok=false for UNKNOWN so the caller falls back to keywords.
func intentWeightForSubject(subject string) (weight int, ok bool) {
	defer func() {
		// Classification must never panic the doc engine.
		if recover() != nil {
			weight, ok = 0, false
		}
	}()
	ext := commit_reasoning.NewIntentExtractor()
	res := ext.Extract(context.Background(), &git.CommitMeta{Subject: subject}, "")
	switch res.Intent {
	case commit_reasoning.IntentAddFeature:
		return 15, true
	case commit_reasoning.IntentRefactor:
		return 15, true
	case commit_reasoning.IntentFixBug:
		return 8, true
	case commit_reasoning.IntentPerformance:
		return 5, true
	case commit_reasoning.IntentSecurity:
		return 5, true
	case commit_reasoning.IntentTest:
		return 2, true
	case commit_reasoning.IntentDocs:
		return 2, true
	case commit_reasoning.IntentInfrastructure, commit_reasoning.IntentDependencyUpdate:
		return 1, true
	default:
		return 0, false
	}
}

// keywordWeightForSubject is the Appendix B keyword fallback:
// feat/add=15, refactor=15, fix=8, perf=5, secur=5, test=2, docs=2,
// depend/chore=1, default=5.
func keywordWeightForSubject(subject string) int {
	lower := strings.ToLower(subject)
	switch {
	case strings.Contains(lower, "refactor"):
		return 15
	case strings.Contains(lower, "feat"):
		return 15
	case strings.Contains(lower, "add"):
		return 15
	case strings.Contains(lower, "fix"):
		return 8
	case strings.Contains(lower, "perf"):
		return 5
	case strings.Contains(lower, "secur"):
		return 5
	case strings.Contains(lower, "test"):
		return 2
	case strings.Contains(lower, "docs"), strings.Contains(lower, "readme"):
		return 2
	case strings.Contains(lower, "depend"), strings.Contains(lower, "chore"):
		return 1
	default:
		return 5
	}
}

// hasNonExecutableCodeBlocks reports whether markdown contains a fenced
// code block without an exec/no_run tag (D2 tutorial-exec linkage). A
// block counts as executable when its opening fence info string mentions
// "exec" or "no_run"/"norun", or when the document carries a
// gmb:snippet:exec directive. Advisory only — callers emit warnings.
func hasNonExecutableCodeBlocks(markdown string) bool {
	s := strings.ReplaceAll(markdown, "\r\n", "\n")
	if !strings.Contains(s, "```") && !strings.Contains(s, "~~~") {
		return false
	}
	if strings.Contains(s, "gmb:snippet:exec") {
		return false
	}
	inFence := false
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(t, "```") && !strings.HasPrefix(t, "~~~") {
			continue
		}
		if inFence {
			inFence = false
			continue
		}
		info := strings.ToLower(strings.TrimSpace(t[3:]))
		if !strings.Contains(info, "exec") && !strings.Contains(info, "no_run") && !strings.Contains(info, "norun") {
			return true
		}
		inFence = true
	}
	return false
}

// ────────────────────────────────────────────────────────────────────────────
// P9 asserts — AKG-backed symbol-exists closure
// ────────────────────────────────────────────────────────────────────────────

// buildAssertSymbolExists returns a symbol-exists closure backed by the AKG
// head graph when available, falling back to a lightweight Go AST scan of
// repoRoot (exported idents only). The closure is never nil: without any
// symbol source it reports every symbol as known so asserts never fail
// blindly on missing infrastructure.
func buildAssertSymbolExists(graph *akg.CodePropertyGraph, repoRoot string) func(string) bool {
	known := make(map[string]bool)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		known[s] = true
		if idx := strings.LastIndex(s, "::"); idx >= 0 {
			known[s[idx+2:]] = true
		}
		if idx := strings.LastIndex(s, "."); idx >= 0 {
			known[s[idx+1:]] = true
		}
	}
	if graph != nil && graph.Nodes != nil {
		graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
			add(id)
			if n != nil {
				add(n.Name)
			}
		})
		return func(sym string) bool {
			return known[strings.TrimSpace(sym)]
		}
	}
	for id := range scanRepoExportedIdents(repoRoot) {
		add(id)
	}
	if len(known) == 0 {
		return func(string) bool { return true }
	}
	return func(sym string) bool {
		return known[strings.TrimSpace(sym)]
	}
}

// scanRepoExportedIdents collects exported Go identifiers (funcs, methods as
// Recv.Name, types, vars) across repoRoot. Best-effort: parse errors skip files.
func scanRepoExportedIdents(repoRoot string) map[string]bool {
	out := make(map[string]bool)
	if repoRoot == "" {
		return out
	}
	_ = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		node, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil || node == nil {
			return nil
		}
		for _, decl := range node.Decls {
			switch t := decl.(type) {
			case *ast.FuncDecl:
				if ast.IsExported(t.Name.Name) {
					out[t.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range t.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if ast.IsExported(s.Name.Name) {
							out[s.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if ast.IsExported(name.Name) {
								out[name.Name] = true
							}
						}
					}
				}
			}
		}
		return nil
	})
	return out
}

// ────────────────────────────────────────────────────────────────────────────
// Git helpers (all non-fatal — callers decide how to degrade)
// ────────────────────────────────────────────────────────────────────────────

// runGitOutput runs one git command in dir and returns its stdout.
func runGitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// currentGitBranch returns the current branch via
// `git rev-parse --abbrev-ref HEAD`. Non-fatal: "" means unknown (and, in
// detached-HEAD checkouts, git reports the literal "HEAD").
func currentGitBranch(repoRoot string) string {
	out, err := runGitOutput(repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// gitHeadCommit resolves HEAD to a full commit hash in repoRoot.
// Non-fatal: "" on any error (non-git dir, empty repo). Inherits the
// process environment (including GIT_DIR/GIT_WORK_TREE set by hooks),
// with cmd.Dir anchoring relative paths — the standard git resolution.
func gitHeadCommit(repoRoot string) (string, error) {
	out, err := runGitOutput(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	head := strings.TrimSpace(out)
	if len(head) < 7 {
		return "", fmt.Errorf("doc_engine: invalid HEAD hash %q", head)
	}
	return head, nil
}

// isExactTagHead reports whether HEAD is an exact tag match
// (`git describe --exact-match HEAD` succeeds).
func isExactTagHead(repoRoot string) bool {
	_, err := runGitOutput(repoRoot, "describe", "--exact-match", "HEAD")
	return err == nil
}

// gitCommitSubject returns the subject of ref (or HEAD when ref is empty)
// via `git log -1 --format=%s`. Non-fatal: "" on any error.
func gitCommitSubject(repoRoot, ref string) string {
	if strings.TrimSpace(ref) == "" {
		ref = "HEAD"
	}
	out, err := runGitOutput(repoRoot, "log", "-1", "--format=%s", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// gitWorkTreeAvailable reports whether dir is inside a git work tree.
func gitWorkTreeAvailable(repoRoot string) bool {
	_, err := runGitOutput(repoRoot, "rev-parse", "--git-dir")
	return err == nil
}
