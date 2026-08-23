package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/git"
	"github.com/Syamchand123/GlassMarble/internal/product"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/programs/analyze"
	"github.com/spf13/cobra"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Run full source code ingestion and build Architecture Knowledge Graph (AKG)",
	Long:  `Executes ingestion, normalization, topology aggregation, CPG linking, and commits the graph state to AKG.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir, _ := cmd.Flags().GetString("dir")
		commitHash, _ := cmd.Flags().GetString("commit")
		full, _ := cmd.Flags().GetBool("full")
		workers, _ := cmd.Flags().GetInt("workers")
		verbose, _ := cmd.Flags().GetBool("verbose")
		linkLevel, _ := cmd.Flags().GetString("link-level")
		macroInference, _ := cmd.Flags().GetString("macro-inference")
		maxNodes, _ := cmd.Flags().GetInt("max-nodes")
		abortOnLimit, _ := cmd.Flags().GetBool("abort-on-limit")
		asJSON, _ := cmd.Flags().GetBool("json")
		storeCode, _ := cmd.Flags().GetBool("store-code")
		isBench, _ := cmd.Flags().GetBool("bench")
		intelligence, _ := cmd.Flags().GetBool("intelligence")
		includeDocs, _ := cmd.Flags().GetBool("include-docs")
		if targetDir == "" {
			targetDir = "."
		}
		opts := runAnalysisOptions{
			targetDir:      targetDir,
			commitHash:     commitHash,
			full:           full,
			storeCode:      storeCode,
			workers:        workers,
			verbose:        verbose,
			linkLevel:      linkLevel,
			macroInference: macroInference,
			maxNodes:       maxNodes,
			abortOnLimit:   abortOnLimit,
			json:           asJSON,
			bench:          isBench,
			intelligence:   intelligence,
			includeDocs:    includeDocs,
			out:            cmd.OutOrStdout(),
		}
		if isBench {
			return runAnalysisBenchmark(cmd, opts)
		}
		// --json is machine-readable and must bypass the interactive layer.
		if asJSON {
			return runAnalysis(cmd, opts)
		}
		if tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return runAnalyzeTUI(cmd, opts)
		}
		return runAnalysis(cmd, opts)
	},
}

// runAnalysisOptions carries the flags that control a single analysis run.
type runAnalysisOptions struct {
	targetDir      string
	commitHash     string
	full           bool
	storeCode      bool
	workers        int
	verbose        bool
	linkLevel      string
	macroInference string
	maxNodes       int
	abortOnLimit   bool
	json           bool
	bench          bool
	// progress, when non-nil, receives phase-boundary updates so a BubbleTea
	// program can animate the pipeline. It is purely additive: nil behaves
	// exactly as before.
	progress func(step int, name string, current, total int)
	// onSummary, when non-nil, receives the QA numbers of a completed run so
	// the TUI layer can render its own styled summary card.
	onSummary func(s analysisSummary)
	// intelligence controls whether architecture intelligence runs after
	// the graph is committed (human output only).
	intelligence bool
	// includeDocs controls whether knowledge fusion (ADR/README/PR
	// claims) runs after the graph is committed. Opt-in by design — doc
	// scanning and git-history walks are not free on large repositories.
	includeDocs bool
	// out is the writer for human-readable output. When nil, os.Stdout or
	// cmd.OutOrStdout() is used. TUI mode sets progress != nil and suppresses
	// direct writes (C6-2).
	out io.Writer
}

// analysisSummary carries the QA numbers of a completed analysis run for the
// TUI layer (mirrors the human "Analyzed N files | ..." report line).
type analysisSummary struct {
	targetDir     string
	filesAnalyzed int
	nodes         int
	edges         int
	virtualNodes  int
	danglingEdges int
	stateBytes    int64
	duration      time.Duration
	storageDir    string
}

// analysisJSON is the machine-readable result of a successful analysis run.
type analysisJSON struct {
	TargetDir     string   `json:"target_dir"`
	CommitHash    string   `json:"commit_hash,omitempty"`
	FilesAnalyzed int      `json:"files_analyzed"`
	Nodes         int      `json:"nodes"`
	Edges         int      `json:"edges"`
	VirtualNodes  int      `json:"virtual_nodes"`
	DanglingEdges int      `json:"dangling_edges"`
	StateBytes    int64    `json:"state_bytes"`
	DurationMs    int64    `json:"duration_ms"`
	Skipped       []string `json:"skipped,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
	StorageDir    string   `json:"storage_dir"`
}

