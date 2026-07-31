package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
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

		// Stage 1: Tree-sitter Ingestion
		cfg := stage1.DefaultConfig(absDir)
		if workers > 0 {
			cfg.WorkerCount = workers
		}

		stage1Out, err := stage1.RunIngestion(cfg)
		if err != nil {
			return fmt.Errorf("stage 1 ingestion failed: %w", err)
		}
		if verbose {
			fmt.Printf("Stage 1: Discovered and parsed %d source files.\n", len(stage1Out.Updated))
		}

		// Stage 2: GAST Normalization
		if full {
			// In full mode, perhaps we ignore diffs (but actually RunIngestion handles it)
		}

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
		tm, err := akg.NewAKGTransactionManager(storageDir)
		if err != nil {
			return fmt.Errorf("failed to initialize AKG transaction manager: %w", err)
		}
		defer tm.Close()

		var modifiedFiles []string
		for relPath := range stage2Payload.UpsertedTrees {
			modifiedFiles = append(modifiedFiles, relPath)
		}
		modifiedFiles = append(modifiedFiles, stage2Payload.DeletedPaths...)

		// Stage 4: CPG Linker (Incremental Delta Mode)
		linkerCfg := stage4.LinkerConfig{
			LevelOfDetail: "full",
		}
		if linkLevel, _ := cmd.Flags().GetString("link-level"); linkLevel != "" {
			linkerCfg.LevelOfDetail = linkLevel
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
		edgesCount := 0
		for _, edges := range cpg.OutboundEdges {
			edgesCount += len(edges)
		}

		fmt.Printf("Analyzed %d files | %d nodes | %d edges | %.1fs\n", len(stage1Out.Updated), len(cpg.GraphNodes), edgesCount, duration.Seconds())

		if verbose {
			fmt.Printf("AKG database updated at %s\n", filepath.Join(storageDir, "akg_state.ttl"))
		}

		return nil
	},
}

func init() {
	analyzeCmd.Flags().String("dir", ".", "Target repository directory to analyze")
	analyzeCmd.Flags().String("commit", "HEAD", "Git commit hash to tag the analysis")
	analyzeCmd.Flags().Bool("full", false, "Force a full clean rebuild of the AKG")
	analyzeCmd.Flags().Int("workers", 0, "Number of parallel workers (default: CPUs)")
	analyzeCmd.Flags().String("link-level", "full", "Linker detail level: architecture (no CFG/DFG), standard (aggregate CFG), full (per-branch CFG+DFG)")
	analyzeCmd.Flags().String("macro-inference", "all", "Macro inference mode: disabled, structural (only rules with evidence), all (full heuristic+structural)")
	analyzeCmd.Flags().Int("max-nodes", 0, "Max total CPG nodes before warning/abort (0 = unlimited)")
	analyzeCmd.Flags().Bool("abort-on-limit", false, "Abort analysis if --max-nodes is exceeded (otherwise warn)")
	analyzeCmd.Flags().Bool("verbose", false, "Enable verbose output")
	rootCmd.AddCommand(analyzeCmd)
}
