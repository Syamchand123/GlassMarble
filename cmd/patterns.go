package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/config"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var patternsCmd = &cobra.Command{
	Use:     "patterns",
	GroupID: GroupGovern.ID,
	Short:   "Detect architectural patterns and smells from the committed AKG",
	Long: `Runs Architecture Intelligence against the committed AKG:
component inference, pattern detection (e.g. Clean Architecture, Event-Driven, DDD Bounded Context)
and — with --smells — code & structural smell detection.`,
	Example: `  # Run pattern detection on the active AKG
  gmb patterns

  # Include architectural smells in the output report
  gmb patterns --smells

  # Output detected patterns and smells as JSON
  gmb patterns --smells --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolveDir(cmd)
		asJSON, _ := cmd.Flags().GetBool("json")
		showSmells, _ := cmd.Flags().GetBool("smells")

		storageDir := filepath.Join(dir, ".glassmarble")
		tm, err := newAKGManager(storageDir, cmd)
		if err != nil {
			return fmt.Errorf("failed to open AKG database: %w — try 'gmb analyze'", err)
		}
		defer tm.Close()

		graph := tm.GetActiveSnapshot()
		if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
			if asJSON {
				out, _ := json.MarshalIndent(map[string]any{"error": "no active AKG database"}, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			return producterrs.Tagged("AKG database is empty — try 'gmb analyze' first", producterrs.ErrEmptySubgraph)
		}

		cfg := config.DefaultIntelligenceConfig()
		if local, lerr := loadIntelligenceConfig(storageDir); lerr == nil {
			cfg = local
		}

		opts := []arch_intelligence.EngineOption{
			arch_intelligence.WithConfig(cfg),
			arch_intelligence.WithLayerForbidden(cfgForbiddenPairs(storageDir)),
		}
		if !asJSON {
			opts = append(opts, arch_intelligence.WithLogger(func(format string, args ...any) {
				if verboseFlag(cmd) {
					fmt.Printf(format+"\n", args...)
				}
			}))
		}
		engine := arch_intelligence.NewEngineWithOptions(graph, opts...)
		res := engine.Run()

		if asJSON {
			type patternsJSON struct {
				Components []archmodel.DetectedComponent `json:"components"`
				Patterns   []archmodel.DetectedPattern   `json:"patterns"`
				Smells     []archmodel.ArchSmell         `json:"smells"`
				Metrics    archmodel.ArchMetrics         `json:"metrics"`
			}
			out, _ := json.MarshalIndent(patternsJSON{
				Components: res.Components,
				Patterns:   res.Patterns,
				Smells:     res.Smells,
				Metrics:    res.Metrics,
			}, "", "  ")
			fmt.Println(string(out))
			return nil
		}

		fmt.Println("=== Architecture Intelligence ===")
		fmt.Println("")
		if len(res.Patterns) == 0 {
			fmt.Println("Patterns: none detected")
		} else {
			fmt.Println("Patterns:")
			for _, p := range res.Patterns {
				fmt.Printf("  %-12s %-30s confidence=%.2f\n", p.Kind, p.Name, p.Confidence)
			}
		}
		fmt.Println("")
		fmt.Printf("Components: %d\n", len(res.Components))
		for _, c := range res.Components {
			fmt.Printf("  %-45s %-20s %d nodes\n", c.ID, c.Name, len(c.NodeIDs))
		}
		if showSmells {
			fmt.Println("")
			if len(res.Smells) == 0 {
				fmt.Println("Smells: none detected")
			} else {
				fmt.Println("Smells:")
				for _, s := range res.Smells {
					fmt.Printf("  [%s] %-10s %s\n", s.Severity, s.Kind, s.Title)
				}
			}
		}
		return nil
	},
}

func init() {
	patternsCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	patternsCmd.Flags().Bool("smells", false, "Also run smell detection and include smells in the report")
	rootCmd.AddCommand(patternsCmd)
}

// loadIntelligenceConfig reads the intelligence config from the project-local
// config.yaml, merging onto defaults so a bare file still parses.
func loadIntelligenceConfig(storageDir string) (*config.IntelligenceConfig, error) {
	cfg := config.DefaultIntelligenceConfig()
	data, err := os.ReadFile(filepath.Join(storageDir, "config.yaml"))
	if err != nil {
		return cfg, nil
	}
	var local config.Config
	if err := yaml.Unmarshal(data, &local); err != nil {
		return cfg, nil
	}
	if local.Intelligence != nil {
		cfg = local.Intelligence
	}
	return cfg, nil
}

// cfgForbiddenPairs returns the drift forbidden-dependency rules from the
// project config, or nil.
func cfgForbiddenPairs(storageDir string) []config.ForbiddenDepRule {
	if data, err := os.ReadFile(filepath.Join(storageDir, "config.yaml")); err == nil {
		var local config.Config
		if yaml.Unmarshal(data, &local) == nil {
			return local.Drift.ForbiddenDeps
		}
	}
	return nil
}

func verboseFlag(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	v, _ := cmd.Flags().GetBool("verbose")
	return v
}
