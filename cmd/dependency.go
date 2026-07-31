package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/spf13/cobra"
)

var dependencyCmd = &cobra.Command{
	Use:   "dependency [target_file_or_symbol]",
	Short: "Analyze inbound and outbound dependencies for a file or symbol",
	Long:  `Inspects direct and transitive call and composition dependencies for a target file or symbol.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := ""
		if len(args) > 0 {
			target = args[0]
		}
		dir, _ := cmd.Flags().GetString("dir")
		if dir == "" {
			dir = "."
		}

		storageDir := filepath.Join(dir, ".glassmarble")
		tm, err := akg.NewAKGTransactionManager(storageDir)
		if err != nil {
			return fmt.Errorf("failed to open AKG database: %w", err)
		}

		snapshot := tm.GetActiveSnapshot()
		if snapshot == nil || snapshot.Nodes.Len() == 0 {
			return fmt.Errorf("AKG database is empty -- run 'glassmarble analyze' first")
		}

		if target == "" {
			fmt.Println("=== Repository Dependency Summary ===")
			fmt.Printf("Total Graph Nodes: %d\n", snapshot.Nodes.Len())
			fmt.Printf("Total Outbound Edge Mappings: %d\n", snapshot.OutboundEdges.Len())
			fmt.Printf("Total Inbound Edge Mappings: %d\n\n", snapshot.InboundEdges.Len())
			fmt.Println("Top Dependency Nodes:")
			count := 0
			var done bool
			snapshot.OutboundEdges.Iterate(func(id string, outbound []stage4.ResolvedEdge) {
				if done {
					return
				}
				if len(outbound) > 0 {
					fmt.Printf("  Node: %s (%d outbound dependencies)\n", id, len(outbound))
					count++
					if count >= 20 {
						done = true
					}
				}
			})
			return nil
		}

		fmt.Printf("=== Dependency Analysis for '%s' ===\n", target)

		var matchingNodes []string
		lowerTarget := strings.ToLower(target)

		snapshot.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
			if strings.Contains(strings.ToLower(id), lowerTarget) || strings.Contains(strings.ToLower(node.FileSpec.Path), lowerTarget) || strings.EqualFold(node.Name, target) {
				matchingNodes = append(matchingNodes, id)
			}
		})

		if len(matchingNodes) == 0 {
			return fmt.Errorf("no matching node or file found for '%s'", target)
		}

		for _, nodeID := range matchingNodes {
			fmt.Printf("\nNode: %s\n", nodeID)

			outbound, ok := snapshot.OutboundEdges.Get(nodeID)
			if ok && len(outbound) > 0 {
				fmt.Println("  Direct Outbound Dependencies:")
				for _, edge := range outbound {
					fmt.Printf("    -> [%s] %s (L%d)\n", edge.Type, edge.TargetID, edge.LineNumber)
				}
			} else {
				fmt.Println("  Direct Outbound Dependencies: None")
			}

			inbound, ok := snapshot.InboundEdges.Get(nodeID)
			if ok && len(inbound) > 0 {
				fmt.Println("  Direct Inbound Callers/Dependents:")
				for _, edge := range inbound {
					fmt.Printf("    <- [%s] %s (L%d)\n", edge.Type, edge.SourceID, edge.LineNumber)
				}
			} else {
				fmt.Println("  Direct Inbound Callers/Dependents: None")
			}
		}

		return nil
	},
}

func init() {
	dependencyCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ database folder")
	rootCmd.AddCommand(dependencyCmd)
}
