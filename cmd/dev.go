package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/product"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
	"github.com/spf13/cobra"
)

var devCmd = &cobra.Command{
	Use:    "dev",
	Short:  "Developer utility commands for GlassMarble maintenance and testing",
	Long:   `Developer commands for updating test goldens, benchmark baselines, and schema validation.`,
	Hidden: true,
}

var rebaseGoldensCmd = &cobra.Command{
	Use:   "rebase-goldens",
	Short: "Regenerate golden diagram test fixtures from current pipeline output",
	Long:  `Rebases golden diagram fixtures in internal/visualization_engine/testdata/golden/ across all 31 diagram types and supported output formats. Use after reviewed intent changes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir, _ := cmd.Flags().GetString("dir")
		if targetDir == "" {
			targetDir = "."
		}
		goldenDir, _ := cmd.Flags().GetString("golden-dir")
		if goldenDir == "" {
			goldenDir = filepath.Join("internal", "visualization_engine", "testdata", "golden")
		}

		if err := os.MkdirAll(goldenDir, 0755); err != nil {
			return fmt.Errorf("failed to create golden directory: %w", err)
		}

		diagramTypes := types.AllDiagramTypes()
		formats := []string{"mermaid", "plantuml", "dot"}

		count := 0
		skipped := 0
		var skippedTypes []string
		for _, dtype := range diagramTypes {
			for _, fmtStr := range formats {
				req := product.BuildDiagramRequest{
					StatePath:   filepath.Join(targetDir, ".glassmarble", "akg.json"),
					ParseFn:     akg.ParseGraphForQuery,
					DiagramType: dtype,
					Format:      fmtStr,
					Options: product.DiagramOptions{
						Scope: types.ScopeGlobal,
					},
				}
				markup, _, err := product.BuildDiagram(req)
				if err != nil || markup == "" {
					// C6-D15: track skipped entrypoint-required types instead of silencing.
					skipped++
					skippedTypes = append(skippedTypes, fmt.Sprintf("%s/%s", dtype, fmtStr))
					continue
				}

				filename := fmt.Sprintf("%s.%s", dtype, formatExt(fmtStr))
				outPath := filepath.Join(goldenDir, filename)
				if err := os.WriteFile(outPath, []byte(markup), 0644); err == nil {
					count++
				}
			}
		}

		fmt.Printf("Successfully rebased %d golden fixtures in %s\n", count, goldenDir)
		if skipped > 0 {
			fmt.Printf("Warning: skipped %d types (no entrypoint in target): %v\n", skipped, skippedTypes)
		}
		return nil
	},
}

func formatExt(fmtStr string) string {
	switch fmtStr {
	case "plantuml":
		return "puml"
	case "dot":
		return "dot"
	default:
		return "mmd"
	}
}

func init() {
	rebaseGoldensCmd.Flags().String("dir", ".", "Repository directory to analyze for golden generation")
	rebaseGoldensCmd.Flags().String("golden-dir", filepath.Join("internal", "visualization_engine", "testdata", "golden"), "Target golden fixtures directory")
	devCmd.AddCommand(rebaseGoldensCmd)
	rootCmd.AddCommand(devCmd)
}
