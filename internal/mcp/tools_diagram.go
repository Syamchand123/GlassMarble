package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerDiagramTools binds diagram generation and catalog tools to the MCP server.
func (s *Server) registerDiagramTools() {
	if s.shouldRegister("gmb_render_diagram", "diagram") {
		renderDiagramTool := mcp.NewTool("gmb_render_diagram",
			mcp.WithDescription("Render architecture diagrams (C4, UML, ER, Flowchart, Dependency, Layered) from the active AKG."),
			mcp.WithString("type",
				mcp.Required(),
				mcp.Description("Diagram type: C4_CONTEXT, C4_CONTAINER, UML_CLASS, UML_SEQUENCE, DEPENDENCY_GRAPH, CALL_GRAPH, LAYERED_GRAPH, COMPONENT_GRAPH, FLOWCHART, ER_DIAGRAM, HOTSPOT_GRAPH"),
			),
			mcp.WithString("format",
				mcp.Description("Output markup format: mermaid (default), plantuml, dot, d2"),
			),
			mcp.WithString("scope",
				mcp.Description("Scope of the diagram: global (default), folder:<path>, file:<path>"),
			),
			mcp.WithString("entry",
				mcp.Description("Entry point node ID (e.g. 'cmd/root.go::Execute' - required for UML_SEQUENCE)"),
			),
			mcp.WithNumber("depth",
				mcp.Description("Maximum traversal depth from entry point (0 = unlimited)"),
			),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_render_diagram",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(renderDiagramTool, s.handleRenderDiagramTool)
	}
	if s.shouldRegister("gmb_list_diagram_types", "diagram") {
		listDiagramsTool := mcp.NewTool("gmb_list_diagram_types",
			mcp.WithDescription("List all supported architectural diagram types with descriptions and formats."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_list_diagram_types",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(listDiagramsTool, s.handleListDiagramTypesTool)
	}
}
func (s *Server) handleRenderDiagramTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if r, cancelled := checkCancellation(ctx); cancelled {
		return r, nil
	}
	diagramTypeStr, err := requireStringArg(req, "type")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(diagramTypeStr) > maxStringArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "type", maxStringArgLen, len(diagramTypeStr))), nil
	}

	token := getProgressToken(req)
	_ = s.sendProgress(ctx, token, 0, 100, "starting diagram render for "+diagramTypeStr)

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

	format := getStringArg(req, "format", "mermaid")
	if len(format) > maxStringArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "format", maxStringArgLen, len(format))), nil
	}
	scopeStr := getStringArg(req, "scope", "global")
	if len(scopeStr) > maxStringArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "scope", maxStringArgLen, len(scopeStr))), nil
	}
	entry := getStringArg(req, "entry", "")
	if len(entry) > maxIDArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "entry", maxIDArgLen, len(entry))), nil
	}
	depth := getIntArgClamped(req, "depth", 0, 0, 50)

	normType := strings.ToUpper(strings.TrimSpace(diagramTypeStr))
	normType = strings.ReplaceAll(normType, "-", "_")
	normType = strings.ReplaceAll(normType, " ", "_")

	dt := types.DiagramType(normType)

	scopeLevel, scopePath := parseDiagramScope(scopeStr)

	_ = s.sendProgress(ctx, token, 40, 100, "projecting diagram pipeline")
	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
	}

	opts := types.QueryOptions{
		Format:        format,
		Scope:         scopeLevel,
		ScopePath:     scopePath,
		EntryPointID:  entry,
		MaxDepth:      depth,
		IncludeUnused: false,
		PipelineCfg: &types.PipelineConfig{
			EnableMetrics:     true,
			EnableCommunities: true,
			EnableSCC:         true,
		},
	}

	markup, err := visualization_engine.ProjectDiagramFromGraph(graph.ToNativeGraph(), dt, opts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Diagram rendering failed: %v", err)), nil
	}

	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
	}
	_ = s.sendProgress(ctx, token, 80, 100, "serializing markup")

	result := map[string]any{
		"type":   normType,
		"format": format,
		"markup": markup,
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Serialization error: %v", err)), nil
	}

	_ = s.sendProgress(ctx, token, 100, 100, "complete")
	return mcp.NewToolResultText(string(out)), nil
}

func parseDiagramScope(scope string) (types.ScopeLevel, string) {
	if strings.HasPrefix(scope, "folder:") {
		return types.ScopeFolder, strings.TrimPrefix(scope, "folder:")
	}
	if strings.HasPrefix(scope, "file:") {
		return types.ScopeFile, strings.TrimPrefix(scope, "file:")
	}
	return types.ScopeGlobal, ""
}

func (s *Server) handleListDiagramTypesTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	type diagramInfo struct {
		Type        string   `json:"type"`
		Category    string   `json:"category"`
		Description string   `json:"description"`
		Formats     []string `json:"supported_formats"`
	}

	typesList := []diagramInfo{
		{Type: "C4_CONTEXT", Category: "C4", Description: "System Context diagram showing software systems and human actors", Formats: []string{"mermaid", "plantuml", "dot"}},
		{Type: "C4_CONTAINER", Category: "C4", Description: "Container diagram showing high-level deployable units and data stores", Formats: []string{"mermaid", "plantuml", "dot"}},
		{Type: "C4_COMPONENT", Category: "C4", Description: "Component diagram detailing structural modules inside containers", Formats: []string{"mermaid", "plantuml", "dot"}},
		{Type: "UML_CLASS", Category: "UML", Description: "Class diagram showing structs, interfaces, methods, and inheritance", Formats: []string{"mermaid", "plantuml", "dot"}},
		{Type: "UML_SEQUENCE", Category: "UML", Description: "Sequence diagram tracing execution flow from an entry point", Formats: []string{"mermaid", "plantuml"}},
		{Type: "DEPENDENCY_GRAPH", Category: "Structural", Description: "Comprehensive package and module dependency graph", Formats: []string{"mermaid", "plantuml", "dot", "d2"}},
		{Type: "CALL_GRAPH", Category: "Behavioral", Description: "Function and method invocation call graph", Formats: []string{"mermaid", "plantuml", "dot"}},
		{Type: "LAYERED_GRAPH", Category: "Governance", Description: "Layered architecture graph checking layer boundary compliance", Formats: []string{"mermaid", "dot"}},
		{Type: "FLOWCHART", Category: "Behavioral", Description: "Control flow diagram of functions and decision branches", Formats: []string{"mermaid"}},
		{Type: "ER_DIAGRAM", Category: "Data", Description: "Entity-Relationship diagram of database entities and schema", Formats: []string{"mermaid", "plantuml"}},
		{Type: "HOTSPOT_GRAPH", Category: "Quality", Description: "Architectural hotspot visualization highlighting high-risk nodes", Formats: []string{"mermaid", "dot"}},
	}

	out, err := json.MarshalIndent(map[string]any{
		"total_types":   len(typesList),
		"diagram_types": typesList,
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Serialization error: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}
