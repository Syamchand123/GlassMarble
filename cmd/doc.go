package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"syscall"

	doc_engine "github.com/Syamchand123/GlassMarble/internal/doc_engine"
	docconfig "github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/grounding/langmatrix"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/ledger"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/review"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/verifier"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/programs/doc_view"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

// docExit exits the process with the given code. It is a variable (not a
// direct os.Exit call) so the Section 11 exit-code contract is unit
// testable: internal tests stub it to capture the code instead of exiting.
var docExit = os.Exit

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

  # Generate migration guide between two git refs
  gmb doc release v1.1.0..v1.2.0

  # Export RAG-ready chunked knowledge base
  gmb doc export --format rag`,
	// doc without a subcommand runs the update pipeline.
	Args: cobra.NoArgs,
	RunE: runDocUpdate,
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc (no subcommand) — update all dirty sections
// ────────────────────────────────────────────────────────────────────────────

func runDocUpdate(cmd *cobra.Command, args []string) error {
	// F4: real background mode — re-execute detached without --bg.
	bg, _ := cmd.Flags().GetBool("bg")
	if bg {
		if spawnErr := spawnDocBackground(cmd); spawnErr != nil {
			docWarnf(cmd, "doc: warning: background spawn failed (%v); continuing in foreground\n", spawnErr)
		} else {
			return nil
		}
	}

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
	forceWrite, _ := cmd.Flags().GetBool("write")

	opts := doc_engine.RunOptions{
		CommitHash:   commitHash,
		Verbose:      verbose,
		NoLLM:        noLLM,
		Force:        force,
		DocID:        docID,
		Tag:          tag,
		BranchPolicy: branchPolicy,
		ForceWrite:   forceWrite,
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
// F4: real background execution (`gmb doc --bg`)
// ────────────────────────────────────────────────────────────────────────────

// spawnDocBackground re-executes the current binary with the same arguments
// minus --bg as a detached background process and reports its pid.
// It returns nil after starting the child (the caller must return without
// waiting). On any error it returns non-nil so the caller can fall back to
// synchronous execution (non-fatal).
func spawnDocBackground(cmd *cobra.Command) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable: %w", err)
	}
	lowered := strings.ToLower(exe)
	if strings.Contains(lowered, "go-build") || strings.HasSuffix(lowered, ".exe.tmp") || strings.HasSuffix(lowered, ".tmp") {
		docWarnf(cmd, "doc: warning: --bg under `go run` uses a temporary executable that the toolchain may clean up; build the binary first for a persistent background worker\n")
	}

	// Drop every --bg form so the child runs in the foreground exactly once.
	var childArgs []string
	for _, a := range os.Args[1:] {
		if a == "--bg" || strings.HasPrefix(a, "--bg=") {
			continue
		}
		childArgs = append(childArgs, a)
	}

	child := exec.Command(exe, childArgs...)
	child.Env = os.Environ()
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	child.SysProcAttr = detachSysProcAttr()
	if err := child.Start(); err != nil {
		return fmt.Errorf("starting detached process: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "doc engine running in background (pid %d)\n", child.Process.Pid)
	return nil
}

// detachSysProcAttr returns process attributes that detach the child:
// on Windows the window is hidden and the process is created detached
// (HideWindow + DETACHED_PROCESS 0x00000008); on unix the child becomes a
// session leader (Setsid). The fields are set via reflection because
// syscall.SysProcAttr is OS-specific and this file must compile on every
// GOOS; the branch is selected at runtime via runtime.GOOS.
func detachSysProcAttr() *syscall.SysProcAttr {
	attr := &syscall.SysProcAttr{}
	v := reflect.ValueOf(attr).Elem()
	if runtime.GOOS == "windows" {
		if f := v.FieldByName("HideWindow"); f.IsValid() && f.CanSet() && f.Kind() == reflect.Bool {
			f.SetBool(true)
		}
		if f := v.FieldByName("CreationFlags"); f.IsValid() && f.CanSet() {
			f.SetUint(0x00000008) // DETACHED_PROCESS
		}
	} else {
		if f := v.FieldByName("Setsid"); f.IsValid() && f.CanSet() && f.Kind() == reflect.Bool {
			f.SetBool(true)
		}
	}
	return attr
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
	Args:  cobra.NoArgs,
	Long: `Audits all managed documents for drift and freshness without modifying any files.

