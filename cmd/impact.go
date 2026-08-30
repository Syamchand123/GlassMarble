package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/impact_analyzer"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

var (
	impactDepthFlag     int
	impactTestsOnlyFlag bool
	impactJSONFlag      bool
	impactVisualizeFlag bool
	impactThresholdFlag int
)

var impactCmd = &cobra.Command{
	Use:     "impact [symbol or file]",
	Aliases: []string{"blast-radius"},
	GroupID: GroupInspect.ID,
	Short:   "Simulate refactoring blast-radius and transitive dependency impact",
	Long: `Performs topological reverse reachability analysis across the Architecture
Knowledge Graph to compute direct and transitive callers, impacted entrypoints,
and exposed test suites before refactoring or deleting a symbol.`,
	Example: `  # Analyze blast radius of modifying a struct or function
  gmb impact UserStore

  # View blast radius of changing a file with depth limit
  gmb impact internal/auth/auth.go --depth 3

  # Output only the test suites that must be re-run
  gmb impact ProcessPayment --tests-only

  # Render a visual Mermaid blast-radius flowchart
  gmb impact DatabaseDriver --visualize

  # Check in CI if refactoring risk exceeds threshold (fails if risk > 50)
  gmb impact LegacyEngine --threshold 50`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		targetQuery := args[0]
		rootDir := resolveDir(cmd)

		storageDir := filepath.Join(rootDir, ".glassmarble")
		tm, err := newAKGManager(storageDir, cmd)
		if err != nil {
			return fmt.Errorf("failed to open AKG database: %w — try 'gmb analyze'", err)
		}
		graph := tm.GetActiveSnapshot()
		if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
			return producterrs.Tagged("no analyzed Architecture Knowledge Graph found — try 'gmb analyze' first", producterrs.ErrEmptySubgraph)
		}

		opts := impact_analyzer.ImpactOptions{
			MaxDepth:  impactDepthFlag,
			TestsOnly: impactTestsOnlyFlag,
		}

		rep, err := impact_analyzer.AnalyzeImpact(graph, targetQuery, opts)
		if err != nil {
			return producterrs.Annotate(err, producterrs.ErrValidation)
		}

		if impactVisualizeFlag {
			diagram := impact_analyzer.RenderMermaidImpact(rep)
			fmt.Println(diagram)
			return nil
		}

		if impactTestsOnlyFlag {
			if len(rep.ImpactedTestFiles) == 0 {
				fmt.Printf("No test suites directly or transitively depend on %q\n", targetQuery)
			} else {
				fmt.Println(strings.Join(rep.ImpactedTestFiles, "\n"))
			}
			return nil
		}

		if impactJSONFlag {
			out, _ := json.MarshalIndent(rep, "", "  ")
			fmt.Println(string(out))
			if impactThresholdFlag > 0 && rep.RiskScore > impactThresholdFlag {
				return producterrs.Tagged(fmt.Sprintf("risk score %d exceeds threshold %d", rep.RiskScore, impactThresholdFlag), producterrs.ErrValidation)
			}
			return nil
		}

		fmt.Println(views.RenderImpactReport(rep))

		if impactThresholdFlag > 0 && rep.RiskScore > impactThresholdFlag {
			return producterrs.Tagged(fmt.Sprintf("risk score %d exceeds configured threshold of %d", rep.RiskScore, impactThresholdFlag), producterrs.ErrValidation)
		}

		return nil
	},
}

func init() {
	impactCmd.Flags().IntVarP(&impactDepthFlag, "depth", "d", 0, "Maximum reverse dependency traversal depth (0 = unlimited)")
	impactCmd.Flags().BoolVar(&impactTestsOnlyFlag, "tests-only", false, "Print only the list of impacted test files")
	impactCmd.Flags().BoolVar(&impactJSONFlag, "json", false, "Emit machine-readable JSON output")
	impactCmd.Flags().BoolVar(&impactVisualizeFlag, "visualize", false, "Render Mermaid blast-radius flowchart")
	impactCmd.Flags().IntVar(&impactThresholdFlag, "threshold", 0, "Fail with non-zero exit code if risk score exceeds threshold")

	rootCmd.AddCommand(impactCmd)
}
