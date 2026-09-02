package mcp

import (
	"context"
	"encoding/json"
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