With --check-external, a second pass verifies external http(s) links via
HEAD requests (internal reference checks never touch the network).
Unreachable hosts and network errors are skipped silently (offline-safe);
only completed HTTP 4xx/5xx responses are reported as failures.

Exit codes (plan Section 11):
  0  All documents are fresh (warnings allowed)
  1  Drift detected (one or more failures, including doc-lint asserts)
  2  Hard error (config or state could not be loaded)`,
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

		// --fix runs BEFORE the audit so Check evaluates fixed files.
		// Without it a drifted doc would return before fixes apply.
		fixSnippets, _ := cmd.Flags().GetBool("fix")
		verifySnippets, _ := cmd.Flags().GetBool("verify-snippets")
		if fixSnippets {
			if err := applyCheckFixes(cmd, absDir, verifySnippets); err != nil {
				return err
			}
		}

		result, err := doc_engine.Check(absDir, opts)
		if err != nil {
			// Exit-code contract (plan Section 11): hard errors
			// (config/state load) are exit 2.
			fmt.Fprintf(cmd.ErrOrStderr(), "doc check: %v\n", err)
			docExit(2)
		}

		// --check-external runs a SECOND pass after Check (doc_engine.go is
		// untouched): verifier.CheckReferences over each managed doc with
		// external checking enabled. Only external targets are reported;
		// internal references were already covered by Check above.
		if checkExternal, _ := cmd.Flags().GetBool("check-external"); checkExternal {
			for _, f := range checkExternalRefs(absDir, docID, tag) {
				result.AllFresh = false
				result.Failures = append(result.Failures, f)
			}
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

		verifySnippets, _ = cmd.Flags().GetBool("verify-snippets")
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
					// P14: repo-aware arity verification (func signatures from
					// the working tree), not just syntax + name presence.
					// Fixes (if --fix) already applied pre-audit above; this
					// pass is report-only.
					errs, _ := doc_engine.VerifySnippetsInRepo(absDir, string(data), nil)
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

// applyCheckFixes applies explicit --fix repairs before the audit runs:
// snippet fixes (only with --verify-snippets) and TOC regeneration
// (always; no marker → no-op per document).
func applyCheckFixes(cmd *cobra.Command, absDir string, verifySnippets bool) error {
	cfg, _ := docconfig.LoadDocsConfig(absDir)
	if cfg == nil {
		return nil
	}
	for _, doc := range cfg.Documents {
		absPath := filepath.Join(absDir, doc.TargetPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		if verifySnippets {
			errs, _ := doc_engine.VerifySnippetsInRepo(absDir, string(data), nil)
			if len(errs) > 0 {
				fixed := doc_engine.ApplySnippetFixes(string(data), errs)
				if fixed != string(data) {
					if wErr := os.WriteFile(absPath, []byte(fixed), 0644); wErr != nil {
						return fmt.Errorf("snippet fix failed for %s: %w", doc.TargetPath, wErr)
					}
					docPrintf(cmd, "  SNIPPET FIXED [%s]: applied %d deterministic fix(es)\n", doc.TargetPath, len(errs))
					data = []byte(fixed)
				}
			}
		}
		// C2: TOC regeneration (no marker → no-op).
		if regenerated := doc_engine.RegenerateTOC(string(data)); regenerated != string(data) {
			if wErr := os.WriteFile(absPath, []byte(regenerated), 0644); wErr != nil {
				return fmt.Errorf("toc regeneration failed for %s: %w", doc.TargetPath, wErr)
			}
			docPrintf(cmd, "  TOC REGENERATED [%s]\n", doc.TargetPath)
		}
	}
	return nil
}

// checkExternalRefs is the --check-external second pass: it runs
// verifier.CheckReferences with external checking enabled over every managed
// document (honouring the same --doc/--tag filter as Check) and returns one
// failure string per broken EXTERNAL link. Internal references are excluded
// (Check already covers them). Unreachable hosts and network errors are
// skipped silently by the verifier, so offline runs never fail here — only
// completed HTTP 4xx/5xx responses surface.
func checkExternalRefs(absDir, docID, tag string) []string {
	cfg, err := docconfig.LoadDocsConfig(absDir)
	if err != nil || cfg == nil {
		return nil
	}
	var failures []string
	for _, doc := range cfg.Documents {
		if docID != "" && doc.ID != docID {
			continue
		}
		if tag != "" {
			matched := false
			for _, t := range doc.Tags {
				if t == tag {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		data, err := os.ReadFile(filepath.Join(absDir, doc.TargetPath))
		if err != nil {
			continue
		}
		rep := verifier.CheckReferences(absDir, doc.TargetPath, string(data), nil, true)
		for _, br := range rep.Broken {
			if !isExternalTarget(br.Target) {
				continue
			}
			failures = append(failures, fmt.Sprintf("%s: broken external reference (line %d): %s — %s",
				doc.TargetPath, br.Line, br.Target, br.Reason))
		}
	}
	return failures
}

// isExternalTarget reports whether a broken-reference target is an
// external http(s) URL (the only class the --check-external pass reports).
func isExternalTarget(target string) bool {
	l := strings.ToLower(strings.TrimSpace(target))
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
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
			// Exit-code contract (plan Section 11): pending changes are exit 1.
			if diffRes.HasChanges {
				docExit(1)
			}
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
		// Exit-code contract (plan Section 11): pending changes are exit 1.
		docExit(1)
		return nil
	},
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc status — freshness dashboard
// ────────────────────────────────────────────────────────────────────────────

var docStatusCmd = &cobra.Command{
	Use:   "status",
	Args:  cobra.NoArgs,
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
		return nil
	},
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc export — RAG knowledge base export
// ────────────────────────────────────────────────────────────────────────────

var docExportCmd = &cobra.Command{
	Use:   "export",
	Args:  cobra.NoArgs,
	Short: "Export managed docs as a RAG-ready chunked knowledge base",
	Long: `Generates pre-chunked, AKG-tagged knowledge chunks under .glassmarble/rag/.

