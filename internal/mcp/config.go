package mcp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// ServerConfig holds the runtime configuration for the GlassMarble MCP server.
type ServerConfig struct {
	// RootDir is the target repository root directory (default: ".").
	RootDir string `json:"root_dir"`

	// StorageDir is the .glassmarble storage directory. Computed if empty.
	StorageDir string `json:"storage_dir"`

	// Transport is the protocol transport: "stdio" (default), "http", or "sse".
	Transport string `json:"transport"`

	// Port is the HTTP/SSE listening port (default: 8765).
	Port int `json:"port"`

	// AuthToken is an optional Bearer token required for HTTP/SSE access.
	AuthToken string `json:"auth_token,omitempty"`

	// MaxJSONMB is the maximum allowed size (in MiB) for AKG state files (0 = unlimited).
	MaxJSONMB int `json:"max_json_mb,omitempty"`

	// ReadOnly enforces strict read-only execution across all tools.
	ReadOnly bool `json:"read_only"`

	// ToolsFilter restricts active tools to specific categories or names.
	ToolsFilter []string `json:"tools_filter,omitempty"`
}

// DefaultConfig returns the default server configuration.
func DefaultConfig() ServerConfig {
	return ServerConfig{
		RootDir:   ".",
		Transport: "stdio",
		Port:      8765,
		ReadOnly:  true,
	}
}

// Validate normalizes paths and checks configuration sanity.
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

	if c.Port <= 0 || c.Port > 65535 {
		c.Port = 8765
	}

	return nil
}

// ClientConfigs contains generated client configuration snippets.
type ClientConfigs struct {
	ClaudeDesktop map[string]any `json:"claude_desktop"`
	Cursor        map[string]any `json:"cursor"`
	Zed           map[string]any `json:"zed"`
	ContinueDev   map[string]any `json:"continue_dev"`
}

// FormatClientConfigs generates copy-paste ready JSON configurations for major AI tools.
func FormatClientConfigs(targetDir string) string {
	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		absDir = targetDir
	}

	claudeConfig := map[string]any{
		"mcpServers": map[string]any{
			"glassmarble": map[string]any{
				"command": "gmb",
				"args":    []string{"mcp", "--dir", absDir},
			},
		},
	}

	cursorConfig := map[string]any{
		"mcpServers": map[string]any{
			"glassmarble": map[string]any{
				"command": "gmb",
				"args":    []string{"mcp", "--dir", "${workspaceFolder}"},
			},
		},
	}

	zedConfig := map[string]any{
		"experimental.model_context_protocol": map[string]any{
			"servers": map[string]any{
				"glassmarble": map[string]any{
					"command": "gmb",
					"args":    []string{"mcp", "--dir", absDir},
				},
			},
		},
	}

	continueConfig := map[string]any{
		"mcpServers": []map[string]any{
			{
				"name":    "glassmarble",
				"command": "gmb",
				"args":    []string{"mcp", "--dir", absDir},
			},
		},
	}

	claudeJSON, _ := json.MarshalIndent(claudeConfig, "", "  ")
	cursorJSON, _ := json.MarshalIndent(cursorConfig, "", "  ")
	zedJSON, _ := json.MarshalIndent(zedConfig, "", "  ")
	continueJSON, _ := json.MarshalIndent(continueConfig, "", "  ")

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
