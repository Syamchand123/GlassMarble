package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
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
			return producterrs.Tagged(fmt.Sprintf("AKG database is empty -- run 'glassmarble analyze' first"), producterrs.ErrEmptySubgraph)
		}

		var degrees []nodeDegree
		snapshot.Nodes.Iterate(func(id string, node *link.ResolvedNode) {
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

		// C6-D12: validate --top to avoid panic on negative/zero.
		if hotspotTop <= 0 {
			return producterrs.Tagged(fmt.Sprintf("invalid --top %d: must be between 1 and %d", hotspotTop, len(degrees)), producterrs.ErrValidation)
		}
		if hotspotTop > len(degrees) {
			hotspotTop = len(degrees)
		}
		limit := hotspotTop
		top := degrees[:limit]

		if asJSON {
			out, _ := json.MarshalIndent(map[string]any{
				"top":      limit,
				"hotspots": top,
			}, "", "  ")
			fmt.Println(string(out))
			return nil
		}

		var viewRows []views.HotspotRow
		for i := 0; i < len(degrees) && i < hotspotTop; i++ {
			d := degrees[i]
			viewRows = append(viewRows, views.HotspotRow{
				Rank:      i + 1,
				Name:      d.ID,
				Kind:      d.Kind,
				InDegree:  d.InDegree,
				OutDegree: d.OutDegree,
				Primitive: d.Primitive,
			})
		}
		fmt.Println(views.RenderHotspot(hotspotTop, viewRows))

		return nil
	},
}

func init() {
	hotspotCmd.Flags().IntVar(&hotspotTop, "top", 10, "Number of top hotspot symbols to display")
	hotspotCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ database folder")
	hotspotCmd.Flags().Bool("json", false, "Emit machine-readable JSON instead of the human report")
	rootCmd.AddCommand(hotspotCmd)
}
