package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

type importReceiptJSON struct {
	Status     string `json:"status"`
	InputFile  string `json:"input_file"`
	StorageDir string `json:"storage_dir"`
	Nodes      int    `json:"nodes"`
	Edges      int    `json:"edges"`
}

var importCmd = &cobra.Command{
	Use:     "import [graph.json]",
	GroupID: GroupVisualize.ID,
	Short:   "Import a portable graph document, replacing the active AKG snapshot",
	Long: `Replaces the active AKG database with the contents of a GraphJSON file
produced by 'gmb export' (or any compatible GraphJSON document). The
previous snapshot is overwritten by a single atomic akg.json write.

Dangling references in the imported document are rejected so the persisted
state always stays verified.`,
	Example: `  # Import a GraphJSON snapshot into active repository
  gmb import graph.json

  # Import and emit JSON receipt
  gmb import graph.json --json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolveDir(cmd)
		input := args[0]
		asJSON, _ := cmd.Flags().GetBool("json")

		f, err := os.Open(input)
		if err != nil {
			return fmt.Errorf("failed to open import file %q: %w — try verifying the file exists", input, err)
		}
		defer f.Close()

		graph, err := akg.ImportGraphJSON(f)
		if err != nil {
			return fmt.Errorf("failed to import graph: %w — try validating the GraphJSON format", err)
		}

		storageDir := filepath.Join(dir, ".glassmarble")
		tm, err := newAKGManager(storageDir, cmd)
		if err != nil {
			return fmt.Errorf("failed to open AKG database: %w — try 'gmb analyze'", err)
		}
		defer tm.Close()

		if err := tm.ReplaceGraph(graph); err != nil {
			return fmt.Errorf("import rejected: %w", err)
		}

		edges := countGraphEdges(graph)
		if asJSON {
			receipt := importReceiptJSON{
				Status:     "success",
				InputFile:  input,
				StorageDir: storageDir,
				Nodes:      graph.Nodes.Len(),
				Edges:      edges,
			}
			out, _ := json.MarshalIndent(receipt, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		}

		tui.Fprintln(cmd.OutOrStdout(), views.RenderImportSuccess(input, storageDir, graph.Nodes.Len(), edges))
		return nil
	},
}

// countGraphEdges totals outbound edges across the graph.
func countGraphEdges(graph *akg.CodePropertyGraph) int {
	count := 0
	graph.OutboundEdges.Iterate(func(_ string, edges []link.ResolvedEdge) {
		count += len(edges)
	})
	return count
}

func init() {
	importCmd.Flags().Bool("json", false, "Emit machine-readable JSON receipt")
	rootCmd.AddCommand(importCmd)
}
