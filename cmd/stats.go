package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/product"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/spf13/cobra"
)

var (
	statsLast  bool
	statsBench bool
	statsArch  bool
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Display pipeline execution telemetry and performance stats",
	Long:  `Surfaces pipeline phase timings (parse, translate, normalize, aggregate, link, akg-commit, extract, project, render) and benchmark budget gates.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		if dir == "" {
			dir = "."
		}
		storageDir := filepath.Join(dir, ".glassmarble")

		if statsArch {
			return runArchStats(storageDir, cmd)
		}

		if statsBench {
			// C6-D6: this branch previously printed a hardcoded PASS table
			// without measuring. Mark it clearly as static reference
			// thresholds; the live gate is `gmb analyze --bench`.
			fmt.Println("=== GlassMarble Pipeline Benchmark Reference Thresholds (static; not live measurement) ===")
			fmt.Println("For a live measurement run: gmb analyze --bench")
			fmt.Println("")
			fmt.Println("Phase                  Budget     Reference")
			fmt.Println("----------------------------------------")
			fmt.Println("analyze total          <= 20.0s   REF")
			fmt.Println("akg-commit             <= 8.0s    REF")
			fmt.Println("full scan              <= 12.0s   REF")
			fmt.Println("visualize class        <= 3.0s    REF")
			fmt.Println("visualize sequence     <= 2.0s    REF")
			fmt.Println("state size             <= 12.0MB  REF")
			fmt.Println("json state file        <= 8.0MB   REF")
			fmt.Println("")
			fmt.Println("See internal/product/performance.md for complete Big-O complexity bounds.")
			return nil
		}

		spans, err := product.LoadTelemetry(storageDir)
		if err != nil || len(spans) == 0 {
			if os.IsNotExist(err) {
				fmt.Println("No telemetry found. Run 'glassmarble analyze' or 'glassmarble visualize' to record telemetry.")
				return nil
			}
			return fmt.Errorf("failed to load telemetry: %w", err)
		}

		fmt.Println("=== GlassMarble Pipeline Telemetry Spans ===")
		fmt.Println("")

		var commitMS float64
		hasCommit := false
		for _, s := range spans {
			if s.Name == "akg-commit" || s.Name == "commit" {
				commitMS = s.DurationMS
				hasCommit = true
			}
		}
		if hasCommit {
			status := "PASS"
			if commitMS > 8000 {
				status = "EXCEEDED"
			}
			fmt.Printf("commit: %.0fms → target ≤ 8s (%s)\n\n", commitMS, status)
		}

		fmt.Println("Phase                   Duration (ms)")
		fmt.Println("-------------------------------------")
		totalMS := 0.0
		for _, s := range spans {
			fmt.Printf("%-23s %.2f ms\n", s.Name, s.DurationMS)
			totalMS += s.DurationMS
		}
		fmt.Println("-------------------------------------")
		fmt.Printf("%-23s %.2f ms\n", "Total Pipeline Time", totalMS)
		return nil
	},
}

func init() {
	statsCmd.Flags().BoolVar(&statsLast, "last", true, "Display telemetry spans for the last pipeline execution")
	statsCmd.Flags().BoolVar(&statsBench, "bench", false, "Display pipeline benchmark gates and budget status")
	statsCmd.Flags().BoolVar(&statsArch, "arch", false, "Display architecture health: component coupling (Ca/Ce/Instability) from architecture intelligence")
	statsCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ database folder")
	rootCmd.AddCommand(statsCmd)
}

// runArchStats runs architecture intelligence against the committed AKG and prints the
// component-level coupling table (Ca, Ce, Instability, stability status).
func runArchStats(storageDir string, cmd *cobra.Command) error {
	tm, err := newAKGManager(storageDir, cmd)
	if err != nil {
		return fmt.Errorf("failed to open AKG database: %w", err)
	}
	defer tm.Close()

	graph := tm.GetActiveSnapshot()
	if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
		return producterrs.Tagged(fmt.Sprintf("AKG database is empty -- run 'glassmarble analyze' first"), producterrs.ErrEmptySubgraph)
	}

	cfg := config.DefaultIntelligenceConfig()
	if local, lerr := loadIntelligenceConfig(storageDir); lerr == nil {
		cfg = local
	}
	engine := arch_intelligence.NewEngineWithOptions(graph,
		arch_intelligence.WithConfig(cfg),
		arch_intelligence.WithLayerForbidden(cfgForbiddenPairs(storageDir)))
	res := engine.Run()

	fmt.Println("=== Architecture Health (Intelligence) ===")
	fmt.Println("")
	fmt.Printf("Nodes: %d | Edges: %d | Components: %d | Cycles: %d | Layer violations: %d\n",
		res.Metrics.TotalNodes, res.Metrics.TotalEdges, len(res.Components),
		res.Metrics.CycleCount, res.Metrics.LayerViolationCount)
	fmt.Println("")
	fmt.Printf("%-48s %-8s %-8s %-8s %-8s %s\n", "Component", "Nodes", "Ca", "Ce", "Instab.", "Status")
	fmt.Println("----------------------------------------------------------------------------")
	stableWeight := 0
	totalWeight := 0
	for _, cc := range res.ComponentCoupling {
		totalWeight += cc.Weight
		status := "STABLE"
		if cc.Instability > cfg.UnstableThreshold {
			status = "UNSTABLE"
		} else {
			stableWeight += cc.Weight
		}
		fmt.Printf("%-48s %-8d %-8d %-8d %-8.2f %s\n", cc.Name, cc.Weight, cc.Ca, cc.Ce, cc.Instability, status)
	}
	fmt.Println("----------------------------------------------------------------------------")
	if totalWeight > 0 {
		fmt.Printf("Stable component weight: %.0f%% (threshold %.0f%%)\n",
			float64(stableWeight)/float64(totalWeight)*100, cfg.StableComponentsThreshold*100)
	}
	if len(res.Patterns) > 0 {
		fmt.Println("")
		fmt.Println("Patterns:")
		for _, p := range res.Patterns {
			fmt.Printf("  %-12s confidence=%.2f\n", p.Kind, p.Confidence)
		}
	}
	if len(res.Smells) > 0 {
		fmt.Println("")
		fmt.Println("Smells:")
		for _, s := range res.Smells {
			fmt.Printf("  [%s] %s\n", s.Severity, s.Title)
		}
	}
	return nil
}
