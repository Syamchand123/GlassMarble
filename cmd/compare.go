package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

var compareCmd = &cobra.Command{
	Use:     "compare [base_graph.json] [head_graph.json]",
	GroupID: GroupGovern.ID,
	Short:   "Diff two AKG snapshots and report the architectural changes",
	Long: `Compares two GraphJSON snapshots (e.g. main-branch export vs PR branch)
and prints the structural delta: added/removed nodes, added/removed edges, and
modified files. When run with no file arguments, diffs the committed AKG (git HEAD)
against the current working tree.`,
	Example: `  # Compare committed graph against uncommitted working tree
  gmb compare

  # Compare two exported graph JSON files
  gmb compare base.json head.json

  # Output architectural comparison as JSON
  gmb compare --json`,
	Args: cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")

		var base, head *akg.CodePropertyGraph
		var err error
		if len(args) == 2 {
			base, err = loadGraphJSONFile(args[0])
			if err != nil {
				return fmt.Errorf("failed to load base graph: %w — try verifying '%s'", err, args[0])
			}
			head, err = loadGraphJSONFile(args[1])
			if err != nil {
				return fmt.Errorf("failed to load head graph: %w — try verifying '%s'", err, args[1])
			}
		} else if len(args) == 0 {
			dir := resolveDir(cmd)
			base, head, err = loadWorkingTreeSnapshots(dir, cmd)
			if err != nil {
				return err
			}
		} else {
			return producterrs.Tagged("expected two graph files or --dir with no file arguments — try 'gmb compare [base.json head.json]'", producterrs.ErrValidation)
		}

		diff := akg.DiffGraphs(base, head)

		if asJSON {
			out, _ := json.MarshalIndent(diff, "", "  ")
			fmt.Println(string(out))
			return nil
		}

		fmt.Println(views.RenderCompare(diff))
		return nil
	},
}

func loadGraphJSONFile(path string) (*akg.CodePropertyGraph, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return akg.ImportGraphJSON(f)
}

func loadWorkingTreeSnapshots(dir string, cmd *cobra.Command) (*akg.CodePropertyGraph, *akg.CodePropertyGraph, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	storageDir := filepath.Join(absDir, ".glassmarble")
	baseTM, err := newAKGManager(storageDir, cmd)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open base AKG: %w — try 'gmb analyze'", err)
	}
	base := baseTM.GetActiveSnapshot()
	if base == nil || base.Nodes.Len() == 0 {
		return nil, nil, fmt.Errorf("AKG database is empty — try 'gmb analyze' first")
	}

	baseClone := base.Clone()

	if diff, err := ingest.CollectGitDiff(absDir, ""); err == nil && len(diff) == 0 {
		return baseClone, baseClone, nil
	}

	if err := runAnalysis(cmd, runAnalysisOptions{targetDir: absDir, full: true}); err != nil {
		return nil, nil, fmt.Errorf("working-tree analysis failed: %w — try 'gmb analyze --full'", err)
	}

	headTM, err := newAKGManager(storageDir, cmd)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open head AKG: %w — try 'gmb analyze'", err)
	}
	head := headTM.GetActiveSnapshot()
	if head == nil {
		return nil, nil, fmt.Errorf("working-tree analysis produced no graph")
	}
	return baseClone, head, nil
}

func init() {
	compareCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	rootCmd.AddCommand(compareCmd)
}