// runAnalysis executes the full four-phase pipeline. It is shared by
// `gmb analyze` and `gmb watch` so both commands drive the same engine.
func runAnalysis(cmd *cobra.Command, opts runAnalysisOptions) error {
	start := time.Now()
	targetDir := opts.targetDir
	commitHash := opts.commitHash
	full := opts.full
	workers := opts.workers
	verbose := opts.verbose

	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		return producterrs.Annotate(fmt.Errorf("failed to resolve path: %w", err), producterrs.ErrValidation)
	}

	akg.SetStoreCode(opts.storeCode)
	if commitHash == "" {
		if h, err := git.GetHEADCommitHash(absDir); err == nil && h != "" {
			commitHash = h
		}
	}

	// Resolve output writer (C6-2): route through io.Writer passed via
	// runAnalysisOptions or cmd.OutOrStdout(); gate on progress==nil so the
	// bubbletea TUI is not corrupted by raw fmt.Printf lines.
	outWriter := opts.out
	if outWriter == nil {
		if cmd != nil {
			outWriter = cmd.OutOrStdout()
		} else {
			outWriter = os.Stdout
		}
	}
	gatedPrintf := func(format string, a ...any) {
		if opts.progress != nil {
			return
		}
		fmt.Fprintf(outWriter, format, a...)
	}

	gatedPrintf("Starting GlassMarble Analysis on %s...\n", absDir)

	if opts.progress != nil {
		opts.progress(1, "Tree-sitter Ingestion", 0, 0)
	}

	// Ingestion: Tree-sitter Ingestion.
	// Default: incremental delta against the working tree (git diff HEAD).
	// --full forces a clean full scan of every file (AUDIT Issue 1.1 /
	// Phase 1C-9 — the old --full flag was a no-op).
	cfg := ingest.DefaultConfig(absDir)
	// Wire internal/config knobs (AUDIT Issue 4 Phase 4A-4): workers and
	// max file size come from .glassmarble/config.yaml / GLASSMARBLE_* /
	// flags, with flags winning.
	if conf, confErr := config.Load(config.Config{WorkerCount: workers}); confErr == nil {
		if workers == 0 {
			cfg.WorkerCount = conf.WorkerCount
		}
		if conf.MaxFileBytes > 0 {
			cfg.MaxFileBytes = conf.MaxFileBytes
		}
	}
	if workers > 0 {
		cfg.WorkerCount = workers
	}
	if _, err := os.Stat(filepath.Join(absDir, ".git")); err == nil {
		cfg.GitTrackedOnly = true
	}
	// Live per-file progress during ingestion so the TUI can animate a real
	// counter (currently ingestion only reports start/end boundaries).
	if opts.progress != nil {
		cfg.OnProgress = func(done, total int) {
			if total <= 0 {
				total = done
			}
			if total < done {
				total = done
			}
			opts.progress(1, "Tree-sitter Ingestion", done, total)
		}
	}

	// A full rescan (--full, no base state, empty git diff, diff error, or a
	// root-commit "diff" that spans the entire tree) re-ingests every file.
	// The delta linkers must then not deduplicate against the persisted base
	// graph: they only re-emit "new" nodes, and the commit sweep removes every
	// modified-file node missing from the delta, so deduplicating would
	// silently delete untouched derived nodes. The linker therefore links full
	// rescans against an empty base.
	fullRescan := full
	var ingestOut *ingest.IngestOutput
	doneParse := product.StartSpan("parse")
	if !full {
		// A base state only counts when akg.json actually contains graph
		// nodes: the empty state written by `gmb init` must still force a
		// full scan, because an incremental delta against an empty base
		// would ingest only the changed files and produce a partial graph.
		// The check streams just the first node and stops, so it stays
		// bounded-memory (AUDIT Issue 4 Phase 4A-2).
		hasBaseState := false
		if err := akg.StreamNodes(filepath.Join(absDir, ".glassmarble"), func(*link.ResolvedNode) bool {
			hasBaseState = true
			return false
		}); err != nil {
			hasBaseState = false
		}
		// A root commit has no parent, so its "diff" is the whole tree —
		// not an incremental delta. Treat it as a full rescan.
		isRoot, rootErr := git.IsRootCommit(absDir, commitHash)
		diff, diffErr := ingest.CollectGitDiff(absDir, commitHash)
		if hasBaseState && diffErr == nil && !(rootErr == nil && isRoot) && len(diff) > 0 {
			ingestOut, err = ingest.RunIngestionForDelta(cfg, diff)
			if err == nil {
				if verbose {
					gatedPrintf("Ingestion (delta): parsed %d changed files, %d deleted.\n",
						len(ingestOut.Updated), len(ingestOut.Deleted))
				}
			} else if verbose || opts.progress == nil {
				// C6-12: delta fallback warning — surface the delta error
				// before silently promoting to full rescan.
				gatedPrintf("Note: delta ingestion failed (%v); falling back to full scan.\n", err)
				if opts.progress != nil {
					fmt.Fprintf(os.Stderr, "Note: delta ingestion failed (%v); falling back to full scan.\n", err)
				}
			}
		}
	}
	if ingestOut == nil {
		fullRescan = true
		ingestOut, err = ingest.RunIngestion(cfg)
		if err != nil {
			doneParse()
			return fmt.Errorf("ingestion failed: %w", err)
		}
		if verbose {
			gatedPrintf("Ingestion (full): discovered and parsed %d source files.\n", len(ingestOut.Updated))
		}
	}
	doneParse()
	if opts.progress != nil {
		opts.progress(1, "Tree-sitter Ingestion", len(ingestOut.Updated), len(ingestOut.Updated))
	}

	// Normalization: GAST Normalization
	doneNormalize := product.StartSpan("normalize")
	if opts.progress != nil {
		opts.progress(2, "GAST Normalization", 0, 0)
	}
	normalizePayload, err := normalize.Normalize(ingestOut, commitHash)
	if err != nil {
		doneNormalize()
		return fmt.Errorf("normalization failed: %w", err)
	}
	doneNormalize()
	if opts.progress != nil {
		opts.progress(2, "GAST Normalization", len(normalizePayload.UpsertedTrees), len(normalizePayload.UpsertedTrees))
	}
	if verbose {
		gatedPrintf("Normalization: Normalized %d syntax trees.\n", len(normalizePayload.UpsertedTrees))
	}

	// Aggregation: Topology Aggregation
	doneAggregate := product.StartSpan("aggregate")
	if opts.progress != nil {
		opts.progress(3, "Topology Aggregation", 0, 0)
	}
	aggregateOut, err := aggregate.Aggregate(normalizePayload, nil, absDir)
	if err != nil {
		doneAggregate()
		return fmt.Errorf("aggregation failed: %w", err)
	}
	doneAggregate()
	if opts.progress != nil {
		opts.progress(3, "Topology Aggregation", 1, 1)
	}
	if verbose {
		gatedPrintf("Aggregation: Built topology with %d global definition symbols.\n", len(aggregateOut.GlobalDefinitionIndex))
	}

	// Initialize AKG before Linking so we have a persistent GraphDB for incremental lookups
	storageDir := filepath.Join(absDir, ".glassmarble")
	tm, err := newAKGManager(storageDir, cmd)
	if err != nil {
		return fmt.Errorf("failed to initialize AKG transaction manager: %w", err)
	}
	defer tm.Close()

	var modifiedFiles []string
	for relPath := range normalizePayload.UpsertedTrees {
		modifiedFiles = append(modifiedFiles, relPath)
	}
	modifiedFiles = append(modifiedFiles, normalizePayload.DeletedPaths...)

	// Linking: CPG Linker (Incremental Delta Mode).
	linkerCfg := link.LinkerConfig{}
	if opts.linkLevel != "" {
		linkerCfg.LevelOfDetail = opts.linkLevel
	}
	if full {
		linkerCfg.LevelOfDetail = link.LevelFull
	}
	if opts.macroInference != "" {
		linkerCfg.MacroInference = opts.macroInference
	}
	if opts.maxNodes > 0 {
		linkerCfg.MaxTotalNodes = opts.maxNodes
	}
	if opts.abortOnLimit {
		linkerCfg.AbortOnLimit = true
	}
	if opts.progress != nil {
		opts.progress(4, "Semantic Linking", 0, 0)
	}
	doneLink := product.StartSpan("link")
	var linkBase link.GraphDB = tm.GetActiveGraph()
	if fullRescan {
		linkBase = akg.NewCodePropertyGraph("rescan")
	}
	cpg, err := link.Link(aggregateOut, modifiedFiles, linkBase, linkerCfg)
	if err != nil {
		doneLink()
		return fmt.Errorf("linking failed: %w", err)
	}
	doneLink()
	if opts.progress != nil {
		opts.progress(4, "Semantic Linking", len(cpg.GraphNodes), len(cpg.GraphNodes))
	}
	if verbose {
		gatedPrintf("Linking: Bound Delta CPG with %d new/modified nodes.\n", len(cpg.GraphNodes))
	}

	if opts.progress != nil {
		opts.progress(5, "Committing graph", 0, 1)
	}
	// Baseline quality BEFORE the delta commits: the final report measures
	// the merged graph, so the delta is this-run minus this baseline.
	baseQ := akg.MeasureGraphQuality(tm.GetActiveGraph())
	doneCommit := product.StartSpan("akg-commit")
	if err := tm.ExecuteDeltaTransaction(cpg, modifiedFiles); err != nil {
		doneCommit()
		return fmt.Errorf("failed to commit AKG transaction: %w", err)
	}
	doneCommit()
	if opts.progress != nil {
		opts.progress(5, "Committing graph", 1, 1)
	}

	duration := time.Since(start)

	_ = product.SaveTelemetry(storageDir)

	// Quality budget report (AUDIT Issue 1 Phase 1C-10 / Issue 5 item 3).
	// Measured on the committed MERGED graph (base + delta): the delta
	// payload alone would count cross-file edges as dangling. Sizes are
	// part of the noise budget so bloat stays visible.
	q := akg.MeasureGraphQuality(tm.GetActiveGraph())
	stateSize := akgStateSize(storageDir)

	if opts.onSummary != nil {
		opts.onSummary(analysisSummary{
			targetDir:     absDir,
			filesAnalyzed: len(ingestOut.Updated),
			nodes:         q.TotalNodes,
			edges:         q.TotalEdges,
			virtualNodes:  q.VirtualNodes,
			danglingEdges: q.DanglingEdges,
			stateBytes:    stateSize,
			duration:      duration,
			storageDir:    storageDir,
		})
	}

	if opts.json {
		out, _ := json.MarshalIndent(analysisJSON{
			TargetDir:     absDir,
			CommitHash:    commitHash,
			FilesAnalyzed: len(ingestOut.Updated),
			Nodes:         q.TotalNodes,
			Edges:         q.TotalEdges,
			VirtualNodes:  q.VirtualNodes,
			DanglingEdges: q.DanglingEdges,
			StateBytes:    stateSize,
			DurationMs:    duration.Milliseconds(),
			Skipped:       ingestOut.Skipped,
			Warnings:      ingestOut.Warnings,
			StorageDir:    storageDir,
		}, "", "  ")
		fmt.Fprintln(outWriter, string(out))
		return nil
	}

	gatedPrintf("Analyzed %d files | %d nodes (+%d) | %d edges (+%d) | %d virtual (+%d) | %d dangling | state=%s | %.1fs\n",
		len(ingestOut.Updated), q.TotalNodes, q.TotalNodes-baseQ.TotalNodes,
		q.TotalEdges, q.TotalEdges-baseQ.TotalEdges,
		q.VirtualNodes, q.VirtualNodes-baseQ.VirtualNodes,
		q.DanglingEdges, humanBytes(stateSize), duration.Seconds())
	if q.DanglingEdges > 0 {
		gatedPrintf("WARNING: %d edges reference missing nodes (dangling). Run `gmb analyze --full` to rebuild.\n", q.DanglingEdges)
	}
	// architecture intelligence + developer memory
	// (human mode only; the JSON contract above must stay stable for
	// machine consumers). Both phases are non-fatal by design.
	if opts.intelligence {
		runMemoryPipeline(storageDir, tm, commitHash, verbose)
	}
	// knowledge fusion: ADR/README/PR claims fused into developer
	// memory. Also human-output-only and non-fatal by design.
	if opts.includeDocs {
		runFusion(storageDir, tm, verbose)
	}
	// convention-learning layer: refresh the project conventions
	// (.glassmarble/memory/conventions.json) from the graph, the memory
	// and the correction log. Corrections themselves are applied at query
	// time (gmb memory), not here. Non-fatal by design (§15.6).
	runLearning(storageDir, tm, verbose)
	// knowledge aging: freshness decay on every claim plus
	// deterministic state transitions, persisted as replayable
	// STATE_CHANGE events in the memory WAL (master plan §13.1 — aging
	// runs on every analysis). Non-fatal by design (§15.6).
	runAging(storageDir, verbose)
	// Surface files that were skipped (oversized, unknown grammar) or that
	// produced warnings so silent data loss stays visible (AUDIT Issue 1
	// Phase 1C-10: skipped/warnings were collected but never printed).
	if len(ingestOut.Skipped) > 0 {
		gatedPrintf("WARNING: %d file(s) skipped during ingestion (oversized or unsupported language):\n", len(ingestOut.Skipped))
		for _, s := range ingestOut.Skipped {
			gatedPrintf("  - %s\n", s)
		}
	}
	if len(ingestOut.Warnings) > 0 {
		gatedPrintf("Note: %d ingestion warning(s):\n", len(ingestOut.Warnings))
		for _, w := range ingestOut.Warnings {
			gatedPrintf("  - %s\n", w)
		}
	}

	if verbose {
		gatedPrintf("AKG database updated at %s\n", filepath.Join(storageDir, "akg.json"))
	}

	return nil
}

