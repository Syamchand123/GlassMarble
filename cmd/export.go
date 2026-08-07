package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export the AKG snapshot to a portable graph document (JSON or Turtle)",
	Long: `Writes the active AKG snapshot to a machine-readable, diff-friendly file:

  gmb export --output graph.json        (GraphJSON interchange format)
  gmb export --output graph.ttl         (canonical RDF Turtle)

GraphJSON is the recommended interchange format: it is lossless (edge
confidence, parallel edges), deterministic, and trivially reviewable in pull
requests. Turtle matches the legacy akg_state.ttl format and is offered for
interoperability with RDF tooling.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		output, _ := cmd.Flags().GetString("output")
		if dir == "" {
			dir = "."
		}
		if output == "" {
			return producterrs.Tagged(fmt.Sprintf("--output is required (e.g. graph.json or graph.ttl)"), producterrs.ErrValidation)
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

		f, err := os.Create(output)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer f.Close()

		format := "graphjson"
		switch filepath.Ext(output) {
		case ".json":
			if err := akg.ExportGraphJSON(graph, f); err != nil {
				return fmt.Errorf("failed to export graph JSON: %w", err)
			}
		case ".ttl", ".turtle":
			format = "turtle"
			if err := akg.SerializeToTurtle(graph, f); err != nil {
				return fmt.Errorf("failed to export Turtle: %w", err)
			}
		default:
			return producterrs.Tagged(fmt.Sprintf("unsupported output format %q (use .json or .ttl)", filepath.Ext(output)), producterrs.ErrValidation)
		}

		st, _ := f.Stat()
		fmt.Println(views.RenderExportSuccess(format, output, graph.Nodes.Len(), st.Size()))
		return nil
	},
}

func init() {
	exportCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ database folder")
	exportCmd.Flags().StringP("output", "o", "", "Output file path (graph.json or graph.ttl)")
	rootCmd.AddCommand(exportCmd)
}
