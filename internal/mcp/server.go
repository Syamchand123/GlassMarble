package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/product"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ProtocolVersion is the latest MCP protocol revision this server supports.
// The actual version for a session is negotiated during initialize by the SDK
// against mcp.ValidProtocolVersions — this value is informational (the server
// previously claimed a pinned "2024-11-05" while the SDK negotiated newer
// revisions; the two must never diverge again).
var ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION

// SupportedProtocolVersions lists every protocol revision the server accepts
// during initialize negotiation, newest first.
func SupportedProtocolVersions() []string {
	return append([]string(nil), mcp.ValidProtocolVersions...)
}

// GetProtocolVersion returns the latest supported MCP protocol version
// (package-level accessor).
func GetProtocolVersion() string { return ProtocolVersion }

// Server encapsulates the GlassMarble Model Context Protocol (MCP) server.
type Server struct {
	mcpServer      *server.MCPServer
	bridge         *Bridge
	cfg            ServerConfig
	toolsFilterSet map[string]bool

	// rootsMu protects cached client roots for sandboxing (§3.5).
	rootsMu      sync.RWMutex
	cachedRoots  []mcp.Root
	rootsFetched time.Time
}

// ProtocolVersion returns the latest MCP protocol version this server
// instance supports; per-session versions are negotiated by the SDK.
func (s *Server) ProtocolVersion() string { return ProtocolVersion }

// shouldRegister reports whether a tool with given name/category should be registered
// under the current ToolsFilter. Empty filter means register all (enterprise superset = 56 tools,
// spec minimum = 41). Filter entries are lower-cased categories or exact tool names.
func (s *Server) shouldRegister(name, category string) bool {
	if len(s.toolsFilterSet) == 0 {
		return true
	}
	name = strings.ToLower(strings.TrimSpace(name))
	cat := strings.ToLower(strings.TrimSpace(category))
	if s.toolsFilterSet[name] {
		return true
	}
	if cat != "" && s.toolsFilterSet[cat] {
		return true
	}
	return false
}

// NewServer creates and initializes an enterprise GlassMarble MCP server instance.
func NewServer(cfg ServerConfig) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid MCP server configuration: %w", err)
	}

	// cfg.StorageDir was previously computed by Validate and then silently
	// ignored — the bridge recomputed its own. Pass it through.
	bridge := NewBridge(cfg.RootDir, cfg.StorageDir, cfg.MaxJSONMB)

	// Build ToolsFilter set (lower-cased) for shouldRegister checks.
	filterSet := make(map[string]bool, len(cfg.ToolsFilter))
	for _, f := range cfg.ToolsFilter {
		f = strings.ToLower(strings.TrimSpace(f))
		if f != "" {
			filterSet[f] = true
		}
	}

	mcpServer := server.NewMCPServer(
		"GlassMarble Architecture Intelligence",
		product.Version,
		server.WithRecovery(), // panic isolation (Section 4.1)
		server.WithToolCapabilities(true),
		// subscribe=false: the server has no notifications/resources/updated
		// emitters, so advertising the subscribe capability would be a lie —
		// clients would subscribe and never hear back. listChanged stays on
		// (NotifyResourcesChanged is the emitting hook).
		server.WithResourceCapabilities(false, true),
		server.WithPromptCapabilities(true),
		server.WithLogging(),
		server.WithRoots(),
		// Cursor pagination for tools/list, resources/list and prompts/list
		// (the enterprise superset is 56 tools — one page was unbounded).
		server.WithPaginationLimit(50),
		server.WithInstructions(fmt.Sprintf("GlassMarble Architecture Intelligence MCP Server — protocol negotiated per session (latest %s) — Architecture Intelligence Platform. Use tools prefixed gmb_* for governance/impact/memory/snapshot/diagram/code and akg_*/code_*/diagram_* for graph queries.", ProtocolVersion)),
	)
	// Enable sampling capability (§3.2) — allows server to request LLM completions via sampling/createMessage.
	mcpServer.EnableSampling()

	s := &Server{
		mcpServer:      mcpServer,
		bridge:         bridge,
		cfg:            cfg,
		toolsFilterSet: filterSet,
	}

	// Register baseline Phase 1 system tools
	s.registerSystemTools()

	// Register Phase 2 AKG graph & inspect tools
	s.registerAKGTools()
	s.registerInspectTools()

	// Register Phase 3 Impact & Governance tools
	s.registerImpactTools()
	s.registerGovernanceTools()

	// Register Phase 4 Memory & Snapshot tools
	s.registerMemoryTools()
	s.registerSnapshotTools()

	// Register Phase 5 Diagram & Code tools
	s.registerDiagramTools()
	s.registerCodeTools()

	// Register sampling / evidence tools (§3.2)
	s.registerSamplingTools()

	// Register Phase 6 Resources & Prompts
	s.registerResources()
	s.registerPrompts()

	// Register handler for roots/list_changed notifications (§3.5)
	mcpServer.AddNotificationHandler(string(mcp.MethodNotificationRootsListChanged), s.handleRootsListChanged)

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
	// Inner layer: progress (§3.3) and roots-aware sandboxing (§3.5) — close to handler.
	inner := Chain(handler, s.RootsAwareSecurityGuardMiddleware(), s.ProgressMiddleware(tool.Name))
	// Outer layer: standard enterprise middlewares (Recovery outermost, Payload innermost),
	// including a per-request timeout so a stuck handler cannot hang a client forever.
	wrapped := Chain(inner, StandardMiddlewares(tool.Name, s.cfg.RootDir, s.cfg.ToolTimeout())...)
	s.mcpServer.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return wrapped(ctx, req)
	})
}

