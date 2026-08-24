package cmd

import (
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

var treeCmd = &cobra.Command{
	Use:   "tree",
	Short: "Display architectural directory and symbol hierarchy tree",
	Long:  `Renders a tree representation of indexed packages, classes, and methods up to specified depth.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
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
			return producterrs.Tagged(fmt.Sprintf("AKG database is empty -- run 'glassmarble analyze' first"), producterrs.ErrEmptySubgraph)
		}

		lines := []string{"=== Architecture Workspace Tree ==="}

		// Group nodes by file path
		fileTree := make(map[string][]string)
		snapshot.Nodes.Iterate(func(_ string, node *link.ResolvedNode) {
			if node.FileSpec.Path != "" {
				sym := node.Name
				if node.Primitive != "" {
					sym = fmt.Sprintf("%s [%s] <%s>", node.Name, node.Kind, node.Primitive)
				} else {
					sym = fmt.Sprintf("%s [%s]", node.Name, node.Kind)
				}
				fileTree[node.FileSpec.Path] = append(fileTree[node.FileSpec.Path], sym)
			}
		})

		printedFiles := 0
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

		for _, path := range paths {
			symbols := fileTree[path]
			// C6-D23: use segment count, not slash count, so depth 4 means
			// 4 path segments (e.g. a/b/c/d) instead of 3 slashes.
			if treeDepth > 0 && len(strings.Split(path, "/")) > treeDepth {
				continue
			}
			// C6-D24: sort symbols per file deterministically (map order is random).
			sort.Strings(symbols)
			lines = append(lines, fmt.Sprintf("├── %s", path))
			for _, sym := range symbols {
				lines = append(lines, fmt.Sprintf("│   └── %s", sym))
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
			fmt.Println(l)
		}

		return nil
	},
}

func init() {
	treeCmd.Flags().IntVar(&treeDepth, "depth", 4, "Maximum directory depth")
	treeCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ database folder")
	rootCmd.AddCommand(treeCmd)
}
