package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("glassmarble/mcp")

// Enterprise hardening constants (Section 5 & 6).
const (
	// maxPayloadBytes is the token-budget guardrail (Section 5.5).
	// Responses exceeding this are truncated with a pagination hint.
	maxPayloadBytes = 50 * 1024 // 50 KiB

	// maxFileBytes is the per-file read guard for code tools (Section 6.4).
	// Mirrors internal/ai_engine/tools/code_tools.go maxFileBytes (1 MiB).
	maxFileBytes = 1 << 20

	// maxStringArgLen is the general string argument length limit (Section 8).
	maxStringArgLen = 1000

	// maxIDArgLen is the tighter limit for identifier-like arguments (Section 8).
	maxIDArgLen = 500
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
// All logs are directed to os.Stderr to avoid corrupting stdio JSON-RPC streams (Section 7.1).
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

// TracingMiddleware provides REAL OpenTelemetry tracing integration (Section 7.2).
// It always creates a span using the global TracerProvider. If no provider is configured,
// otel's global tracer is a no-op (zero overhead), so no env-var check is needed.
// The span records tool name, lifecycle events, and error status.
func TracingMiddleware(toolName string) Middleware {
	return func(next ToolHandlerFunc) ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx, span := tracer.Start(ctx, "mcp.tool."+toolName, trace.WithAttributes(attribute.String("mcp.tool.name", toolName)))
			defer span.End()

			span.AddEvent("mcp.tool.start", trace.WithAttributes(attribute.String("mcp.tool.name", toolName)))

			res, err := next(ctx, req)

			isError := err != nil || (res != nil && res.IsError)
			span.SetAttributes(attribute.Bool("mcp.tool.is_error", isError))

			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				span.AddEvent("mcp.tool.error", trace.WithAttributes(attribute.String("error", err.Error())))
			} else if res != nil && res.IsError {
				// Extract error text from result content for span status.
				msg := "tool returned IsError=true"
				if len(res.Content) > 0 {
					if tc, ok := res.Content[0].(mcp.TextContent); ok && tc.Text != "" {
						// Truncate to avoid oversized span attribute.
						if len(tc.Text) > 500 {
							msg = tc.Text[:500]
						} else {
							msg = tc.Text
						}
					}
				}
				span.RecordError(fmt.Errorf("%s", msg))
				span.SetStatus(codes.Error, msg)
				span.AddEvent("mcp.tool.error", trace.WithAttributes(attribute.String("error", msg)))
			} else {
				span.SetStatus(codes.Ok, "")
			}

			span.AddEvent("mcp.tool.completed", trace.WithAttributes(
				attribute.String("mcp.tool.name", toolName),
				attribute.Bool("mcp.tool.is_error", isError),
			))

			return res, err
		}
	}
}

// PayloadCompactionMiddleware implements Response Compaction & Truncation Guardrails (Section 5.5).
// If a tool result text exceeds maxPayloadBytes (50 KiB), it truncates and appends a pagination hint.
// This prevents context bloat and token budget exhaustion in the host LLM.
func PayloadCompactionMiddleware(toolName string) Middleware {
	return func(next ToolHandlerFunc) ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			res, err := next(ctx, req)
			if err != nil || res == nil || len(res.Content) == 0 {
				return res, err
			}
			// Only compact text contents; binary/resources are not handled here.
			for i, c := range res.Content {
				if tc, ok := c.(mcp.TextContent); ok {
					if len(tc.Text) > maxPayloadBytes {
						slog.Warn("MCP payload truncated (token guardrail)",
							"tool", toolName,
							"original_bytes", len(tc.Text),
							"max_bytes", maxPayloadBytes,
						)
						tc.Text = TruncatePayload(tc.Text, maxPayloadBytes)
						res.Content[i] = tc
					}
				}
			}
			return res, err
		}
	}
}

// TruncatePayload truncates text to maxBytes with a warning footer.
// Exported for direct use in handlers that build large JSON responses without middleware.
func TruncatePayload(text string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = maxPayloadBytes
	}
	if len(text) <= maxBytes {
		return text
	}
	truncated := text[:maxBytes]
	warning := fmt.Sprintf("\n\n[TRUNCATED: output exceeded %d bytes (%d bytes total). Use limit/offset pagination or narrow scope to retrieve remaining data.]", maxBytes, len(text))
	return truncated + warning
}

// validateArgLengths enforces 1000-char general limit and 500-char ID limit (Section 8 checklist).
// Keys containing "id", "symbol", "target" are subject to maxIDArgLen; all other string args to maxStringArgLen.
// Also keys like "query", "name", "path" fall under the general 1000 limit.
func validateArgLengths(args map[string]any) error {
	for key, val := range args {
		str, ok := val.(string)
		if !ok {
			continue
		}
		lowerKey := strings.ToLower(key)
		isIDLike := strings.Contains(lowerKey, "id") || strings.Contains(lowerKey, "symbol") || strings.Contains(lowerKey, "target")
		if isIDLike {
			if len(str) > maxIDArgLen {
				return fmt.Errorf("input too long: argument %q exceeds %d chars (got %d)", key, maxIDArgLen, len(str))
			}
		}
		if len(str) > maxStringArgLen {
			return fmt.Errorf("input too long: argument %q exceeds %d chars (got %d)", key, maxStringArgLen, len(str))
		}
	}
	return nil
}

