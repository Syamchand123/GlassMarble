package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/product"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server encapsulates the GlassMarble Model Context Protocol (MCP) server.
type Server struct {
	mcpServer *server.MCPServer
	bridge    *Bridge
	cfg       ServerConfig
}

// NewServer creates and initializes an enterprise GlassMarble MCP server instance.
func NewServer(cfg ServerConfig) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid MCP server configuration: %w", err)
	}

	bridge := NewBridge(cfg.RootDir, cfg.MaxJSONMB)

	mcpServer := server.NewMCPServer(
		"GlassMarble Architecture Intelligence",
		product.Version,
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithLogging(),
	)

	s := &Server{
		mcpServer: mcpServer,
		bridge:    bridge,
		cfg:       cfg,
	}

	// Register baseline Phase 1 system tools
	s.registerSystemTools()

	// Register Phase 2 AKG graph & inspect tools
	s.registerAKGTools()
	s.registerInspectTools()

	// Register Phase 3 Impact & Governance tools
	s.registerImpactTools()
	s.registerGovernanceTools()

	return s, nil
}

// MCPServer returns the underlying mcp-go server instance.
func (s *Server) MCPServer() *server.MCPServer {
	return s.mcpServer
}

// Bridge returns the GlassMarble domain bridge instance.
func (s *Server) Bridge() *Bridge {
	return s.bridge
}

// Config returns the active server configuration.
func (s *Server) Config() ServerConfig {
	return s.cfg
}

// RegisterTool registers an MCP tool with standard enterprise middlewares applied.
func (s *Server) RegisterTool(tool mcp.Tool, handler ToolHandlerFunc) {
	wrapped := Chain(handler, StandardMiddlewares(tool.Name, s.cfg.RootDir)...)
	s.mcpServer.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return wrapped(ctx, req)
	})
}

// Serve starts the server listening on the configured transport (stdio or SSE/HTTP).
func (s *Server) Serve(ctx context.Context) error {
	defer s.bridge.Close()

	if s.cfg.Transport == "http" || s.cfg.Transport == "sse" {
		addr := fmt.Sprintf(":%d", s.cfg.Port)
		baseURL := fmt.Sprintf("http://localhost:%d", s.cfg.Port)
		slog.Info("Starting GlassMarble MCP SSE server", "addr", addr, "baseURL", baseURL)
		sseServer := server.NewSSEServer(s.mcpServer, server.WithBaseURL(baseURL))
		return sseServer.Start(addr)
	}

	slog.Info("Starting GlassMarble MCP Stdio server", "rootDir", s.cfg.RootDir)
	return server.ServeStdio(s.mcpServer)
}

// Close gracefully releases server and bridge resources.
func (s *Server) Close() {
	if s.bridge != nil {
		s.bridge.Close()
	}
}

// registerSystemTools registers the core system discovery and status tools.
func (s *Server) registerSystemTools() {
	// 1. gmb_status Tool
	statusTool := mcp.NewTool("gmb_status",
		mcp.WithDescription("Get the active GlassMarble repository status, node/edge/file counts, analysis freshness, and storage health."),
	)
	s.RegisterTool(statusTool, s.handleStatusTool)

	// 2. gmb_server_info Tool
	serverInfoTool := mcp.NewTool("gmb_server_info",
		mcp.WithDescription("Get GlassMarble MCP server metadata, version, active repository root, and supported capabilities."),
	)
	s.RegisterTool(serverInfoTool, s.handleServerInfoTool)
}

// handleStatusTool executes the gmb_status query against the active AKG state.
func (s *Server) handleStatusTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	storageDir := s.bridge.StorageDir()
	jsonPath := s.bridge.AKGStatePath()

	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		out, _ := json.MarshalIndent(map[string]any{
			"initialized":  false,
			"error":        "AKG database not found — run 'gmb analyze' first",
			"storage_dir":  storageDir,
			"generated_at": time.Now().Format(time.RFC3339),
		}, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}

	commitHash, schemaVersion, version, err := akg.StateMetadata(storageDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read AKG metadata: %v — try 'gmb analyze'", err)), nil
	}

	stateInfo, err := os.Stat(jsonPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to stat AKG state: %v", err)), nil
	}

	stats, err := akg.StreamGraphStats(storageDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to scan AKG: %v", err)), nil
	}

	virtualShare := 0.0
	if stats.NodeCount > 0 {
		virtualShare = 100 * float64(stats.VirtualCount) / float64(stats.NodeCount)
	}

	result := map[string]any{
		"initialized":       true,
		"storage_dir":       storageDir,
		"schema_version":    schemaVersion,
		"graph_version":     version,
		"commit_hash":       commitHash,
		"last_analysis":     stateInfo.ModTime().Format(time.RFC3339),
		"nodes":             stats.NodeCount,
		"edges":             stats.Edges,
		"indexed_files":     stats.IndexedFiles,
		"entrypoints":       stats.Entrypoints,
		"virtual_nodes":     stats.VirtualCount,
		"virtual_share_pct": virtualShare,
		"dangling":          stats.Dangling,
		"json_bytes":        stateInfo.Size(),
		"generated_at":      time.Now().Format(time.RFC3339),
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to serialize status: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

// handleServerInfoTool returns runtime server metadata.
func (s *Server) handleServerInfoTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	info := map[string]any{
		"name":         "GlassMarble Architecture Intelligence",
		"version":      product.Version,
		"root_dir":     s.bridge.RootDir(),
		"storage_dir":  s.bridge.StorageDir(),
		"transport":    s.cfg.Transport,
		"has_akg":      s.bridge.HasAKG(),
		"capabilities": []string{"tools", "resources", "prompts", "logging", "sampling"},
		"timestamp":    time.Now().Format(time.RFC3339),
	}

	out, _ := json.MarshalIndent(info, "", "  ")
	return mcp.NewToolResultText(string(out)), nil
}