Each chunk is tagged with exact AST symbol IDs, file paths, and line numbers.
Compatible with LlamaIndex, LangChain, and direct embedding APIs.

Formats:
  rag     Section-level chunks with full AKG metadata (default)
  jsonl   One JSON object per chunk in JSONL format
  state   Raw docs_state.json dump (engine state, for interchange tooling)`,
	Example: `  gmb doc export --format rag
  gmb doc export --format jsonl --out .glassmarble/rag/
  gmb doc export --format state
  gmb doc export --format state --out .glassmarble/state.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("doc export: %w", err)
		}

		asJSON, _ := cmd.Flags().GetBool("json")
		format, _ := cmd.Flags().GetString("format")
		outDir, _ := cmd.Flags().GetString("out")

		if format == "state" {
			sm := storage.NewStateManager(docconfig.StorageDirPath(absDir))
			data, err := storage.ExportStateJSON(sm)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("out") {
				target := outDir
				if fi, statErr := os.Stat(target); statErr == nil && fi.IsDir() {
					target = filepath.Join(target, "state.json")
				} else if strings.HasSuffix(strings.ToLower(target), ".glassmarble/rag") || (!strings.Contains(filepath.Base(target), ".")) {
					// A directory-style --out (no file extension): land
					// state.json inside it, mirroring the rag/jsonl writers.
					target = filepath.Join(target, "state.json")
				}
				if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					return fmt.Errorf("doc export: creating output dir: %w", err)
				}
				if err := os.WriteFile(target, append(data, '\n'), 0644); err != nil {
					return fmt.Errorf("doc export: writing state: %w", err)
				}
				if asJSON {
					fmt.Fprintln(cmd.OutOrStdout(), string(data))
					return nil
				}
				docPrintf(cmd, "doc export: exported state to %s\n", target)
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}

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

