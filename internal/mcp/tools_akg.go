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
		}

		s.RegisterTool(mcpTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

			result, err := t.Handler(ctx, env, args)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			if raw, ok := result.(tools.Raw); ok {
				return mcp.NewToolResultText(string(raw)), nil
			}

			jsonBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("serialization failed: %v", err)), nil
			}

			return mcp.NewToolResultText(string(jsonBytes)), nil
		})
	}
}
