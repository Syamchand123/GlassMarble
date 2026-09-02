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
	if s.shouldRegister("gmb_code_definition", "code") {
		codeDefinitionTool := mcp.NewTool("gmb_code_definition",
			mcp.WithDescription("Locate symbol definition in the Architecture Knowledge Graph and return its source code implementation."),
			mcp.WithString("symbol",
				mcp.Required(),
				mcp.Description("Symbol ID (e.g. 'cmd/root.go::Execute') or function/struct name"),
			),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_code_definition",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(codeDefinitionTool, s.handleCodeDefinitionTool)
	}
	if s.shouldRegister("gmb_code_references", "code") {
		codeReferencesTool := mcp.NewTool("gmb_code_references",
			mcp.WithDescription("Find all callers, imports, and referencing locations for a symbol across the repository."),
			mcp.WithString("symbol",
				mcp.Required(),
				mcp.Description("Target symbol ID or name"),
			),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_code_references",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(codeReferencesTool, s.handleCodeReferencesTool)
	}
	if s.shouldRegister("gmb_code_callgraph", "code") {
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
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_code_callgraph",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(codeCallgraphTool, s.handleCodeCallgraphTool)
	}
	if s.shouldRegister("gmb_code_context", "code") {
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
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_code_context",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(codeContextTool, s.handleCodeContextTool)
	}
}
func (s *Server) handleCodeDefinitionTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if r, cancelled := checkCancellation(ctx); cancelled {
		return r, nil
	}
	symbol, err := requireStringArg(req, "symbol")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(symbol) > maxIDArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "symbol", maxIDArgLen, len(symbol))), nil
	}
	if len(symbol) > maxStringArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "symbol", maxStringArgLen, len(symbol))), nil
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

	node, ok := graph.GetNode(symbol)
	if !ok {
		// Search by name fallback
		var matches []*link.ResolvedNode
		graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if strings.EqualFold(n.Name, symbol) || strings.HasSuffix(id, "::"+symbol) {
				matches = append(matches, n)
			}
		})
		select {
		case <-ctx.Done():
			return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
		default:
		}
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
	// 1 MiB guard (Section 6.4 Compute Exhaustion & 6.6 Zero-Trust).
	if fi, err := os.Stat(absPath); err == nil && fi.Size() > maxFileBytes {
		return mcp.NewToolResultError(fmt.Sprintf("file %q is too large (%d bytes > %d bytes) — definition truncated to line range", node.FileSpec.Path, fi.Size(), maxFileBytes)), nil
	}
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
	if r, cancelled := checkCancellation(ctx); cancelled {
		return r, nil
	}
	symbol, err := requireStringArg(req, "symbol")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(symbol) > maxIDArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "symbol", maxIDArgLen, len(symbol))), nil
	}
	if len(symbol) > maxStringArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "symbol", maxStringArgLen, len(symbol))), nil
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

	inEdges := graph.GetInboundEdges(symbol)
	if len(inEdges) == 0 {
		// Fallback check if symbol is exact name
		graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if strings.EqualFold(n.Name, symbol) || strings.HasSuffix(id, "::"+symbol) {
				inEdges = append(inEdges, graph.GetInboundEdges(id)...)
			}
		})
		select {
		case <-ctx.Done():
			return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
		default:
		}
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
	if r, cancelled := checkCancellation(ctx); cancelled {
		return r, nil
	}
	symbol, err := requireStringArg(req, "symbol")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(symbol) > maxIDArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "symbol", maxIDArgLen, len(symbol))), nil
	}
	if len(symbol) > maxStringArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "symbol", maxStringArgLen, len(symbol))), nil
	}

	direction := getStringArg(req, "direction", "both")
	if len(direction) > maxStringArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "direction", maxStringArgLen, len(direction))), nil
	}
	depth := getIntArgClamped(req, "depth", 3, 1, 10)

	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
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
			select {
			case <-ctx.Done():
				return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
			default:
			}
			var nextQueue []string
			for _, curr := range queue {
				select {
				case <-ctx.Done():
					return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
				default:
				}
				for _, e := range graph.GetOutboundEdges(curr) {
					select {
					case <-ctx.Done():
						return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
					default:
					}
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
			select {
			case <-ctx.Done():
				return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
			default:
			}
			var nextQueue []string
			for _, curr := range queue {
				select {
				case <-ctx.Done():
					return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
				default:
				}
				for _, e := range graph.GetInboundEdges(curr) {
					select {
					case <-ctx.Done():
						return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
					default:
					}
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
	if r, cancelled := checkCancellation(ctx); cancelled {
		return r, nil
	}
	file, err := requireStringArg(req, "file")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(file) > maxStringArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "file", maxStringArgLen, len(file))), nil
	}
	if len(file) > maxIDArgLen && strings.Contains(file, "::") {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "file", maxIDArgLen, len(file))), nil
	}

	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
	}

	line := getIntArgClamped(req, "line", 1, 1, 1000000)
	radius := getIntArgClamped(req, "radius", 20, 5, 100)

	absPath := filepath.Join(s.bridge.RootDir(), filepath.FromSlash(file))
	// 1 MiB guard (Section 6.4): reject overly large files before reading.
	if fi, err := os.Stat(absPath); err == nil && fi.Size() > maxFileBytes {
		return mcp.NewToolResultError(fmt.Sprintf("file %q is too large (%d bytes > %d bytes) — use a smaller radius or read a bounded line range", file, fi.Size(), maxFileBytes)), nil
	}
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
			select {
			case <-ctx.Done():
				return
			default:
			}
			if filepath.ToSlash(n.FileSpec.Path) == cleanFile {
				if n.FileSpec.LineStart <= line && (n.FileSpec.LineEnd >= line || n.FileSpec.LineEnd == 0) {
					enclosingSymbols = append(enclosingSymbols, fmt.Sprintf("%s [%s]", n.ID, n.Kind))
				}
			}
		})
		select {
		case <-ctx.Done():
			return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
		default:
		}
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