// sortedKeysFloat returns map keys in ascending order for deterministic CLI output.
func sortedKeysFloat(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc eval — D1 faithfulness scoring
// ────────────────────────────────────────────────────────────────────────────

var docEvalCmd = &cobra.Command{
	Use:   "eval",
	Short: "Score managed sections for faithfulness against fresh FactSheets",
	Args:  cobra.NoArgs,
	Long: `Scores every non-empty managed section against a freshly assembled
FactSheet using the deterministic faithfulness scorer (no network, no AKG
required). Score = supported claims / total claims (RAGAS formula).

Exit codes:
  0  Global score at or above --min-score
  1  Global score below --min-score`,
	Example: `  gmb doc eval
  gmb doc eval --sample 20 --min-score 0.9 --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("doc eval: %w", err)
		}
		sampleN, _ := cmd.Flags().GetInt("sample")
		minScore, _ := cmd.Flags().GetFloat64("min-score")
		asJSON, _ := cmd.Flags().GetBool("json")

		result, err := doc_engine.EvalFaithfulness(absDir, sampleN)
		if err != nil {
			return err
		}
		if asJSON {
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
		} else {
			docPrintf(cmd, "doc eval: global faithfulness %.2f across %d section(s)\n", result.GlobalScore, result.Samples)
			for _, d := range result.Docs {
				docPrintf(cmd, "  %-45s  %.2f  (%d sections)\n", d.TargetPath, d.Score, d.Sections)
				for _, u := range d.Unsupported {
					docPrintf(cmd, "   unsupported: %s\n", u)
				}
			}
			if len(result.ArchetypeScores) > 0 {
				docPrintf(cmd, "  per-archetype:\n")
				for _, arch := range sortedKeysFloat(result.ArchetypeScores) {
					docPrintf(cmd, "    %-20s  %.2f\n", arch, result.ArchetypeScores[arch])
				}
			}
		}
		if result.GlobalScore < minScore {
			return fmt.Errorf("faithfulness %.2f below minimum %.2f", result.GlobalScore, minScore)
		}
		return nil
	},
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc ledger — D5 run-ledger rollup
// ────────────────────────────────────────────────────────────────────────────

var docLedgerCmd = &cobra.Command{
	Use:   "ledger",
	Short: "Show the structured run ledger (tokens, tracks, repairs, freshness)",
	Args:  cobra.NoArgs,
	Long: `Prints the D5 observability rollup over recent doc-engine runs:
total tokens, per-track render counts, repairs, fallbacks, and freshness.
The ledger is append-only JSONL under .glassmarble/runs/.

With --alerts, the rollup is evaluated against alert thresholds
(--max-tokens, --min-freshness, --max-fallbacks; 0 disables that dimension)
via ledger.CheckAlerts and every breach is printed.

Exit codes:
  0  Rollup printed and (with --alerts) no alert fired
  1  One or more alerts fired (--alerts only)`,
	Example: `  gmb doc ledger
  gmb doc ledger --last 20 --json
  gmb doc ledger --alerts --max-tokens 50000 --min-freshness 70 --max-fallbacks 10`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("doc ledger: %w", err)
		}
		lastN, _ := cmd.Flags().GetInt("last")
		asJSON, _ := cmd.Flags().GetBool("json")
		wantAlerts, _ := cmd.Flags().GetBool("alerts")

		summary, err := doc_engine.LedgerSummary(absDir, lastN)
		if err != nil {
			return err
		}

		var alerts []string
		if wantAlerts {
			maxTokens, _ := cmd.Flags().GetInt("max-tokens")
			minFreshness, _ := cmd.Flags().GetFloat64("min-freshness")
			maxFallbacks, _ := cmd.Flags().GetInt("max-fallbacks")
			alerts = ledger.CheckAlerts(summary, ledger.AlertThresholds{
				MaxTokensPerRun: maxTokens,
				MinFreshness:    minFreshness,
				MaxFallbacks:    maxFallbacks,
			})
		}

		if asJSON {
			if wantAlerts {
				data, _ := json.MarshalIndent(struct {
					Runs                 int            `json:"runs"`
					TotalTokens          int            `json:"total_tokens"`
					AvgDurationMs        float64        `json:"avg_duration_ms"`
					TotalDocsUpdated     int            `json:"total_docs_updated"`
					TotalSectionsUpdated int            `json:"total_sections_updated"`
					TotalRepairs         int            `json:"total_repairs"`
					TotalFallbacks       int            `json:"total_fallbacks"`
					TracksUsed           map[string]int `json:"tracks_used"`
					AvgFreshness         float64        `json:"avg_freshness"`
					FirstRun             string         `json:"first_run"`
					LastRun              string         `json:"last_run"`
					Alerts               []string       `json:"alerts"`
				}{
					Runs:                 summary.Runs,
					TotalTokens:          summary.TotalTokens,
					AvgDurationMs:        summary.AvgDurationMs,
					TotalDocsUpdated:     summary.TotalDocsUpdated,
					TotalSectionsUpdated: summary.TotalSectionsUpdated,
					TotalRepairs:         summary.TotalRepairs,
					TotalFallbacks:       summary.TotalFallbacks,
					TracksUsed:           summary.TracksUsed,
					AvgFreshness:         summary.AvgFreshness,
					FirstRun:             summary.FirstRun,
					LastRun:              summary.LastRun,
					Alerts:               alerts,
				}, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(data))
			} else {
				data, _ := json.MarshalIndent(summary, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(data))
			}
		} else {
			docPrintf(cmd, "doc ledger: %d run(s) | %d tokens | %d docs updated | %d sections | %d repairs | %d fallbacks | freshness %.1f%%\n",
				summary.Runs, summary.TotalTokens, summary.TotalDocsUpdated, summary.TotalSectionsUpdated,
				summary.TotalRepairs, summary.TotalFallbacks, summary.AvgFreshness)
			for track, n := range summary.TracksUsed {
				docPrintf(cmd, "  track %-14s %d render(s)\n", track, n)
			}
			if summary.Runs > 0 {
				docPrintf(cmd, "  window: %s → %s\n", summary.FirstRun, summary.LastRun)
			}
			if wantAlerts {
				if len(alerts) == 0 {
					docPrintf(cmd, "doc ledger: no alerts (thresholds clear)\n")
				}
				for _, a := range alerts {
					docPrintf(cmd, "  ALERT: %s\n", a)
				}
			}
		}
		// Exit-code contract: alert breaches fail the command (exit 1).
		if len(alerts) > 0 {
			return fmt.Errorf("doc ledger: %d alert(s) fired", len(alerts))
		}
		return nil
	},
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc review — D6 human review queue
// ────────────────────────────────────────────────────────────────────────────

var docReviewCmd = &cobra.Command{
	Use:   "review [approve|reject] <id>",
	Short: "List or resolve human-review items (conflicts, fixes, ADR drafts)",
	Args:  cobra.RangeArgs(0, 2),
	Long: `The D6 review queue holds items needing a human decision: merge
conflicts, applied snippet fixes, and auto-generated ADR drafts.
With no arguments, lists pending items. Approve or reject by ID.
With --tuning, prints prompt/style suggestions aggregated from resolved
outcomes; --apply (requires --tuning) additionally applies the SAFE subset
(style suggestions naming a jargon term are appended to the docs.yaml
style.jargon_blacklist, everything else is reported as manual-action).`,
	Example: `  gmb doc review
  gmb doc review approve r1a2b3c4d --reason "looks right"
  gmb doc review reject r1a2b3c4d --reason "wrong callers"
  gmb doc review --stats
  gmb doc review --tuning
  gmb doc review record-revert --doc docs/auth.md --section intro --reason "human rewrote the table"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("doc review: %w", err)
		}
		if showTuning, _ := cmd.Flags().GetBool("tuning"); showTuning {
			suggestions, err := review.SuggestTuning(absDir)
			if err != nil {
				return err
			}
			if len(suggestions) == 0 {
				docPrintf(cmd, "doc review: no tuning suggestions (no resolved outcomes yet)\n")
				return nil
			}
			docPrintf(cmd, "doc review: %d tuning suggestion(s):\n", len(suggestions))
			for _, s := range suggestions {
				docPrintf(cmd, "  [%s] %s → %s\n", s.Area, s.Evidence, s.Action)
			}
			// --apply consumes the tuning loop: safe auto-fixes are
			// applied via review.TuningApply, the rest are reported.
			if apply, _ := cmd.Flags().GetBool("apply"); apply {
				applied, manual := 0, 0
				for _, s := range suggestions {
					if err := review.TuningApply(absDir, s); err != nil {
						manual++
						docPrintf(cmd, "  [manual] [%s] %s\n", s.Area, err)
					} else {
						applied++
						docPrintf(cmd, "  [applied] [%s] %s\n", s.Area, s.Evidence)
					}
				}
				docPrintf(cmd, "doc review: tuning apply: %d applied, %d require manual action\n", applied, manual)
			}
			return nil
		}
		if applyOnly, _ := cmd.Flags().GetBool("apply"); applyOnly {
			return fmt.Errorf("doc review: --apply requires --tuning")
		}
		if showStats, _ := cmd.Flags().GetBool("stats"); showStats {
			pending, approved, rejected, observed, err := doc_engine.ReviewStats(absDir)
			if err != nil {
				return err
			}
			docPrintf(cmd, "doc review: %d pending | %d approved | %d rejected | %d observed\n",
				pending, approved, rejected, observed)
			return nil
		}
		if len(args) == 0 {
			items, err := doc_engine.ListPendingReviews(absDir)
			if err != nil {
				return err
			}
			if len(items) == 0 {
				docPrintf(cmd, "doc review: no pending items\n")
				return nil
			}
			for _, it := range items {
				docPrintf(cmd, "  %s [%s] %s — %s\n", it.ID, it.Kind, it.DocPath, it.Summary)
			}
			return nil
		}
		if len(args) != 2 {
			return fmt.Errorf("doc review: usage: gmb doc review [approve|reject] <id>")
		}
		action, id := args[0], args[1]
		reason, _ := cmd.Flags().GetString("reason")
		var rerr error
		switch action {
		case "approve":
			rerr = doc_engine.ResolveReview(absDir, id, true, reason)
		case "reject":
			rerr = doc_engine.ResolveReview(absDir, id, false, reason)
		default:
			return fmt.Errorf("doc review: unknown action %q (want approve|reject)", action)
		}
		if rerr != nil {
			return rerr
		}
		docPrintf(cmd, "doc review: %s %sd\n", id, action)
		return nil
	},
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc review record-revert — log a human revert observation
// ────────────────────────────────────────────────────────────────────────────

