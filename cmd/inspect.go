package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
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
	inspectLangs  bool
)

type inspectNodeRowJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Primitive string `json:"primitive,omitempty"`
	File      string `json:"file"`
	Line      int    `json:"line"`
}

type inspectListSearchJSON struct {
	Mode  string               `json:"mode"` // "list" | "search"
	Query string               `json:"query,omitempty"`
	Count int                  `json:"count"`
	Nodes []inspectNodeRowJSON `json:"nodes"`
}

type inspectEdgeJSON struct {
	SourceID   string `json:"source_id,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	Type       string `json:"type"`
	LineNumber int    `json:"line_number"`
}

type inspectDetailJSON struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Kind       string                 `json:"kind"`
	Primitive  string                 `json:"primitive,omitempty"`
	File       map[string]interface{} `json:"file"`
	Properties map[string]string      `json:"properties,omitempty"`
	Outbound   []inspectEdgeJSON      `json:"outbound"`
	Inbound    []inspectEdgeJSON      `json:"inbound"`
}

var inspectCmd = &cobra.Command{
	Use:     "inspect [node_id]",
	GroupID: GroupInspect.ID,
	Short:   "Inspect AKG graph nodes, search symbols, and discover entry points",
	Long:    `Queries the active Architecture Knowledge Graph to search nodes, discover entry points, or view detailed symbol metadata and inbound/outbound dependency edges.`,
	Example: `  # Search for a symbol or class
  gmb inspect --search UserService

  # List candidate entry points (functions and methods)
  gmb inspect --list --type FUNCTION

  # View details and edges for a specific symbol ID
  gmb inspect "cmd/root.go::Execute"

  # Find symbol at a specific file and line number
  gmb inspect --file internal/app/app.go --line 42

  # Output symbol details as JSON
  gmb inspect --search Database --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolveDir(cmd)
		asJSON, _ := cmd.Flags().GetBool("json")

		storageDir := filepath.Join(dir, ".glassmarble")
		if _, err := os.Stat(filepath.Join(storageDir, "akg.json")); os.IsNotExist(err) {
			if asJSON {
				out, _ := json.MarshalIndent(map[string]string{"error": "no active AKG database"}, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			return producterrs.Tagged("AKG database is uninitialized — try 'gmb analyze' first", producterrs.ErrEmptySubgraph)
		}

		if inspectFile != "" && inspectLine <= 0 {
			return producterrs.Tagged("--file requires --line to locate a symbol at a specific line — try 'gmb inspect --file <path> --line <num>'", producterrs.ErrValidation)
		}

		if inspectFile != "" && inspectLine > 0 {
			target, err := findNodeAtLine(storageDir, inspectFile, inspectLine)
			if err != nil {
				return err
			}
			args = []string{target}
		}

		if inspectLangs {
			return printLanguagesReport(cmd, asJSON)
		}

		interactive := !asJSON && tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout())

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
			return streamNodeList(storageDir, asJSON)
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
			return streamNodeSearch(storageDir, asJSON)
		}

		if len(args) > 0 {
			if interactive {
				return inspectprog.RenderDetail(cmd.OutOrStdout(), storageDir, args[0])
			}
			return showNodeDetails(storageDir, args[0], asJSON)
		}

		return cmd.Help()
	},
}

