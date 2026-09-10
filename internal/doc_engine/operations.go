// Package doc_engine — operations.go
// Implements Diff, Gaps, Suggest, Release, Report, and Export operations
// for the CLI surface (subcommands of `gmb doc`).
package doc_engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/archfeatures"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/compliance"
	docconfig "github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/devex"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/federation"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/patcher"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/renderer"
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
		NoLLM:   true, // Deterministic diff preview
		Verbose: false,
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
// Gaps — documentation gap discovery
// ────────────────────────────────────────────────────────────────────────────

// DocGap represents an undocumented subsystem or frequent query topic.
type DocGap struct {
	Topic       string `json:"topic"`
	Package     string `json:"package"`
	QueryCount  int    `json:"query_count"`
	SuggestedID string `json:"suggested_id"`
	Description string `json:"description"`
}

// GapsResult contains discovered documentation gaps.
type GapsResult struct {
	Gaps []DocGap `json:"gaps"`
}

// Gaps discovers documentation gaps by inspecting query history and AKG symbols.
func Gaps(repoRoot string) (GapsResult, error) {
	gapsPath := filepath.Join(repoRoot, ".glassmarble", "docs_gaps.json")
	var result GapsResult

	if data, err := os.ReadFile(gapsPath); err == nil {
		_ = json.Unmarshal(data, &result)
	}

	// Also load query gaps recorded by devex
	queryGaps, _ := devex.LoadQueryGaps(repoRoot)
	for _, qg := range queryGaps {
		result.Gaps = append(result.Gaps, DocGap{
			Topic:       qg.Topic,
			Package:     qg.Query,
			QueryCount:  qg.Count,
			SuggestedID: qg.Topic + "-reference",
			Description: fmt.Sprintf("Missing documentation for query %q in %s", qg.Query, qg.MissingFrom),
		})
	}

	if len(result.Gaps) > 0 {
		return result, nil
	}

	// Fallback: discover packages that don't have a matching doc spec
	cfg, err := docconfig.LoadDocsConfig(repoRoot)
	if err == nil {
		knownPaths := make(map[string]bool)
		for _, d := range cfg.Documents {
			for _, p := range d.Scope.Paths {
				knownPaths[p] = true
			}
		}

		// Inspect internal packages
		internalDir := filepath.Join(repoRoot, "internal")
		entries, _ := os.ReadDir(internalDir)
		for _, e := range entries {
			if e.IsDir() {
				pkgPath := fmt.Sprintf("internal/%s/**", e.Name())
				if !knownPaths[pkgPath] {
					result.Gaps = append(result.Gaps, DocGap{
						Topic:       e.Name(),
						Package:     "internal/" + e.Name(),
						QueryCount:  1,
						SuggestedID: e.Name() + "-reference",
						Description: fmt.Sprintf("No managed documentation tracking %s", pkgPath),
					})
				}
			}
		}
	}

	return result, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Suggest — draft section / doc suggestions for gaps
// ────────────────────────────────────────────────────────────────────────────

// SuggestedDoc provides a ready-to-use DocSpec recommendation for a gap.
type SuggestedDoc struct {
	ID         string   `json:"id"`
	TargetPath string   `json:"target_path"`
	Archetype  string   `json:"archetype"`
	Title      string   `json:"title"`
	ScopePaths []string `json:"scope_paths"`
	Sections   []string `json:"sections"`
}

// Suggest generates recommended document specifications for unmanaged code areas.
func Suggest(repoRoot string) ([]SuggestedDoc, error) {
	gaps, err := Gaps(repoRoot)
	if err != nil {
		return nil, err
	}

	var suggestions []SuggestedDoc
	for _, g := range gaps.Gaps {
		arch := "module"
		if strings.Contains(g.Topic, "arch") {
			arch = "architecture"
		}
		docSpec, _ := renderer.GetArchetype(arch)

		var secTitles []string
		for _, s := range docSpec.Sections {
			secTitles = append(secTitles, s.Title)
		}

		suggestions = append(suggestions, SuggestedDoc{
			ID:         g.SuggestedID,
			TargetPath: fmt.Sprintf("docs/%s.md", g.Topic),
			Archetype:  arch,
			Title:      strings.Title(strings.ReplaceAll(g.Topic, "_", " ")) + " Reference",
			ScopePaths: []string{g.Package + "/**"},
			Sections:   secTitles,
		})
	}
	return suggestions, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Status — dashboard of all managed docs with freshness scores
// ────────────────────────────────────────────────────────────────────────────

// DocStatus is a single row of the `gmb doc status` dashboard.
type DocStatus struct {
	ID          string `json:"id"`
	TargetPath  string `json:"target_path"`
	Freshness   int    `json:"freshness"`
	Mode        string `json:"mode"`
	LastSync    string `json:"last_sync"`
	Status      string `json:"status"`
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
// Snapshot — versioned documentation freeze
// ────────────────────────────────────────────────────────────────────────────

// Snapshot creates a frozen documentation matrix snapshot under docs/versions/<versionTag>.
func Snapshot(repoRoot, versionTag string) (federation.SnapshotResult, error) {
	return federation.SnapshotDocSuite(repoRoot, versionTag)
}

// ────────────────────────────────────────────────────────────────────────────
// Report — documentation debt analytics
// ────────────────────────────────────────────────────────────────────────────

// ReportResult contains health and ROI metrics for the repository docs.
type ReportResult struct {
	TotalDocuments      int      `json:"total_documents"`
	TotalSections       int      `json:"total_sections"`
	GlobalFreshness     int      `json:"global_freshness"`
	CoverageRatio       float64  `json:"coverage_ratio"`
	TotalTokensUsed     int      `json:"total_tokens_used"`
	DriftedDocuments    []string `json:"drifted_documents,omitempty"`
	RottingAreas        []string `json:"rotting_areas,omitempty"`
	EstimatedHoursSaved float64  `json:"estimated_hours_saved,omitempty"`
}

// Report computes overall documentation health, coverage, and debt metrics.
func Report(repoRoot string) (ReportResult, error) {
	dr, err := federation.GenerateDebtReport(repoRoot)
	if err != nil {
		checkRes, cErr := Check(repoRoot, CheckOptions{Verbose: false})
		if cErr != nil {
			return ReportResult{}, err
		}
		var drifted []string
		for _, d := range checkRes.Documents {
			if d.Status != "fresh" {
				drifted = append(drifted, fmt.Sprintf("%s (%d%%)", d.TargetPath, d.Freshness))
			}
		}
		return ReportResult{
			TotalDocuments:   len(checkRes.Documents),
			GlobalFreshness:  checkRes.GlobalFreshness,
			CoverageRatio:    float64(len(checkRes.Documents)) * 25.0,
			DriftedDocuments: drifted,
		}, nil
	}

	var rottingStrs []string
	for _, ra := range dr.RottingAreas {
		rottingStrs = append(rottingStrs, fmt.Sprintf("%s (freshness: %d%%, velocity: %d commits, risk: %d)", ra.Path, ra.Freshness, ra.Velocity, ra.RiskScore))
	}

	return ReportResult{
		TotalDocuments:      dr.TotalDocuments,
		TotalSections:       dr.TotalSections,
		GlobalFreshness:     dr.GlobalFreshness,
		CoverageRatio:       dr.CoverageRatio,
		TotalTokensUsed:     dr.TotalTokensUsed,
		DriftedDocuments:    dr.DriftedDocuments,
		RottingAreas:        rottingStrs,
		EstimatedHoursSaved: dr.EstimatedHoursSaved,
	}, nil
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
// Phase 6 Architectural, SRE, Compliance & Publishing Operations
// ────────────────────────────────────────────────────────────────────────────

// GenerateADR scaffolds an Architecture Decision Record (ADR) file under docs/adr/.
func GenerateADR(repoRoot string, event archfeatures.ADREvent) (string, error) {
	return archfeatures.GenerateADR(repoRoot, event)
}

// GenerateEvolutionChapter generates an architecture time-machine evolution chapter.
func GenerateEvolutionChapter(repoRoot, fromRef, toRef string) (string, error) {
	return archfeatures.GenerateEvolutionChapter(repoRoot, fromRef, toRef)
}

// GenerateDomainGlossary generates the DDD ubiquitous language and domain glossary.
func GenerateDomainGlossary(repoRoot string) (string, error) {
	return archfeatures.GenerateDomainGlossary(repoRoot)
}

// ComputeSubsystemHealth computes cyclomatic complexity, hotspot rank and instability for a package.
func ComputeSubsystemHealth(repoRoot, pkgRel string) sre.HealthProfile {
	return sre.ComputeHealthProfile(repoRoot, pkgRel)
}

// GenerateErrorCatalog extracts sentinel errors and builds the operational triage playbook.
func GenerateErrorCatalog(repoRoot string) (string, error) {
	return sre.GenerateErrorCatalog(repoRoot)
}

// GenerateConcurrencyContracts catalogs mutexes, channels, and thread-safety contracts.
func GenerateConcurrencyContracts(repoRoot string) (string, error) {
	return sre.GenerateConcurrencyContracts(repoRoot)
}

// GenerateTestTopology maps package test suites, test categories, and verification guarantees.
func GenerateTestTopology(repoRoot string) (string, error) {
	return sre.GenerateTestTopology(repoRoot)
}

// GenerateThreatModel generates the security threat model, cryptographic primitives, and egress map.
func GenerateThreatModel(repoRoot string) (string, error) {
	return compliance.GenerateThreatModel(repoRoot)
}

// GenerateConfigDictionary generates the environment variable and runtime configuration reference.
func GenerateConfigDictionary(repoRoot string) (string, error) {
	return compliance.GenerateConfigDictionary(repoRoot)
}

// GenerateSBOM extracts dependencies from go.mod and classifies licenses (SPDX).
func GenerateSBOM(repoRoot string) (string, error) {
	return compliance.GenerateSBOM(repoRoot)
}

// CheckAPISurfaceBoundaries audits public, internal, and private architectural boundaries.
func CheckAPISurfaceBoundaries(repoRoot string) (compliance.BoundaryReport, error) {
	return compliance.CheckAPISurfaceBoundaries(repoRoot)
}

// VerifyCodeSnippets checks code snippets in markdown for syntax validity and symbol drift.
func VerifyCodeSnippets(markdown string, knownSymbols map[string]bool) ([]devex.SnippetError, error) {
	return devex.VerifyCodeSnippets(markdown, knownSymbols)
}

// ExportFederationManifest exports the microservice documentation manifest (.glassmarble/manifest.json).
func ExportFederationManifest(repoRoot string) (string, error) {
	return federation.ExportFederationManifest(repoRoot)
}

// ImportFederationManifest imports a remote service manifest into docs/federation/<service>.md.
func ImportFederationManifest(repoRoot, serviceName, manifestPath string) (string, error) {
	return federation.ImportFederationManifest(repoRoot, serviceName, manifestPath)
}

// GenerateI18nInventory inventories internationalization string keys across packages.
func GenerateI18nInventory(repoRoot string) (string, error) {
	return federation.GenerateI18nInventory(repoRoot)
}

// FormatForPlatform converts admonition blocks between documentation site generators.
func FormatForPlatform(markdown, platform string) string {
	return federation.FormatForPlatform(markdown, platform)
}

func truncateLine(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