// runAnalyzeTUI drives the full pipeline from inside the analyze BubbleTea
// program, wiring phase progress and the final QA summary into the model.
func runAnalyzeTUI(c *cobra.Command, opts runAnalysisOptions) error {
	var summary analyze.Summary
	run := func(progress func(step int, name string, current, total int)) (analyze.Summary, error) {
		opts.progress = progress
		opts.onSummary = func(s analysisSummary) {
			summary = analyze.Summary{
				TargetDir:     s.targetDir,
				FilesAnalyzed: s.filesAnalyzed,
				Nodes:         s.nodes,
				Edges:         s.edges,
				VirtualNodes:  s.virtualNodes,
				DanglingEdges: s.danglingEdges,
				StateBytes:    s.stateBytes,
				Duration:      s.duration,
			}
		}
		err := runAnalysis(c, opts)
		return summary, err
	}
	return analyze.RunAnalyze(analyze.Options{
		TargetDir:      opts.targetDir,
		CommitHash:     opts.commitHash,
		Full:           opts.full,
		Workers:        opts.workers,
		Verbose:        opts.verbose,
		LinkLevel:      opts.linkLevel,
		MacroInference: opts.macroInference,
		MaxNodes:       opts.maxNodes,
		AbortOnLimit:   opts.abortOnLimit,
	}, run, c.InOrStdin(), c.OutOrStdout())
}

