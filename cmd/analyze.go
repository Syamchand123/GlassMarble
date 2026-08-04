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
		if targetDir == "" {
			targetDir = "."
		}
		opts := runAnalysisOptions{
			targetDir:      targetDir,
			commitHash:     commitHash,
			full:           full,
			workers:        workers,
			verbose:        verbose,
			linkLevel:      linkLevel,
			macroInference: macroInference,
			maxNodes:       maxNodes,
			abortOnLimit:   abortOnLimit,
			json:           asJSON,
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
	workers        int
	verbose        bool
	linkLevel      string
	macroInference string
	maxNodes       int
	abortOnLimit   bool
	json           bool
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
		return fmt.Errorf("failed to resolve path: %w", err)
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
	if !full {
		// Delta only when a persisted base graph exists to link against.
		// On a first run (no .glassmarble/akg_state.ttl) a delta build would
		// produce a graph of only the changed files — cross-file calls to
		// unchanged files would all dangle (AUDIT Issue 1 Phase 1C-9: the
		// base-graph merge is supplied by the AKG persistence layer).
		hasBaseState := false
		if _, err := os.Stat(filepath.Join(absDir, ".glassmarble", "akg_state.ttl")); err == nil {
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
		// Full scan (no git repo, empty diff, or --full requested)
		stage1Out, err = stage1.RunIngestion(cfg)
		if err != nil {
			return fmt.Errorf("stage 1 ingestion failed: %w", err)
		}
		if verbose {
			fmt.Printf("Stage 1 (full): discovered and parsed %d source files.\n", len(stage1Out.Updated))
		}
	}
	if opts.progress != nil {
		opts.progress(1, "Tree-sitter Ingestion", len(stage1Out.Updated), len(stage1Out.Updated))
	}

	// Stage 2: GAST Normalization
	if opts.progress != nil {
		opts.progress(2, "GAST Normalization", 0, 0)
	}
	stage2Payload, err := stage2.Normalize(stage1Out, commitHash)
	if err != nil {
		return fmt.Errorf("stage 2 normalization failed: %w", err)
	}
	if opts.progress != nil {
		opts.progress(2, "GAST Normalization", len(stage2Payload.UpsertedTrees), len(stage2Payload.UpsertedTrees))
	}
	if verbose {
		fmt.Printf("Stage 2: Normalized %d syntax trees.\n", len(stage2Payload.UpsertedTrees))
	}

	// Stage 3: Topology Aggregation
	if opts.progress != nil {
		opts.progress(3, "Topology Aggregation", 0, 0)
	}
	stage3Out, err := stage3.Aggregate(stage2Payload, nil, absDir)
	if err != nil {
		return fmt.Errorf("stage 3 aggregation failed: %w", err)
	}
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
	// Default detail level is "architecture" — module/type/call/dependency
	// edges only. CFG/DFG and per-statement heuristic passes are opt-in via
	// --link-level=standard|full (AUDIT Issue 1.1 / Phase 1A-1).
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
	if verbose {
		if linkerCfg.LevelOfDetail == "architecture" {
			fmt.Println("Linker level: architecture (CFG/DFG disabled)")
		} else if linkerCfg.LevelOfDetail == "standard" {
			fmt.Println("Linker level: standard (aggregate CFG only)")
		} else if linkerCfg.LevelOfDetail == "full" {
			fmt.Println("Linker level: full (per-branch CFG+DFG, heuristic passes)")
		}
		if linkerCfg.MacroInference == "disabled" {
			fmt.Println("Macro inference: disabled")
		} else if linkerCfg.MacroInference == "structural" {
			fmt.Println("Macro inference: structural rules only")
		}
		if linkerCfg.MaxTotalNodes > 0 {
			fmt.Printf("Max nodes: %d\n", linkerCfg.MaxTotalNodes)
		}
	}
	if opts.progress != nil {
		opts.progress(4, "Semantic Linking", 0, 0)
	}
	cpg, err := stage4.Link(stage3Out, modifiedFiles, tm.GetActiveGraph(), linkerCfg)
	if err != nil {
		return fmt.Errorf("stage 4 linker failed: %w", err)
	}
	if opts.progress != nil {
		opts.progress(4, "Semantic Linking", len(cpg.GraphNodes), len(cpg.GraphNodes))
	}
	if verbose {
		fmt.Printf("Stage 4: Bound Delta CPG with %d new/modified nodes.\n", len(cpg.GraphNodes))
	}

	if opts.progress != nil {
		opts.progress(5, "Committing graph", 0, 1)
	}
	if err := tm.ExecuteDeltaTransaction(cpg, modifiedFiles); err != nil {
		return fmt.Errorf("failed to commit AKG transaction: %w", err)
	}
	if opts.progress != nil {
		opts.progress(5, "Committing graph", 1, 1)
	}

	duration := time.Since(start)

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

func init() {
	analyzeCmd.Flags().String("dir", ".", "Target repository directory to analyze")
	analyzeCmd.Flags().String("commit", "HEAD", "Git commit hash to tag the analysis")
	analyzeCmd.Flags().Bool("full", false, "Force a full clean scan of every file at full linker detail (default: incremental delta)")
	analyzeCmd.Flags().Int("workers", 0, "Number of parallel workers (default: CPUs)")
	analyzeCmd.Flags().String("link-level", "architecture", "Linker detail level: architecture (module/type/call/dep edges), standard (aggregate CFG), full (per-branch CFG+DFG)")
	analyzeCmd.Flags().String("macro-inference", "all", "Macro inference mode: disabled, structural (only rules with evidence), all (full heuristic+structural)")
	analyzeCmd.Flags().Int("max-nodes", 0, "Max total CPG nodes before warning/abort (0 = unlimited)")
	analyzeCmd.Flags().Bool("abort-on-limit", false, "Abort analysis if --max-nodes is exceeded (otherwise warn)")
	analyzeCmd.Flags().Bool("verbose", false, "Enable verbose output")
	analyzeCmd.Flags().Bool("json", false, "Emit machine-readable JSON instead of the human summary")
	rootCmd.AddCommand(analyzeCmd)
}
