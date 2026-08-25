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
	Use:     "drift",
	GroupID: GroupGovern.ID,
	Short:   "Detect architecture drift against declared layering and cycle budgets",
	Long: `Compares the committed AKG against the invariants declared in
.glassmarble/config.yaml under the "drift" key.

Reports forbidden cross-layer dependencies and layer cycles, and exits non-zero
when declared cycle budgets or forbidden dependencies are breached (suitable for CI gates).`,
	Example: `  # Check current architecture for layer drift and cycles
  gmb drift

  # Output drift report as JSON for CI gating
  gmb drift --json

  # Run drift checks on a specific directory
  gmb drift --dir ./backend`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolveDir(cmd)
		asJSON, _ := cmd.Flags().GetBool("json")

		storageDir := filepath.Join(dir, ".glassmarble")
		tm, err := newAKGManager(storageDir, cmd)
		if err != nil {
			return fmt.Errorf("failed to open AKG database: %w — try 'gmb analyze'", err)
		}

		graph := tm.GetActiveSnapshot()
		if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
			if asJSON {
				out, _ := json.MarshalIndent(map[string]string{"error": "no active AKG database"}, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			return producterrs.Tagged("AKG database is empty — try 'gmb analyze' first", producterrs.ErrEmptySubgraph)
		}

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
			return fmt.Errorf("architecture drift detected: %d forbidden dependency(ies), %d cycle(s) exceed budget %d — try inspecting violations with 'gmb inspect'",
				rep.ForbiddenEdges, rep.CycleCount, rep.CycleBudget)
		}
		return nil
	},
}

func init() {
	driftCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	rootCmd.AddCommand(driftCmd)
}