// akgStateSize reports the persisted state artifact size for the QA report.
// The canonical GraphJSON store (akg.json) is the single state artifact.
func akgStateSize(storageDir string) int64 {
	if st, err := os.Stat(filepath.Join(storageDir, "akg.json")); err == nil {
		return st.Size()
	}
	return 0
}

// humanBytes renders a byte count for the QA report.
func humanBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

func runAnalysisBenchmark(cmd *cobra.Command, opts runAnalysisOptions) error {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "=== GlassMarble Pipeline Benchmark Gate (Phase 8 / §12.0) ===")
	fmt.Fprintln(out, "")

	// Architecture Intelligence adds analysis time that would skew the benchmark gates; skip it.
	opts.intelligence = false

	var commitMS float64
	var totalDuration time.Duration

	opts.onSummary = func(s analysisSummary) {
		totalDuration = s.duration
	}

	start := time.Now()
	err := runAnalysis(cmd, opts)
	if err != nil {
		return err
	}
	if totalDuration == 0 {
		totalDuration = time.Since(start)
	}

	absDir, _ := filepath.Abs(opts.targetDir)
	storageDir := filepath.Join(absDir, ".glassmarble")

	spans, _ := product.LoadTelemetry(storageDir)
	for _, s := range spans {
		if s.Name == "akg-commit" || s.Name == "commit" {
			commitMS = s.DurationMS
		}
	}

	stateSize := akgStateSize(storageDir)

	totalSec := totalDuration.Seconds()
	commitSec := commitMS / 1000.0
	stateMB := float64(stateSize) / (1024 * 1024)

	passTotal := totalSec <= 20.0
	passCommit := commitSec <= 8.0 || commitMS == 0
	passState := stateMB <= 12.0 || stateSize == 0

	statusStr := func(p bool) string {
		if p {
			return "PASS"
		}
		return "EXCEEDED"
	}

	fmt.Fprintf(out, "%-22s %-12s %-10s %s\n", "Phase", "Measured", "Budget", "Status")
	fmt.Fprintln(out, "-----------------------------------------------------")
	fmt.Fprintf(out, "%-22s %-12s %-10s %s\n", "analyze total", fmt.Sprintf("%.2fs", totalSec), "<= 20.0s", statusStr(passTotal))
	fmt.Fprintf(out, "%-22s %-12s %-10s %s\n", "akg-commit", fmt.Sprintf("%.2fs", commitSec), "<= 8.0s", statusStr(passCommit))
	fmt.Fprintf(out, "%-22s %-12s %-10s %s\n", "state size", fmt.Sprintf("%.2fMB", stateMB), "<= 12.0MB", statusStr(passState))

	if !passTotal || !passCommit || !passState {
		return producterrs.Tagged("benchmark gate exceeded performance budget", producterrs.ErrValidation)
	}
	return nil
}

