package cmd

import (
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
	"github.com/spf13/cobra"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Run full source code ingestion and build Architecture Knowledge Graph (AKG)",
	Long:  `Executes Stage 1 (ingestion), Stage 2 (normalization), Stage 3 (topology aggregation), Stage 4 (CPG linking), and commits the graph state to AKG.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		start := time.Now()
		targetDir, _ := cmd.Flags().GetString("dir")
		commitHash, _ := cmd.Flags().GetString("commit")
		full, _ := cmd.Flags().GetBool("full")
		workers, _ := cmd.Flags().GetInt("workers")
		verbose, _ := cmd.Flags().GetBool("verbose")
		if targetDir == "" {
			targetDir = "."
		}

		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}

		fmt.Printf("Starting GlassMarble Analysis on %s...\n", absDir)

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

		// Stage 2: GAST Normalization
		stage2Payload, err := stage2.Normalize(stage1Out, commitHash)
		if err != nil {
			return fmt.Errorf("stage 2 normalization failed: %w", err)
		}
		if verbose {
			fmt.Printf("Stage 2: Normalized %d syntax trees.\n", len(stage2Payload.UpsertedTrees))
		}

		// Stage 3: Topology Aggregation
		stage3Out, err := stage3.Aggregate(stage2Payload, nil)
		if err != nil {
			return fmt.Errorf("stage 3 aggregation failed: %w", err)
		}
		if verbose {
			fmt.Printf("Stage 3: Built topology with %d global definition symbols.\n", len(stage3Out.GlobalDefinitionIndex))
		}

		// Initialize AKG before Stage 4 so we have a persistent GraphDB for incremental lookups
		storageDir := filepath.Join(absDir, ".glassmarble")
		tm, err := newAKGManager(storageDir, cmd)
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
		if linkLevel, _ := cmd.Flags().GetString("link-level"); linkLevel != "" {
			linkerCfg.LevelOfDetail = linkLevel
		}
		if full {
			linkerCfg.LevelOfDetail = stage4.LevelFull
		}
		if mi, _ := cmd.Flags().GetString("macro-inference"); mi != "" {
			linkerCfg.MacroInference = mi
		}
		if maxNodes, _ := cmd.Flags().GetInt("max-nodes"); maxNodes > 0 {
			linkerCfg.MaxTotalNodes = maxNodes
		}
		if abortOnLimit, _ := cmd.Flags().GetBool("abort-on-limit"); abortOnLimit {
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
		cpg, err := stage4.Link(stage3Out, modifiedFiles, tm.GetActiveGraph(), linkerCfg)
		if err != nil {
			return fmt.Errorf("stage 4 linker failed: %w", err)
		}
		if verbose {
			fmt.Printf("Stage 4: Bound Delta CPG with %d new/modified nodes.\n", len(cpg.GraphNodes))
		}

		if err := tm.ExecuteDeltaTransaction(cpg, modifiedFiles); err != nil {
			return fmt.Errorf("failed to commit AKG transaction: %w", err)
		}

		duration := time.Since(start)

		// Quality budget report (AUDIT Issue 1 Phase 1C-10 / Issue 5 item 3).
		// Measured on the committed MERGED graph (base + delta): the delta
		// payload alone would count cross-file edges as dangling. Sizes are
		// part of the noise budget so bloat stays visible.
		q := akg.MeasureGraphQuality(tm.GetActiveGraph())
		ttlSize, walSize := akgStorageSizes(storageDir)
		fmt.Printf("Analyzed %d files | %d nodes | %d edges | %d virtual | %d dangling | ttl=%s wal=%s | %.1fs\n",
			len(stage1Out.Updated), q.TotalNodes, q.TotalEdges, q.VirtualNodes, q.DanglingEdges, humanBytes(ttlSize), humanBytes(walSize), duration.Seconds())
		if q.DanglingEdges > 0 {
			fmt.Printf("WARNING: %d edges reference missing nodes (dangling). Run `gmb analyze --full` to rebuild.\n", q.DanglingEdges)
		}

		if verbose {
			fmt.Printf("AKG database updated at %s\n", filepath.Join(storageDir, "akg_state.ttl"))
		}

		return nil
	},
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
	rootCmd.AddCommand(analyzeCmd)
}
