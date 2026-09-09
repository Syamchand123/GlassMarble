package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	doc_engine "github.com/Syamchand123/GlassMarble/internal/doc_engine"
	docconfig "github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/programs/doc_view"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

// ────────────────────────────────────────────────────────────────────────────
// gmb doc — root command
// ────────────────────────────────────────────────────────────────────────────

var docCmd = &cobra.Command{
	Use:     "doc",
	GroupID: GroupAI.ID,
	Short:   "Documentation Intelligence Engine — generate and maintain living docs",
	Long: `The GlassMarble Documentation Intelligence Engine maintains living markdown
documents that are grounded in the Architecture Knowledge Graph (AKG).

95% of intelligence is deterministic (AKG, AST, commit reasoning).
The LLM is used only for converting structured facts into readable prose (5%).

Documents are section-targeted and delta-driven: only sections whose AKG
subgraph changed since the last run are ever re-rendered. Everything else
produces zero git diff.`,
	Example: `  # Update all dirty managed documents for HEAD
  gmb doc

  # Scaffold a new living document
  gmb doc init docs/auth.md

  # CI freshness check (exits 1 if drift detected)
  gmb doc check

  # Preview what would change without writing
  gmb doc diff docs/auth.md

  # Dashboard of all managed docs with freshness scores
  gmb doc status

  # Identify documentation gaps from gmb ai query history
  gmb doc gaps

  # Generate migration guide between two git refs
  gmb doc release v1.1.0..v1.2.0

  # Documentation debt and ROI analytics
  gmb doc report

  # Export RAG-ready chunked knowledge base
  gmb doc export --format rag`,
	// doc without a subcommand runs the update pipeline.
	RunE: runDocUpdate,
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc (no subcommand) — update all dirty sections
// ────────────────────────────────────────────────────────────────────────────

func runDocUpdate(cmd *cobra.Command, args []string) error {
	targetDir := resolveDir(cmd)
	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("doc: %w", err)
	}

	commitHash, _ := cmd.Flags().GetString("commit")
	verbose, _ := cmd.Flags().GetBool("verbose")
	noLLM, _ := cmd.Flags().GetBool("no-llm")
	force, _ := cmd.Flags().GetBool("force")
	docID, _ := cmd.Flags().GetString("doc")
	tag, _ := cmd.Flags().GetString("tag")
	asJSON, _ := cmd.Flags().GetBool("json")
	branchPolicy, _ := cmd.Flags().GetString("branch-policy")

	opts := doc_engine.RunOptions{
		CommitHash:   commitHash,
		Verbose:      verbose,
		NoLLM:        noLLM,
		Force:        force,
		DocID:        docID,
		Tag:          tag,
		BranchPolicy: branchPolicy,
		Out:          cmd.ErrOrStderr(),
	}

	result := doc_engine.Run(absDir, opts)

	if asJSON {
		type docRunJSON struct {
			Commit            string   `json:"commit"`
			DocsUpdated       int      `json:"docs_updated"`
			SectionsProcessed int      `json:"sections_processed"`
			SectionsUpdated   int      `json:"sections_updated"`
			TokensUsed        int      `json:"tokens_used"`
			DurationMs        int64    `json:"duration_ms"`
			Warnings          []string `json:"warnings,omitempty"`
			Error             string   `json:"error,omitempty"`
		}
		out := docRunJSON{
			Commit:            result.Commit,
			DocsUpdated:       result.DocsUpdated,
			SectionsProcessed: result.SectionsProcessed,
			SectionsUpdated:   result.SectionsUpdated,
			TokensUsed:        result.TokensUsed,
			DurationMs:        result.Duration.Milliseconds(),
			Warnings:          result.Warnings,
		}
		if result.Err != nil {
			out.Error = result.Err.Error()
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	if result.Err != nil {
		docWarnf(cmd, "doc: warning: %v\n", result.Err)
	}
	for _, w := range result.Warnings {
		docWarnf(cmd, "doc: warning: %s\n", w)
	}
	if result.SectionsUpdated > 0 || verbose {
		docPrintf(cmd, "doc: %d section(s) updated in %d document(s) | %d tokens | %s\n",
			result.SectionsUpdated, result.DocsUpdated, result.TokensUsed, result.Duration.Round(1e6))
	}
	return nil
}

func docPrintf(cmd *cobra.Command, format string, a ...any) {
	if tui.Quiet() {
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), format, a...)
}

func docWarnf(cmd *cobra.Command, format string, a ...any) {
	if tui.Quiet() {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), format, a...)
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc init — scaffold a new managed document
// ────────────────────────────────────────────────────────────────────────────

var docInitCmd = &cobra.Command{
	Use:   "init <target.md>",
	Short: "Scaffold a new managed document with AKG-grounded sections",
	Long: `Scaffolds a new living document and adds its specification to
.glassmarble/docs.yaml.

Runs an interactive 5-step questionnaire to configure:
  1. Scope (code paths and entry points)
  2. Audience (shapes LLM tone and depth)
  3. Archetype (built-in section template)
  4. Living diagrams (auto-detected entry points)
  5. Section boundaries (managed vs. human-only)`,
	Example: `  # Scaffold a new module reference
  gmb doc init docs/auth.md

  # Scaffold with a specific archetype
  gmb doc init docs/runbook.md --archetype runbook

  # Scaffold with a pre-set scope (non-interactive)
  gmb doc init docs/storage.md --scope "internal/storage/**" --archetype module`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("doc init: %w", err)
		}

		archetype, _ := cmd.Flags().GetString("archetype")
		scope, _ := cmd.Flags().GetStringArray("scope")
		title, _ := cmd.Flags().GetString("title")
		purpose, _ := cmd.Flags().GetString("purpose")
		audience, _ := cmd.Flags().GetString("audience")
		id, _ := cmd.Flags().GetString("id")

		opts := doc_engine.InitOptions{
			TargetPath:  args[0],
			Archetype:   archetype,
			ScopePaths:  scope,
			Title:       title,
			Purpose:     purpose,
			Audience:    audience,
			DocID:       id,
			Interactive: len(scope) == 0,
			Out:         cmd.OutOrStdout(),
		}

		return doc_engine.Init(absDir, opts)
	},
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc check — CI freshness gate
// ────────────────────────────────────────────────────────────────────────────

var docCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check documentation freshness and drift (CI gate)",
	Long: `Audits all managed documents for drift and freshness without modifying any files.

Exit codes:
  0  All documents are fresh (at or above their freshness thresholds)
  1  One or more documents are drifted (below MinFreshnessThreshold)
  2  One or more documents are stale (below MinFreshnessFail)`,
	Example: `  # Check all docs
  gmb doc check

  # Machine-readable output for CI
  gmb doc check --json

  # Check a specific document
  gmb doc check --doc auth`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("doc check: %w", err)
		}

		verbose, _ := cmd.Flags().GetBool("verbose")
		asJSON, _ := cmd.Flags().GetBool("json")
		docID, _ := cmd.Flags().GetString("doc")
		tag, _ := cmd.Flags().GetString("tag")

		opts := doc_engine.CheckOptions{
			Verbose: verbose,
			JSON:    asJSON,
			DocID:   docID,
			Tag:     tag,
			Out:     cmd.OutOrStdout(),
		}

		result, err := doc_engine.Check(absDir, opts)
		if err != nil {
			return err
		}

		if asJSON {
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
		} else {
			printCheckResult(cmd, result)
		}

		// Exit code contract.
		if len(result.Failures) > 0 {
			return fmt.Errorf("documentation drift detected: %d document(s) below fail threshold", len(result.Failures))
		}

		verifySnippets, _ := cmd.Flags().GetBool("verify-snippets")
		if verifySnippets {
			snippetErrors := 0
			cfg, _ := docconfig.LoadDocsConfig(absDir)
			if cfg != nil {
				for _, doc := range cfg.Documents {
					absPath := filepath.Join(absDir, doc.TargetPath)
					data, err := os.ReadFile(absPath)
					if err != nil {
						continue
					}
					errs, _ := doc_engine.VerifyCodeSnippets(string(data), nil)
					for _, se := range errs {
						snippetErrors++
						docPrintf(cmd, "  SNIPPET ERROR [%s: line %d]: %s\n", doc.TargetPath, se.LineNumber, se.ErrorMessage)
					}
				}
			}
			if snippetErrors > 0 {
				return fmt.Errorf("snippet verification failed: %d broken code snippet(s)", snippetErrors)
			}
		}
		return nil
	},
}

