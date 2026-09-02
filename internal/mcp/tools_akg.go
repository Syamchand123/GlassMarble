package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/tools"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerAKGTools binds all graph query, code, and diagram tools from internal/ai_engine/tools to the MCP server.
func (s *Server) registerAKGTools() {
	allTools := tools.All()

	for _, t := range allTools {
		// Skip destructive or write tools (MCP server is read-only)
		if t.Name == "save_artifact" {
			continue
		}
		// Special alias handling: code_list_dir <-> code_list_directory share fate under filter.
		if t.Name == "code_list_dir" {
			if !s.shouldRegister("code_list_dir", t.Category) && !s.shouldRegister("code_list_directory", t.Category) {
				continue
			}
		} else if !s.shouldRegister(t.Name, t.Category) {
			continue
		}

		t := t // Capture loop variable

		// Convert JSON-schema parameters map to mcp.ToolInputSchema
		var inputSchema mcp.ToolInputSchema
		if t.Parameters != nil {
			if schemaBytes, err := json.Marshal(t.Parameters); err == nil {
				_ = json.Unmarshal(schemaBytes, &inputSchema)
			}
		}
		if inputSchema.Type == "" {
			inputSchema.Type = "object"
		}

		mcpTool := mcp.Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: inputSchema,
			Annotations: mcp.ToolAnnotation{
				Title:           t.Name,
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			},
		}

		// Shared handler for delegated tool (read-only) with real cancellation (§3.4).
		handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if r, cancelled := checkCancellation(ctx); cancelled {
				return r, nil
			}
			select {
			case <-ctx.Done():
				return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
			default:
			}
			akgBridge, err := s.bridge.AKGBridge()
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("AKG database unavailable: %v — run 'gmb analyze' first", err)), nil
			}

			env := &tools.Env{
				RootDir: s.bridge.RootDir(),
				Bridge:  akgBridge,
			}

			args := make(map[string]any)
			if req.Params.Arguments != nil {
				if m, ok := req.Params.Arguments.(map[string]any); ok {
					args = m
				}
			}

			select {
			case <-ctx.Done():
				return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
			default:
			}
			result, err := t.Handler(ctx, env, args)
			if err != nil {
				// Map context cancellation inside handler to tool error
				if ctx.Err() != nil {
					return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
				}
				return mcp.NewToolResultError(err.Error()), nil
			}
			select {
			case <-ctx.Done():
				return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
			default:
			}

			if raw, ok := result.(tools.Raw); ok {
				return mcp.NewToolResultText(string(raw)), nil
			}

			jsonBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("serialization failed: %v", err)), nil
			}

			return mcp.NewToolResultText(string(jsonBytes)), nil
		}

		s.RegisterTool(mcpTool, handler)

		// Spec-required alias: code_list_directory (spec) vs code_list_dir (actual).
		// Reuse the same handler and schema so both names resolve identically.
		if t.Name == "code_list_dir" && s.shouldRegister("code_list_directory", t.Category) {
			aliasTool := mcp.Tool{
				Name:        "code_list_directory",
				Description: t.Description + " (alias for code_list_dir)",
				InputSchema: inputSchema,
				Annotations: mcp.ToolAnnotation{
					Title:           "code_list_directory",
					ReadOnlyHint:    mcp.ToBoolPtr(true),
					DestructiveHint: mcp.ToBoolPtr(false),
					IdempotentHint:  mcp.ToBoolPtr(true),
					OpenWorldHint:   mcp.ToBoolPtr(false),
				},
			}
			s.RegisterTool(aliasTool, handler)
		}
	}
}
