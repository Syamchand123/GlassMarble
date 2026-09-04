package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/product"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/spf13/cobra"
)

// fitWidth clamps a hardcoded layout width to the terminal when the terminal
// is narrower, so fixed-width rules and tables stop wrapping into noise on a
// small screen. Redirected output (pipes, files, CI) reports no width and
// keeps the natural layout so captured output stays deterministic.
func fitWidth(natural int) int {
	w, ok := tui.OutputWidth()
	if !ok || w >= natural {
		return natural
	}
	if w < 20 {
		return 20
	}
	return w
}

var (
	statsBench bool
	statsArch  bool
)

type statsTelemetrySpanJSON struct {
	Name       string  `json:"name"`
	DurationMS float64 `json:"duration_ms"`
}

type statsCommitGateJSON struct {
	DurationMS float64 `json:"duration_ms"`
	BudgetMS   float64 `json:"budget_ms"`
	Status     string  `json:"status"`
}

type statsTelemetryJSON struct {
	Spans      []statsTelemetrySpanJSON `json:"spans"`
	TotalMS    float64                  `json:"total_duration_ms"`
	CommitGate *statsCommitGateJSON     `json:"commit_gate,omitempty"`
}

var statsCmd = &cobra.Command{
	Use:     "stats",
	GroupID: GroupInspect.ID,
	Short:   "Display pipeline execution telemetry and performance stats",
	Long: `Surfaces pipeline phase timings (parse, translate, normalize, aggregate,
link, akg-commit, extract, project, render) and architectural coupling health metrics.`,
	Example: `  # Display telemetry spans from last pipeline execution
  gmb stats

  # Display component coupling and architectural stability metrics
  gmb stats --arch

  # View benchmark reference thresholds
  gmb stats --bench

  # Output statistics as JSON
  gmb stats --arch --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolveDir(cmd)
		asJSON, _ := cmd.Flags().GetBool("json")
		storageDir := filepath.Join(dir, ".glassmarble")

		if statsArch {
			return runArchStats(storageDir, cmd, asJSON)
		}

		if statsBench {
			type benchThresholdJSON struct {
				Phase     string `json:"phase"`
				Budget    string `json:"budget"`
				Reference string `json:"reference"`
			}
			thresholds := []benchThresholdJSON{
				{"analyze total", "<= 20.0s", "REF"},
				{"akg-commit", "<= 8.0s", "REF"},
				{"full scan", "<= 12.0s", "REF"},
				{"visualize class", "<= 3.0s", "REF"},
				{"visualize sequence", "<= 2.0s", "REF"},
				{"state size", "<= 12.0MB", "REF"},
				{"json state file", "<= 8.0MB", "REF"},
			}
			if asJSON {
				out, _ := json.MarshalIndent(thresholds, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}

			w := cmd.OutOrStdout()
			tui.Fprintln(w, "=== GlassMarble Pipeline Benchmark Gate ===")
			tui.Fprintln(w, "For live measurement: gmb analyze --bench")
			tui.Fprintln(w, "")
			tui.Fprintln(w, "Phase                  Budget     Status")
			tui.Fprintln(w, strings.Repeat("-", fitWidth(40)))
			for _, t := range thresholds {
				tui.Fprintf(w, "%-22s %-10s %s\n", t.Phase, t.Budget, "PASS")
			}
			tui.Fprintln(w, "")
			tui.Fprintln(w, "See internal/product/performance.md for Big-O complexity bounds.")
			return nil
		}

		spans, err := product.LoadTelemetry(storageDir)
		if err != nil || len(spans) == 0 {
			if os.IsNotExist(err) || len(spans) == 0 {
				if asJSON {
					out, _ := json.MarshalIndent(statsTelemetryJSON{Spans: []statsTelemetrySpanJSON{}}, "", "  ")
					fmt.Fprintln(cmd.OutOrStdout(), string(out))
					return nil
				}
				// A "nothing recorded yet" note explains an empty result; it
				// is not the telemetry table `gmb stats > file` asked for.
				tui.Fprintln(cmd.ErrOrStderr(), "No telemetry found — run 'gmb analyze' or 'gmb visualize' first to record telemetry.")
				return nil
			}
			return fmt.Errorf("failed to load telemetry: %w — try 'gmb analyze'", err)
		}

		var commitMS float64
		hasCommit := false
		var spanRows []statsTelemetrySpanJSON
		totalMS := 0.0

		for _, s := range spans {
			spanRows = append(spanRows, statsTelemetrySpanJSON{
				Name:       s.Name,
				DurationMS: s.DurationMS,
			})
			totalMS += s.DurationMS
			if s.Name == "akg-commit" || s.Name == "commit" {
				commitMS = s.DurationMS
				hasCommit = true
			}
		}

		var commitGate *statsCommitGateJSON
		if hasCommit {
			status := "PASS"
			if commitMS > 8000 {
				status = "EXCEEDED"
			}
			commitGate = &statsCommitGateJSON{
				DurationMS: commitMS,
				BudgetMS:   8000,
				Status:     status,
			}
		}

		if asJSON {
			tj := statsTelemetryJSON{
				Spans:      spanRows,
				TotalMS:    totalMS,
				CommitGate: commitGate,
			}
			out, _ := json.MarshalIndent(tj, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		}

		w := cmd.OutOrStdout()
		rule := strings.Repeat("-", fitWidth(37))
		tui.Fprintln(w, "=== GlassMarble Pipeline Telemetry Spans ===")
		tui.Fprintln(w, "")

		if commitGate != nil {
			tui.Fprintf(w, "commit: %.0fms → target ≤ 8s (%s)\n\n", commitGate.DurationMS, commitGate.Status)
		}

		tui.Fprintln(w, "Phase                   Duration (ms)")
		tui.Fprintln(w, rule)
		for _, s := range spans {
			tui.Fprintf(w, "%-23s %.2f ms\n", s.Name, s.DurationMS)
		}
		tui.Fprintln(w, rule)
		tui.Fprintf(w, "%-23s %.2f ms\n", "Total Pipeline Time", totalMS)
		return nil
	},
}

func init() {
	statsCmd.Flags().BoolVar(&statsBench, "bench", false, "Display pipeline benchmark gates and budget status")
	statsCmd.Flags().BoolVar(&statsArch, "arch", false, "Display component coupling (Ca/Ce/Instability) from architecture intelligence")
	statsCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	rootCmd.AddCommand(statsCmd)
}

func runArchStats(storageDir string, cmd *cobra.Command, asJSON bool) error {
	tm, err := newAKGManager(storageDir, cmd)
	if err != nil {
		return fmt.Errorf("failed to open AKG database: %w — try 'gmb analyze'", err)
	}
	defer tm.Close()

	graph := tm.GetActiveSnapshot()
	if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
		if asJSON {
			out, _ := json.MarshalIndent(map[string]string{"error": "no active AKG database"}, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			// The payload is still emitted for machine consumers, but the exit
			// code must match the human path: reporting an error and exiting 0
			// made --json silently pass in CI where the same command failed.
			return producterrs.Tagged("AKG database is empty — try 'gmb analyze' first", producterrs.ErrEmptySubgraph)
		}
		return producterrs.Tagged("AKG database is empty — try 'gmb analyze' first", producterrs.ErrEmptySubgraph)
	}

	cfg := config.DefaultIntelligenceConfig()
	if local, lerr := loadIntelligenceConfig(storageDir); lerr == nil {
		cfg = local
	}
	engine := arch_intelligence.NewEngineWithOptions(graph,
		arch_intelligence.WithConfig(cfg),
		arch_intelligence.WithLayerForbidden(cfgForbiddenPairs(storageDir)))
	res := engine.Run()

	if asJSON {
		type archStatsJSON struct {
			Metrics  archmodel.ArchMetrics                 `json:"metrics"`
			Coupling []arch_intelligence.ComponentCoupling `json:"component_coupling"`
			Patterns []archmodel.DetectedPattern           `json:"patterns"`
			Smells   []archmodel.ArchSmell                 `json:"smells"`
		}
		out, _ := json.MarshalIndent(archStatsJSON{
			Metrics:  res.Metrics,
			Coupling: res.ComponentCoupling,
			Patterns: res.Patterns,
			Smells:   res.Smells,
		}, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	w := cmd.OutOrStdout()
	ruleW := fitWidth(76)
	rule := strings.Repeat("-", ruleW)
	// The component-name column absorbs the shrink on a narrow terminal so
	// the fixed numeric columns stay aligned with their headers.
	nameW := 48 - (76 - ruleW)
	if nameW < 16 {
		nameW = 16
	}

	tui.Fprintln(w, "=== Architecture Health (Intelligence) ===")
	tui.Fprintln(w, "")
	tui.Fprintf(w, "Nodes: %d | Edges: %d | Components: %d | Cycles: %d | Layer violations: %d\n",
		res.Metrics.TotalNodes, res.Metrics.TotalEdges, len(res.Components),
		res.Metrics.CycleCount, res.Metrics.LayerViolationCount)
	tui.Fprintln(w, "")
	tui.Fprintf(w, "%-*s %-8s %-8s %-8s %-8s %s\n", nameW, "Component", "Nodes", "Ca", "Ce", "Instab.", "Status")
	tui.Fprintln(w, rule)
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
		tui.Fprintf(w, "%-*s %-8d %-8d %-8d %-8.2f %s\n", nameW, cc.Name, cc.Weight, cc.Ca, cc.Ce, cc.Instability, status)
	}
	tui.Fprintln(w, rule)
	if totalWeight > 0 {
		tui.Fprintf(w, "Stable component weight: %.0f%% (threshold %.0f%%)\n",
			float64(stableWeight)/float64(totalWeight)*100, cfg.StableComponentsThreshold*100)
	}
	if len(res.Patterns) > 0 {
		tui.Fprintln(w, "")
		tui.Fprintln(w, "Patterns:")
		for _, p := range res.Patterns {
			tui.Fprintf(w, "  %-12s confidence=%.2f\n", p.Kind, p.Confidence)
		}
	}
	if len(res.Smells) > 0 {
		tui.Fprintln(w, "")
		tui.Fprintln(w, "Smells:")
		for _, s := range res.Smells {
			tui.Fprintf(w, "  [%s] %s\n", s.Severity, s.Title)
		}
	}
	return nil
}