var docReviewRecordRevertCmd = &cobra.Command{
	Use:   "record-revert",
	Short: "Record a human revert of machine text as an observed review item",
	Long: `Logs a human revert of machine-generated text as a terminal
informational observation (Kind "revert", Status "observed"). The doc and
section flags are optional; the reason is required and feeds prompt/style
tuning via 'gmb doc review --tuning'.`,
	Example: `  gmb doc review record-revert --doc docs/auth.md --section intro --reason "human rewrote the table"`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("doc review record-revert: %w", err)
		}
		docPath, _ := cmd.Flags().GetString("doc")
		section, _ := cmd.Flags().GetString("section")
		reason, _ := cmd.Flags().GetString("reason")
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("doc review record-revert: --reason is required")
		}
		if err := review.RecordRevert(absDir, docPath, section, reason); err != nil {
			return err
		}
		docPrintf(cmd, "doc review: revert recorded for %s#%s\n", docPath, section)
		return nil
	},
}

// ────────────────────────────────────────────────────────────────────────────
// gmb doc langmatrix — B4c grounding depth matrix
// ────────────────────────────────────────────────────────────────────────────

var docLangmatrixCmd = &cobra.Command{
	Use:   "langmatrix",
	Short: "Print the per-language grounding depth matrix",
	Args:  cobra.NoArgs,
	Long: `Grades every embedded language fixture across the seven grounding
dimensions (signatures, doc-comments, call-edges, errors, concurrency,
config, tests) and prints the Markdown report table. With --json, prints
the per-language grade reports as JSON instead.`,
	Example: `  gmb doc langmatrix
  gmb doc langmatrix --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		reports := langmatrix.GradeAll()
		if asJSON {
			data, err := json.MarshalIndent(reports, "", "  ")
			if err != nil {
				return fmt.Errorf("doc langmatrix: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), langmatrix.MarkdownReport(reports))
		return nil
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
	docCmd.Flags().Bool("write", false, "Force writes even on non-main branches (overrides --branch-policy, except draft/wip)")

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
	docCheckCmd.Flags().Bool("fix", false, "Deterministically fix failing snippets (modifies files; use without --json in CI gate mode)")
	docCheckCmd.Flags().Bool("check-external", false, "Also verify external http(s) links via HEAD (network errors are skipped; HTTP 4xx/5xx fail)")

	// ── gmb doc diff flags ────────────────────────────────────────────────
	docDiffCmd.Flags().String("doc", "", "Only diff the document with this ID")
	docDiffCmd.Flags().String("tag", "", "Only diff documents with this tag")
	docDiffCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")

	// ── gmb doc status flags ──────────────────────────────────────────────
	docStatusCmd.Flags().String("doc", "", "Only check document with this ID")
	docStatusCmd.Flags().String("tag", "", "Only check documents with this tag")
	docStatusCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")

	// ── gmb doc release flags ─────────────────────────────────────────────
	docReleaseCmd.Flags().String("out", "", "Output file path for the migration guide")

	// ── gmb doc export flags ──────────────────────────────────────────────
	docExportCmd.Flags().String("format", "rag", "Export format: rag|jsonl|state")
	docExportCmd.Flags().String("out", ".glassmarble/rag", "Output directory")
	docExportCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")

	// ── gmb doc view flags ────────────────────────────────────────────────
	docViewCmd.Flags().String("doc", "", "Document ID to open")

	// ── gmb doc eval flags ────────────────────────────────────────────────
	docEvalCmd.Flags().Int("sample", 0, "Maximum sections to score (0 = all)")
	docEvalCmd.Flags().Float64("min-score", 0.75, "Minimum global faithfulness (exit 1 below)")
	docEvalCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")

	// ── gmb doc ledger flags ──────────────────────────────────────────────
	docLedgerCmd.Flags().Int("last", 0, "Roll up only the last N runs (0 = all)")
	docLedgerCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	docLedgerCmd.Flags().Bool("alerts", false, "Evaluate alert thresholds over the rollup (exit 1 if any fires)")
	docLedgerCmd.Flags().Int("max-tokens", 0, "Alert when total tokens exceed this budget (0 = disabled)")
	docLedgerCmd.Flags().Float64("min-freshness", 0, "Alert when average freshness drops below this (0 = disabled)")
	docLedgerCmd.Flags().Int("max-fallbacks", 0, "Alert when total fallbacks exceed this (0 = disabled)")

	// ── gmb doc review flags ──────────────────────────────────────────────
	docReviewCmd.Flags().String("reason", "", "Reason recorded with approve/reject")
	docReviewCmd.Flags().Bool("stats", false, "Show review queue counts")
	docReviewCmd.Flags().Bool("tuning", false, "Show prompt/style tuning suggestions from resolved outcomes")
	docReviewCmd.Flags().Bool("apply", false, "Apply safe tuning suggestions (requires --tuning; style jargon only)")

	// ── gmb doc review record-revert flags ──────────────────────────────────
	docReviewRecordRevertCmd.Flags().String("doc", "", "Reverted document path (optional)")
	docReviewRecordRevertCmd.Flags().String("section", "", "Reverted section ID (optional)")
	docReviewRecordRevertCmd.Flags().String("reason", "", "Why the text was reverted (required)")

	// ── gmb doc langmatrix flags ────────────────────────────────────────────
	docLangmatrixCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")

	docReviewCmd.AddCommand(docReviewRecordRevertCmd)

	// ── Register subcommands ──────────────────────────────────────────────
	docCmd.AddCommand(
		docInitCmd,
		docCheckCmd,
		docDiffCmd,
		docStatusCmd,
		docReleaseCmd,
		docExportCmd,
		docViewCmd,
		docEvalCmd,
		docLedgerCmd,
		docReviewCmd,
		docLangmatrixCmd,
	)

	// ── Register gmb doc with root ────────────────────────────────────────
	rootCmd.AddCommand(docCmd)
}
