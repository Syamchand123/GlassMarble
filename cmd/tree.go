package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	treeprog "github.com/Syamchand123/GlassMarble/internal/tui/programs/tree"
	"github.com/spf13/cobra"
)

var treeDepth int

type treeSymbolJSON struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Primitive string `json:"primitive,omitempty"`
}

type treeFileJSON struct {
	Path    string           `json:"path"`
	Symbols []treeSymbolJSON `json:"symbols"`
}

type treeJSON struct {
	Depth        int            `json:"depth"`
	TotalFiles   int            `json:"total_files"`
	TotalSymbols int            `json:"total_symbols"`
	Files        []treeFileJSON `json:"files"`
}

var treeCmd = &cobra.Command{
	Use:     "tree",
	GroupID: GroupInspect.ID,
	Short:   "Display architectural directory and symbol hierarchy tree",
	Long:    `Renders a tree representation of indexed packages, classes, and methods up to the specified directory segment depth.`,
	Example: `  # Render workspace symbol hierarchy tree (default depth 4)
  gmb tree

  # Limit tree traversal to 2 directory segments
  gmb tree --depth 2

  # Output full symbol tree as JSON
  gmb tree --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolveDir(cmd)
		asJSON, _ := cmd.Flags().GetBool("json")

		storageDir := filepath.Join(dir, ".glassmarble")
		tm, err := newAKGManager(storageDir, cmd)
		if err != nil {
			return fmt.Errorf("failed to open AKG database: %w — try 'gmb analyze'", err)
		}

		snapshot := tm.GetActiveSnapshot()
		if snapshot == nil || snapshot.Nodes.Len() == 0 {
			if asJSON {
				out, _ := json.MarshalIndent(treeJSON{Depth: treeDepth, TotalFiles: 0, TotalSymbols: 0, Files: []treeFileJSON{}}, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			return producterrs.Tagged(fmt.Sprintf("AKG database is empty — try 'gmb analyze' first"), producterrs.ErrEmptySubgraph)
		}

		// Group nodes by file path
		fileTree := make(map[string][]treeSymbolJSON)
		snapshot.Nodes.Iterate(func(_ string, node *link.ResolvedNode) {
			if node.FileSpec.Path != "" {
				fileTree[node.FileSpec.Path] = append(fileTree[node.FileSpec.Path], treeSymbolJSON{
					Name:      node.Name,
					Kind:      string(node.Kind),
					Primitive: string(node.Primitive),
				})
			}
		})

		paths := make([]string, 0, len(fileTree))
		for p := range fileTree {
			paths = append(paths, p)
		}
		sort.Strings(paths)

		totalFiles := len(paths)
		totalSymbols := 0
		for _, syms := range fileTree {
			totalSymbols += len(syms)
		}

		if asJSON {
			files := make([]treeFileJSON, 0, len(paths))
			for _, path := range paths {
				if treeDepth > 0 && len(strings.Split(path, "/")) > treeDepth {
					continue
				}
				syms := fileTree[path]
				sort.Slice(syms, func(i, j int) bool {
					return syms[i].Name < syms[j].Name
				})
				files = append(files, treeFileJSON{
					Path:    path,
					Symbols: syms,
				})
			}
			tj := treeJSON{
				Depth:        treeDepth,
				TotalFiles:   totalFiles,
				TotalSymbols: totalSymbols,
				Files:        files,
			}
			out, _ := json.MarshalIndent(tj, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		}

		lines := []string{"=== Architecture Workspace Tree ==="}
		printedFiles := 0
		for _, path := range paths {
			syms := fileTree[path]
			if treeDepth > 0 && len(strings.Split(path, "/")) > treeDepth {
				continue
			}
			sort.Slice(syms, func(i, j int) bool {
				return syms[i].Name < syms[j].Name
			})
			lines = append(lines, fmt.Sprintf("├── %s", path))
			for _, sym := range syms {
				label := sym.Name
				if sym.Primitive != "" {
					label = fmt.Sprintf("%s [%s] <%s>", sym.Name, sym.Kind, sym.Primitive)
				} else {
					label = fmt.Sprintf("%s [%s]", sym.Name, sym.Kind)
				}
				lines = append(lines, fmt.Sprintf("│   └── %s", label))
			}
			printedFiles++
			if printedFiles >= 200 {
				lines = append(lines, fmt.Sprintf("└── ... (showing 200 of %d files)", totalFiles))
				break
			}
		}

		lines = append(lines, fmt.Sprintf("  %d file(s) indexed · %d symbol(s)", totalFiles, totalSymbols))

		if tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return treeprog.Run(treeprog.Config{
				Lines: lines,
				Depth: treeDepth,
				In:    cmd.InOrStdin(),
				Out:   cmd.OutOrStdout(),
			})
		}

		for _, l := range lines {
			tui.Fprintln(cmd.OutOrStdout(), l)
		}

		return nil
	},
}

func init() {
	treeCmd.Flags().IntVar(&treeDepth, "depth", 4, "Maximum directory segment depth")
	treeCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	rootCmd.AddCommand(treeCmd)
}
