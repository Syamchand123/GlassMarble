package cmd

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/spf13/cobra"
)

var hotspotTop int

type nodeDegree struct {
	ID        string
	Name      string
	Kind      string
	InDegree  int
	OutDegree int
	Primitive string
}

var hotspotCmd = &cobra.Command{
	Use:   "hotspot",
	Short: "Identify high-coupling architectural hotspots and most-depended-upon symbols",
	Long:  `Ranks nodes by in-degree call density and centrality to highlight architectural hotspots.`,
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

		var degrees []nodeDegree
		snapshot.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
			inEdges, _ := snapshot.InboundEdges.Get(id)
			outEdges, _ := snapshot.OutboundEdges.Get(id)
			inCount := len(inEdges)
			outCount := len(outEdges)
			if inCount > 0 || outCount > 0 {
				degrees = append(degrees, nodeDegree{
					ID:        id,
					Name:      node.Name,
					Kind:      node.Kind,
					InDegree:  inCount,
					OutDegree: outCount,
					Primitive: node.Primitive,
				})
			}
		})

		sort.Slice(degrees, func(i, j int) bool {
			return degrees[i].InDegree > degrees[j].InDegree
		})

		fmt.Printf("=== Top %d Architectural Hotspots (Ranked by In-Degree Centrality) ===\n\n", hotspotTop)
		fmt.Printf("%-5s %-45s %-12s %-10s %-10s %-15s\n", "Rank", "Symbol ID", "Kind", "In-Degree", "Out-Degree", "Primitive")
		fmt.Println("---------------------------------------------------------------------------------------------------")

		for i := 0; i < len(degrees) && i < hotspotTop; i++ {
			d := degrees[i]
			idDisp := d.ID
			if len(idDisp) > 42 {
				idDisp = "..." + idDisp[len(idDisp)-39:]
			}
			fmt.Printf("%-5d %-45s %-12s %-10d %-10d %-15s\n", i+1, idDisp, d.Kind, d.InDegree, d.OutDegree, d.Primitive)
		}

		return nil
	},
}

func init() {
	hotspotCmd.Flags().IntVar(&hotspotTop, "top", 10, "Number of top hotspot symbols to display")
	hotspotCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ database folder")
	rootCmd.AddCommand(hotspotCmd)
}