func printCheckResult(cmd *cobra.Command, result doc_engine.CheckResult) {
	if result.AllFresh {
		docPrintf(cmd, "doc check: all %d document(s) fresh (global freshness: %d%%)\n",
			len(result.Documents), result.GlobalFreshness)
		return
	}
	docPrintf(cmd, "doc check: global freshness %d%% | %d warning(s) | %d failure(s)\n",
		result.GlobalFreshness, len(result.Warnings), len(result.Failures))
	for _, d := range result.Documents {
		icon := "✓"
		if d.Status == "warn" {
			icon = "⚠"
		} else if d.Status == "stale" || d.Status == "missing" {
			icon = "✗"
		}
		docPrintf(cmd, "  %s %-45s  %3d%%  %s\n", icon, d.TargetPath, d.Freshness, d.Status)
	}
	for _, f := range result.Failures {
		docPrintf(cmd, "  FAIL: %s\n", f)
	}
	for _, w := range result.Warnings {
		docPrintf(cmd, "  WARN: %s\n", w)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc diff — preview changes without writing
// ────────────────────────────────────────────────────────────────────────────

var docDiffCmd = &cobra.Command{
	Use:   "diff [target.md]",
	Short: "Preview what the engine would change without writing files",
	Long: `Computes what sections would be updated and shows the pending diff
without touching any files on disk.

Exit codes:
  0  No changes pending
  1  Changes would be made`,
	Example: `  gmb doc diff docs/auth.md
  gmb doc diff --doc auth`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("doc diff: %w", err)
		}

		docID, _ := cmd.Flags().GetString("doc")
		tag, _ := cmd.Flags().GetString("tag")
		asJSON, _ := cmd.Flags().GetBool("json")

		if len(args) > 0 && docID == "" {
			docID = args[0]
		}

		cfg, err := docconfig.LoadDocsConfig(absDir)
		if err != nil {
			return fmt.Errorf("doc diff: %w", err)
		}
		if len(cfg.Documents) == 0 {
			if asJSON {
				fmt.Fprintln(cmd.OutOrStdout(), `{"has_changes":false,"sections":[]}`)
				return nil
			}
			docPrintf(cmd, "doc diff: 0 document(s) configured; run 'gmb doc init' to create one\n")
			return nil
		}

		diffRes, err := doc_engine.Diff(absDir, doc_engine.RunOptions{
			DocID: docID,
			Tag:   tag,
		})
		if err != nil {
			return err
		}

		if asJSON {
			data, _ := json.MarshalIndent(diffRes, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}

		if !diffRes.HasChanges {
			docPrintf(cmd, "doc diff: all documents are up-to-date (no changes pending)\n")
			return nil
		}

		docPrintf(cmd, "doc diff: pending documentation changes:\n\n")
		for _, sec := range diffRes.Sections {
			if sec.HasChange {
				docPrintf(cmd, "--- %s [%s] ---\n%s\n\n", sec.TargetPath, sec.SectionID, sec.DiffPreview)
			}
		}
		return nil
	},
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc status — freshness dashboard
// ────────────────────────────────────────────────────────────────────────────

var docStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show freshness dashboard for all managed documents",
	Long: `Displays a table of all managed documents with their freshness scores,
last sync commit, and render mode.

Freshness colour coding:
  ✓ green  Document is fully in sync with HEAD
  ⚠ yellow Freshness is below the warn threshold
  ✗ red    Freshness is below the fail threshold (CI would fail)`,
	Example: `  gmb doc status
  gmb doc status --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("doc status: %w", err)
		}

		asJSON, _ := cmd.Flags().GetBool("json")
		docID, _ := cmd.Flags().GetString("doc")
		tag, _ := cmd.Flags().GetString("tag")

		result, err := doc_engine.Check(absDir, doc_engine.CheckOptions{
			Verbose: true,
			JSON:    asJSON,
			DocID:   docID,
			Tag:     tag,
			Out:     cmd.OutOrStdout(),
		})
		if err != nil {
			return err
		}

		if asJSON {
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}

		statusView := views.RenderDocStatus(views.DocStatusData{
			GlobalFreshness: result.GlobalFreshness,
			AllFresh:        result.AllFresh,
			Documents:       result.Documents,
			Warnings:        result.Warnings,
			Failures:        result.Failures,
		})
		fmt.Fprintln(cmd.OutOrStdout(), statusView)
		return nil
	},
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc gaps — documentation gap discovery
// ────────────────────────────────────────────────────────────────────────────

var docGapsCmd = &cobra.Command{
	Use:   "gaps",
	Short: "List documentation gaps discovered from gmb ai query history",
	Long: `When gmb ai answers a question by querying the AKG directly (rather than
from a managed document), it records a documentation gap.

This command lists all recorded gaps, sorted by query frequency.
Use 'gmb doc suggest' to draft sections for the top gaps.`,
	Example: `  gmb doc gaps
  gmb doc gaps --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("doc gaps: %w", err)
		}

		asJSON, _ := cmd.Flags().GetBool("json")
		gapsRes, err := doc_engine.Gaps(absDir)
		if err != nil {
			return err
		}

		if asJSON {
			data, _ := json.MarshalIndent(gapsRes, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}

		if len(gapsRes.Gaps) == 0 {
			docPrintf(cmd, "doc gaps: no documentation gaps discovered\n")
			return nil
		}

		docPrintf(cmd, "doc gaps: %d documentation gap(s) identified:\n\n", len(gapsRes.Gaps))
		docPrintf(cmd, "  %-25s %-25s %s\n", "TOPIC", "PACKAGE", "DESCRIPTION")
		docPrintf(cmd, "  ----------------------------------------------------------------------\n")
		for _, g := range gapsRes.Gaps {
			docPrintf(cmd, "  %-25s %-25s %s\n", g.Topic, g.Package, g.Description)
		}
		docPrintf(cmd, "\nRun 'gmb doc suggest' to draft specifications for these gaps.\n")
		return nil
	},
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc suggest — draft doc additions for gaps
// ────────────────────────────────────────────────────────────────────────────

var docSuggestCmd = &cobra.Command{
	Use:   "suggest",
	Short: "Draft document specifications for identified documentation gaps",
	Long:  `Suggests new document specifications and section layouts based on undocumented code surfaces.`,
	Example: `  gmb doc suggest
  gmb doc suggest --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("doc suggest: %w", err)
		}

		asJSON, _ := cmd.Flags().GetBool("json")
		suggestions, err := doc_engine.Suggest(absDir)
		if err != nil {
			return err
		}

		if suggestions == nil {
			suggestions = []doc_engine.SuggestedDoc{}
		}

		if asJSON {
			data, _ := json.MarshalIndent(suggestions, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}

		if len(suggestions) == 0 {
			docPrintf(cmd, "doc suggest: no documentation additions suggested\n")
			return nil
		}

		docPrintf(cmd, "doc suggest: %d recommended document addition(s):\n\n", len(suggestions))
		for _, s := range suggestions {
			docPrintf(cmd, "  Target:     %s\n", s.TargetPath)
			docPrintf(cmd, "  Title:      %s\n", s.Title)
			docPrintf(cmd, "  Archetype:  %s\n", s.Archetype)
			docPrintf(cmd, "  Scope:      %s\n", strings.Join(s.ScopePaths, ", "))
			docPrintf(cmd, "  Sections:   %s\n", strings.Join(s.Sections, ", "))
			docPrintf(cmd, "  Command:    gmb doc init %s --archetype %s\n\n", s.TargetPath, s.Archetype)
		}
		return nil
	},
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc release — migration guide generator
// ────────────────────────────────────────────────────────────────────────────

var docReleaseCmd = &cobra.Command{
	Use:   "release <ref1>..<ref2>",
	Short: "Generate a migration guide between two git refs",
	Long: `Computes the public API breaking changes between two git refs using
AKG symbol diffing and generates a migration guide markdown file.

The output includes:
  - Deleted exported functions
  - Changed function signatures (before/after)
  - Renamed struct fields or config vars
  - Auto-generated migration code examples`,
	Example: `  gmb doc release v1.1.0..v1.2.0
  gmb doc release v1.1.0..HEAD --out docs/migrations/v1.1-to-v1.2.md`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("doc release: %w", err)
		}

		parts := strings.Split(args[0], "..")
		if len(parts) != 2 {
			return fmt.Errorf("doc release: expected <ref1>..<ref2> (e.g. v1.1.0..v1.2.0)")
		}

		outFile, _ := cmd.Flags().GetString("out")
		guide, err := doc_engine.Release(absDir, parts[0], parts[1], outFile)
		if err != nil {
			return err
		}

		if outFile != "" {
			docPrintf(cmd, "doc release: migration guide written to %s\n", outFile)
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), guide)
		}

		snapshot, _ := cmd.Flags().GetBool("snapshot")
		if snapshot {
			snapRes, err := doc_engine.Snapshot(absDir, parts[1])
			if err != nil {
				return fmt.Errorf("doc release snapshot: %w", err)
			}
			docPrintf(cmd, "doc release: snapshot %s created with %d file(s) in %s\n", snapRes.VersionTag, snapRes.FilesCount, snapRes.TargetDir)
		}
		return nil
	},
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc report — documentation debt analytics
// ────────────────────────────────────────────────────────────────────────────

var docReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Generate documentation debt and ROI analytics",
	Long: `Generates a comprehensive documentation health report including:
  - Coverage ratio (% of public interfaces documented)
  - Global freshness score
  - Top-5 rotting areas (high commit velocity, low freshness)
  - Drift velocity trend
  - Token cost summary`,
	Example: `  gmb doc report
  gmb doc report --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("doc report: %w", err)
		}

		asJSON, _ := cmd.Flags().GetBool("json")
		rep, err := doc_engine.Report(absDir)
		if err != nil {
			return err
		}

		if asJSON {
			data, _ := json.MarshalIndent(rep, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}

		docPrintf(cmd, "GlassMarble Documentation Health & Debt Report\n\n")
		docPrintf(cmd, "  Managed Documents:    %d\n", rep.TotalDocuments)
		docPrintf(cmd, "  Managed Sections:     %d\n", rep.TotalSections)
		docPrintf(cmd, "  Global Freshness:     %d%%\n", rep.GlobalFreshness)
		docPrintf(cmd, "  Public Surface Cov:   %.1f%%\n", rep.CoverageRatio)
		docPrintf(cmd, "  Total Token Spend:    %d tokens\n", rep.TotalTokensUsed)
		if len(rep.DriftedDocuments) > 0 {
			docPrintf(cmd, "\n  Drifted Documents:\n")
			for _, d := range rep.DriftedDocuments {
				docPrintf(cmd, "    - %s\n", d)
			}
		}
		return nil
	},
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc export — RAG knowledge base export
// ────────────────────────────────────────────────────────────────────────────

var docExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export managed docs as a RAG-ready chunked knowledge base",
	Long: `Generates pre-chunked, AKG-tagged knowledge chunks under .glassmarble/rag/.

Each chunk is tagged with exact AST symbol IDs, file paths, and line numbers.
Compatible with LlamaIndex, LangChain, and direct embedding APIs.

Formats:
  rag     Section-level chunks with full AKG metadata (default)
  jsonl   One JSON object per chunk in JSONL format`,
	Example: `  gmb doc export --format rag
  gmb doc export --format jsonl --out .glassmarble/rag/`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("doc export: %w", err)
		}

		asJSON, _ := cmd.Flags().GetBool("json")
		format, _ := cmd.Flags().GetString("format")
		outDir, _ := cmd.Flags().GetString("out")

		exp, err := doc_engine.Export(absDir, format, outDir)
		if err != nil {
			return err
		}

		if asJSON {
			data, _ := json.MarshalIndent(exp, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}

		docPrintf(cmd, "doc export: exported %d RAG chunk(s) to %s\n", exp.ChunksCount, exp.OutputDir)
		return nil
	},
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc view — terminal TUI reader
// ────────────────────────────────────────────────────────────────────────────

