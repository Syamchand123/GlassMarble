package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
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
			return fmt.Errorf("AKG database is empty -- run 'glassmarble analyze' first")
		}

		fmt.Println("=== Architecture Workspace Tree ===")

		// Group nodes by file path
		fileTree := make(map[string][]string)
		snapshot.Nodes.Iterate(func(_ string, node *stage4.ResolvedNode) {
			if node.FileSpec.Path != "" {
				fileTree[node.FileSpec.Path] = append(fileTree[node.FileSpec.Path], fmt.Sprintf("%s [%s]", node.Name, node.Kind))
			}
		})

		printedFiles := 0
		paths := make([]string, 0, len(fileTree))
		for p := range fileTree {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, path := range paths {
			symbols := fileTree[path]
			if treeDepth > 0 && strings.Count(path, "/") >= treeDepth {
				continue
			}
			fmt.Printf("├── %s\n", path)
			for _, sym := range symbols {
				fmt.Printf("│   └── %s\n", sym)
			}
			printedFiles++
			if printedFiles >= 50 {
				fmt.Println("└── ... (showing top 50 files)")
				break
			}
		}

		return nil
	},
}

func init() {
	treeCmd.Flags().IntVar(&treeDepth, "depth", 4, "Maximum directory depth")
	treeCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ database folder")
	rootCmd.AddCommand(treeCmd)
}
