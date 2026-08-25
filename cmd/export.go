package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

type exportReceiptJSON struct {
	Format    string `json:"format"`
	Output    string `json:"output"`
	NodeCount int    `json:"node_count"`
	SizeBytes int64  `json:"size_bytes"`
}

var exportCmd = &cobra.Command{
	Use:     "export",
	GroupID: GroupVisualize.ID,
	Short:   "Export the AKG snapshot to GraphJSON or Neo4j Cypher format",
	Long: `Writes the active AKG snapshot to a machine-readable, diff-friendly file.

GraphJSON is the canonical interchange format. Neo4j Cypher mode generates
executable CREATE scripts for direct import into Neo4j graph database instances.`,
	Example: `  # Export AKG snapshot as canonical GraphJSON
  gmb export --output graph.json

  # Export AKG snapshot as Neo4j Cypher import script
  gmb export --format neo4j --output dump.cypher

  # Export and emit JSON receipt
  gmb export --output graph.json --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolveDir(cmd)
		output, _ := cmd.Flags().GetString("output")
		formatFlag, _ := cmd.Flags().GetString("format")
		asJSON, _ := cmd.Flags().GetBool("json")

		if output == "" {
			return producterrs.Tagged("--output is required (e.g. graph.json or dump.cypher) — try 'gmb export --output graph.json'", producterrs.ErrValidation)
		}

		storageDir := filepath.Join(dir, ".glassmarble")
		tm, err := newAKGManager(storageDir, cmd)
		if err != nil {
			return fmt.Errorf("failed to open AKG database: %w — try 'gmb analyze'", err)
		}

		graph := tm.GetActiveSnapshot()
		if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
			return producterrs.Tagged("AKG database is empty — try 'gmb analyze' first", producterrs.ErrEmptySubgraph)
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

		switch format {
		case "json", "graphjson":
			if ext != "" && ext != ".json" {
				return producterrs.Tagged(fmt.Sprintf("unsupported extension %q for GraphJSON export (use .json) — try 'gmb export --output graph.json'", ext), producterrs.ErrValidation)
			}
		case "neo4j", "cypher":
			if ext != "" && ext != ".cypher" && ext != ".sql" && ext != ".txt" {
				return producterrs.Tagged(fmt.Sprintf("unsupported extension %q for Neo4j Cypher export (use .cypher) — try 'gmb export --format neo4j --output dump.cypher'", ext), producterrs.ErrValidation)
			}
		default:
			return producterrs.Tagged(fmt.Sprintf("unsupported export format %q (supported: graphjson, neo4j) — try 'gmb export --format graphjson'", formatFlag), producterrs.ErrValidation)
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
		size := int64(0)
		if st != nil {
			size = st.Size()
		}

		if asJSON {
			receipt := exportReceiptJSON{
				Format:    format,
				Output:    output,
				NodeCount: graph.Nodes.Len(),
				SizeBytes: size,
			}
			out, _ := json.MarshalIndent(receipt, "", "  ")
			fmt.Println(string(out))
			return nil
		}

		fmt.Println(views.RenderExportSuccess(format, output, graph.Nodes.Len(), size))
		return nil
	},
}

func init() {
	exportCmd.Flags().StringP("output", "o", "", "Output file path (graph.json or dump.cypher)")
	exportCmd.Flags().StringP("format", "f", "graphjson", "Export format: graphjson (default) or neo4j")
	exportCmd.Flags().Bool("json", false, "Emit machine-readable JSON receipt")

	_ = exportCmd.RegisterFlagCompletionFunc("format", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"graphjson", "neo4j"}, cobra.ShellCompDirectiveNoFileComp
	})

	rootCmd.AddCommand(exportCmd)
}