// ArgLengthGuardMiddleware validates string argument lengths before handler execution.
// It returns mcp.NewToolResultError with actionable message if violated.
func ArgLengthGuardMiddleware() Middleware {
	return func(next ToolHandlerFunc) ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if req.Params.Arguments != nil {
				if argsMap, ok := req.Params.Arguments.(map[string]any); ok {
					if err := validateArgLengths(argsMap); err != nil {
						// Extract key and limit for precise error formatting per spec.
						// The error already contains the formatted message; we propagate it.
						return mcp.NewToolResultError(err.Error()), nil
					}
				}
			}
			return next(ctx, req)
		}
	}
}

// pathArgKeys lists every tool argument that may carry a filesystem path
// (Section 6.3). Shared by SecurityGuardMiddleware and the roots-aware guard
// in server.go so the two lists cannot drift.
var pathArgKeys = []string{"path", "file", "target_file", "rules_file", "config_file", "scope_path", "filename", "target", "scopePath", "scope"}

// SecurityGuardMiddleware validates that file path arguments remain within the repository root
// and are not blocked secrets (Section 6.3, 6.6 Zero-Trust Validation).
func SecurityGuardMiddleware(rootDir string) Middleware {
	return func(next ToolHandlerFunc) ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			absRoot, err := filepath.Abs(rootDir)
			if err != nil {
				absRoot = rootDir
			}

			pathKeys := pathArgKeys
			args := req.Params.Arguments

			if argsMap, ok := args.(map[string]any); ok {
				for _, key := range pathKeys {
					if val, found := argsMap[key]; found {
						if pathStr, isStr := val.(string); isStr && strings.TrimSpace(pathStr) != "" {
							// Skip symbol IDs that contain "::" – they are not file paths.
							if strings.Contains(pathStr, "::") {
								continue
							}
							// "global" and similar scope keywords are not paths.
							if pathStr == "global" || pathStr == "summary" {
								continue
							}
							// Strip "folder:" / "file:" prefixes used by diagram scope.
							checkPath := pathStr
							if strings.HasPrefix(checkPath, "folder:") {
								checkPath = strings.TrimPrefix(checkPath, "folder:")
							} else if strings.HasPrefix(checkPath, "file:") {
								checkPath = strings.TrimPrefix(checkPath, "file:")
							}
							if strings.TrimSpace(checkPath) == "" {
								continue
							}
							if err := validateSafePath(absRoot, checkPath); err != nil {
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

// isBlockedPath checks whether the path matches blacklisted secrets patterns
// (Section 6, Threat 5: Information / Secret Leakage).
// Blocked: .env*, *.pem, *.key, id_rsa, id_dsa, id_ecdsa, id_ed25519, *.credentials, *.p12, *.pfx,
// .git/*, secrets.yaml, secrets.yml, credentials.json, credentials.yaml
func isBlockedPath(targetPath string) bool {
	clean := filepath.ToSlash(filepath.Clean(targetPath))
	base := strings.ToLower(filepath.Base(clean))
	lowerPath := strings.ToLower(clean)

	// Check base filename patterns.
	if strings.HasPrefix(base, ".env") {
		return true
	}
	if strings.HasSuffix(base, ".pem") {
		return true
	}
	if strings.HasSuffix(base, ".key") {
		return true
	}
	if strings.HasSuffix(base, ".credentials") {
		return true
	}
	if strings.HasSuffix(base, ".p12") || strings.HasSuffix(base, ".pfx") {
		return true
	}
	// Private key filenames.
	if base == "id_rsa" || base == "id_dsa" || base == "id_ecdsa" || base == "id_ed25519" {
		return true
	}
	// Additional secrets files per Threat 5 hardening.
	if base == "secrets.yaml" || base == "secrets.yml" {
		return true
	}
	if base == "credentials.json" || base == "credentials.yaml" || base == "credentials.yml" {
		return true
	}
	// Block .git directory and any file inside it (e.g. .git/config, a/.git/objects)
	if lowerPath == ".git" || strings.HasPrefix(lowerPath, ".git/") || strings.Contains(lowerPath, "/.git/") || strings.HasSuffix(lowerPath, "/.git") {
		return true
	}
	// Also block any path segment that equals a secrets pattern (e.g. a/.env.local)
	for _, seg := range strings.Split(lowerPath, "/") {
		if strings.HasPrefix(seg, ".env") {
			return true
		}
		if seg == "id_rsa" || seg == "id_dsa" || seg == "id_ecdsa" || seg == "id_ed25519" {
			return true
		}
		if seg == ".git" {
			return true
		}
		if seg == "secrets.yaml" || seg == "secrets.yml" {
			return true
		}
		if seg == "credentials.json" || seg == "credentials.yaml" || seg == "credentials.yml" {
			return true
		}
	}
	return false
}

// validateSafePath checks that targetPath is within rootDir or relative without escaping
// and is not a blocked secrets file (Section 6.3 & 6.6).
func validateSafePath(rootDir, targetPath string) error {
	// 1. Block secrets file patterns before any other checks (OWASP Secret Leakage).
	if isBlockedPath(targetPath) {
		return fmt.Errorf("path %q is blocked (secrets file pattern: .env*, *.pem, *.key, id_rsa, *.credentials, *.p12)", targetPath)
	}

	cleanPath := filepath.Clean(targetPath)

	// 2. Block Unix-style absolute paths on Windows (e.g. "/etc/passwd") which
	// filepath.IsAbs reports as non-absolute on Windows but are still absolute escapes.
	if strings.HasPrefix(cleanPath, "/") || strings.HasPrefix(cleanPath, "\\") {
		return fmt.Errorf("path %q is absolute and outside repository root", targetPath)
	}

	// 3. Disallow raw absolute paths pointing outside rootDir
	if filepath.IsAbs(cleanPath) {
		rel, err := filepath.Rel(rootDir, cleanPath)
		if err != nil || strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, string(filepath.Separator)) {
			return fmt.Errorf("path %q is outside repository root", targetPath)
		}
		// Even absolute paths inside root must not be secrets.
		if isBlockedPath(rel) {
			return fmt.Errorf("path %q is blocked (secrets file pattern)", targetPath)
		}
		return nil
	}

	// 4. For relative paths, verify cleaning does not start with ".."
	if strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) || cleanPath == ".." {
		return fmt.Errorf("path %q escapes repository root via traversal", targetPath)
	}

	full := filepath.Join(rootDir, cleanPath)
	rel, err := filepath.Rel(rootDir, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("resolved path %q escapes repository root", targetPath)
	}

	// 5. Double-check resolved relative path against blocklist (e.g. "a/.env").
	if isBlockedPath(rel) {
		return fmt.Errorf("path %q is blocked (secrets file pattern)", targetPath)
	}

	return nil
}

// TimeoutMiddleware bounds every tool invocation with a hard deadline so a
// stuck handler can never hang the client indefinitely (Section 4). The
// handler runs in a goroutine (with its own panic recovery — RecoveryMiddleware
// cannot see across goroutines); on timeout the result is an isError tool
// result and the abandoned handler's context is cancelled.
func TimeoutMiddleware(toolName string, timeout time.Duration) Middleware {
	return func(next ToolHandlerFunc) ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if timeout <= 0 {
				return next(ctx, req)
			}
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			type outcome struct {
				res *mcp.CallToolResult
				err error
			}
			done := make(chan outcome, 1)
			go func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("MCP tool panic recovered (timeout goroutine)", "tool", toolName, "panic", r)
						done <- outcome{mcp.NewToolResultError(fmt.Sprintf("tool execution failed unexpectedly (panic recovered): %v", r)), nil}
					}
				}()
				res, err := next(ctx, req)
				done <- outcome{res, err}
			}()

			select {
			case o := <-done:
				return o.res, o.err
			case <-ctx.Done():
				if ctx.Err() == context.DeadlineExceeded {
					slog.Warn("MCP tool timed out", "tool", toolName, "timeout", timeout)
					return mcp.NewToolResultError(fmt.Sprintf("tool %q timed out after %s — narrow the scope or raise --tool-timeout", toolName, timeout)), nil
				}
				return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
			}
		}
	}
}