func init() {
	analyzeCmd.Flags().String("dir", ".", "Target repository directory to analyze")
	analyzeCmd.Flags().String("commit", "", "Git commit hash to tag the analysis. Empty (default) diffs the working tree against HEAD (incremental delta); a hash diffs that commit against its parent")
	analyzeCmd.Flags().Bool("full", false, "Force a full clean scan of every file at full linker detail (default: incremental delta)")
	analyzeCmd.Flags().Int("workers", 0, "Number of parallel workers (default: CPUs)")
	analyzeCmd.Flags().String("link-level", "architecture", "Linker detail level: architecture (module/type/call/dep edges), standard (aggregate CFG), full (per-branch CFG+DFG)")
	analyzeCmd.Flags().String("macro-inference", "all", "Macro inference mode: disabled, structural (only rules with evidence), all (full heuristic+structural)")
	analyzeCmd.Flags().Int("max-nodes", 0, "Max total CPG nodes before warning/abort (0 = unlimited)")
	analyzeCmd.Flags().Bool("abort-on-limit", false, "Abort analysis if --max-nodes is exceeded (otherwise warn)")
	analyzeCmd.Flags().Bool("verbose", false, "Enable verbose output")
	analyzeCmd.Flags().Bool("store-code", false, "Store source code content snippets in AKG nodes (default: false)")
	analyzeCmd.Flags().Bool("json", false, "Emit machine-readable JSON instead of the human summary")
	analyzeCmd.Flags().Bool("bench", false, "Run analysis benchmark battery and verify performance against budget gates")
	analyzeCmd.Flags().Bool("intelligence", true, "Run architecture intelligence after committing the graph (human output only)")
	analyzeCmd.Flags().Bool("include-docs", false, "Run knowledge fusion: fuse ADR/README/PR claims from documentation and git history into developer memory")
	rootCmd.AddCommand(analyzeCmd)
}
