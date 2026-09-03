package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ServerConfig holds the runtime configuration for the GlassMarble MCP server.
type ServerConfig struct {
	// RootDir is the target repository root directory (default: ".").
	RootDir string `json:"root_dir"`

	// StorageDir is the .glassmarble storage directory. Computed if empty.
	StorageDir string `json:"storage_dir"`

	// Transport is the protocol transport: "stdio" (default), "http"
	// (Streamable HTTP, endpoint /mcp), or "sse" (legacy SSE).
	Transport string `json:"transport"`

	// Host is the HTTP/SSE bind address (default: 127.0.0.1 — never expose
	// the graph to the network without opting in explicitly).
	Host string `json:"host,omitempty"`

	// Port is the HTTP/SSE listening port (default: 8765).
	Port int `json:"port"`

	// AuthToken is an optional Bearer token required for HTTP/SSE access.
	AuthToken string `json:"auth_token,omitempty"`

	// MaxJSONMB is the maximum allowed size (in MiB) for AKG state files (0 = unlimited).
	MaxJSONMB int `json:"max_json_mb,omitempty"`

	// ToolTimeoutSec bounds each tool invocation (default: 60; 0 = default,
	// negative = no timeout).
	ToolTimeoutSec int `json:"tool_timeout_sec,omitempty"`

	// ToolsFilter restricts active tools to specific categories or names.
	ToolsFilter []string `json:"tools_filter,omitempty"`
}

// DefaultToolTimeoutSec is the default per-tool execution deadline.
const DefaultToolTimeoutSec = 60

// DefaultConfig returns the default server configuration.
func DefaultConfig() ServerConfig {
	return ServerConfig{
		RootDir:   ".",
		Transport: "stdio",
		Host:      "127.0.0.1",
		Port:      8765,
	}
}

// ToolTimeout returns the effective per-tool deadline.
func (c ServerConfig) ToolTimeout() time.Duration {
	switch {
	case c.ToolTimeoutSec < 0:
		return 0 // explicitly unbounded
	case c.ToolTimeoutSec == 0:
		return DefaultToolTimeoutSec * time.Second
	default:
		return time.Duration(c.ToolTimeoutSec) * time.Second
	}
}

// Validate normalizes paths and checks configuration sanity.
// It also wires optional Bearer token auth via GLASSMARBLE_MCP_TOKEN env var (Section 9.2):
// if AuthToken is empty, the env var is consulted and adopted if present.
func (c *ServerConfig) Validate() error {
	if c.RootDir == "" {
		c.RootDir = "."
	}
	absRoot, err := filepath.Abs(c.RootDir)
	if err != nil {
		return fmt.Errorf("invalid root directory %q: %w", c.RootDir, err)
	}
	c.RootDir = absRoot

	if c.StorageDir == "" {
		c.StorageDir = filepath.Join(c.RootDir, ".glassmarble")
	} else if !filepath.IsAbs(c.StorageDir) {
		c.StorageDir = filepath.Join(c.RootDir, c.StorageDir)
	}

	c.Transport = strings.ToLower(strings.TrimSpace(c.Transport))
	if c.Transport == "" {
		c.Transport = "stdio"
	}
	if c.Transport != "stdio" && c.Transport != "http" && c.Transport != "sse" {
		return fmt.Errorf("unsupported transport %q (must be 'stdio', 'http', or 'sse')", c.Transport)
	}

	if strings.TrimSpace(c.Host) == "" {
		c.Host = "127.0.0.1"
	}

	if c.Port <= 0 || c.Port > 65535 {
		c.Port = 8765
	}

	// Wire Bearer token from env if not explicitly set (Section 6.5, 9.2).
	// This enables: GLASSMARBLE_MCP_TOKEN=xxx gmb mcp --transport sse
	if strings.TrimSpace(c.AuthToken) == "" {
		if envTok := strings.TrimSpace(os.Getenv("GLASSMARBLE_MCP_TOKEN")); envTok != "" {
			c.AuthToken = envTok
		}
	}
	// Basic token hygiene: warn if token is too short (likely misconfiguration).
	if c.AuthToken != "" && len(c.AuthToken) < 16 {
		return fmt.Errorf("auth token too short (%d chars); expected >=16 chars for GLASSMARBLE_MCP_TOKEN", len(c.AuthToken))
	}

	// Normalize ToolsFilter: trim, lowercase, split commas, dedupe, drop empties.
	// Supports both pre-split slices and raw comma-separated entries.
	var normalized []string
	seen := make(map[string]bool)
	for _, f := range c.ToolsFilter {
		// Allow entries that still contain commas (e.g. from raw flag before split).
		parts := strings.Split(f, ",")
		for _, p := range parts {
			p = strings.TrimSpace(strings.ToLower(p))
			if p == "" {
				continue
			}
			if seen[p] {
				continue
			}
			seen[p] = true
			normalized = append(normalized, p)
		}
	}
	c.ToolsFilter = normalized

	return nil
}

