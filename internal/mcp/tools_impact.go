package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/impact_analyzer"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerImpactTools binds blast-radius and hotspot analysis tools to the MCP server.
func (s *Server) registerImpactTools() {
	// 1. gmb_impact_analysis Tool
	impactTool := mcp.NewTool("gmb_impact_analysis",
		mcp.WithDescription("Compute reverse topological blast-radius and architectural risk assessment for modifying a symbol or file."),
		mcp.WithString("target",
			mcp.Required(),
			mcp.Description("Target symbol ID (e.g. 'cmd/root.go::Execute') or relative file path (e.g. 'internal/akg/graph.go')"),
		),
		mcp.WithNumber("depth",
			mcp.Description("Maximum reverse traversal depth (default 0 = unlimited closure)"),
		),
		mcp.WithBoolean("tests_only",
			mcp.Description("Restrict affected dependents report exclusively to impacted test files"),
		),
	)
	s.RegisterTool(impactTool, s.handleImpactTool)

	// 2. gmb_hotspot_rankings Tool
	hotspotTool := mcp.NewTool("gmb_hotspot_rankings",
		mcp.WithDescription("Identify top architectural hotspots: symbols with highest in-degree / most dependents."),
		mcp.WithNumber("top",
			mcp.Description("Number of top hotspot nodes to return (default 10, max 100)"),
		),
	)
	s.RegisterTool(hotspotTool, s.handleHotspotTool)
}

func (s *Server) handleImpactTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	target, err := requireStringArg(req, "target")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	depth := getIntArg(req, "depth", 0)
	testsOnly := false
	m := getArgMap(req)
	if val, ok := m["tests_only"]; ok {
		if b, ok := val.(bool); ok {
			testsOnly = b
		}
	}

	graph, err := s.bridge.Snapshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("AKG database unavailable: %v — run 'gmb analyze' first", err)), nil
	}

	opts := impact_analyzer.ImpactOptions{
		MaxDepth:  depth,
		TestsOnly: testsOnly,
	}

	report, err := impact_analyzer.AnalyzeImpact(graph, target, opts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Impact analysis failed: %v", err)), nil
	}

	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to serialize impact report: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func (s *Server) handleHotspotTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	top := getIntArg(req, "top", 10)
	if top < 1 {
		top = 10
	}
	if top > 100 {
		top = 100
	}

	graph, err := s.bridge.Snapshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("AKG database unavailable: %v — run 'gmb analyze' first", err)), nil
	}

	type hotspotNode struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		Primitive string `json:"primitive,omitempty"`
		File      string `json:"file"`
		Line      int    `json:"line"`
		InDegree  int    `json:"in_degree"`
		OutDegree int    `json:"out_degree"`
	}

	var allNodes []hotspotNode
	graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		inEdges := len(graph.GetInboundEdges(id))
		outEdges := len(graph.GetOutboundEdges(id))
		allNodes = append(allNodes, hotspotNode{
			ID:        n.ID,
			Name:      n.Name,
			Kind:      string(n.Kind),
			Primitive: string(n.Primitive),
			File:      filepath.ToSlash(n.FileSpec.Path),
			Line:      n.FileSpec.LineStart,
			InDegree:  inEdges,
			OutDegree: outEdges,
		})
	})

	sort.Slice(allNodes, func(i, j int) bool {
		if allNodes[i].InDegree == allNodes[j].InDegree {
			return allNodes[i].ID < allNodes[j].ID
		}
		return allNodes[i].InDegree > allNodes[j].InDegree
	})

	total := len(allNodes)
	if len(allNodes) > top {
		allNodes = allNodes[:top]
	}

	out, err := json.MarshalIndent(map[string]any{
		"total_nodes": total,
		"top":         top,
		"hotspots":    allNodes,
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to serialize hotspots: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}
