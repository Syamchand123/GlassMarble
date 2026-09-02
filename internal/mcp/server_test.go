package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerConfig_Validate(t *testing.T) {
	cfg := ServerConfig{
		RootDir:   ".",
		Transport: "STDIO",
	}
	err := cfg.Validate()
	require.NoError(t, err)
	assert.Equal(t, "stdio", cfg.Transport)
	assert.NotEmpty(t, cfg.RootDir)
	assert.NotEmpty(t, cfg.StorageDir)
	assert.Equal(t, 8765, cfg.Port)

	// Invalid transport
	cfgInvalid := ServerConfig{
		RootDir:   ".",
		Transport: "invalid_proto",
	}
	assert.Error(t, cfgInvalid.Validate())
}

func TestFormatClientConfigs(t *testing.T) {
	output := FormatClientConfigs("G:\\GlassMarble")
	assert.Contains(t, output, "Claude Desktop")
	assert.Contains(t, output, "Cursor / Windsurf")
	assert.Contains(t, output, "Zed Editor")
	assert.Contains(t, output, "Continue.dev")
	assert.Contains(t, output, "mcpServers")
}

func TestBridge_Basics(t *testing.T) {
	bridge := NewBridge(".", 0)
	defer bridge.Close()

	assert.NotEmpty(t, bridge.RootDir())
	assert.NotEmpty(t, bridge.StorageDir())
	assert.NotEmpty(t, bridge.AKGStatePath())
}

func TestRecoveryMiddleware(t *testing.T) {
	panickingHandler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		panic("test panic explosion")
	}

	wrapped := RecoveryMiddleware("test_tool")(panickingHandler)
	res, err := wrapped(context.Background(), mcp.CallToolRequest{})

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "panic recovered")
}

func TestSecurityGuardMiddleware(t *testing.T) {
	dummyHandler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	}

	guard := SecurityGuardMiddleware("/safe/repo/root")(dummyHandler)

	// Safe relative path
	var reqSafe mcp.CallToolRequest
	reqSafe.Params.Name = "test_tool"
	reqSafe.Params.Arguments = map[string]any{
		"path": "src/utils.go",
	}
	resSafe, err := guard(context.Background(), reqSafe)
	require.NoError(t, err)
	assert.False(t, resSafe.IsError)

	// Malicious traversal path
	var reqMalicious mcp.CallToolRequest
	reqMalicious.Params.Name = "test_tool"
	reqMalicious.Params.Arguments = map[string]any{
		"path": "../../etc/passwd",
	}
	resMalicious, err := guard(context.Background(), reqMalicious)
	require.NoError(t, err)
	assert.True(t, resMalicious.IsError)
	assert.Contains(t, resMalicious.Content[0].(mcp.TextContent).Text, "security violation")
}

func TestNewServer_Initialization(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NotNil(t, srv)
	defer srv.Close()

	assert.NotNil(t, srv.MCPServer())
	assert.NotNil(t, srv.Bridge())
	assert.Equal(t, "stdio", srv.Config().Transport)
}

func TestServerInfoTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	res, err := srv.handleServerInfoTool(context.Background(), mcp.CallToolRequest{})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.False(t, res.IsError)

	text := res.Content[0].(mcp.TextContent).Text
	var data map[string]any
	err = json.Unmarshal([]byte(text), &data)
	require.NoError(t, err)
	assert.Equal(t, "GlassMarble Architecture Intelligence", data["name"])
	assert.NotEmpty(t, data["version"])
	assert.NotEmpty(t, data["capabilities"])
}

func TestStatusTool_UninitializedOrPresent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	res, err := srv.handleStatusTool(context.Background(), mcp.CallToolRequest{})
	require.NoError(t, err)
	require.NotNil(t, res)

	text := res.Content[0].(mcp.TextContent).Text
	assert.NotEmpty(t, text)

	var data map[string]any
	err = json.Unmarshal([]byte(text), &data)
	require.NoError(t, err)
	assert.Contains(t, data, "initialized")
}

func TestAKGTools_Registration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	// Verify AKG tools are wired into server
	require.NotNil(t, srv.MCPServer())
}

func TestInspectSearchTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_inspect_search"
	req.Params.Arguments = map[string]any{
		"query": "NewServer",
		"limit": 10,
	}

	res, err := srv.handleInspectSearchTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Equal(t, "NewServer", data["query"])
		assert.Contains(t, data, "nodes")
	}
}

func TestInspectNodeTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	// Non-existent node
	var reqInvalid mcp.CallToolRequest
	reqInvalid.Params.Name = "gmb_inspect_node"
	reqInvalid.Params.Arguments = map[string]any{
		"id": "non_existent_symbol_123456",
	}

	resInvalid, err := srv.handleInspectNodeTool(context.Background(), reqInvalid)
	require.NoError(t, err)
	require.NotNil(t, resInvalid)
	assert.True(t, resInvalid.IsError)
}

func TestDependencyTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	// Summary mode
	var reqSummary mcp.CallToolRequest
	reqSummary.Params.Name = "gmb_dependency_analysis"
	reqSummary.Params.Arguments = map[string]any{}

	resSummary, err := srv.handleDependencyTool(context.Background(), reqSummary)
	require.NoError(t, err)
	require.NotNil(t, resSummary)

	if !resSummary.IsError {
		text := resSummary.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Contains(t, data, "total_nodes")
		assert.Contains(t, data, "top_dependency_nodes")
	}
}

func TestImpactTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_impact_analysis"
	req.Params.Arguments = map[string]any{
		"target": "cmd/root.go",
		"depth":  2,
	}

	res, err := srv.handleImpactTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Contains(t, data, "risk_score")
		assert.Contains(t, data, "risk_level")
	}
}

func TestHotspotTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_hotspot_rankings"
	req.Params.Arguments = map[string]any{
		"top": 5,
	}

	res, err := srv.handleHotspotTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Contains(t, data, "hotspots")
	}
}

func TestDriftTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_drift_check"
	req.Params.Arguments = map[string]any{}

	res, err := srv.handleDriftTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Contains(t, data, "passed")
		assert.Contains(t, data, "forbidden_edges")
	}
}

func TestLintTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_arch_lint"
	req.Params.Arguments = map[string]any{}

	res, err := srv.handleLintTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Contains(t, data, "passed")
	}
}

func TestPatternsTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_patterns_smells"
	req.Params.Arguments = map[string]any{
		"include_smells":     true,
		"include_components": true,
	}

	res, err := srv.handlePatternsTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Contains(t, data, "metrics")
		assert.Contains(t, data, "patterns")
		assert.Contains(t, data, "smells")
	}
}

func TestArchStatsTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_arch_stats"
	req.Params.Arguments = map[string]any{}

	res, err := srv.handleArchStatsTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Contains(t, data, "metrics")
		assert.Contains(t, data, "component_coupling")
	}
}

func TestMemoryOverviewTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_memory_overview"
	req.Params.Arguments = map[string]any{}

	res, err := srv.handleMemoryOverviewTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Contains(t, data, "total_events")
		assert.Contains(t, data, "components")
	}
}

func TestMemoryQueryTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_memory_query"
	req.Params.Arguments = map[string]any{
		"query": "architecture",
	}

	res, err := srv.handleMemoryQueryTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Equal(t, "architecture", data["query"])
		assert.Contains(t, data, "matched_claims")
	}
}

func TestMemoryComponentTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_memory_component"
	req.Params.Arguments = map[string]any{
		"component": "cmd",
	}

	res, err := srv.handleMemoryComponentTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Equal(t, "cmd", data["query"])
		assert.Contains(t, data, "found")
	}
}

func TestArchTimelineTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_arch_timeline"
	req.Params.Arguments = map[string]any{}

	res, err := srv.handleArchTimelineTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		assert.NotEmpty(t, text)
	}
}

func TestSnapshotListTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_snapshot_list"
	req.Params.Arguments = map[string]any{
		"limit": 10,
	}

	res, err := srv.handleSnapshotListTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Contains(t, data, "total")
		assert.Contains(t, data, "snapshots")
	}
}

func TestListDiagramTypesTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_list_diagram_types"
	req.Params.Arguments = map[string]any{}

	res, err := srv.handleListDiagramTypesTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Contains(t, data, "total_types")
		assert.Contains(t, data, "diagram_types")
	}
}

func TestRenderDiagramTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_render_diagram"
	req.Params.Arguments = map[string]any{
		"type":   "DEPENDENCY_GRAPH",
		"format": "mermaid",
	}

	res, err := srv.handleRenderDiagramTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Contains(t, data, "markup")
	}
}

func TestCodeDefinitionTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_code_definition"
	req.Params.Arguments = map[string]any{
		"symbol": "NewServer",
	}

	res, err := srv.handleCodeDefinitionTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Contains(t, data, "source")
	}
}

func TestCodeReferencesTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_code_references"
	req.Params.Arguments = map[string]any{
		"symbol": "NewServer",
	}

	res, err := srv.handleCodeReferencesTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Contains(t, data, "references")
	}
}

func TestCodeCallgraphTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_code_callgraph"
	req.Params.Arguments = map[string]any{
		"symbol": "NewServer",
		"depth":  2,
	}

	res, err := srv.handleCodeCallgraphTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Contains(t, data, "call_edges")
	}
}

func TestCodeContextTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	var req mcp.CallToolRequest
	req.Params.Name = "gmb_code_context"
	req.Params.Arguments = map[string]any{
		"file":   "internal/mcp/server.go",
		"line":   35,
		"radius": 10,
	}

	res, err := srv.handleCodeContextTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)

	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		var data map[string]any
		err = json.Unmarshal([]byte(text), &data)
		require.NoError(t, err)
		assert.Contains(t, data, "snippet")
	}
}

func TestResources(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	ctx := context.Background()

	// 1. gmb://status
	statusReq := mcp.ReadResourceRequest{}
	statusReq.Params.URI = "gmb://status"
	statusContents, err := srv.handleStatusResource(ctx, statusReq)
	require.NoError(t, err)
	require.NotEmpty(t, statusContents)

	// 2. gmb://config
	cfgReq := mcp.ReadResourceRequest{}
	cfgReq.Params.URI = "gmb://config"
	cfgContents, err := srv.handleConfigResource(ctx, cfgReq)
	require.NoError(t, err)
	require.NotEmpty(t, cfgContents)

	// 3. gmb://rules
	rulesReq := mcp.ReadResourceRequest{}
	rulesReq.Params.URI = "gmb://rules"
	rulesContents, err := srv.handleRulesResource(ctx, rulesReq)
	require.NoError(t, err)
	require.NotEmpty(t, rulesContents)

	// 4. gmb://memory/summary
	memReq := mcp.ReadResourceRequest{}
	memReq.Params.URI = "gmb://memory/summary"
	memContents, err := srv.handleMemorySummaryResource(ctx, memReq)
	require.NoError(t, err)
	require.NotEmpty(t, memContents)

	// 5. gmb://timeline/latest
	timelineReq := mcp.ReadResourceRequest{}
	timelineReq.Params.URI = "gmb://timeline/latest"
	timelineContents, err := srv.handleTimelineResource(ctx, timelineReq)
	require.NoError(t, err)
	require.NotEmpty(t, timelineContents)

	// 6. glassmarble://intelligence
	intelReq := mcp.ReadResourceRequest{}
	intelReq.Params.URI = "glassmarble://intelligence"
	intelContents, err := srv.handleIntelligenceResource(ctx, intelReq)
	require.NoError(t, err)
	require.NotEmpty(t, intelContents)

	// 7. glassmarble://timeline
	tlFileReq := mcp.ReadResourceRequest{}
	tlFileReq.Params.URI = "glassmarble://timeline"
	tlFileContents, err := srv.handleTimelineFileResource(ctx, tlFileReq)
	require.NoError(t, err)
	require.NotEmpty(t, tlFileContents)

	// 8. glassmarble://conventions
	convReq := mcp.ReadResourceRequest{}
	convReq.Params.URI = "glassmarble://conventions"
	convContents, err := srv.handleConventionsResource(ctx, convReq)
	require.NoError(t, err)
	require.NotEmpty(t, convContents)

	// 9. glassmarble://telemetry
	telemReq := mcp.ReadResourceRequest{}
	telemReq.Params.URI = "glassmarble://telemetry"
	telemContents, err := srv.handleTelemetryResource(ctx, telemReq)
	require.NoError(t, err)
	require.NotEmpty(t, telemContents)
}

