package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/spf13/cobra"
)

var (
	inspectList   bool
	inspectSearch string
	inspectKind   string
	inspectFile   string
	inspectLine   int
)

var inspectCmd = &cobra.Command{
	Use:   "inspect [node_id]",
	Short: "Inspect AKG graph nodes, search symbols, and discover entry points",
	Long:  `Queries the active Architecture Knowledge Graph to search nodes, discover entry points, or view detailed symbol metadata.`,
	RunE: func(cmd *cobra.Command, args []string) error {
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

		if inspectFile != "" && inspectLine > 0 {
			nodes, exists := snapshot.LineIndex.Get(inspectFile)
			if !exists {
				return fmt.Errorf("no symbols found for file: %s", inspectFile)
			}

			// Binary search for the narrowest matching node (or the last node starting before/on line)
			idx := sort.Search(len(nodes), func(i int) bool {
				return nodes[i].FileSpec.LineStart > inspectLine
			})

			if idx > 0 {
				node := nodes[idx-1]
				if node.FileSpec.LineStart <= inspectLine && (node.FileSpec.LineEnd == 0 || node.FileSpec.LineEnd >= inspectLine) {
					// We found a matching node, print its details
					args = []string{node.ID}
				} else {
					return fmt.Errorf("no symbol covers line %d in %s", inspectLine, inspectFile)
				}
			} else {
				return fmt.Errorf("no symbol covers line %d in %s", inspectLine, inspectFile)
			}
		}

		if inspectList {
			fmt.Println("=== Entry Points & Callable Symbols ===")
			count := 0
			for _, id := range snapshot.Nodes.Keys() {
				node, _ := snapshot.Nodes.Get(id)
				if node.Kind == "FUNCTION" || node.Kind == "METHOD" {
					if inspectKind == "" || strings.EqualFold(node.Kind, inspectKind) {
						fmt.Printf("  - [%s] %s (%s:L%d)\n", node.Kind, id, node.FileSpec.Path, node.FileSpec.LineStart)
						count++
						if count >= 30 {
							fmt.Println("  ... (showing first 30 entry points)")
							break
						}
					}
				}
			}
			return nil
		}

		if inspectSearch != "" {
			fmt.Printf("=== Search Results for '%s' ===\n", inspectSearch)
			count := 0
			lowerSearch := strings.ToLower(inspectSearch)
			for _, id := range snapshot.Nodes.Keys() {
				node, _ := snapshot.Nodes.Get(id)
				if strings.Contains(strings.ToLower(id), lowerSearch) || strings.Contains(strings.ToLower(node.Name), lowerSearch) {
					fmt.Printf("  ID: %s\n  Kind: %s | File: %s:L%d\n  Primitive: %s\n\n", id, node.Kind, node.FileSpec.Path, node.FileSpec.LineStart, node.Primitive)
					count++
					if count >= 20 {
						fmt.Println("... (truncated to top 20 matches)")
						break
					}
				}
			}
			return nil
		}

		if len(args) > 0 {
			targetID := args[0]
			node, exists := snapshot.Nodes.Get(targetID)
			if !exists {
				return fmt.Errorf("node ID '%s' not found in AKG", targetID)
			}

			fmt.Printf("=== Node Details: %s ===\n", node.ID)
			fmt.Printf("  Name:      %s\n", node.Name)
			fmt.Printf("  Kind:      %s\n", node.Kind)
			fmt.Printf("  Primitive: %s\n", node.Primitive)
			fmt.Printf("  File Path: %s (L%d - L%d)\n", node.FileSpec.Path, node.FileSpec.LineStart, node.FileSpec.LineEnd)

			if len(node.Properties) > 0 {
				fmt.Println("  Properties:")
				for k, v := range node.Properties {
					fmt.Printf("    %s: %s\n", k, v)
				}
			}

			outEdges, _ := snapshot.OutboundEdges.Get(targetID)
			if len(outEdges) > 0 {
				fmt.Printf("  Outbound Edges (%d):\n", len(outEdges))
				for _, e := range outEdges {
					fmt.Printf("    -> %s [%s] (L%d)\n", e.TargetID, e.Type, e.LineNumber)
				}
			}

			inEdges, _ := snapshot.InboundEdges.Get(targetID)
			if len(inEdges) > 0 {
				fmt.Printf("  Inbound Edges (%d):\n", len(inEdges))
				for _, e := range inEdges {
					fmt.Printf("    <- %s [%s] (L%d)\n", e.SourceID, e.Type, e.LineNumber)
				}
			}

			return nil
		}

		return cmd.Help()
	},
}

func init() {
	inspectCmd.Flags().BoolVar(&inspectList, "list", false, "List candidate entry points for sequence diagrams")
	inspectCmd.Flags().StringVar(&inspectSearch, "search", "", "Search nodes by symbol name or path fragment")
	inspectCmd.Flags().StringVar(&inspectKind, "type", "", "Filter by node kind (FUNCTION, METHOD, STRUCT, CLASS, INTERFACE)")
	inspectCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ database folder")
	inspectCmd.Flags().StringVar(&inspectFile, "file", "", "File path to look up")
	inspectCmd.Flags().IntVar(&inspectLine, "line", 0, "Line number to look up")
	rootCmd.AddCommand(inspectCmd)
}
