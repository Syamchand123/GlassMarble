package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

var importCmd = &cobra.Command{
	Use:   "import [graph.json]",
	Short: "Import a portable graph document, replacing the active AKG snapshot",
	Long: `Replaces the active AKG database with the contents of a GraphJSON file
produced by ` + "`gmb export`" + ` (or any compatible GraphJSON document). The
previous snapshot is overwritten; the WAL is truncated after import.

Dangling references in the imported document are rejected so the persisted
state always stays verified.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		input := args[0]
		if dir == "" {
			dir = "."
		}

		f, err := os.Open(input)
		if err != nil {
			return fmt.Errorf("failed to open import file: %w", err)
		}
		defer f.Close()

		graph, err := akg.ImportGraphJSON(f)
		if err != nil {
			return fmt.Errorf("failed to import graph: %w", err)
		}

		storageDir := filepath.Join(dir, ".glassmarble")
		tm, err := newAKGManager(storageDir, cmd)
		if err != nil {
			return fmt.Errorf("failed to open AKG database: %w", err)
		}
		defer tm.Close()

		if err := tm.ReplaceGraph(graph); err != nil {
			return fmt.Errorf("import rejected: %w", err)
		}

		fmt.Println(views.RenderImportSuccess(input, storageDir, graph.Nodes.Len(), countGraphEdges(graph)))
		return nil
	},
}

// countGraphEdges totals outbound edges across the graph (used for the import
// confirmation line).
func countGraphEdges(graph *akg.CodePropertyGraph) int {
	count := 0
	graph.OutboundEdges.Iterate(func(_ string, edges []stage4.ResolvedEdge) {
		count += len(edges)
	})
	return count
}

func init() {
	importCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ database folder")
	rootCmd.AddCommand(importCmd)
}