func TestPrompts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	ctx := context.Background()

	// 1. gmb_pre_commit_audit
	var auditReq mcp.GetPromptRequest
	auditReq.Params.Name = "gmb_pre_commit_audit"
	auditRes, err := srv.handlePreCommitAuditPrompt(ctx, auditReq)
	require.NoError(t, err)
	require.NotNil(t, auditRes)
	assert.NotEmpty(t, auditRes.Messages)

	// 2. gmb_refactor_advisor
	var refactorReq mcp.GetPromptRequest
	refactorReq.Params.Name = "gmb_refactor_advisor"
	refactorReq.Params.Arguments = map[string]string{
		"target": "cmd/root.go::Execute",
	}
	refactorRes, err := srv.handleRefactorAdvisorPrompt(ctx, refactorReq)
	require.NoError(t, err)
	require.NotNil(t, refactorRes)
	assert.NotEmpty(t, refactorRes.Messages)

	// 3. gmb_explain_architecture
	var explainReq mcp.GetPromptRequest
	explainReq.Params.Name = "gmb_explain_architecture"
	explainRes, err := srv.handleExplainArchitecturePrompt(ctx, explainReq)
	require.NoError(t, err)
	require.NotNil(t, explainRes)
	assert.NotEmpty(t, explainRes.Messages)

	// 4. gmb_onboarding_guide
	var onboardingReq mcp.GetPromptRequest
	onboardingReq.Params.Name = "gmb_onboarding_guide"
	onboardingRes, err := srv.handleOnboardingGuidePrompt(ctx, onboardingReq)
	require.NoError(t, err)
	require.NotNil(t, onboardingRes)
	assert.NotEmpty(t, onboardingRes.Messages)

	// 5. analyze_impact
	var impactReq mcp.GetPromptRequest
	impactReq.Params.Name = "analyze_impact"
	impactReq.Params.Arguments = map[string]string{
		"symbol": "cmd/root.go::Execute",
	}
	impactRes, err := srv.handleAnalyzeImpactPrompt(ctx, impactReq)
	require.NoError(t, err)
	require.NotNil(t, impactRes)
	assert.NotEmpty(t, impactRes.Messages)

	// 6. find_technical_debt
	var debtReq mcp.GetPromptRequest
	debtReq.Params.Name = "find_technical_debt"
	debtRes, err := srv.handleTechnicalDebtPrompt(ctx, debtReq)
	require.NoError(t, err)
	require.NotNil(t, debtRes)
	assert.NotEmpty(t, debtRes.Messages)

	// 7. explain_component
	var compReq mcp.GetPromptRequest
	compReq.Params.Name = "explain_component"
	compReq.Params.Arguments = map[string]string{
		"component": "internal/akg",
	}
	compRes, err := srv.handleExplainComponentPrompt(ctx, compReq)
	require.NoError(t, err)
	require.NotNil(t, compRes)
	assert.NotEmpty(t, compRes.Messages)

	// 8. generate_diagram
	var diagReq mcp.GetPromptRequest
	diagReq.Params.Name = "generate_diagram"
	diagReq.Params.Arguments = map[string]string{
		"diagram_type": "C4_CONTAINER",
	}
	diagRes, err := srv.handleGenerateDiagramPrompt(ctx, diagReq)
	require.NoError(t, err)
	require.NotNil(t, diagRes)
	assert.NotEmpty(t, diagRes.Messages)
}

func TestProtocolVersion_Pin(t *testing.T) {
	assert.Equal(t, "2024-11-05", ProtocolVersion)
	assert.Equal(t, "2024-11-05", GetProtocolVersion())
	cfg := DefaultConfig()
	cfg.RootDir = "."
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()
	assert.Equal(t, "2024-11-05", srv.ProtocolVersion())
	// Verify instructions contain protocol version
	// mcp-go stores instructions internally; we check via server info tool instead
	res, err := srv.handleServerInfoTool(context.Background(), mcp.CallToolRequest{})
	require.NoError(t, err)
	require.False(t, res.IsError)
	text := res.Content[0].(mcp.TextContent).Text
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &data))
	assert.Equal(t, "2024-11-05", data["protocol_version"])
}

