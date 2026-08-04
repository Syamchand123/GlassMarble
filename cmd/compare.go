package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

var compareCmd = &cobra.Command{
	Use:   "compare [base_graph.json] [head_graph.json]",
	Short: "Diff two AKG snapshots and report the architectural changes",
	Long: `Compares two GraphJSON snapshots (e.g. the main-branch export vs the
current PR branch) and prints the structural delta: added/removed nodes,
added/removed edges, and the files touched. This is the command the CI
workflow runs to produce its PR architecture comment.

  gmb compare base.json head.json

With --dir instead of two files, the base is the previously committed snapshot
(git HEAD) and the head is the current working tree.`,
	Args: cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")

		var base, head *akg.CodePropertyGraph
		var err error
		if len(args) == 2 {
			base, err = loadGraphJSONFile(args[0])
			if err != nil {
				return fmt.Errorf("failed to load base graph: %w", err)
			}
			head, err = loadGraphJSONFile(args[1])
			if err != nil {
				return fmt.Errorf("failed to load head graph: %w", err)
			}
		} else if len(args) == 0 {
			dir, _ := cmd.Flags().GetString("dir")
			if dir == "" {
				dir = "."
			}
			base, head, err = loadWorkingTreeSnapshots(dir, cmd)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("expected two graph files, or --dir with no args")
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

// loadGraphJSONFile reads a GraphJSON document from disk.
func loadGraphJSONFile(path string) (*akg.CodePropertyGraph, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return akg.ImportGraphJSON(f)
}

// loadWorkingTreeSnapshots compares the committed AKG (git HEAD) with the
// current working tree. The base is read from .glassmarble/akg_state.ttl; the
// head is produced by a fresh analysis of the working tree, then both are
// normalized to GraphJSON.
func loadWorkingTreeSnapshots(dir string, cmd *cobra.Command) (*akg.CodePropertyGraph, *akg.CodePropertyGraph, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	storageDir := filepath.Join(absDir, ".glassmarble")
	baseTM, err := newAKGManager(storageDir, cmd)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open base AKG: %w", err)
	}
	base := baseTM.GetActiveSnapshot()
	if base == nil || base.Nodes.Len() == 0 {
		return nil, nil, fmt.Errorf("AKG database is empty -- run 'glassmarble analyze' first")
	}

	// Clone the base so the head analysis does not mutate the snapshot.
	baseClone := base.Clone()

	if err := runAnalysis(runAnalysisOptions{targetDir: absDir, full: true}); err != nil {
		return nil, nil, fmt.Errorf("working-tree analysis failed: %w", err)
	}

	headTM, err := newAKGManager(storageDir, cmd)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open head AKG: %w", err)
	}
	head := headTM.GetActiveSnapshot()
	if head == nil {
		return nil, nil, fmt.Errorf("working-tree analysis produced no graph")
	}
	return baseClone, head, nil
}

func init() {
	compareCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ database folder (used with no file args)")
	compareCmd.Flags().Bool("json", false, "Emit machine-readable JSON instead of the human report")
	rootCmd.AddCommand(compareCmd)
}
