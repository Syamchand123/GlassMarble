package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

var hotspotTop int

type nodeDegree struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	InDegree  int    `json:"in_degree"`
	OutDegree int    `json:"out_degree"`
	Primitive string `json:"primitive,omitempty"`
}

var hotspotCmd = &cobra.Command{
	Use:     "hotspot",
	GroupID: GroupInspect.ID,
	Short:   "Identify high-coupling architectural hotspots and most-depended-upon symbols",
	Long:    `Ranks nodes by in-degree call density and centrality to highlight architectural hotspots and high-risk refactoring targets.`,
	Example: `  # Display top 10 architectural hotspots
  gmb hotspot

  # Display top 25 hotspots
  gmb hotspot --top 25

  # Output hotspot rankings as JSON
  gmb hotspot --json`,
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
				out, _ := json.MarshalIndent(map[string]any{"top": 0, "hotspots": []nodeDegree{}}, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			return producterrs.Tagged("AKG database is empty — try 'gmb analyze' first", producterrs.ErrEmptySubgraph)
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
					Kind:      string(node.Kind),
					InDegree:  inCount,
					OutDegree: outCount,
					Primitive: string(node.Primitive),
				})
			}
		})

		sort.Slice(degrees, func(i, j int) bool {
			if degrees[i].InDegree == degrees[j].InDegree {
				return degrees[i].ID < degrees[j].ID
			}
			return degrees[i].InDegree > degrees[j].InDegree
		})

		if hotspotTop <= 0 {
			return producterrs.Tagged(fmt.Sprintf("invalid --top %d: must be >= 1 — try 'gmb hotspot --top 10'", hotspotTop), producterrs.ErrValidation)
		}
		requestedTop := hotspotTop
		limit := hotspotTop
		if limit > len(degrees) {
			limit = len(degrees)
		}
		top := degrees[:limit]

		if asJSON {
			out, _ := json.MarshalIndent(map[string]any{
				"top":      limit,
				"hotspots": top,
			}, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		}

		var viewRows []views.HotspotRow
		for i := 0; i < limit; i++ {
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
		tui.Fprintln(cmd.OutOrStdout(), views.RenderHotspot(requestedTop, viewRows))

		return nil
	},
}

func init() {
	hotspotCmd.Flags().IntVar(&hotspotTop, "top", 10, "Number of top hotspot symbols to display (1-100)")
	hotspotCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	rootCmd.AddCommand(hotspotCmd)
}
