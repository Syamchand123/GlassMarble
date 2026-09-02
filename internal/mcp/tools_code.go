package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerCodeTools binds source code inspection and navigation tools to the MCP server.
func (s *Server) registerCodeTools() {
	// 1. gmb_code_definition Tool
	codeDefinitionTool := mcp.NewTool("gmb_code_definition",
		mcp.WithDescription("Locate symbol definition in the Architecture Knowledge Graph and return its source code implementation."),
		mcp.WithString("symbol",
			mcp.Required(),
			mcp.Description("Symbol ID (e.g. 'cmd/root.go::Execute') or function/struct name"),
		),
	)
	s.RegisterTool(codeDefinitionTool, s.handleCodeDefinitionTool)

	// 2. gmb_code_references Tool
	codeReferencesTool := mcp.NewTool("gmb_code_references",
		mcp.WithDescription("Find all callers, imports, and referencing locations for a symbol across the repository."),
		mcp.WithString("symbol",
			mcp.Required(),
			mcp.Description("Target symbol ID or name"),
		),
	)
	s.RegisterTool(codeReferencesTool, s.handleCodeReferencesTool)

	// 3. gmb_code_callgraph Tool
	codeCallgraphTool := mcp.NewTool("gmb_code_callgraph",
		mcp.WithDescription("Traverse function/method invocation call graph from a starting symbol."),
		mcp.WithString("symbol",
			mcp.Required(),
			mcp.Description("Root function or method symbol ID"),
		),
		mcp.WithString("direction",
			mcp.Description("Call traversal direction: outgoing (callees), incoming (callers), or both (default)"),
		),
		mcp.WithNumber("depth",
			mcp.Description("Maximum call chain traversal depth (default 3, max 10)"),
		),
	)
	s.RegisterTool(codeCallgraphTool, s.handleCodeCallgraphTool)

	// 4. gmb_code_context Tool
	codeContextTool := mcp.NewTool("gmb_code_context",
		mcp.WithDescription("Retrieve surrounding source code context, enclosing symbols, and dependencies for a file and line number."),
		mcp.WithString("file",
			mcp.Required(),
			mcp.Description("Relative file path (e.g. 'internal/mcp/server.go')"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("1-based line number"),
		),
		mcp.WithNumber("radius",
			mcp.Description("Context radius lines before and after (default 20, max 100)"),
		),
	)
	s.RegisterTool(codeContextTool, s.handleCodeContextTool)
}

func (s *Server) handleCodeDefinitionTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	symbol, err := requireStringArg(req, "symbol")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	graph, err := s.bridge.Snapshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("AKG database unavailable: %v — run 'gmb analyze' first", err)), nil
	}

	node, ok := graph.GetNode(symbol)
	if !ok {
		// Search by name fallback
		var matches []*link.ResolvedNode
		graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
			if strings.EqualFold(n.Name, symbol) || strings.HasSuffix(id, "::"+symbol) {
				matches = append(matches, n)
			}
		})
		if len(matches) > 0 {
			node = matches[0]
			ok = true
		}
	}

	if !ok || node == nil {
		return mcp.NewToolResultError(fmt.Sprintf("Symbol %q not found in Architecture Knowledge Graph", symbol)), nil
	}

	if node.FileSpec.Path == "" || node.FileSpec.LineStart <= 0 {
		return mcp.NewToolResultError(fmt.Sprintf("Symbol %q has no source file location recorded in AKG", symbol)), nil
	}

	absPath := filepath.Join(s.bridge.RootDir(), filepath.FromSlash(node.FileSpec.Path))
	data, err := os.ReadFile(absPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read source file %s: %v", node.FileSpec.Path, err)), nil
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	start := node.FileSpec.LineStart
	end := node.FileSpec.LineEnd
	if start < 1 {
		start = 1
	}
	if end < start {
		end = start + 30
	}
	if end > len(lines) {
		end = len(lines)
	}

	var codeLines []string
	for i := start; i <= end; i++ {
		codeLines = append(codeLines, fmt.Sprintf("%6d | %s", i, lines[i-1]))
	}

	result := map[string]any{
		"id":         node.ID,
		"name":       node.Name,
		"kind":       node.Kind,
		"file":       filepath.ToSlash(node.FileSpec.Path),
		"line_start": start,
		"line_end":   end,
		"properties": node.Properties,
		"source":     strings.Join(codeLines, "\n"),
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Serialization error: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func (s *Server) handleCodeReferencesTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	symbol, err := requireStringArg(req, "symbol")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	graph, err := s.bridge.Snapshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("AKG database unavailable: %v — run 'gmb analyze' first", err)), nil
	}

	inEdges := graph.GetInboundEdges(symbol)
	if len(inEdges) == 0 {
		// Fallback check if symbol is exact name
		graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
			if strings.EqualFold(n.Name, symbol) || strings.HasSuffix(id, "::"+symbol) {
				inEdges = append(inEdges, graph.GetInboundEdges(id)...)
			}
		})
	}

	type refItem struct {
		SourceID   string `json:"source_id"`
		SourceName string `json:"source_name,omitempty"`
		Type       string `json:"type"`
		File       string `json:"file,omitempty"`
		Line       int    `json:"line,omitempty"`
	}

	references := make([]refItem, 0, len(inEdges))
	for _, e := range inEdges {
		item := refItem{
			SourceID: e.SourceID,
			Type:     string(e.Type),
			Line:     e.LineNumber,
		}
		if srcNode, ok := graph.GetNode(e.SourceID); ok {
			item.SourceName = srcNode.Name
			item.File = filepath.ToSlash(srcNode.FileSpec.Path)
			if item.Line <= 0 {
				item.Line = srcNode.FileSpec.LineStart
			}
		}
		references = append(references, item)
	}

	result := map[string]any{
		"symbol":     symbol,
		"total_refs": len(references),
		"references": references,
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Serialization error: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func (s *Server) handleCodeCallgraphTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	symbol, err := requireStringArg(req, "symbol")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	direction := getStringArg(req, "direction", "both")
	depth := getIntArg(req, "depth", 3)
	if depth < 1 {
		depth = 1
	}
	if depth > 10 {
		depth = 10
	}

	graph, err := s.bridge.Snapshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("AKG database unavailable: %v — run 'gmb analyze' first", err)), nil
	}

	type callEdge struct {
		Caller string `json:"caller"`
		Callee string `json:"callee"`
		Type   string `json:"type"`
	}

	var callEdges []callEdge
	visited := make(map[string]bool)

	// Collect outgoing calls
	if direction == "outgoing" || direction == "both" {
		var queue []string
		queue = append(queue, symbol)
		visited[symbol] = true

		for d := 0; d < depth && len(queue) > 0; d++ {
			var nextQueue []string
			for _, curr := range queue {
				for _, e := range graph.GetOutboundEdges(curr) {
					if strings.Contains(strings.ToLower(string(e.Type)), "call") || strings.Contains(strings.ToLower(string(e.Type)), "invoke") {
						callEdges = append(callEdges, callEdge{
							Caller: curr,
							Callee: e.TargetID,
							Type:   string(e.Type),
						})
						if !visited[e.TargetID] {
							visited[e.TargetID] = true
							nextQueue = append(nextQueue, e.TargetID)
						}
					}
				}
			}
			queue = nextQueue
		}
	}

	// Collect incoming calls
	if direction == "incoming" || direction == "both" {
		var queue []string
		queue = append(queue, symbol)
		visited[symbol] = true

		for d := 0; d < depth && len(queue) > 0; d++ {
			var nextQueue []string
			for _, curr := range queue {
				for _, e := range graph.GetInboundEdges(curr) {
					if strings.Contains(strings.ToLower(string(e.Type)), "call") || strings.Contains(strings.ToLower(string(e.Type)), "invoke") {
						callEdges = append(callEdges, callEdge{
							Caller: e.SourceID,
							Callee: curr,
							Type:   string(e.Type),
						})
						if !visited[e.SourceID] {
							visited[e.SourceID] = true
							nextQueue = append(nextQueue, e.SourceID)
						}
					}
				}
			}
			queue = nextQueue
		}
	}

	result := map[string]any{
		"root_symbol": symbol,
		"direction":   direction,
		"depth":       depth,
		"total_calls": len(callEdges),
		"call_edges":  callEdges,
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Serialization error: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func (s *Server) handleCodeContextTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	file, err := requireStringArg(req, "file")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	line := getIntArg(req, "line", 1)
	radius := getIntArg(req, "radius", 20)
	if radius < 5 {
		radius = 5
	}
	if radius > 100 {
		radius = 100
	}

	absPath := filepath.Join(s.bridge.RootDir(), filepath.FromSlash(file))
	data, err := os.ReadFile(absPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read file %s: %v", file, err)), nil
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	start := line - radius
	if start < 1 {
		start = 1
	}
	end := line + radius
	if end > len(lines) {
		end = len(lines)
	}

	var codeLines []string
	for i := start; i <= end; i++ {
		prefix := "  "
		if i == line {
			prefix = "> "
		}
		codeLines = append(codeLines, fmt.Sprintf("%s%6d | %s", prefix, i, lines[i-1]))
	}

	// Identify enclosing symbol from AKG
	graph, _ := s.bridge.Snapshot()
	var enclosingSymbols []string
	if graph != nil {
		cleanFile := filepath.ToSlash(file)
		graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
			if filepath.ToSlash(n.FileSpec.Path) == cleanFile {
				if n.FileSpec.LineStart <= line && (n.FileSpec.LineEnd >= line || n.FileSpec.LineEnd == 0) {
					enclosingSymbols = append(enclosingSymbols, fmt.Sprintf("%s [%s]", n.ID, n.Kind))
				}
			}
		})
	}

	result := map[string]any{
		"file":              file,
		"line":              line,
		"start_line":        start,
		"end_line":          end,
		"enclosing_symbols": enclosingSymbols,
		"snippet":           strings.Join(codeLines, "\n"),
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Serialization error: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}
