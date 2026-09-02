package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/mcp"
	"github.com/spf13/cobra"
)

var (
	mcpTransportFlag    string
	mcpPortFlag         int
	mcpConfigClientFlag string
	mcpPrintConfigFlag  bool
	mcpMaxJSONMBFlag    int
	mcpStrictPathsFlag  bool
	mcpAuthTokenFlag    string
	mcpToolsFlag        string
)

var mcpCmd = &cobra.Command{
	Use:     "mcp",
	GroupID: GroupAI.ID,
	Short:   "Start Model Context Protocol (MCP) server for Claude Desktop, Cursor, Zed, Continue",
	Long: `Starts the GlassMarble Model Context Protocol (MCP) server.
Allows AI clients (Claude Desktop, Cursor, Windsurf, Zed, Continue.dev) to query the Architecture Knowledge Graph,
evaluate blast-radius risk, render architecture diagrams, search developer memory, and inspect point-in-time snapshots.`,
	Example: `  # Start MCP server over standard input/output (Claude Desktop, Cursor)
  gmb mcp

  # Start MCP server over HTTP/SSE on port 8088
  gmb mcp --transport sse --port 8088

  # Print ready-to-paste client configuration JSON for Claude Desktop
  gmb mcp --config-client claude

  # Print client configuration for Cursor / Windsurf
  gmb mcp --config-client cursor

  # Print all client configuration snippets
  gmb mcp --config-client all`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolveDir(cmd)
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("failed to resolve workspace directory: %w", err)
		}

		// Handle client configuration generation flag
		if mcpPrintConfigFlag && mcpConfigClientFlag == "" {
			mcpConfigClientFlag = "all"
		}
		if mcpConfigClientFlag != "" {
			return runMCPClientConfig(absDir, mcpConfigClientFlag, mcpPortFlag, cmd)
		}

		// Server execution configuration
		// AuthToken prefers --auth-token flag over GLASSMARBLE_MCP_TOKEN env var (flag > env).
		authToken := strings.TrimSpace(mcpAuthTokenFlag)
		if authToken == "" {
			authToken = strings.TrimSpace(os.Getenv("GLASSMARBLE_MCP_TOKEN"))
		}
		// ToolsFilter parsed from --tools comma-separated flag (categories or exact tool names).
		var toolsFilter []string
		if strings.TrimSpace(mcpToolsFlag) != "" {
			for _, part := range strings.Split(mcpToolsFlag, ",") {
				p := strings.TrimSpace(part)
				if p != "" {
					toolsFilter = append(toolsFilter, p)
				}
			}
		}
		cfg := mcp.ServerConfig{
			RootDir:     absDir,
			Transport:   mcpTransportFlag,
			Port:        mcpPortFlag,
			MaxJSONMB:   mcpMaxJSONMBFlag,
			AuthToken:   authToken,
			ToolsFilter: toolsFilter,
		}

		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("invalid MCP server configuration: %w", err)
		}

		srv, err := mcp.NewServer(cfg)
		if err != nil {
			return fmt.Errorf("failed to initialize MCP server: %w", err)
		}
		defer srv.Close()

		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		return srv.Serve(ctx)
	},
}

func runMCPClientConfig(absDir, client string, port int, cmd *cobra.Command) error {
	configs := mcp.GetClientConfigs(absDir)
	asJSON, _ := cmd.Flags().GetBool("json")

	switch strings.ToLower(client) {
	case "claude", "claude_desktop", "claude-desktop":
		out, _ := json.MarshalIndent(configs.ClaudeDesktop, "", "  ")
		if asJSON {
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "// Paste into Claude Desktop config (claude_desktop_config.json):\n%s\n", string(out))
		}
	case "cursor", "windsurf":
		out, _ := json.MarshalIndent(configs.Cursor, "", "  ")
		if asJSON {
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "// Paste into Cursor / Windsurf MCP configuration:\n%s\n", string(out))
		}
	case "zed":
		out, _ := json.MarshalIndent(configs.Zed, "", "  ")
		if asJSON {
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "// Paste into Zed settings.json (context_servers):\n%s\n", string(out))
		}
	case "continue", "continue.dev":
		out, _ := json.MarshalIndent(configs.ContinueDev, "", "  ")
		if asJSON {
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "// Paste into Continue.dev config.json (experimental.modelContextProtocolServers):\n%s\n", string(out))
		}
	case "all":
		if asJSON {
			out, _ := json.MarshalIndent(configs, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), mcp.FormatClientConfigs(absDir))
		}
	default:
		return fmt.Errorf("unknown client %q — supported clients: claude, cursor, zed, continue, all", client)
	}

	return nil
}

func init() {
	mcpCmd.Flags().StringVar(&mcpTransportFlag, "transport", "stdio", "MCP transport protocol: stdio (default), http, or sse")
	mcpCmd.Flags().IntVar(&mcpPortFlag, "port", 8088, "Port for http/sse transport")
	mcpCmd.Flags().StringVar(&mcpConfigClientFlag, "config-client", "", "Generate ready-to-paste client configuration (claude, cursor, zed, continue, all)")
	mcpCmd.Flags().BoolVar(&mcpPrintConfigFlag, "print-config", false, "Print ready-to-paste client configuration JSON snippets")
	mcpCmd.Flags().IntVar(&mcpMaxJSONMBFlag, "max-json-mb", 256, "Maximum AKG JSON payload size budget in megabytes")
	mcpCmd.Flags().BoolVar(&mcpStrictPathsFlag, "strict-paths", true, "Enforce strict workspace root boundaries for file operations")
	mcpCmd.Flags().Bool("json", false, "Emit output as JSON")
	mcpCmd.Flags().StringVar(&mcpAuthTokenFlag, "auth-token", "", "Bearer token for HTTP/SSE transport (or GLASSMARBLE_MCP_TOKEN)")
	mcpCmd.Flags().StringVar(&mcpToolsFlag, "tools", "", "Comma-separated tool filter: categories (system,akg,code,diagram) or exact tool names, e.g. --tools akg,impact")

	rootCmd.AddCommand(mcpCmd)
}
