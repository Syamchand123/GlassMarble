package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
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
	Long:  `Executes Stage 1 (ingestion), Stage 2 (normalization), Stage 3 (topology aggregation), Stage 4 (CPG linking), and commits the graph state to AKG.`,
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
		}
		if isBench {
			return runAnalysisBenchmark(cmd, opts)
		}
		// --json is machine-readable and must bypass the interactive layer.
		if asJSON {
			return runAnalysis(opts)
		}
		if tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return runAnalyzeTUI(cmd, opts)
		}
		return runAnalysis(opts)
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
	// progress, when non-nil, receives stage-boundary updates so a BubbleTea
	// program can animate the pipeline. It is purely additive: nil behaves
	// exactly as before.
	progress func(stage int, name string, current, total int)
	// onSummary, when non-nil, receives the QA numbers of a completed run so
	// the TUI layer can render its own styled summary card.
	onSummary func(s analysisSummary)
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
	ttlBytes      int64
	walBytes      int64
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
	TTLBytes      int64    `json:"ttl_bytes"`
	WALBytes      int64    `json:"wal_bytes"`
	DurationMs    int64    `json:"duration_ms"`
	Skipped       []string `json:"skipped,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
	StorageDir    string   `json:"storage_dir"`
}

// runAnalysis executes the full four-stage pipeline. It is shared by
// `gmb analyze` and `gmb watch` so both commands drive the same engine.
func runAnalysis(opts runAnalysisOptions) error {
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

	fmt.Printf("Starting GlassMarble Analysis on %s...\n", absDir)

	if opts.progress != nil {
		opts.progress(1, "Tree-sitter Ingestion", 0, 0)
	}

	// Stage 1: Tree-sitter Ingestion.
	// Default: incremental delta against the working tree (git diff HEAD).
	// --full forces a clean full scan of every file (AUDIT Issue 1.1 /
	// Phase 1C-9 — the old --full flag was a no-op).
	cfg := stage1.DefaultConfig(absDir)
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
	// counter (currently stage 1 only reports start/end boundaries).
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

	var stage1Out *stage1.StageOutput
	doneParse := product.StartSpan("parse")
	if !full {
		hasBaseState := false
		if st, err := os.Stat(filepath.Join(absDir, ".glassmarble", "akg_state.ttl")); err == nil && st.Size() > 0 {
			hasBaseState = true
		}
		diff, diffErr := stage1.CollectGitDiff(absDir, commitHash)
		if hasBaseState && diffErr == nil && len(diff) > 0 {
			stage1Out, err = stage1.RunIngestionForDelta(cfg, diff)
			if err == nil {
				if verbose {
					fmt.Printf("Stage 1 (delta): parsed %d changed files, %d deleted.\n",
						len(stage1Out.Updated), len(stage1Out.Deleted))
				}
			}
		}
	}
	if stage1Out == nil {
		stage1Out, err = stage1.RunIngestion(cfg)
		if err != nil {
			doneParse()
			return fmt.Errorf("stage 1 ingestion failed: %w", err)
		}
		if verbose {
			fmt.Printf("Stage 1 (full): discovered and parsed %d source files.\n", len(stage1Out.Updated))
		}
	}
	doneParse()
	if opts.progress != nil {
		opts.progress(1, "Tree-sitter Ingestion", len(stage1Out.Updated), len(stage1Out.Updated))
	}

	// Stage 2: GAST Normalization
	doneNormalize := product.StartSpan("normalize")
	if opts.progress != nil {
		opts.progress(2, "GAST Normalization", 0, 0)
	}
	stage2Payload, err := stage2.Normalize(stage1Out, commitHash)
	if err != nil {
		doneNormalize()
		return fmt.Errorf("stage 2 normalization failed: %w", err)
	}
	doneNormalize()
	if opts.progress != nil {
		opts.progress(2, "GAST Normalization", len(stage2Payload.UpsertedTrees), len(stage2Payload.UpsertedTrees))
	}
	if verbose {
		fmt.Printf("Stage 2: Normalized %d syntax trees.\n", len(stage2Payload.UpsertedTrees))
	}

	// Stage 3: Topology Aggregation
	doneStage3 := product.StartSpan("stage3")
	if opts.progress != nil {
		opts.progress(3, "Topology Aggregation", 0, 0)
	}
	stage3Out, err := stage3.Aggregate(stage2Payload, nil, absDir)
	if err != nil {
		doneStage3()
		return fmt.Errorf("stage 3 aggregation failed: %w", err)
	}
	doneStage3()
	if opts.progress != nil {
		opts.progress(3, "Topology Aggregation", 1, 1)
	}
	if verbose {
		fmt.Printf("Stage 3: Built topology with %d global definition symbols.\n", len(stage3Out.GlobalDefinitionIndex))
	}

	// Initialize AKG before Stage 4 so we have a persistent GraphDB for incremental lookups
	storageDir := filepath.Join(absDir, ".glassmarble")
	tm, err := newAKGManager(storageDir, nil)
	if err != nil {
		return fmt.Errorf("failed to initialize AKG transaction manager: %w", err)
	}
	defer tm.Close()

	var modifiedFiles []string
	for relPath := range stage2Payload.UpsertedTrees {
		modifiedFiles = append(modifiedFiles, relPath)
	}
	modifiedFiles = append(modifiedFiles, stage2Payload.DeletedPaths...)

	// Stage 4: CPG Linker (Incremental Delta Mode).
	linkerCfg := stage4.LinkerConfig{}
	if opts.linkLevel != "" {
		linkerCfg.LevelOfDetail = opts.linkLevel
	}
	if full {
		linkerCfg.LevelOfDetail = stage4.LevelFull
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
	doneStage4 := product.StartSpan("stage4")
	cpg, err := stage4.Link(stage3Out, modifiedFiles, tm.GetActiveGraph(), linkerCfg)
	if err != nil {
		doneStage4()
		return fmt.Errorf("stage 4 linker failed: %w", err)
	}
	doneStage4()
	if opts.progress != nil {
		opts.progress(4, "Semantic Linking", len(cpg.GraphNodes), len(cpg.GraphNodes))
	}
	if verbose {
		fmt.Printf("Stage 4: Bound Delta CPG with %d new/modified nodes.\n", len(cpg.GraphNodes))
	}

	if opts.progress != nil {
		opts.progress(5, "Committing graph", 0, 1)
	}
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
	ttlSize, walSize := akgStorageSizes(storageDir)

	if opts.onSummary != nil {
		opts.onSummary(analysisSummary{
			targetDir:     absDir,
			filesAnalyzed: len(stage1Out.Updated),
			nodes:         q.TotalNodes,
			edges:         q.TotalEdges,
			virtualNodes:  q.VirtualNodes,
			danglingEdges: q.DanglingEdges,
			ttlBytes:      ttlSize,
			walBytes:      walSize,
			duration:      duration,
			storageDir:    storageDir,
		})
	}

	if opts.json {
		out, _ := json.MarshalIndent(analysisJSON{
			TargetDir:     absDir,
			CommitHash:    commitHash,
			FilesAnalyzed: len(stage1Out.Updated),
			Nodes:         q.TotalNodes,
			Edges:         q.TotalEdges,
			VirtualNodes:  q.VirtualNodes,
			DanglingEdges: q.DanglingEdges,
			TTLBytes:      ttlSize,
			WALBytes:      walSize,
			DurationMs:    duration.Milliseconds(),
			Skipped:       stage1Out.Skipped,
			Warnings:      stage1Out.Warnings,
			StorageDir:    storageDir,
		}, "", "  ")
		fmt.Println(string(out))
		return nil
	}

	fmt.Printf("Analyzed %d files | %d nodes | %d edges | %d virtual | %d dangling | ttl=%s wal=%s | %.1fs\n",
		len(stage1Out.Updated), q.TotalNodes, q.TotalEdges, q.VirtualNodes, q.DanglingEdges, humanBytes(ttlSize), humanBytes(walSize), duration.Seconds())
	if q.DanglingEdges > 0 {
		fmt.Printf("WARNING: %d edges reference missing nodes (dangling). Run `gmb analyze --full` to rebuild.\n", q.DanglingEdges)
	}
	// Surface files that were skipped (oversized, unknown grammar) or that
	// produced warnings so silent data loss stays visible (AUDIT Issue 1
	// Phase 1C-10: skipped/warnings were collected but never printed).
	if len(stage1Out.Skipped) > 0 {
		fmt.Printf("WARNING: %d file(s) skipped during ingestion (oversized or unsupported language):\n", len(stage1Out.Skipped))
		for _, s := range stage1Out.Skipped {
			fmt.Printf("  - %s\n", s)
		}
	}
	if len(stage1Out.Warnings) > 0 {
		fmt.Printf("Note: %d ingestion warning(s):\n", len(stage1Out.Warnings))
		for _, w := range stage1Out.Warnings {
			fmt.Printf("  - %s\n", w)
		}
	}

	if verbose {
		fmt.Printf("AKG database updated at %s\n", filepath.Join(storageDir, "akg_state.ttl"))
	}

	return nil
}

// runAnalyzeTUI drives the full pipeline from inside the analyze BubbleTea
// program, wiring stage progress and the final QA summary into the model.
func runAnalyzeTUI(c *cobra.Command, opts runAnalysisOptions) error {
	var summary analyze.Summary
	run := func(progress func(stage int, name string, current, total int)) (analyze.Summary, error) {
		opts.progress = progress
		opts.onSummary = func(s analysisSummary) {
			summary = analyze.Summary{
				TargetDir:     s.targetDir,
				FilesAnalyzed: s.filesAnalyzed,
				Nodes:         s.nodes,
				Edges:         s.edges,
				VirtualNodes:  s.virtualNodes,
				DanglingEdges: s.danglingEdges,
				TTLBytes:      s.ttlBytes,
				WALBytes:      s.walBytes,
				Duration:      s.duration,
			}
		}
		err := runAnalysis(opts)
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

// akgStorageSizes reports the persisted artifact sizes for the QA report.
func akgStorageSizes(storageDir string) (ttlBytes, walBytes int64) {
	if st, err := os.Stat(filepath.Join(storageDir, "akg_state.ttl")); err == nil {
		ttlBytes = st.Size()
	}
	if st, err := os.Stat(filepath.Join(storageDir, "wal", "akg_transactions.wal")); err == nil {
		walBytes = st.Size()
	}
	return
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

	var commitMS float64
	var totalDuration time.Duration

	opts.onSummary = func(s analysisSummary) {
		totalDuration = s.duration
	}

	start := time.Now()
	err := runAnalysis(opts)
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

	ttlSize, walSize := akgStorageSizes(storageDir)

	totalSec := totalDuration.Seconds()
	commitSec := commitMS / 1000.0
	ttlMB := float64(ttlSize) / (1024 * 1024)
	walMB := float64(walSize) / (1024 * 1024)

	passTotal := totalSec <= 20.0
	passCommit := commitSec <= 8.0 || commitMS == 0
	passTTL := ttlMB <= 12.0 || ttlSize == 0
	passWAL := walMB <= 8.0

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
	fmt.Fprintf(out, "%-22s %-12s %-10s %s\n", "TTL size", fmt.Sprintf("%.2fMB", ttlMB), "<= 12.0MB", statusStr(passTTL))
	fmt.Fprintf(out, "%-22s %-12s %-10s %s\n", "WAL size", fmt.Sprintf("%.2fMB", walMB), "<= 8.0MB", statusStr(passWAL))

	if !passTotal || !passCommit || !passTTL || !passWAL {
		return producterrs.Tagged("benchmark gate exceeded performance budget", producterrs.ErrRenderLimit)
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
	rootCmd.AddCommand(analyzeCmd)
}