func findNodeAtLine(storageDir, filePath string, line int) (string, error) {
	norm := normalizeInspectPath(filePath)
	bestID := ""
	bestStart := -1
	err := akg.StreamNodes(storageDir, func(n *link.ResolvedNode) bool {
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
		return "", fmt.Errorf("failed to scan AKG: %w — try 'gmb analyze'", err)
	}
	if bestID == "" {
		return "", producterrs.Tagged(fmt.Sprintf("no symbol found in %s at line %d — try 'gmb inspect --search %s'", filePath, line, filepath.Base(filePath)), producterrs.ErrEntryNotFound)
	}
	return bestID, nil
}

func streamNodeList(storageDir string, asJSON bool) error {
	var collected []inspectNodeRowJSON
	err := akg.StreamNodes(storageDir, func(n *link.ResolvedNode) bool {
		if n.Kind == "FUNCTION" || n.Kind == "METHOD" {
			if inspectKind == "" || strings.EqualFold(string(n.Kind), inspectKind) {
				collected = append(collected, inspectNodeRowJSON{
					ID:        n.ID,
					Name:      n.Name,
					Kind:      string(n.Kind),
					Primitive: string(n.Primitive),
					File:      n.FileSpec.Path,
					Line:      n.FileSpec.LineStart,
				})
			}
		}
		return true
	})
	if err != nil {
		return err
	}

	sort.Slice(collected, func(i, j int) bool {
		return collected[i].ID < collected[j].ID
	})

	if asJSON {
		out, _ := json.MarshalIndent(inspectListSearchJSON{
			Mode:  "list",
			Count: len(collected),
			Nodes: collected,
		}, "", "  ")
		fmt.Println(string(out))
		return nil
	}

	fmt.Println("=== Entry Points & Callable Symbols ===")
	count := 0
	for _, n := range collected {
		fmt.Printf("  - [%s] %s (%s:L%d)\n", n.Kind, n.ID, n.File, n.Line)
		count++
		if count >= 30 {
			fmt.Println("  ... (showing first 30 entry points)")
			break
		}
	}
	return nil
}

func streamNodeSearch(storageDir string, asJSON bool) error {
	var collected []inspectNodeRowJSON
	lowerSearch := strings.ToLower(inspectSearch)
	err := akg.StreamNodes(storageDir, func(n *link.ResolvedNode) bool {
		if strings.Contains(strings.ToLower(n.ID), lowerSearch) || strings.Contains(strings.ToLower(n.Name), lowerSearch) {
			if inspectKind == "" || strings.EqualFold(string(n.Kind), inspectKind) {
				collected = append(collected, inspectNodeRowJSON{
					ID:        n.ID,
					Name:      n.Name,
					Kind:      string(n.Kind),
					Primitive: string(n.Primitive),
					File:      n.FileSpec.Path,
					Line:      n.FileSpec.LineStart,
				})
			}
		}
		return true
	})
	if err != nil {
		return err
	}

	sort.Slice(collected, func(i, j int) bool {
		return collected[i].ID < collected[j].ID
	})

	if asJSON {
		out, _ := json.MarshalIndent(inspectListSearchJSON{
			Mode:  "search",
			Query: inspectSearch,
			Count: len(collected),
			Nodes: collected,
		}, "", "  ")
		fmt.Println(string(out))
		return nil
	}

	fmt.Printf("=== Search Results for '%s' ===\n", inspectSearch)
	count := 0
	for _, n := range collected {
		fmt.Printf("  ID: %s\n  Kind: %s | File: %s:L%d\n  Primitive: %s\n\n", n.ID, n.Kind, n.File, n.Line, n.Primitive)
		count++
		if count >= 20 {
			fmt.Println("... (truncated to top 20 matches)")
			break
		}
	}
	return nil
}

func collectNodeRows(storageDir string, search bool, query string) ([]inspectprog.NodeRow, error) {
	limit := 30
	lower := strings.ToLower(query)
	if search {
		limit = 20
	}
	rows := make([]inspectprog.NodeRow, 0, limit)
	err := akg.StreamNodes(storageDir, func(n *link.ResolvedNode) bool {
		if search {
			if !strings.Contains(strings.ToLower(n.ID), lower) && !strings.Contains(strings.ToLower(n.Name), lower) {
				return true
			}
		} else {
			if n.Kind != "FUNCTION" && n.Kind != "METHOD" {
				return true
			}
			if inspectKind != "" && !strings.EqualFold(string(n.Kind), inspectKind) {
				return true
			}
		}
		rows = append(rows, inspectprog.NodeRow{
			ID:   n.ID,
			Kind: string(n.Kind),
			Name: n.Name,
			File: n.FileSpec.Path,
			Line: n.FileSpec.LineStart,
		})
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scan AKG: %w", err)
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].ID < rows[j].ID
	})

	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func showNodeDetails(storageDir, targetID string, asJSON bool) error {
	node, outEdges, inEdges, err := akg.QueryNode(storageDir, targetID)
	if err != nil {
		return fmt.Errorf("failed to open AKG database: %w — try 'gmb analyze'", err)
	}
	if node == nil {
		return producterrs.Tagged(fmt.Sprintf("node ID '%s' not found in AKG — try 'gmb inspect --search %s'", targetID, targetID), producterrs.ErrEntryNotFound)
	}

	if asJSON {
		outb := make([]inspectEdgeJSON, 0, len(outEdges))
		for _, e := range outEdges {
			outb = append(outb, inspectEdgeJSON{TargetID: e.TargetID, Type: string(e.Type), LineNumber: e.LineNumber})
		}
		inb := make([]inspectEdgeJSON, 0, len(inEdges))
		for _, e := range inEdges {
			inb = append(inb, inspectEdgeJSON{SourceID: e.SourceID, Type: string(e.Type), LineNumber: e.LineNumber})
		}

		dj := inspectDetailJSON{
			ID:        node.ID,
			Name:      node.Name,
			Kind:      string(node.Kind),
			Primitive: string(node.Primitive),
			File: map[string]interface{}{
				"path":       node.FileSpec.Path,
				"line_start": node.FileSpec.LineStart,
				"line_end":   node.FileSpec.LineEnd,
			},
			Properties: node.Properties,
			Outbound:   outb,
			Inbound:    inb,
		}
		out, _ := json.MarshalIndent(dj, "", "  ")
		fmt.Println(string(out))
		return nil
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

func normalizeInspectPath(path string) string {
	return filepath.Clean(filepath.ToSlash(path))
}

func printLanguagesReport(cmd *cobra.Command, asJSON bool) error {
	type langSpec struct {
		Language   string   `json:"language"`
		Grammar    string   `json:"grammar"`
		Tier       string   `json:"tier"`
		Extensions []string `json:"extensions"`
	}

	specs := []langSpec{
		{"Go", "go", "T1", []string{".go"}},
		{"Python", "python", "T1", []string{".py", ".pyi"}},
		{"JavaScript", "javascript", "T1", []string{".js", ".mjs", ".cjs", ".jsx"}},
		{"TypeScript", "typescript", "T1", []string{".ts", ".tsx", ".cts", ".mts"}},
		{"Java", "java", "T1", []string{".java"}},
		{"C#", "c-sharp", "T1", []string{".cs"}},
		{"Rust", "rust", "T1", []string{".rs"}},
		{"C", "c", "T2", []string{".c", ".h"}},
		{"C++", "cpp", "T2", []string{".cpp", ".cc", ".cxx", ".hpp"}},
		{"Kotlin", "kotlin", "T2", []string{".kt"}},
		{"PHP", "php", "T2", []string{".php"}},
		{"Ruby", "ruby", "T2", []string{".rb"}},
		{"Swift", "swift", "T2", []string{".swift"}},
		{"Scala", "scala", "T3", []string{".scala"}},
	}

	if asJSON {
		out, _ := json.MarshalIndent(specs, "", "  ")
		fmt.Println(string(out))
		return nil
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "GlassMarble 14-Language Support Matrix (Phase 6) — coverage values are static estimates:")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Language    Grammar     Tier    Extensions")
	fmt.Fprintln(out, "--------------------------------------------------------")
	for _, s := range specs {
		fmt.Fprintf(out, "%-12s%-12s%-8s%s\n", s.Language, s.Grammar, s.Tier, strings.Join(s.Extensions, ", "))
	}
	return nil
}

func init() {
	inspectCmd.Flags().BoolVar(&inspectList, "list", false, "List candidate entry points for sequence diagrams")
	inspectCmd.Flags().BoolVar(&inspectLangs, "languages", false, "Display the 14-Language support matrix report")
	inspectCmd.Flags().StringVar(&inspectSearch, "search", "", "Search nodes by symbol name or path fragment")
	inspectCmd.Flags().StringVar(&inspectKind, "type", "", "Filter by node kind (FUNCTION, METHOD, STRUCT, CLASS, INTERFACE)")
	inspectCmd.Flags().StringVar(&inspectFile, "file", "", "File path to look up (requires --line)")
	inspectCmd.Flags().IntVar(&inspectLine, "line", 0, "Line number to look up (requires --file)")
	inspectCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	rootCmd.AddCommand(inspectCmd)
}