var docViewCmd = &cobra.Command{
	Use:   "view [target.md]",
	Short: "Open a managed document in the terminal TUI reader",
	Long: `Opens the specified document in an interactive terminal reader powered
by Bubble Tea. Supports fuzzy section search and symbol-to-source navigation
(Enter on a symbol link opens $EDITOR at the exact file:line).`,
	Example: `  gmb doc view docs/auth.md
  gmb doc view --doc auth`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("doc view: %w", err)
		}

		docID, _ := cmd.Flags().GetString("doc")
		target := ""
		if len(args) > 0 {
			target = args[0]
		}

		cfg, err := docconfig.LoadDocsConfig(absDir)
		if err != nil {
			return fmt.Errorf("doc view: %w", err)
		}

		var foundDoc *docconfig.DocSpec
		for i := range cfg.Documents {
			d := &cfg.Documents[i]
			if (docID != "" && d.ID == docID) || (target != "" && (d.TargetPath == target || d.ID == target)) {
				foundDoc = d
				break
			}
		}
		if foundDoc == nil && len(cfg.Documents) > 0 {
			foundDoc = &cfg.Documents[0]
		}
		if foundDoc == nil {
			return fmt.Errorf("doc view: no documents configured in docs.yaml")
		}

		absPath := filepath.Join(absDir, foundDoc.TargetPath)
		contentBytes, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("doc view: reading %s: %w", foundDoc.TargetPath, err)
		}

		if !tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout()) {
			fmt.Fprintln(cmd.OutOrStdout(), string(contentBytes))
			return nil
		}

		return doc_view.Run(doc_view.Config{
			Title:      foundDoc.Title,
			TargetPath: foundDoc.TargetPath,
			Content:    string(contentBytes),
			In:         cmd.InOrStdin(),
			Out:        cmd.OutOrStdout(),
		})
	},
}

