// Package doc_engine — operations.go
// Implements Diff, Release, and Export operations
// for the CLI surface (subcommands of `gmb doc`).
package doc_engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/archfeatures"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/compliance"
	docconfig "github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/devex"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/eval"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/grounding"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/ledger"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/patcher"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/renderer"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/review"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/sre"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
)

// ────────────────────────────────────────────────────────────────────────────
// Diff — preview pending changes without modifying disk
// ────────────────────────────────────────────────────────────────────────────

// SectionDiff describes a pending change to a single section.
type SectionDiff struct {
	DocID       string `json:"doc_id"`
	TargetPath  string `json:"target_path"`
	SectionID   string `json:"section_id"`
	HasChange   bool   `json:"has_change"`
	OldContent  string `json:"old_content,omitempty"`
	NewContent  string `json:"new_content,omitempty"`
	DiffPreview string `json:"diff_preview"`
}

// DiffResult is the outcome of a gmb doc diff preview.
type DiffResult struct {
	HasChanges bool          `json:"has_changes"`
	Sections   []SectionDiff `json:"sections"`
}

// Diff previews what documentation updates would be generated without writing to disk.
func Diff(repoRoot string, opts RunOptions) (DiffResult, error) {
	cfg, err := docconfig.LoadDocsConfig(repoRoot)
	if err != nil {
		return DiffResult{}, fmt.Errorf("doc diff: %w", err)
	}

	storageDir := docconfig.StorageDirPath(repoRoot)
	sm := storage.NewStateManager(storageDir)
	state, _ := sm.Load()

	docs := filterDocuments(cfg.Documents, opts.DocID, opts.Tag)
	result := DiffResult{}

	orch := renderer.NewOrchestrator(renderer.OrchestratorOptions{
		NoLLM:       true, // Deterministic diff preview
		Verbose:     false,
		GlobalStyle: cfg.Style,
	})

	ctx := context.Background()

	for _, doc := range docs {
		absTarget := filepath.Join(repoRoot, doc.TargetPath)
		rawBytes, _ := os.ReadFile(absTarget)
		parsedDoc := patcher.ParseMarkdown(string(rawBytes))

		for _, sec := range doc.Sections {
			if !sec.Managed || sec.Freeze {
				continue
			}

			zone := patcher.ManagedZone(parsedDoc, sec.ID)
			priorBody := ""
			if zone != nil {
				priorBody = patcher.ExtractBody(zone)
			}

			// Generate candidate
			fs := &docconfig.FactSheet{
				DocID:                doc.ID,
				SectionID:            sec.ID,
				SectionInstruction:   sec.Instruction,
				PriorSectionMarkdown: priorBody,
				GroundTruth:          docconfig.GroundTruthPayload{},
			}

			outcome, rErr := orch.RenderSection(ctx, fs, opts.HeadGraph)
			if rErr != nil {
				continue
			}

			hasChange := strings.TrimSpace(priorBody) != strings.TrimSpace(outcome.Content)
			diffPreview := ""
			if hasChange {
				diffPreview = fmt.Sprintf("@@ section: %s @@\n- %s\n+ %s",
					sec.ID,
					truncateLine(priorBody, 60),
					truncateLine(outcome.Content, 60),
				)
				result.HasChanges = true
			} else {
				diffPreview = fmt.Sprintf("section %s is up-to-date", sec.ID)
			}

			result.Sections = append(result.Sections, SectionDiff{
				DocID:       doc.ID,
				TargetPath:  doc.TargetPath,
				SectionID:   sec.ID,
				HasChange:   hasChange,
				OldContent:  priorBody,
				NewContent:  outcome.Content,
				DiffPreview: diffPreview,
			})
		}
	}

	_ = state
	return result, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Status — dashboard of all managed docs with freshness scores
// ────────────────────────────────────────────────────────────────────────────

// DocStatus is a single row of the `gmb doc status` dashboard.
type DocStatus struct {
	ID         string `json:"id"`
	TargetPath string `json:"target_path"`
	Freshness  int    `json:"freshness"`
	Mode       string `json:"mode"`
	LastSync   string `json:"last_sync"`
	Status     string `json:"status"`
}

// StatusResult is the outcome of a `gmb doc status` dashboard query.
type StatusResult struct {
	Documents       []DocStatus `json:"documents"`
	GlobalFreshness int         `json:"global_freshness"`
}

// Status returns per-document freshness rows by running a non-modifying Check
// and projecting it into dashboard shape (plan §11.2 `gmb doc status` table).
func Status(repoRoot string, opts CheckOptions) (StatusResult, error) {
	check, err := Check(repoRoot, opts)
	if err != nil {
		return StatusResult{}, err
	}
	result := StatusResult{GlobalFreshness: check.GlobalFreshness}
	for _, d := range check.Documents {
		result.Documents = append(result.Documents, DocStatus{
			ID:         d.ID,
			TargetPath: d.TargetPath,
			Freshness:  d.Freshness,
			LastSync:   d.LastUpdated,
			Status:     d.Status,
		})
	}
	return result, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Release — migration guide generator between Git refs
// ────────────────────────────────────────────────────────────────────────────

// Release generates a markdown migration guide detailing changes between two git refs.
func Release(repoRoot, ref1, ref2, outFile string) (string, error) {
	return devex.GenerateMigrationGuide(repoRoot, ref1, ref2, outFile)
}

// ────────────────────────────────────────────────────────────────────────────
// Export — RAG chunked knowledge base
// ────────────────────────────────────────────────────────────────────────────

// RAGChunk is a single section chunk ready for embedding ingestion.
type RAGChunk = devex.RAGChunk

// ExportResult contains stats about the exported RAG knowledge base.
type ExportResult struct {
	ChunksCount int      `json:"chunks_count"`
	OutputDir   string   `json:"output_dir"`
	Files       []string `json:"files"`
}

// Export chunks managed documents into RAG-ready embeddings or JSONL.
func Export(repoRoot, format, outDir string) (ExportResult, error) {
	summary, err := devex.ExportKnowledgeBase(repoRoot, format, outDir)
	if err != nil {
		return ExportResult{}, err
	}
	return ExportResult{
		ChunksCount: summary.TotalChunks,
		OutputDir:   summary.OutputDir,
		Files:       summary.Files,
	}, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Kept Architectural, SRE & Compliance Operations
// ────────────────────────────────────────────────────────────────────────────

// GenerateADR scaffolds an Architecture Decision Record (ADR) file under docs/adr/.
func GenerateADR(repoRoot string, event archfeatures.ADREvent) (string, error) {
	return archfeatures.GenerateADR(repoRoot, event)
}

// GenerateErrorCatalog extracts sentinel errors and builds the operational triage playbook.
func GenerateErrorCatalog(repoRoot string) (string, error) {
	return sre.GenerateErrorCatalog(repoRoot)
}

// GenerateConfigDictionary generates the environment variable and runtime configuration reference.
func GenerateConfigDictionary(repoRoot string) (string, error) {
	return compliance.GenerateConfigDictionary(repoRoot)
}

// VerifyCodeSnippets checks code snippets in markdown for syntax validity and symbol drift.
func VerifyCodeSnippets(markdown string, knownSymbols map[string]bool) ([]devex.SnippetError, error) {
	return devex.VerifyCodeSnippets(markdown, knownSymbols)
}

// VerifySnippetsInRepo checks snippets against real repo func signatures
// (arity verification), not just syntax and name presence.
func VerifySnippetsInRepo(repoRoot, markdown string, knownSymbols map[string]bool) ([]devex.SnippetError, error) {
	return devex.VerifySnippetsInRepo(repoRoot, markdown, knownSymbols)
}

// ApplySnippetFixes deterministically rewrites failing snippet call lines
// with their SuggestedFix signatures. Explicit --fix only.
func ApplySnippetFixes(markdown string, errs []devex.SnippetError) string {
	return devex.ApplySnippetFixes(markdown, errs)
}

func truncateLine(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

// ────────────────────────────────────────────────────────────────────────────
// Eval — D1 faithfulness scoring over current managed sections
// ────────────────────────────────────────────────────────────────────────────

// DocEval is the faithfulness rollup for one document.
type DocEval struct {
	DocID       string   `json:"doc_id"`
	TargetPath  string   `json:"target_path"`
	Score       float64  `json:"score"`
	Sections    int      `json:"sections"`
	Unsupported []string `json:"unsupported,omitempty"`
}

// EvalResult aggregates faithfulness across the doc suite.
type EvalResult struct {
	GlobalScore float64   `json:"global_score"`
	Samples     int       `json:"samples"`
	Docs        []DocEval `json:"docs"`
}

// EvalFaithfulness scores every non-empty managed section body against a
// freshly assembled FactSheet (deterministic judge — no network, no AKG
// required). sampleN caps evaluated sections (<=0 = all), in deterministic
// doc/section order. Empty bodies are skipped, not scored.
func EvalFaithfulness(repoRoot string, sampleN int) (EvalResult, error) {
	cfg, err := docconfig.LoadDocsConfig(repoRoot)
	if err != nil {
		return EvalResult{}, fmt.Errorf("doc eval: %w", err)
	}
	result := EvalResult{}
	evaluated := 0
	for _, doc := range cfg.Documents {
		absTarget := filepath.Join(repoRoot, doc.TargetPath)
		rawBytes, err := os.ReadFile(absTarget)
		if err != nil {
			continue
		}
		parsedDoc := patcher.ParseMarkdown(string(rawBytes))
		de := DocEval{DocID: doc.ID, TargetPath: doc.TargetPath}
		var sum float64
		for _, sec := range doc.Sections {
			if sampleN > 0 && evaluated >= sampleN {
				break
			}
			if !sec.Managed || sec.Freeze {
				continue
			}
			zone := patcher.ManagedZone(parsedDoc, sec.ID)
			if zone == nil {
				continue
			}
			priorBody := strings.TrimSpace(patcher.ExtractBody(zone))
			if priorBody == "" {
				continue
			}
			secCopy := sec
			fs := grounding.AssembleFactSheet(&doc, &secCopy, nil, nil, priorBody, repoRoot)
			if fs == nil {
				continue
			}
			rep := eval.ScoreSection(priorBody, fs)
			sum += rep.Score
			de.Sections++
			evaluated++
			for _, u := range rep.Unsupported {
				if len(de.Unsupported) < 10 {
					de.Unsupported = append(de.Unsupported, fmt.Sprintf("[%s] %s", sec.ID, u))
				}
			}
		}
		if de.Sections > 0 {
			de.Score = sum / float64(de.Sections)
			result.Docs = append(result.Docs, de)
		}
		if sampleN > 0 && evaluated >= sampleN {
			break
		}
	}
	result.Samples = evaluated
	if evaluated > 0 {
		total := 0.0
		for _, d := range result.Docs {
			total += d.Score * float64(d.Sections)
		}
		result.GlobalScore = total / float64(evaluated)
	} else {
		result.GlobalScore = 1.0
	}
	return result, nil
}

// LedgerSummary returns the D5 run-ledger rollup (lastN<=0 = all runs).
func LedgerSummary(repoRoot string, lastN int) (ledger.Summary, error) {
	return ledger.Summarize(repoRoot, lastN)
}

// ReviewStats counts review items by status (D6).
func ReviewStats(repoRoot string) (pending, approved, rejected, observed int, err error) {
	return review.Stats(repoRoot)
}

// ListPendingReviews returns unresolved D6 human-review items.
func ListPendingReviews(repoRoot string) ([]review.ReviewItem, error) {
	return review.ListPending(repoRoot)
}

// ResolveReview approves or rejects a D6 review item with a reason.
func ResolveReview(repoRoot, id string, approve bool, reason string) error {
	return review.Resolve(repoRoot, id, approve, reason)
}
