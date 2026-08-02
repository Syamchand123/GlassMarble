package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/spf13/cobra"
)

// dependencyEdge is the machine-readable form of a resolved dependency edge.
type dependencyEdge struct {
	Type       string `json:"type"`
	OtherID    string `json:"id"`
	LineNumber int    `json:"line"`
}

// dependencyNodeJSON captures the report for one matched target node.
type dependencyNodeJSON struct {
	ID       string            `json:"id"`
	Outbound []dependencyEdge  `json:"outbound"`
	Inbound  []dependencyEdge  `json:"inbound"`
}

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
		asJSON, _ := cmd.Flags().GetBool("json")
		if dir == "" {
			dir = "."
		}

		storageDir := filepath.Join(dir, ".glassmarble")
		tm, err := newAKGManager(storageDir, cmd)
		if err != nil {
			return fmt.Errorf("failed to open AKG database: %w", err)
		}

		snapshot := tm.GetActiveSnapshot()
		if snapshot == nil || snapshot.Nodes.Len() == 0 {
			return fmt.Errorf("AKG database is empty -- run 'glassmarble analyze' first")
		}

		if target == "" {
			summary := dependencySummaryJSON{
				TotalNodes:            snapshot.Nodes.Len(),
				OutboundEdgeMappings:  snapshot.OutboundEdges.Len(),
				InboundEdgeMappings:   snapshot.InboundEdges.Len(),
			}
			var topNodes []topDependencyNode
			var done bool
			count := 0
			snapshot.OutboundEdges.Iterate(func(id string, outbound []stage4.ResolvedEdge) {
				if done {
					return
				}
				if len(outbound) > 0 {
					topNodes = append(topNodes, topDependencyNode{ID: id, Outbound: len(outbound)})
					count++
					if count >= 20 {
						done = true
					}
				}
			})
			summary.TopDependencyNodes = topNodes

			if asJSON {
				out, _ := json.MarshalIndent(summary, "", "  ")
				fmt.Println(string(out))
				return nil
			}

			fmt.Println("=== Repository Dependency Summary ===")
			fmt.Printf("Total Graph Nodes: %d\n", snapshot.Nodes.Len())
			fmt.Printf("Total Outbound Edge Mappings: %d\n", snapshot.OutboundEdges.Len())
			fmt.Printf("Total Inbound Edge Mappings: %d\n\n", snapshot.InboundEdges.Len())
			fmt.Println("Top Dependency Nodes:")
			for _, n := range topNodes {
				fmt.Printf("  Node: %s (%d outbound dependencies)\n", n.ID, n.Outbound)
			}
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

		var jsonNodes []dependencyNodeJSON
		if asJSON {
			for _, nodeID := range matchingNodes {
				entry := dependencyNodeJSON{ID: nodeID}
				if outbound, ok := snapshot.OutboundEdges.Get(nodeID); ok {
					for _, edge := range outbound {
						entry.Outbound = append(entry.Outbound, dependencyEdge{Type: string(edge.Type), OtherID: edge.TargetID, LineNumber: edge.LineNumber})
					}
				}
				if inbound, ok := snapshot.InboundEdges.Get(nodeID); ok {
					for _, edge := range inbound {
						entry.Inbound = append(entry.Inbound, dependencyEdge{Type: string(edge.Type), OtherID: edge.SourceID, LineNumber: edge.LineNumber})
					}
				}
				jsonNodes = append(jsonNodes, entry)
			}
			out, _ := json.MarshalIndent(map[string]any{
				"target": target,
				"nodes":  jsonNodes,
			}, "", "  ")
			fmt.Println(string(out))
			return nil
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

// dependencySummaryJSON is the machine-readable repository summary.
type dependencySummaryJSON struct {
	TotalNodes           int                  `json:"total_nodes"`
	OutboundEdgeMappings int                  `json:"outbound_edge_mappings"`
	InboundEdgeMappings  int                  `json:"inbound_edge_mappings"`
	TopDependencyNodes   []topDependencyNode  `json:"top_dependency_nodes"`
}

type topDependencyNode struct {
	ID       string `json:"id"`
	Outbound int    `json:"outbound_dependencies"`
}

func init() {
	dependencyCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ database folder")
	dependencyCmd.Flags().Bool("json", false, "Emit machine-readable JSON instead of the human report")
	rootCmd.AddCommand(dependencyCmd)
}