// ────────────────────────────────────────────────────────────────────────────
// Registration
// ────────────────────────────────────────────────────────────────────────────

func init() {
	// ── gmb doc (root) flags ──────────────────────────────────────────────
	docCmd.Flags().String("commit", "", "Commit hash to process (default: HEAD)")
	docCmd.Flags().Bool("no-llm", false, "Use deterministic renderer only (offline mode)")
	docCmd.Flags().Bool("force", false, "Bypass fast-bail and reprocess all documents")
	docCmd.Flags().String("doc", "", "Only process the document with this ID")
	docCmd.Flags().String("tag", "", "Only process documents with this tag")
	docCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	docCmd.Flags().Bool("bg", false, "Run in background (non-blocking post-commit mode)")
	docCmd.Flags().String("branch-policy", "main-only", "When to write: main-only|any|tag-only")

	// ── gmb doc init flags ────────────────────────────────────────────────
	docInitCmd.Flags().String("archetype", "", "Built-in document template: architecture|module|runbook|onboarding|migration|adr|api|security|database|config")
	docInitCmd.Flags().StringArray("scope", nil, "Glob patterns for this document's scope (e.g. 'internal/auth/**')")
	docInitCmd.Flags().String("title", "", "Document title")
	docInitCmd.Flags().String("purpose", "", "One-sentence description of what the document is for")
	docInitCmd.Flags().String("audience", "", "Intended reader (e.g. 'On-call SRE engineers')")
	docInitCmd.Flags().String("id", "", "Stable document ID (default: derived from target path)")

	// ── gmb doc check flags ───────────────────────────────────────────────
	docCheckCmd.Flags().String("doc", "", "Only check the document with this ID")
	docCheckCmd.Flags().String("tag", "", "Only check documents with this tag")
	docCheckCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	docCheckCmd.Flags().Bool("verify-snippets", false, "Verify executable code snippets in managed docs")

	// ── gmb doc diff flags ────────────────────────────────────────────────
	docDiffCmd.Flags().String("doc", "", "Only diff the document with this ID")
	docDiffCmd.Flags().String("tag", "", "Only diff documents with this tag")
	docDiffCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")

	// ── gmb doc status flags ──────────────────────────────────────────────
	docStatusCmd.Flags().String("doc", "", "Only check document with this ID")
	docStatusCmd.Flags().String("tag", "", "Only check documents with this tag")
	docStatusCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")

	// ── gmb doc gaps flags ────────────────────────────────────────────────
	docGapsCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")

	// ── gmb doc suggest flags ─────────────────────────────────────────────
	docSuggestCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")

	// ── gmb doc release flags ─────────────────────────────────────────────
	docReleaseCmd.Flags().String("out", "", "Output file path for the migration guide")
	docReleaseCmd.Flags().Bool("snapshot", false, "Freeze the current doc suite as a versioned snapshot")

	// ── gmb doc report flags ──────────────────────────────────────────────
	docReportCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")

	// ── gmb doc export flags ──────────────────────────────────────────────
	docExportCmd.Flags().String("format", "rag", "Export format: rag|jsonl")
	docExportCmd.Flags().String("out", ".glassmarble/rag", "Output directory")
	docExportCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")

	// ── gmb doc view flags ────────────────────────────────────────────────
	docViewCmd.Flags().String("doc", "", "Document ID to open")

	// ── Register subcommands ──────────────────────────────────────────────
	docCmd.AddCommand(
		docInitCmd,
		docCheckCmd,
		docDiffCmd,
		docStatusCmd,
		docGapsCmd,
		docSuggestCmd,
		docReleaseCmd,
		docReportCmd,
		docExportCmd,
		docViewCmd,
	)

	// ── Register gmb doc with root ────────────────────────────────────────
	rootCmd.AddCommand(docCmd)
}