// Serve starts the server listening on the configured transport.
// All logs are emitted to stderr via slog (Section 7.1) to keep stdio JSON-RPC clean.
// Sampling, Progress, and Cancellation are delegated to mcp-go SDK via context propagation (Section 3).
func (s *Server) Serve(ctx context.Context) error {
	configureMCPLogging()
	defer s.bridge.Close()

	switch s.cfg.Transport {
	case "http", "sse":
		return s.serveHTTP(ctx)
	default:
		return s.serveStdio(ctx)
	}
}

// withBearerAuth enforces Bearer token authentication when a token is
// configured (Section 6.5, 9.2). The comparison is constant-time and failures
// answer 401 with a WWW-Authenticate challenge. With no token configured the
// handler passes through (loopback-only binding is the remaining guard).
func withBearerAuth(next http.Handler, token string) http.Handler {
	if token == "" {
		return next
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		// Length-equalizing hash-free compare: ConstantTimeCompare requires
		// equal lengths, so unequal lengths fail without leaking timing.
		if len(got) != len(want) || subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="glassmarble-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// serveHTTP runs the Streamable HTTP transport (--transport http, endpoint
// /mcp) or the legacy SSE transport (--transport sse, endpoints /sse and
// /message). Previously "http" silently aliased to deprecated SSE and the
// configured Bearer token was logged as "ENABLED" but never enforced.
func (s *Server) serveHTTP(ctx context.Context) error {
	addr := net.JoinHostPort(s.cfg.Host, fmt.Sprintf("%d", s.cfg.Port))

	var handler http.Handler
	if s.cfg.Transport == "sse" {
		baseURL := fmt.Sprintf("http://%s", addr)
		slog.Info("Starting GlassMarble MCP SSE server (legacy transport)", "addr", addr, "baseURL", baseURL)
		handler = server.NewSSEServer(s.mcpServer, server.WithBaseURL(baseURL))
	} else {
		slog.Info("Starting GlassMarble MCP Streamable HTTP server", "addr", addr, "endpoint", "/mcp")
		handler = server.NewStreamableHTTPServer(s.mcpServer)
	}

	if s.cfg.AuthToken != "" {
		slog.Info("MCP Bearer authentication enforced", "addr", addr)
	} else {
		slog.Warn("MCP Bearer authentication disabled — set GLASSMARBLE_MCP_TOKEN to require a token",
			"addr", addr, "bind", s.cfg.Host)
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           withBearerAuth(handler, s.cfg.AuthToken),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		err := httpSrv.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		slog.Info("MCP HTTP server shutting down", "reason", ctx.Err())
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutCtx); err != nil {
			slog.Error("MCP HTTP server shutdown error", "error", err)
			return err
		}
		slog.Info("MCP HTTP server stopped gracefully")
		return nil
	case err := <-errCh:
		return err
	}
}

// serveStdio runs the stdio transport. StdioServer.Listen is context-aware:
// it returns when ctx is cancelled or stdin closes, so shutdown no longer
// races a detached ServeStdio goroutine against a 5s timer while the bridge
// is torn down underneath in-flight handlers.
func (s *Server) serveStdio(ctx context.Context) error {
	slog.Info("Starting GlassMarble MCP Stdio server", "rootDir", s.cfg.RootDir)
	stdio := server.NewStdioServer(s.mcpServer)
	stdio.SetErrorLogger(log.New(os.Stderr, "", log.LstdFlags))
	err := stdio.Listen(ctx, os.Stdin, os.Stdout)
	if err != nil && ctx.Err() != nil {
		// Cancellation-driven shutdown is a clean exit, not a failure.
		slog.Info("MCP Stdio server stopped", "reason", ctx.Err())
		return nil
	}
	return err
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
	if s.shouldRegister("gmb_status", "system") {
		statusTool := mcp.NewTool("gmb_status",
			mcp.WithDescription("Get the active GlassMarble repository status, node/edge/file counts, analysis freshness, and storage health."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_status",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(statusTool, s.handleStatusTool)
	}

	// 2. gmb_server_info Tool
	if s.shouldRegister("gmb_server_info", "system") {
		serverInfoTool := mcp.NewTool("gmb_server_info",
			mcp.WithDescription("Get GlassMarble MCP server metadata, version, active repository root, and supported capabilities."),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_server_info",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(serverInfoTool, s.handleServerInfoTool)
	}
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

	// structuredContent (2025-06-18) plus a text fallback for older clients.
	return mcp.NewToolResultStructured(result, string(out)), nil
}

// handleServerInfoTool returns runtime server metadata.
func (s *Server) handleServerInfoTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	info := map[string]any{
		"name":                        "GlassMarble Architecture Intelligence",
		"version":                     product.Version,
		"protocol_version":            ProtocolVersion,
		"supported_protocol_versions": SupportedProtocolVersions(),
		"root_dir":                    s.bridge.RootDir(),
		"storage_dir":                 s.bridge.StorageDir(),
		"transport":                   s.cfg.Transport,
		"has_akg":                     s.bridge.HasAKG(),
		"capabilities":                []string{"tools", "resources", "prompts", "logging", "sampling", "roots", "progress", "cancellation"},
		"timestamp":                   time.Now().Format(time.RFC3339),
	}

	out, _ := json.MarshalIndent(info, "", "  ")
	return mcp.NewToolResultStructured(info, string(out)), nil
}

// ─────────────────────────────────────────────────────────────────────────────
// §3.5 Real Roots / Sandboxing
// ─────────────────────────────────────────────────────────────────────────────

// getClientRoots attempts to fetch the client-provided roots via roots/list (§3.5).
// Returns nil if no session, not supported, or on error — caller falls back to RootDir.
func (s *Server) getClientRoots(ctx context.Context) []mcp.Root {
	// Fast path: use cached roots if fresh (<30s)
	s.rootsMu.RLock()
	if time.Since(s.rootsFetched) < 30*time.Second && len(s.cachedRoots) > 0 {
		cached := append([]mcp.Root(nil), s.cachedRoots...)
		s.rootsMu.RUnlock()
		return cached
	}
	s.rootsMu.RUnlock()

	result, err := s.mcpServer.RequestRoots(ctx, mcp.ListRootsRequest{})
	if err != nil {
		return nil
	}
	if result == nil || len(result.Roots) == 0 {
		return nil
	}
	s.rootsMu.Lock()
	s.cachedRoots = append([]mcp.Root(nil), result.Roots...)
	s.rootsFetched = time.Now()
	s.rootsMu.Unlock()
	return result.Roots
}

// isPathWithinRoots reports whether targetPath is inside any client root.
// Callers must pass an absolute path (validatePathAgainstRoots resolves
// relative arguments against RootDir first); a relative path cannot be
// compared against file:// roots and is rejected.
func isPathWithinRoots(targetPath string, roots []mcp.Root) bool {
	if len(roots) == 0 {
		return false
	}
	cleanTarget := filepath.Clean(targetPath)
	if !filepath.IsAbs(cleanTarget) {
		return false
	}
	for _, r := range roots {
		uri := r.URI
		// Only handle file:// roots per spec.
		rootPath := uri
		if strings.HasPrefix(uri, "file://") {
			u, err := url.Parse(uri)
			if err == nil {
				rootPath = u.Path
				// Windows file:// URIs may have leading slash: file:///G:/GlassMarble
				if os.PathSeparator == '\\' && strings.HasPrefix(rootPath, "/") && len(rootPath) > 2 && rootPath[2] == ':' {
					rootPath = rootPath[1:]
				}
			} else {
				rootPath = strings.TrimPrefix(uri, "file://")
			}
		}
		rootPath = filepath.Clean(rootPath)
		rel, err := filepath.Rel(rootPath, cleanTarget)
		if err == nil && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			return true
		}
	}
	return false
}

// validatePathAgainstRoots checks targetPath against client roots if available; otherwise falls back to RootDir.
func (s *Server) validatePathAgainstRoots(ctx context.Context, targetPath string) error {
	roots := s.getClientRoots(ctx)
	if len(roots) > 0 {
		// Build absolute path to check against roots.
		absRoot := s.bridge.RootDir()
		cleanPath := filepath.Clean(targetPath)
		var absTarget string
		if filepath.IsAbs(cleanPath) {
			absTarget = cleanPath
		} else {
			absTarget = filepath.Join(absRoot, cleanPath)
		}
		if isPathWithinRoots(absTarget, roots) {
			// Also still enforce secrets blocklist
			if isBlockedPath(targetPath) || isBlockedPath(absTarget) {
				return fmt.Errorf("path %q is blocked (secrets file pattern)", targetPath)
			}
			return nil
		}
		// Also allow if within RootDir and RootDir is within any root (common case)
		if err := validateSafePath(absRoot, targetPath); err == nil {
			// Check if RootDir itself is within roots
			if isPathWithinRoots(absRoot, roots) {
				return nil
			}
		}
		return fmt.Errorf("path %q is outside client-provided roots", targetPath)
	}
	// No roots announced — fall back to RootDir sandboxing
	absRoot, err := filepath.Abs(s.bridge.RootDir())
	if err != nil {
		absRoot = s.bridge.RootDir()
	}
	return validateSafePath(absRoot, targetPath)
}

// handleRootsListChanged handles notifications/roots/list_changed (§3.5) by invalidating cached roots.
func (s *Server) handleRootsListChanged(ctx context.Context, notif mcp.JSONRPCNotification) {
	s.rootsMu.Lock()
	s.cachedRoots = nil
	s.rootsFetched = time.Time{}
	s.rootsMu.Unlock()
	slog.Info("MCP roots list changed — cache invalidated")
	// Proactively re-fetch roots for next validation
	_ = s.getClientRoots(ctx)
}

// RootsAwareSecurityGuardMiddleware validates file paths against client roots when available (§3.5).
func (s *Server) RootsAwareSecurityGuardMiddleware() Middleware {
	return func(next ToolHandlerFunc) ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Only enforce roots for tools that carry path-like arguments.
			// pathArgKeys is shared with middleware.go so the two path guards
			// can never drift apart again.
			pathKeys := pathArgKeys
			args := req.Params.Arguments
			if argsMap, ok := args.(map[string]any); ok {
				for _, key := range pathKeys {
					if val, found := argsMap[key]; found {
						if pathStr, isStr := val.(string); isStr && strings.TrimSpace(pathStr) != "" {
							if strings.Contains(pathStr, "::") {
								continue
							}
							if pathStr == "global" || pathStr == "summary" {
								continue
							}
							checkPath := pathStr
							if strings.HasPrefix(checkPath, "folder:") {
								checkPath = strings.TrimPrefix(checkPath, "folder:")
							} else if strings.HasPrefix(checkPath, "file:") {
								checkPath = strings.TrimPrefix(checkPath, "file:")
							}
							if strings.TrimSpace(checkPath) == "" {
								continue
							}
							if err := s.validatePathAgainstRoots(ctx, checkPath); err != nil {
								// If roots validation says outside roots but path is otherwise safe within RootDir,
								// we still reject to honor roots sandboxing. Log at debug for diagnostics.
								slog.Debug("roots sandbox validation failed", "arg", key, "path", pathStr, "error", err)
								return mcp.NewToolResultError(fmt.Sprintf("security violation on argument %q: %v", key, err)), nil
							}
						}
					}
				}
			}
			return next(ctx, req)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// §3.3 Real Progress Notifications
// ─────────────────────────────────────────────────────────────────────────────

// getProgressToken extracts the progress token from request meta if present.
func getProgressToken(req mcp.CallToolRequest) mcp.ProgressToken {
	if req.Params.Meta != nil {
		return req.Params.Meta.ProgressToken
	}
	return nil
}

// sendProgress sends a notifications/progress notification to the client if a progress token is present.
// Uses the real SDK method SendNotificationToClient (§3.3).
func (s *Server) sendProgress(ctx context.Context, token mcp.ProgressToken, progress, total float64, message string) error {
	if token == nil {
		return nil
	}
	params := map[string]any{
		"progressToken": token,
		"progress":      progress,
		"total":         total,
		"message":       message,
	}
	err := s.mcpServer.SendNotificationToClient(ctx, string(mcp.MethodNotificationProgress), params)
	if err != nil {
		slog.Debug("progress notification send failed", "token", token, "progress", progress, "error", err)
	}
	return err
}

// ProgressMiddleware checks for _meta.progressToken and logs its presence (§3.3).
// Actual progress reporting is done inside long-running handlers via sendProgress.
func (s *Server) ProgressMiddleware(toolName string) Middleware {
	return func(next ToolHandlerFunc) ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			token := getProgressToken(req)
			if token != nil {
				slog.Debug("progress token present", "tool", toolName, "token", token)
			}
			return next(ctx, req)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// §3.6 Real list_changed Notifications
// ─────────────────────────────────────────────────────────────────────────────

// NotifyToolsChanged sends a real notifications/tools/list_changed notification to all clients (§3.6).
func (s *Server) NotifyToolsChanged() {
	s.mcpServer.SendNotificationToAllClients(string(mcp.MethodNotificationToolsListChanged), nil)
	slog.Info("MCP tools list changed notification sent")
}

// NotifyResourcesChanged sends a real notifications/resources/list_changed notification.
func (s *Server) NotifyResourcesChanged() {
	s.mcpServer.SendNotificationToAllClients(string(mcp.MethodNotificationResourcesListChanged), nil)
	slog.Info("MCP resources list changed notification sent")
}

// NotifyPromptsChanged sends a real notifications/prompts/list_changed notification.
func (s *Server) NotifyPromptsChanged() {
	s.mcpServer.SendNotificationToAllClients(string(mcp.MethodNotificationPromptsListChanged), nil)
	slog.Info("MCP prompts list changed notification sent")
}

// ─────────────────────────────────────────────────────────────────────────────
// §3.4 Real Cancellation helper
// ─────────────────────────────────────────────────────────────────────────────

// checkCancellation returns a tool error if context was cancelled (§3.4).
func checkCancellation(ctx context.Context) (*mcp.CallToolResult, bool) {
	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), true
	default:
		return nil, false
	}
}