// StandardMiddlewares returns the standard enterprise middleware chain for a tool.
// Pipeline order (Section 4.1):
// [Recovery] -> [Timeout] -> [OTel Tracing] -> [ArgLengthGuard] -> [Logger] -> [PayloadCompaction] -> handler
// Recovery is outermost (panic isolation), PayloadCompaction is innermost
// (truncates final output). Path sandboxing runs once, in the roots-aware
// guard applied by RegisterTool — the previous chain validated every path
// twice through two near-identical middlewares.
func StandardMiddlewares(toolName, rootDir string, timeout time.Duration) []Middleware {
	_ = rootDir // retained in the signature for call-site clarity and future per-root policies
	return []Middleware{
		RecoveryMiddleware(toolName),
		TimeoutMiddleware(toolName, timeout),
		TracingMiddleware(toolName),
		ArgLengthGuardMiddleware(),
		LoggingMiddleware(toolName),
		PayloadCompactionMiddleware(toolName),
	}
}

var configureLoggingOnce sync.Once

// configureMCPLogging routes slog strictly to os.Stderr — stdout must carry
// JSON-RPC frames only in stdio mode. Called from Serve(); this used to live
// in an init(), which hijacked the process-global logger for every gmb
// subcommand that merely imported this package.
func configureMCPLogging() {
	configureLoggingOnce.Do(func() {
		logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
		slog.SetDefault(logger)
	})
}