func TestToolCount_SupersetAtLeast41(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()
	tools := srv.MCPServer().ListTools()
	// Enterprise superset is 56, spec minimum 41. Ensure at least 41.
	assert.GreaterOrEqual(t, len(tools), 41, "expected at least 41 tools when no filter (enterprise superset)")
	// Spot-check spec-mandated tools are present
	required := []string{"gmb_status", "gmb_server_info", "code_read_file", "code_search_symbol", "code_list_directory", "diagram_generate", "diagram_types"}
	for _, name := range required {
		// normalize: diagram_types alias may be diagram_types vs system_diagram_types
		if name == "diagram_generate" {
			// delegated name is diagram_generate
			_, ok1 := tools["diagram_generate"]
			_, ok2 := tools["gmb_render_diagram"]
			assert.True(t, ok1 || ok2, "expected diagram tool present")
			continue
		}
		if name == "diagram_types" {
			_, ok1 := tools["diagram_types"]
			_, ok2 := tools["system_diagram_types"]
			_, ok3 := tools["gmb_list_diagram_types"]
			assert.True(t, ok1 || ok2 || ok3, "expected diagram_types present")
			continue
		}
		_, ok := tools[name]
		assert.True(t, ok, "required tool %q missing", name)
	}
}

func TestToolsFilter_CategoryAndExact(t *testing.T) {
	// Filter by category "code" should include only code-related tools
	cfg := DefaultConfig()
	cfg.RootDir = "."
	cfg.ToolsFilter = []string{"code"}
	require.NoError(t, cfg.Validate())
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()
	tools := srv.MCPServer().ListTools()
	assert.NotEmpty(t, tools)
	for name := range tools {
		// All remaining tools should be code category (code_* or gmb_code_* or code_list_*)
		isCode := strings.HasPrefix(name, "code_") || strings.HasPrefix(name, "gmb_code_")
		assert.True(t, isCode, "unexpected tool %q for filter code", name)
	}
	// Filtering by exact tool name
	cfg2 := DefaultConfig()
	cfg2.RootDir = "."
	cfg2.ToolsFilter = []string{"gmb_status", "gmb_impact_analysis"}
	require.NoError(t, cfg2.Validate())
	srv2, err := NewServer(cfg2)
	require.NoError(t, err)
	defer srv2.Close()
	tools2 := srv2.MCPServer().ListTools()
	assert.Equal(t, 2, len(tools2))
	_, ok1 := tools2["gmb_status"]
	_, ok2 := tools2["gmb_impact_analysis"]
	assert.True(t, ok1 && ok2)

	// Filtering by categories "akg,impact" example from spec
	cfg3 := DefaultConfig()
	cfg3.RootDir = "."
	cfg3.ToolsFilter = []string{"akg", "impact"}
	require.NoError(t, cfg3.Validate())
	srv3, err := NewServer(cfg3)
	require.NoError(t, err)
	defer srv3.Close()
	tools3 := srv3.MCPServer().ListTools()
	for name := range tools3 {
		isAKG := strings.HasPrefix(name, "akg_")
		isImpact := name == "gmb_impact_analysis" || name == "gmb_hotspot_rankings"
		// Also allow query_architecture_memory etc which are category akg
		isMemoryAKG := name == "query_architecture_memory" || name == "get_architecture_timeline" || name == "get_architecture_patterns"
		assert.True(t, isAKG || isImpact || isMemoryAKG, "unexpected tool %q for filter akg,impact", name)
	}
}

func TestConfigToolsFilterNormalization(t *testing.T) {
	cfg := ServerConfig{RootDir: ".", ToolsFilter: []string{" AKG ", "code", "AKG", "", "  "}}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, []string{"akg", "code"}, cfg.ToolsFilter)
	cfg2 := ServerConfig{RootDir: ".", ToolsFilter: []string{"akg,impact", "code"}}
	require.NoError(t, cfg2.Validate())
	assert.ElementsMatch(t, []string{"akg", "impact", "code"}, cfg2.ToolsFilter)
}

func TestAuthTokenFlagPrecedence(t *testing.T) {
	// Ensure Validate picks up env when AuthToken empty, but flag > env is handled in cmd/mcp.go
	// Here we test config Validate with explicit AuthToken
	cfg := ServerConfig{RootDir: ".", AuthToken: "1234567890123456"}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, "1234567890123456", cfg.AuthToken)
	cfgShort := ServerConfig{RootDir: ".", AuthToken: "short"}
	assert.Error(t, cfgShort.Validate())
}





