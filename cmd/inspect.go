package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	inspectprog "github.com/Syamchand123/GlassMarble/internal/tui/programs/inspect"
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
		if _, err := os.Stat(filepath.Join(storageDir, "akg_state.ttl")); os.IsNotExist(err) {
			return producterrs.Tagged(fmt.Sprintf("AKG database is empty -- run 'glassmarble analyze' first"), producterrs.ErrEmptySubgraph)
		}

		// Lazy Query-based reads: inspect never restores the whole graph from
		// disk (AUDIT Issue 4 Phase 4A-2). Each mode streams only what it
		// needs through the single canonical parser.
		if inspectFile != "" && inspectLine > 0 {
			target, err := findNodeAtLine(storageDir, inspectFile, inspectLine)
			if err != nil {
				return err
			}
			args = []string{target}
		}

		interactive := tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout())

		if inspectList {
			if interactive {
				rows, err := collectNodeRows(storageDir, false, "")
				if err != nil {
					return err
				}
				return inspectprog.Run(inspectprog.Config{
					Title:      "Entry Points & Callable Symbols",
					Rows:       rows,
					StorageDir: storageDir,
					In:         cmd.InOrStdin(),
					Out:        cmd.OutOrStdout(),
				})
			}
			return streamNodeList(storageDir)
		}

		if inspectSearch != "" {
			if interactive {
				rows, err := collectNodeRows(storageDir, true, inspectSearch)
				if err != nil {
					return err
				}
				return inspectprog.Run(inspectprog.Config{
					Title:      fmt.Sprintf("Search Results for '%s'", inspectSearch),
					Rows:       rows,
					StorageDir: storageDir,
					In:         cmd.InOrStdin(),
					Out:        cmd.OutOrStdout(),
				})
			}
			return streamNodeSearch(storageDir)
		}

		if len(args) > 0 {
			if interactive {
				return inspectprog.RenderDetail(cmd.OutOrStdout(), storageDir, args[0])
			}
			return showNodeDetails(storageDir, args[0])
		}

		return cmd.Help()
	},
}

// findNodeAtLine reproduces the LineIndex binary-search result without
// loading the index: nodes are streamed and filtered to the file, the node
// with the largest LineStart <= line wins (ties: last seen, matching the
// sorted-index lookup order).
func findNodeAtLine(storageDir, filePath string, line int) (string, error) {
	norm := normalizeInspectPath(filePath)
	bestID := ""
	bestStart := -1
	err := akg.StreamNodes(storageDir, func(n *stage4.ResolvedNode) bool {
		if normalizeInspectPath(n.FileSpec.Path) != norm {
			return true
		}
		if n.FileSpec.LineStart > line {
			return true
		}
		if n.FileSpec.LineStart >= bestStart {
			bestStart = n.FileSpec.LineStart
			bestID = n.ID
		}
		return true
	})
	if err != nil {
		return "", fmt.Errorf("failed to scan AKG: %w", err)
	}
	if bestID == "" {
		return "", fmt.Errorf("no symbols found for file: %s", filePath)
	}
	return bestID, nil
}

func streamNodeList(storageDir string) error {
	fmt.Println("=== Entry Points & Callable Symbols ===")
	count := 0
	err := akg.StreamNodes(storageDir, func(n *stage4.ResolvedNode) bool {
		if n.Kind == "FUNCTION" || n.Kind == "METHOD" {
			if inspectKind == "" || strings.EqualFold(n.Kind, inspectKind) {
				fmt.Printf("  - [%s] %s (%s:L%d)\n", n.Kind, n.ID, n.FileSpec.Path, n.FileSpec.LineStart)
				count++
				if count >= 30 {
					fmt.Println("  ... (showing first 30 entry points)")
					return false
				}
			}
		}
		return true
	})
	return err
}

func streamNodeSearch(storageDir string) error {
	fmt.Printf("=== Search Results for '%s' ===\n", inspectSearch)
	count := 0
	lowerSearch := strings.ToLower(inspectSearch)
	err := akg.StreamNodes(storageDir, func(n *stage4.ResolvedNode) bool {
		if strings.Contains(strings.ToLower(n.ID), lowerSearch) || strings.Contains(strings.ToLower(n.Name), lowerSearch) {
			fmt.Printf("  ID: %s\n  Kind: %s | File: %s:L%d\n  Primitive: %s\n\n", n.ID, n.Kind, n.FileSpec.Path, n.FileSpec.LineStart, n.Primitive)
			count++
			if count >= 20 {
				fmt.Println("... (truncated to top 20 matches)")
				return false
			}
		}
		return true
	})
	return err
}

// collectNodeRows streams nodes into table rows for the interactive inspect
// program, applying the same filters and caps as the plain streamNodeList and
// streamNodeSearch (30 for --list, 20 for --search).
func collectNodeRows(storageDir string, search bool, query string) ([]inspectprog.NodeRow, error) {
	limit := 30
	lower := strings.ToLower(query)
	if search {
		limit = 20
	}
	rows := make([]inspectprog.NodeRow, 0, limit)
	count := 0
	err := akg.StreamNodes(storageDir, func(n *stage4.ResolvedNode) bool {
		if search {
			if !strings.Contains(strings.ToLower(n.ID), lower) && !strings.Contains(strings.ToLower(n.Name), lower) {
				return true
			}
		} else {
			if n.Kind != "FUNCTION" && n.Kind != "METHOD" {
				return true
			}
			if inspectKind != "" && !strings.EqualFold(n.Kind, inspectKind) {
				return true
			}
		}
		rows = append(rows, inspectprog.NodeRow{
			ID:   n.ID,
			Kind: n.Kind,
			Name: n.Name,
			File: n.FileSpec.Path,
			Line: n.FileSpec.LineStart,
		})
		count++
		return count < limit
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scan AKG: %w", err)
	}
	return rows, nil
}

func showNodeDetails(storageDir, targetID string) error {
	node, outEdges, inEdges, err := akg.QueryNode(storageDir, targetID)
	if err != nil {
		return fmt.Errorf("failed to open AKG database: %w", err)
	}
	if node == nil {
		return producterrs.Tagged(fmt.Sprintf("node ID '%s' not found in AKG", targetID), producterrs.ErrEntryNotFound)
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

	if len(outEdges) > 0 {
		fmt.Printf("  Outbound Edges (%d):\n", len(outEdges))
		for _, e := range outEdges {
			fmt.Printf("    -> %s [%s] (L%d)\n", e.TargetID, e.Type, e.LineNumber)
		}
	}

	if len(inEdges) > 0 {
		fmt.Printf("  Inbound Edges (%d):\n", len(inEdges))
		for _, e := range inEdges {
			fmt.Printf("    <- %s [%s] (L%d)\n", e.SourceID, e.Type, e.LineNumber)
		}
	}

	return nil
}

// normalizeInspectPath mirrors akg.normalizePath, the key space of LineIndex.
func normalizeInspectPath(path string) string {
	return filepath.Clean(filepath.ToSlash(path))
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
