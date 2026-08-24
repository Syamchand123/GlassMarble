package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/drift"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var driftCmd = &cobra.Command{
	Use:   "drift",
	Short: "Detect architecture drift against declared layering and cycle budgets",
	Long: `Compares the committed AKG against the invariants declared in
.glassmarble/config.yaml under the "drift" key:

  drift:
    layers:
      - name: presentation
        paths: ["cmd/web/**"]
      - name: domain
        paths: ["internal/domain/**"]
    forbidden_deps:
      - source: presentation
        target: domain
    cycle_budget: 3

Reports forbidden cross-layer dependencies and layer cycles, and exits non-zero
when the declared cycle budget is exceeded.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		asJSON, _ := cmd.Flags().GetBool("json")
		if dir == "" {
			dir = "."
		}

		storageDir := filepath.Join(dir, ".glassmarble")
		tm, err := newAKGManager(storageDir, cmd)
		if err != nil {
			return fmt.Errorf("failed to open AKG database: %w", err)
		}

		graph := tm.GetActiveSnapshot()
		if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
			return producterrs.Tagged(fmt.Sprintf("AKG database is empty -- run 'glassmarble analyze' first"), producterrs.ErrEmptySubgraph)
		}

		// C6-D17: single read from dir/.glassmarble/config.yaml like
		// patterns.go:loadIntelligenceConfig — avoids double I/O and
		// inconsistency when --dir points away from CWD.
		cfg := config.Config{}
		if data, rerr := os.ReadFile(filepath.Join(storageDir, "config.yaml")); rerr == nil {
			var local config.Config
			if yerr := yaml.Unmarshal(data, &local); yerr == nil {
				cfg = local
			}
		}
		if cfg.Drift.Layers == nil && cfg.Drift.ForbiddenDeps == nil {
			if global, gerr := config.Load(config.Config{}); gerr == nil {
				if cfg.Drift.Layers == nil {
					cfg.Drift.Layers = global.Drift.Layers
				}
				if cfg.Drift.ForbiddenDeps == nil {
					cfg.Drift.ForbiddenDeps = global.Drift.ForbiddenDeps
				}
				if cfg.Drift.CycleBudget == 0 {
					cfg.Drift.CycleBudget = global.Drift.CycleBudget
				}
			}
		}

		rep := drift.Analyze(graph, cfg.Drift)

		if asJSON {
			out, _ := json.MarshalIndent(rep, "", "  ")
			fmt.Println(string(out))
		} else {
			fmt.Println(views.RenderDrift(rep))
		}

		if rep.ExceedsBudget() || rep.ForbiddenEdges > 0 {
			return fmt.Errorf("architecture drift detected: %d forbidden dependency(ies), %d cycle(s) exceed budget %d",
				rep.ForbiddenEdges, rep.CycleCount, rep.CycleBudget)
		}
		return nil
	},
}

func init() {
	driftCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ database folder")
	driftCmd.Flags().Bool("json", false, "Emit machine-readable JSON instead of the human report")
	rootCmd.AddCommand(driftCmd)
}
