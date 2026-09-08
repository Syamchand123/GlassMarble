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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	docconfig "github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
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

	// Force bypasses the fast-bail check and reprocesses all documents.
	Force bool

	// DocID, if non-empty, restricts processing to the document with this ID.
	DocID string

	// Tag, if non-empty, restricts processing to documents tagged with this value.
	Tag string

	// BranchPolicy controls when the engine writes output on non-main branches.
	// "main-only" (default), "any", "tag-only"
	BranchPolicy string

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

	if opts.Verbose || !isQuiet(out) {
		fmt.Fprintf(out, "doc_engine: processing %d document(s) for commit %s\n",
			len(docs), shortHash(opts.CommitHash))
	}

	// ── Stages 2-8 are stubs for Phase 0 ──────────────────────────────────
	// Phase 1: catalog + invalidation engine
	// Phase 2: grounding engine
	// Phase 3: patcher + merger + quality firewall
	// Phase 4: renderer (deterministic + LLM actuator)
	// Phase 5: full pipeline integration
	// ──────────────────────────────────────────────────────────────────────
	//
	// For Phase 0, we verify the plumbing is correct end-to-end:
	// - docs.yaml loads without error
	// - state round-trips correctly
	// - the facade compiles and integrates into cmd/analyze.go
	//
	// The actual section processing is implemented in Phase 1-4.

	// Update state with this commit hash so we know we've seen it.
	state.LastCommit = opts.CommitHash
	if saveErr := sm.Save(state); saveErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not save docs_state.json: %v", saveErr))
	}

	result.Duration = time.Since(start)
	return result
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
			// Read freshness from state.
			if ds, ok := state.Documents[doc.TargetPath]; ok {
				docResult.Freshness = ds.FreshnessScore
				docResult.LastUpdated = ds.LastUpdatedAt.Format("2006-01-02T15:04:05Z")
			} else {
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

	if opts.Verbose {
		_ = out
		// Phase 5 will add styled output here.
	}

	return result, nil
}

// Init scaffolds a new managed document: writes the target .md file and
// adds the DocSpec to .glassmarble/docs.yaml.
// Phase 5 wires this to the interactive Charm Huh questionnaire.
func Init(repoRoot string, opts InitOptions) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}

	// Ensure docs.yaml exists.
	if err := docconfig.WriteDefaultDocsConfig(repoRoot); err != nil {
		return fmt.Errorf("doc_engine: init: %w", err)
	}

	// Phase 5 will add: interactive questionnaire, AKG entry-point detection,
	// archetype pre-population, and docs.yaml update.
	fmt.Fprintf(out, "doc_engine: scaffold stub for %s (full implementation in Phase 5)\n", opts.TargetPath)
	return nil
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
