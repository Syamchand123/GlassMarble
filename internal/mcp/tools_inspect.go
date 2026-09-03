package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerInspectTools binds symbol inspection and dependency analysis tools.
func (s *Server) registerInspectTools() {
	if s.shouldRegister("gmb_inspect_search", "inspect") {
		inspectSearchTool := mcp.NewTool("gmb_inspect_search",
			mcp.WithDescription("Search AKG graph nodes by symbol name or ID substring, with optional kind filter."),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Substring to search for in symbol name or node ID"),
			),
			mcp.WithString("kind",
				mcp.Description("Optional node kind filter (e.g. FUNCTION, METHOD, STRUCT, CLASS, INTERFACE, FILE, MODULE, PACKAGE)"),
			),
			mcp.WithNumber("limit",
				mcp.Description("Maximum matching nodes to return (default 50, max 200)"),
			),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_inspect_search",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(inspectSearchTool, s.handleInspectSearchTool)
	}
	if s.shouldRegister("gmb_inspect_node", "inspect") {
		inspectNodeTool := mcp.NewTool("gmb_inspect_node",
			mcp.WithDescription("Get detailed symbol metadata, file location, properties, and inbound/outbound dependency edges for a node ID."),
			mcp.WithString("id",
				mcp.Required(),
				mcp.Description("Full node ID (e.g. 'cmd/root.go::Execute' or 'internal/akg/graph.go::CodePropertyGraph')"),
			),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_inspect_node",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(inspectNodeTool, s.handleInspectNodeTool)
	}
	if s.shouldRegister("gmb_dependency_analysis", "inspect") {
		dependencyTool := mcp.NewTool("gmb_dependency_analysis",
			mcp.WithDescription("Analyze inbound and outbound dependency edges for a file or symbol in the Architecture Knowledge Graph."),
			mcp.WithString("target",
				mcp.Description("Target symbol ID or relative file path (leave empty for global dependency summary)"),
			),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_dependency_analysis",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(dependencyTool, s.handleDependencyTool)
	}
}
type inspectNodeResult struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Primitive string `json:"primitive,omitempty"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Inbound   int    `json:"inbound_edges"`
	Outbound  int    `json:"outbound_edges"`
}

func (s *Server) handleInspectSearchTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if r, cancelled := checkCancellation(ctx); cancelled {
		return r, nil
	}
	query, err := requireStringArg(req, "query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(query) > maxStringArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "query", maxStringArgLen, len(query))), nil
	}
	if len(query) > maxIDArgLen && strings.Contains(strings.ToLower(query), "::") {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "query", maxIDArgLen, len(query))), nil
	}

	kind := getStringArg(req, "kind", "")
	if len(kind) > maxStringArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "kind", maxStringArgLen, len(kind))), nil
	}
	limit := getIntArgClamped(req, "limit", 50, 1, 200)

	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
	}

	graph, err := s.bridge.Snapshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("AKG database unavailable: %v — run 'gmb analyze' first", err)), nil
	}

	lowerQuery := strings.ToLower(query)
	var matched []inspectNodeResult

	graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if strings.Contains(strings.ToLower(id), lowerQuery) || strings.Contains(strings.ToLower(n.Name), lowerQuery) {
			if kind == "" || strings.EqualFold(string(n.Kind), kind) {
				inEdges := len(graph.GetInboundEdges(id))
				outEdges := len(graph.GetOutboundEdges(id))
				matched = append(matched, inspectNodeResult{
					ID:        n.ID,
					Name:      n.Name,
					Kind:      string(n.Kind),
					Primitive: string(n.Primitive),
					File:      filepath.ToSlash(n.FileSpec.Path),
					Line:      n.FileSpec.LineStart,
					Inbound:   inEdges,
					Outbound:  outEdges,
				})
			}
		}
	})

	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
	}

	sort.Slice(matched, func(i, j int) bool {
		if matched[i].Inbound == matched[j].Inbound {
			return matched[i].ID < matched[j].ID
		}
		return matched[i].Inbound > matched[j].Inbound
	})

	totalCount := len(matched)
	if len(matched) > limit {
		matched = matched[:limit]
	}

	out, err := json.MarshalIndent(map[string]any{
		"query":  query,
		"kind":   kind,
		"total":  totalCount,
		"count":  len(matched),
		"limit":  limit,
		"nodes":  matched,
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to serialize search results: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func (s *Server) handleInspectNodeTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if r, cancelled := checkCancellation(ctx); cancelled {
		return r, nil
	}
	id, err := requireStringArg(req, "id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(id) > maxIDArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "id", maxIDArgLen, len(id))), nil
	}
	if len(id) > maxStringArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "id", maxStringArgLen, len(id))), nil
	}

	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
	}

	graph, err := s.bridge.Snapshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("AKG database unavailable: %v — run 'gmb analyze' first", err)), nil
	}

	node, ok := graph.GetNode(id)
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf("symbol %q not found in Architecture Knowledge Graph — try searching with gmb_inspect_search", id)), nil
	}

	inEdges := graph.GetInboundEdges(id)
	outEdges := graph.GetOutboundEdges(id)

	type edgeDetail struct {
		SourceID   string  `json:"source_id,omitempty"`
		TargetID   string  `json:"target_id,omitempty"`
		Type       string  `json:"type"`
		LineNumber int     `json:"line_number,omitempty"`
		Confidence float32 `json:"confidence,omitempty"`
	}

	inbound := make([]edgeDetail, 0, len(inEdges))
	for _, e := range inEdges {
		inbound = append(inbound, edgeDetail{
			SourceID:   e.SourceID,
			Type:       string(e.Type),
			LineNumber: e.LineNumber,
			Confidence: e.Confidence,
		})
	}

	outbound := make([]edgeDetail, 0, len(outEdges))
	for _, e := range outEdges {
		outbound = append(outbound, edgeDetail{
			TargetID:   e.TargetID,
			Type:       string(e.Type),
			LineNumber: e.LineNumber,
			Confidence: e.Confidence,
		})
	}

	detail := map[string]any{
		"id":         node.ID,
		"name":       node.Name,
		"kind":       node.Kind,
		"primitive":  node.Primitive,
		"file":       filepath.ToSlash(node.FileSpec.Path),
		"line":       node.FileSpec.LineStart,
		"line_end":   node.FileSpec.LineEnd,
		"properties": node.Properties,
		"inbound":    inbound,
		"outbound":   outbound,
	}

	out, err := json.MarshalIndent(detail, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to serialize node details: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func (s *Server) handleDependencyTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if r, cancelled := checkCancellation(ctx); cancelled {
		return r, nil
	}
	target := getStringArg(req, "target", "")
	if len(target) > maxIDArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "target", maxIDArgLen, len(target))), nil
	}
	if len(target) > maxStringArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "target", maxStringArgLen, len(target))), nil
	}

	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
	}

	graph, err := s.bridge.Snapshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("AKG database unavailable: %v — run 'gmb analyze' first", err)), nil
	}

	if target == "" || target == "summary" {
		type topNode struct {
			ID       string `json:"id"`
			Outbound int    `json:"outbound_edges"`
		}

		var allNodes []topNode
		graph.OutboundEdges.Iterate(func(id string, edges []link.ResolvedEdge) {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if len(edges) > 0 {
				allNodes = append(allNodes, topNode{ID: id, Outbound: len(edges)})
			}
		})

		select {
		case <-ctx.Done():
			return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
		default:
		}

		sort.Slice(allNodes, func(i, j int) bool {
			if allNodes[i].Outbound == allNodes[j].Outbound {
				return allNodes[i].ID < allNodes[j].ID
			}
			return allNodes[i].Outbound > allNodes[j].Outbound
		})

		limit := 25
		if len(allNodes) < limit {
			limit = len(allNodes)
		}

		summary := map[string]any{
			"total_nodes":            graph.Nodes.Len(),
			"outbound_edge_mappings": graph.OutboundEdges.Len(),
			"inbound_edge_mappings":  graph.InboundEdges.Len(),
			"top_dependency_nodes":   allNodes[:limit],
		}

		out, _ := json.MarshalIndent(summary, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}

	// Target-specific dependency lookup
	var targetIDs []string
	cleanTarget := filepath.ToSlash(target)

	// Check if target matches a file path or a symbol ID
	if _, ok := graph.GetNode(target); ok {
		targetIDs = append(targetIDs, target)
	} else {
			// Try file match
		graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if filepath.ToSlash(n.FileSpec.Path) == cleanTarget || strings.HasSuffix(filepath.ToSlash(n.FileSpec.Path), cleanTarget) {
				targetIDs = append(targetIDs, id)
			}
		})
	}

	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
	}

	if len(targetIDs) == 0 {
		return mcp.NewToolResultError(fmt.Sprintf("target %q not found as a symbol ID or file path in AKG", target)), nil
	}

	type depEdge struct {
		Type       string `json:"type"`
		OtherID    string `json:"id"`
		LineNumber int    `json:"line,omitempty"`
	}

	type nodeDepReport struct {
		ID       string    `json:"id"`
		Outbound []depEdge `json:"outbound"`
		Inbound  []depEdge `json:"inbound"`
	}

	var reports []nodeDepReport
	for _, id := range targetIDs {
		select {
		case <-ctx.Done():
			return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
		default:
		}
		outbound := []depEdge{}
		for _, e := range graph.GetOutboundEdges(id) {
			select {
			case <-ctx.Done():
				return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
			default:
			}
			outbound = append(outbound, depEdge{Type: string(e.Type), OtherID: e.TargetID, LineNumber: e.LineNumber})
		}
		inbound := []depEdge{}
		for _, e := range graph.GetInboundEdges(id) {
			select {
			case <-ctx.Done():
				return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
			default:
			}
			inbound = append(inbound, depEdge{Type: string(e.Type), OtherID: e.SourceID, LineNumber: e.LineNumber})
		}
		reports = append(reports, nodeDepReport{
			ID:       id,
			Outbound: outbound,
			Inbound:  inbound,
		})
	}

	out, err := json.MarshalIndent(map[string]any{
		"target":  target,
		"matched": len(reports),
		"nodes":   reports,
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to serialize dependency report: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func getArgMap(req mcp.CallToolRequest) map[string]any {
	if req.Params.Arguments == nil {
		return make(map[string]any)
	}
	if m, ok := req.Params.Arguments.(map[string]any); ok {
		return m
	}
	return make(map[string]any)
}

func getStringArg(req mcp.CallToolRequest, name, def string) string {
	m := getArgMap(req)
	if val, ok := m[name]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return def
}

func requireStringArg(req mcp.CallToolRequest, name string) (string, error) {
	s := getStringArg(req, name, "")
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("missing required parameter %q", name)
	}
	return s, nil
}

func getIntArg(req mcp.CallToolRequest, name string, def int) int {
	m := getArgMap(req)
	if val, ok := m[name]; ok {
		switch n := val.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return int(i)
			}
		case string:
			// Clients sometimes send numeric arguments as strings; a silent
			// fall-through to the default here masked real caller intent.
			if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
				return i
			}
		}
	}
	return def
}

func getIntArgClamped(req mcp.CallToolRequest, name string, def, lo, hi int) int {
	v := getIntArg(req, name, def)
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

