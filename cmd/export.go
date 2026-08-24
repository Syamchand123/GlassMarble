package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export the AKG snapshot to GraphJSON or Neo4j Cypher format",
	Long: `Writes the active AKG snapshot to a machine-readable, diff-friendly file:

  gmb export --output graph.json                 (GraphJSON interchange format)
  gmb export --format neo4j --output dump.cypher  (Neo4j Cypher import script)

GraphJSON is the canonical interchange format. Neo4j Cypher mode generates
executable CREATE scripts for direct import into Neo4j graph database instances.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		output, _ := cmd.Flags().GetString("output")
		formatFlag, _ := cmd.Flags().GetString("format")
		if dir == "" {
			dir = "."
		}
		if output == "" {
			return producterrs.Tagged(fmt.Sprintf("--output is required (e.g. graph.json or dump.cypher)"), producterrs.ErrValidation)
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

		format := strings.ToLower(formatFlag)
		ext := strings.ToLower(filepath.Ext(output))
		if format == "" {
			if ext == ".cypher" {
				format = "neo4j"
			} else {
				format = "graphjson"
			}
		}

		// C6-D25: validate format/extension before truncating the output file.
		switch format {
		case "json", "graphjson":
			if ext != "" && ext != ".json" {
				return producterrs.Tagged(fmt.Sprintf("unsupported extension %q for GraphJSON export (use .json)", ext), producterrs.ErrValidation)
			}
		case "neo4j", "cypher":
			if ext != "" && ext != ".cypher" && ext != ".sql" && ext != ".txt" {
				return producterrs.Tagged(fmt.Sprintf("unsupported extension %q for Neo4j Cypher export (use .cypher)", ext), producterrs.ErrValidation)
			}
		default:
			return producterrs.Tagged(fmt.Sprintf("unsupported export format %q (supported: graphjson, neo4j)", formatFlag), producterrs.ErrValidation)
		}

		f, err := os.Create(output)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer f.Close()

		switch format {
		case "json", "graphjson":
			if err := akg.ExportGraphJSON(graph, f); err != nil {
				return fmt.Errorf("failed to export graph JSON: %w", err)
			}
		case "neo4j", "cypher":
			if err := akg.ExportNeo4jCypher(graph, f); err != nil {
				return fmt.Errorf("failed to export Neo4j Cypher script: %w", err)
			}
		}

		st, _ := f.Stat()
		fmt.Println(views.RenderExportSuccess(format, output, graph.Nodes.Len(), st.Size()))
		return nil
	},
}

func init() {
	exportCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ database folder")
	exportCmd.Flags().StringP("output", "o", "", "Output file path (graph.json or dump.cypher)")
	exportCmd.Flags().StringP("format", "f", "graphjson", "Export format: graphjson (default) or neo4j")
	rootCmd.AddCommand(exportCmd)
}
