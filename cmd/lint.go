package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/arch_linter"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

var (
	lintRulesFlag      string
	lintInitFlag       bool
	lintStrictFlag     bool
	lintJSONFlag       bool
	lintFailOnWarnFlag bool
)

var lintCmd = &cobra.Command{
	Use:     "lint",
	GroupID: GroupGovern.ID,
	Short:   "Lint repository against architectural rules and layer boundaries",
	Long: `Evaluates the Architecture Knowledge Graph against custom declarative rules
(e.g., .glassmarble/rules.yaml) to catch illegal layer imports, forbidden dependencies,
and cyclic coupling. Returns exit code 1 if violations are found.`,
	Example: `  # Run architecture linter with default .glassmarble/rules.yaml
  gmb lint

  # Scaffold a starter rules.yaml file
  gmb lint --init

  # Lint with custom rules file and strict mode
  gmb lint --rules my-rules.yaml --strict

  # Output lint results as JSON for CI/CD pipelines
  gmb lint --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		rootDir := resolveDir(cmd)

		if lintInitFlag {
			targetPath := lintRulesFlag
			if targetPath == "" {
				targetPath = filepath.Join(rootDir, ".glassmarble", "rules.yaml")
			} else if !filepath.IsAbs(targetPath) {
				targetPath = filepath.Join(rootDir, targetPath)
			}

			createdPath, err := arch_linter.ScaffoldRules(targetPath)
			if err != nil {
				return err
			}
			tui.Fprintf(cmd.OutOrStdout(), "✓ Created starter architecture rules at %s\n", createdPath)
			tui.Fprintln(cmd.OutOrStdout(), "Run 'gmb lint' to evaluate your repository against these rules.")
			return nil
		}

		// Locate rules file
		rulesPath := lintRulesFlag
		if rulesPath == "" {
			candidates := []string{
				filepath.Join(rootDir, ".glassmarble", "rules.yaml"),
				filepath.Join(rootDir, ".glassmarble", "rules.yml"),
				filepath.Join(rootDir, ".gmb-rules.yaml"),
				filepath.Join(rootDir, "gmb-rules.yaml"),
			}
			for _, c := range candidates {
				if _, err := os.Stat(c); err == nil {
					rulesPath = c
					break
				}
			}
		} else if !filepath.IsAbs(rulesPath) {
			rulesPath = filepath.Join(rootDir, rulesPath)
		}

		if rulesPath == "" {
			return producterrs.Tagged("no architecture rules file found — run 'gmb lint --init' to generate a starter .glassmarble/rules.yaml", producterrs.ErrValidation)
		}

		ruleset, err := arch_linter.LoadRules(rulesPath)
		if err != nil {
			return producterrs.Annotate(err, producterrs.ErrValidation)
		}

		// Load AKG Graph
		storageDir := filepath.Join(rootDir, ".glassmarble")
		tm, err := newAKGManager(storageDir, cmd)
		if err != nil {
			return fmt.Errorf("failed to open AKG database: %w — try 'gmb analyze'", err)
		}
		graph := tm.GetActiveSnapshot()
		if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
			return producterrs.Tagged("no analyzed Architecture Knowledge Graph found — try 'gmb analyze' first", producterrs.ErrEmptySubgraph)
		}

		// Run Lint
		res, err := arch_linter.Lint(graph, ruleset)
		if err != nil {
			return fmt.Errorf("lint evaluation failed: %w", err)
		}

		if lintJSONFlag {
			out, _ := json.MarshalIndent(res, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			if !res.Passed || (lintStrictFlag && res.WarningsCount > 0) || (lintFailOnWarnFlag && res.WarningsCount > 0) {
				return producterrs.Tagged(fmt.Sprintf("%d architectural violations detected", res.ViolationsTotal), producterrs.ErrPolicyViolation)
			}
			return nil
		}

		relRulesPath, _ := filepath.Rel(rootDir, rulesPath)
		if relRulesPath == "" {
			relRulesPath = rulesPath
		}
		tui.Fprintln(cmd.OutOrStdout(), views.RenderLintResult(res, relRulesPath))

		if !res.Passed || (lintStrictFlag && res.WarningsCount > 0) || (lintFailOnWarnFlag && res.WarningsCount > 0) {
			return producterrs.Tagged(fmt.Sprintf("%d architectural violation(s) detected — check report above", res.ViolationsTotal), producterrs.ErrPolicyViolation)
		}

		return nil
	},
}

func init() {
	lintCmd.Flags().StringVarP(&lintRulesFlag, "rules", "r", "", "Path to architecture rules YAML file")
	lintCmd.Flags().BoolVar(&lintInitFlag, "init", false, "Scaffold a starter .glassmarble/rules.yaml file")
	lintCmd.Flags().BoolVar(&lintStrictFlag, "strict", false, "Treat warnings as errors (fail on any violation)")
	lintCmd.Flags().BoolVar(&lintFailOnWarnFlag, "fail-on-warn", false, "Exit with non-zero status on warnings")
	lintCmd.Flags().BoolVar(&lintJSONFlag, "json", false, "Emit machine-readable JSON output")

	rootCmd.AddCommand(lintCmd)
}
