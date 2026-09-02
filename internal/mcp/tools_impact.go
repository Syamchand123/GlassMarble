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
	if s.shouldRegister("gmb_impact_analysis", "impact") {
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
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_impact_analysis",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(impactTool, s.handleImpactTool)
	}
	if s.shouldRegister("gmb_hotspot_rankings", "impact") {
		hotspotTool := mcp.NewTool("gmb_hotspot_rankings",
			mcp.WithDescription("Identify top architectural hotspots: symbols with highest in-degree / most dependents."),
			mcp.WithNumber("top",
				mcp.Description("Number of top hotspot nodes to return (default 10, max 100)"),
			),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_hotspot_rankings",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(hotspotTool, s.handleHotspotTool)
	}
}
func (s *Server) handleImpactTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if r, cancelled := checkCancellation(ctx); cancelled {
		return r, nil
	}
	target, err := requireStringArg(req, "target")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(target) > maxIDArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "target", maxIDArgLen, len(target))), nil
	}
	if len(target) > maxStringArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "target", maxStringArgLen, len(target))), nil
	}

	depth := getIntArgClamped(req, "depth", 0, 0, 50)
	testsOnly := false
	m := getArgMap(req)
	if val, ok := m["tests_only"]; ok {
		if b, ok := val.(bool); ok {
			testsOnly = b
		}
	}

	token := getProgressToken(req)
	_ = s.sendProgress(ctx, token, 0, 100, "starting impact analysis for "+target)

	graph, err := s.bridge.Snapshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("AKG database unavailable: %v — run 'gmb analyze' first", err)), nil
	}

	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
	}

	opts := impact_analyzer.ImpactOptions{
		MaxDepth:  depth,
		TestsOnly: testsOnly,
	}

	_ = s.sendProgress(ctx, token, 40, 100, "traversing reverse dependencies")
	report, err := impact_analyzer.AnalyzeImpact(graph, target, opts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Impact analysis failed: %v", err)), nil
	}

	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
	}
	_ = s.sendProgress(ctx, token, 80, 100, "serializing report")

	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to serialize impact report: %v", err)), nil
	}

	_ = s.sendProgress(ctx, token, 100, 100, "complete")
	return mcp.NewToolResultText(string(out)), nil
}

func (s *Server) handleHotspotTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if r, cancelled := checkCancellation(ctx); cancelled {
		return r, nil
	}
	top := getIntArgClamped(req, "top", 10, 1, 100)

	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
	}

	graph, err := s.bridge.Snapshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("AKG database unavailable: %v — run 'gmb analyze' first", err)), nil
	}

	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
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
		select {
		case <-ctx.Done():
			return
		default:
		}
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

	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
	}

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
