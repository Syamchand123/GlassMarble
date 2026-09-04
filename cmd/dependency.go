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
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
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
	ID       string           `json:"id"`
	Outbound []dependencyEdge `json:"outbound"`
	Inbound  []dependencyEdge `json:"inbound"`
}

var dependencyCmd = &cobra.Command{
	Use:     "dependency [target_file_or_symbol]",
	Aliases: []string{"dep"},
	GroupID: GroupInspect.ID,
	Short:   "Analyze inbound and outbound dependencies for a file or symbol",
	Long:    `Inspects direct call, import, and composition dependencies for a target file or symbol in the Architecture Knowledge Graph.`,
	Example: `  # View global dependency summary and top outbound nodes
  gmb dependency

  # Inspect inbound and outbound edges for a specific symbol
  gmb dependency "cmd/root.go::Execute"

  # Inspect dependencies for an entire file
  gmb dependency internal/app/app.go

  # Output dependency edges as JSON
  gmb dependency --json`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := ""
		if len(args) > 0 {
			target = args[0]
		}
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
				out, _ := json.MarshalIndent(map[string]string{"error": "no active AKG database"}, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			return producterrs.Tagged("AKG database is empty — try 'gmb analyze' first", producterrs.ErrEmptySubgraph)
		}

		if target == "" {
			summary := dependencySummaryJSON{
				TotalNodes:           snapshot.Nodes.Len(),
				OutboundEdgeMappings: snapshot.OutboundEdges.Len(),
				InboundEdgeMappings:  snapshot.InboundEdges.Len(),
			}
			var allNodes []topDependencyNode
			keys := snapshot.OutboundEdges.Keys()
			for _, id := range keys {
				if outbound, ok := snapshot.OutboundEdges.Get(id); ok && len(outbound) > 0 {
					allNodes = append(allNodes, topDependencyNode{ID: id, Outbound: len(outbound)})
				}
			}

			// Sort by outbound degree descending
			sort.Slice(allNodes, func(i, j int) bool {
				if allNodes[i].Outbound == allNodes[j].Outbound {
					return allNodes[i].ID < allNodes[j].ID
				}
				return allNodes[i].Outbound > allNodes[j].Outbound
			})

			topLimit := 20
			if len(allNodes) < topLimit {
				topLimit = len(allNodes)
			}
			topNodes := allNodes[:topLimit]
			summary.TopDependencyNodes = topNodes

			if asJSON {
				out, _ := json.MarshalIndent(summary, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}

			var topNodesView []views.TopDependencyNode
			for _, n := range topNodes {
				topNodesView = append(topNodesView, views.TopDependencyNode{ID: n.ID, Outbound: n.Outbound})
			}
			tui.Fprintln(cmd.OutOrStdout(), views.RenderDependencySummary(snapshot.Nodes.Len(), snapshot.OutboundEdges.Len(), snapshot.InboundEdges.Len(), topNodesView))
			return nil
		}

		var matchingNodes []string
		lowerTarget := strings.ToLower(target)

		snapshot.Nodes.Iterate(func(id string, node *link.ResolvedNode) {
			if strings.Contains(strings.ToLower(id), lowerTarget) || strings.Contains(strings.ToLower(node.FileSpec.Path), lowerTarget) || strings.EqualFold(node.Name, target) {
				matchingNodes = append(matchingNodes, id)
			}
		})

		if len(matchingNodes) == 0 {
			return producterrs.Tagged(fmt.Sprintf("no matching node or file found for '%s' — try 'gmb inspect --search %s'", target, target), producterrs.ErrEntryNotFound)
		}

		sort.Strings(matchingNodes)

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
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		}

		for _, nodeID := range matchingNodes {
			var outboundView, inboundView []views.DependencyEdge
			outbound, ok := snapshot.OutboundEdges.Get(nodeID)
			if ok && len(outbound) > 0 {
				for _, edge := range outbound {
					outboundView = append(outboundView, views.DependencyEdge{Type: string(edge.Type), OtherID: edge.TargetID, LineNumber: edge.LineNumber})
				}
			}
			inbound, ok := snapshot.InboundEdges.Get(nodeID)
			if ok && len(inbound) > 0 {
				for _, edge := range inbound {
					inboundView = append(inboundView, views.DependencyEdge{Type: string(edge.Type), OtherID: edge.SourceID, LineNumber: edge.LineNumber})
				}
			}
			tui.Fprintln(cmd.OutOrStdout(), views.RenderDependencyTarget(nodeID, outboundView, inboundView))
		}

		return nil
	},
}

// dependencySummaryJSON is the machine-readable repository summary.
type dependencySummaryJSON struct {
	TotalNodes           int                 `json:"total_nodes"`
	OutboundEdgeMappings int                 `json:"outbound_edge_mappings"`
	InboundEdgeMappings  int                 `json:"inbound_edge_mappings"`
	TopDependencyNodes   []topDependencyNode `json:"top_dependency_nodes"`
}

type topDependencyNode struct {
	ID       string `json:"id"`
	Outbound int    `json:"outbound_dependencies"`
}

func init() {
	dependencyCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	rootCmd.AddCommand(dependencyCmd)
}
