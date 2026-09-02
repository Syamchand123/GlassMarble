package mcp

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_FullClientLifecycle simulates a real MCP client session lifecycle:
// 1. Server initialization
// 2. Tool discovery & invocation
// 3. Resource discovery & reading
// 4. Prompt discovery & fetching
func TestE2E_FullClientLifecycle(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	ctx := context.Background()

	// 1. Tool Call Verification (gmb_server_info)
	var serverInfoReq mcp.CallToolRequest
	serverInfoReq.Params.Name = "gmb_server_info"
	res, err := srv.handleServerInfoTool(ctx, serverInfoReq)
	require.NoError(t, err)
	require.False(t, res.IsError)
	assert.NotEmpty(t, res.Content)

	// 2. Tool Call Verification (gmb_status)
	var statusReq mcp.CallToolRequest
	statusReq.Params.Name = "gmb_status"
	statusRes, err := srv.handleStatusTool(ctx, statusReq)
	require.NoError(t, err)
	require.False(t, statusRes.IsError)

	// 3. Tool Call Verification (gmb_inspect_search)
	var searchReq mcp.CallToolRequest
	searchReq.Params.Name = "gmb_inspect_search"
	searchReq.Params.Arguments = map[string]any{"query": "Execute"}
	searchRes, err := srv.handleInspectSearchTool(ctx, searchReq)
	require.NoError(t, err)
	require.NotNil(t, searchRes)

	// 4. Tool Call Verification (gmb_inspect_node)
	var nodeReq mcp.CallToolRequest
	nodeReq.Params.Name = "gmb_inspect_node"
	nodeReq.Params.Arguments = map[string]any{"id": "cmd/root.go::Execute"}
	nodeRes, err := srv.handleInspectNodeTool(ctx, nodeReq)
	require.NoError(t, err)
	require.NotNil(t, nodeRes)

	// 5. Tool Call Verification (gmb_impact_analysis)
	var impactReq mcp.CallToolRequest
	impactReq.Params.Name = "gmb_impact_analysis"
	impactReq.Params.Arguments = map[string]any{"target": "cmd/root.go"}
	impactRes, err := srv.handleImpactTool(ctx, impactReq)
	require.NoError(t, err)
	require.NotNil(t, impactRes)

	// 6. Tool Call Verification (gmb_drift_check)
	var driftReq mcp.CallToolRequest
	driftReq.Params.Name = "gmb_drift_check"
	driftRes, err := srv.handleDriftTool(ctx, driftReq)
	require.NoError(t, err)
	require.NotNil(t, driftRes)

	// 7. Tool Call Verification (gmb_list_diagram_types)
	var diagListReq mcp.CallToolRequest
	diagListReq.Params.Name = "gmb_list_diagram_types"
	diagListRes, err := srv.handleListDiagramTypesTool(ctx, diagListReq)
	require.NoError(t, err)
	require.False(t, diagListRes.IsError)

	// 8. Tool Call Verification (gmb_render_diagram)
	var diagRenderReq mcp.CallToolRequest
	diagRenderReq.Params.Name = "gmb_render_diagram"
	diagRenderReq.Params.Arguments = map[string]any{
		"type":   "DEPENDENCY_GRAPH",
		"format": "mermaid",
	}
	diagRenderRes, err := srv.handleRenderDiagramTool(ctx, diagRenderReq)
	require.NoError(t, err)
	require.NotNil(t, diagRenderRes)

	// 9. Resource Read (gmb://status)
	var resReq mcp.ReadResourceRequest
	resReq.Params.URI = "gmb://status"
	resContent, err := srv.handleStatusResource(ctx, resReq)
	require.NoError(t, err)
	require.NotEmpty(t, resContent)

	// 10. Prompt Fetch (gmb_onboarding_guide)
	var promptReq mcp.GetPromptRequest
	promptReq.Params.Name = "gmb_onboarding_guide"
	promptResult, err := srv.handleOnboardingGuidePrompt(ctx, promptReq)
	require.NoError(t, err)
	require.NotNil(t, promptResult)
	assert.NotEmpty(t, promptResult.Messages)
}

// TestE2E_SecurityBoundaries tests parameter boundaries and non-existent queries.
func TestE2E_SecurityBoundaries(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	ctx := context.Background()

	// 1. Missing required argument in gmb_impact_analysis
	var req mcp.CallToolRequest
	req.Params.Name = "gmb_impact_analysis"
	req.Params.Arguments = map[string]any{}
	res, err := srv.handleImpactTool(ctx, req)
	require.NoError(t, err)
	assert.True(t, res.IsError, "Expected error on missing target")

	// 2. Missing required argument in gmb_code_definition
	req.Params.Name = "gmb_code_definition"
	res, err = srv.handleCodeDefinitionTool(ctx, req)
	require.NoError(t, err)
	assert.True(t, res.IsError, "Expected error on missing symbol")

	// 3. Non-existent symbol query
	req.Params.Arguments = map[string]any{"symbol": "non_existent_symbol_123456"}
	res, err = srv.handleCodeDefinitionTool(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, res)
}