// ClientConfigs contains generated client configuration snippets.
type ClientConfigs struct {
	ClaudeDesktop map[string]any `json:"claude_desktop"`
	Cursor        map[string]any `json:"cursor"`
	Zed           map[string]any `json:"zed"`
	ContinueDev   map[string]any `json:"continue_dev"`
}

// GetClientConfigs generates structured configurations for major AI tools.
func GetClientConfigs(targetDir string) ClientConfigs {
	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		absDir = targetDir
	}

	return ClientConfigs{
		ClaudeDesktop: map[string]any{
			"mcpServers": map[string]any{
				"glassmarble": map[string]any{
					"command": "gmb",
					"args":    []string{"mcp", "--dir", absDir},
				},
			},
		},
		Cursor: map[string]any{
			"mcpServers": map[string]any{
				"glassmarble": map[string]any{
					"command": "gmb",
					"args":    []string{"mcp", "--dir", "${workspaceFolder}"},
				},
			},
		},
		Zed: map[string]any{
			"experimental.model_context_protocol": map[string]any{
				"servers": map[string]any{
					"glassmarble": map[string]any{
						"command": "gmb",
						"args":    []string{"mcp", "--dir", absDir},
					},
				},
			},
		},
		ContinueDev: map[string]any{
			"mcpServers": []map[string]any{
				{
					"name":    "glassmarble",
					"command": "gmb",
					"args":    []string{"mcp", "--dir", absDir},
				},
			},
		},
	}
}

// FormatClientConfigs generates copy-paste ready JSON configurations for major AI tools.
func FormatClientConfigs(targetDir string) string {
	configs := GetClientConfigs(targetDir)

	claudeJSON, _ := json.MarshalIndent(configs.ClaudeDesktop, "", "  ")
	cursorJSON, _ := json.MarshalIndent(configs.Cursor, "", "  ")
	zedJSON, _ := json.MarshalIndent(configs.Zed, "", "  ")
	continueJSON, _ := json.MarshalIndent(configs.ContinueDev, "", "  ")

	var b strings.Builder
	b.WriteString("=================================================================\n")
	b.WriteString(" GlassMarble MCP Client Integration Configurations\n")
	b.WriteString("=================================================================\n\n")

	b.WriteString("1. Claude Desktop (claude_desktop_config.json):\n")
	b.WriteString("   - macOS:   ~/Library/Application Support/Claude/claude_desktop_config.json\n")
	b.WriteString("   - Windows: %APPDATA%\\Claude\\claude_desktop_config.json\n")
	b.WriteString("   - Linux:   ~/.config/Claude/claude_desktop_config.json\n\n")
	b.WriteString(string(claudeJSON))
	b.WriteString("\n\n")

	b.WriteString("2. Cursor / Windsurf (.cursor/mcp.json or settings):\n\n")
	b.WriteString(string(cursorJSON))
	b.WriteString("\n\n")

	b.WriteString("3. Zed Editor (settings.json):\n\n")
	b.WriteString(string(zedJSON))
	b.WriteString("\n\n")

	b.WriteString("4. Continue.dev (~/.continue/config.json):\n\n")
	b.WriteString(string(continueJSON))
	b.WriteString("\n\n")
	b.WriteString("=================================================================\n")

	return b.String()
}
