package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// ToolHandlerFunc defines the signature for an MCP tool execution handler.
type ToolHandlerFunc func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)

// Middleware wraps a ToolHandlerFunc with cross-cutting concerns.
type Middleware func(next ToolHandlerFunc) ToolHandlerFunc

// Chain combines multiple middlewares into a single handler.
func Chain(h ToolHandlerFunc, middlewares ...Middleware) ToolHandlerFunc {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// RecoveryMiddleware recovers from panics during tool execution and returns an error result.
func RecoveryMiddleware(toolName string) Middleware {
	return func(next ToolHandlerFunc) ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (res *mcp.CallToolResult, err error) {
			defer func() {
				if r := recover(); r != nil {
					stack := string(debug.Stack())
					slog.Error("MCP tool panic recovered",
						"tool", toolName,
						"panic", r,
						"stack", stack,
					)
					res = mcp.NewToolResultError(fmt.Sprintf("tool execution failed unexpectedly (panic recovered): %v", r))
					err = nil
				}
			}()
			return next(ctx, req)
		}
	}
}

// LoggingMiddleware logs tool invocations and execution duration to stderr.
func LoggingMiddleware(toolName string) Middleware {
	return func(next ToolHandlerFunc) ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			start := time.Now()
			slog.Debug("MCP tool started", "tool", toolName)

			res, err := next(ctx, req)
			duration := time.Since(start)

			if err != nil {
				slog.Error("MCP tool returned error",
					"tool", toolName,
					"duration_ms", duration.Milliseconds(),
					"error", err,
				)
			} else if res != nil && res.IsError {
				slog.Warn("MCP tool returned error result",
					"tool", toolName,
					"duration_ms", duration.Milliseconds(),
				)
			} else {
				slog.Info("MCP tool completed successfully",
					"tool", toolName,
					"duration_ms", duration.Milliseconds(),
				)
			}

			return res, err
		}
	}
}

// SecurityGuardMiddleware validates that file path arguments remain within the repository root.
func SecurityGuardMiddleware(rootDir string) Middleware {
	return func(next ToolHandlerFunc) ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			absRoot, err := filepath.Abs(rootDir)
			if err != nil {
				absRoot = rootDir
			}

			pathKeys := []string{"path", "file", "target_file", "rules_file", "scope_path", "filename"}
			args := req.Params.Arguments

			if argsMap, ok := args.(map[string]any); ok {
				for _, key := range pathKeys {
					if val, found := argsMap[key]; found {
						if pathStr, isStr := val.(string); isStr && strings.TrimSpace(pathStr) != "" {
							if err := validateSafePath(absRoot, pathStr); err != nil {
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

// validateSafePath checks that targetPath is within rootDir or relative without escaping.
func validateSafePath(rootDir, targetPath string) error {
	cleanPath := filepath.Clean(targetPath)

	// Disallow raw absolute paths pointing outside rootDir
	if filepath.IsAbs(cleanPath) {
		rel, err := filepath.Rel(rootDir, cleanPath)
		if err != nil || strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, string(filepath.Separator)) {
			return fmt.Errorf("path %q is outside repository root", targetPath)
		}
		return nil
	}

	// For relative paths, verify cleaning does not start with ".."
	if strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) || cleanPath == ".." {
		return fmt.Errorf("path %q escapes repository root via traversal", targetPath)
	}

	full := filepath.Join(rootDir, cleanPath)
	rel, err := filepath.Rel(rootDir, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("resolved path %q escapes repository root", targetPath)
	}

	return nil
}

// StandardMiddlewares returns the standard enterprise middleware chain for a tool.
func StandardMiddlewares(toolName, rootDir string) []Middleware {
	return []Middleware{
		RecoveryMiddleware(toolName),
		LoggingMiddleware(toolName),
		SecurityGuardMiddleware(rootDir),
	}
}

func init() {
	// Configure default slog handler to write strictly to os.Stderr
	// NEVER write non-JSON-RPC logs to os.Stdout in stdio mode.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)
}
