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
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/patcher"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/renderer"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
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

	// Fast-bail: if we've already processed this commit and --force is not set,
	// skip the entire run.
	if !opts.Force && state.LastCommit == opts.CommitHash && opts.CommitHash != "" {
		if opts.Verbose {
			fmt.Fprintf(out, "doc_engine: already processed commit %s, skipping (use --force to override)\n", opts.CommitHash[:8])
		}
		result.Duration = time.Since(start)
		return result
	}

	// Filter documents by --doc and --tag flags.
	docs := filterDocuments(cfg.Documents, opts.DocID, opts.Tag)
	if len(docs) == 0 {
		if opts.DocID != "" {
			result.Err = fmt.Errorf("doc_engine: no document with id %q found in docs.yaml", opts.DocID)
		}
		result.Duration = time.Since(start)
		return result
	}

	// ── Phase 1: Catalog & Invalidation Engine ────────────────────────────
	cat := catalog.New(docs)

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
	// nothing to render — mark processed and exit without LLM calls.
	if dossier != nil && !opts.Force && dossierChangeCount(dossier) == 0 {
		if opts.Verbose || !isQuiet(out) {
			fmt.Fprintln(out, "doc_engine: no code changes in dossier (comments/whitespace only), skipping render")
		}
		state.LastCommit = opts.CommitHash
		if saveErr := sm.Save(state); saveErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not save docs_state.json: %v", saveErr))
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
			}
		}
	}

	orch := renderer.NewOrchestrator(renderer.OrchestratorOptions{
		NoLLM:       opts.NoLLM,
		Model:       opts.Model,
		Provider:    opts.Provider,
		Verbose:     opts.Verbose,
		Out:         out,
		GlobalStyle: cfg.Style,
	})

	// Index dirty sections by document ID
	dirtyByDoc := make(map[string][]string)
	for _, ds := range dirtySections {
		dirtyByDoc[ds.DocID] = append(dirtyByDoc[ds.DocID], ds.SectionID)
	}

	ctx := context.Background()
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
		if opts.Force || len(secIDs) > 0 {
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

	// P8 freshness (master-plan Appendix B): refresh per-document scores.
	// Reload state first — ProcessDocument performs its own Load/Save cycles,
	// so the in-memory snapshot from the top of Run() is stale.
	freshState, loadErr := sm.Load()
	if loadErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not reload state for freshness update: %v", loadErr))
		freshState = state
	}
	for i := range docs {
		ds := storage.GetOrCreateDocState(freshState, docs[i].TargetPath)
		freshScore, behind := ComputeFreshnessScore(repoRoot, docs[i], ds.LastUpdatedCommit)
		ds.FreshnessScore = freshScore
		ds.CommitsBehind = behind
	}

	// Update state with this commit hash so we know we've seen it.
	freshState.LastCommit = opts.CommitHash
	if saveErr := sm.Save(freshState); saveErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not save docs_state.json: %v", saveErr))
	}

	// P16 auto-trigger: autonomous ADR generation (non-fatal, warnings only).
	commitSubject := gitCommitSubject(repoRoot, opts.CommitHash)
	if created, adrErr := archfeatures.AutoGenerateADRs(repoRoot, opts.CommitHash, commitSubject); adrErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("auto ADR generation failed: %v", adrErr))
	} else if opts.Verbose {
		for _, p := range created {
			fmt.Fprintf(out, "doc_engine: auto-generated ADR %s\n", p)
		}
	}

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
					result.Failures = append(result.Failures,
						fmt.Sprintf("%s: %s", doc.TargetPath, msg))
				}
			}
		}

		result.Documents = append(result.Documents, docResult)
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

// ComputeFreshnessScore scores how in-sync a document is with HEAD (0-100)
// and counts the in-scope commits since lastSyncCommit.
//
// It runs `git rev-list --count` and `git log --format=%H|%s|%ct
// <lastSync>..HEAD -- <scope paths>` (scope globs pass through verbatim;
// entry_points are ignored for history). Each commit (capped at 100) is
// weighted by intent — via commit_reasoning, falling back to keyword weights
// — and decayed by recency: contribution = weight * base / sqrt(days+1),
// where base is 1.0 when any subject carries dossier-arch keywords
// (split/cycle/layer/service/database/interface), else 0.5.
// score = max(0, 100 - round(sum)). Any git failure yields (0, 0).
func ComputeFreshnessScore(repoRoot string, doc docconfig.DocSpec, lastSyncCommit string) (score int, commitsBehind int) {
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

	base := 0.5
	for _, c := range commits {
		if hasDossierArchKeyword(c.subject) {
			base = 1.0
			break
		}
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

// hasDossierArchKeyword reports whether a commit subject carries
// dossier-arch keywords (split/cycle/layer/service/database/interface).
func hasDossierArchKeyword(subject string) bool {
	lower := strings.ToLower(subject)
	for _, kw := range []string{"split", "cycle", "layer", "service", "database", "interface"} {
		if strings.Contains(lower, kw) {
			return true
		}
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
