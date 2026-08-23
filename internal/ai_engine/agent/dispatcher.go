package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/tools"
)

// Dispatcher executes model-requested tool calls: it validates arguments
// against the tool's JSON Schema, runs the handler, renders the result for
// the model, and caps result sizes.
type Dispatcher struct {
	Tools          []tools.Tool
	MaxResultBytes int
}

func (d *Dispatcher) maxBytes() int {
	if d.MaxResultBytes <= 0 {
		return 8192
	}
	return d.MaxResultBytes
}

// Dispatch executes every call and returns results correlated by ID.
// Context is checked before each tool execution; if cancelled, remaining
// calls receive an error result and no further handlers run.
func (d *Dispatcher) Dispatch(ctx context.Context, env *tools.Env, calls []provider.ToolCall) []provider.ToolResult {
	out := make([]provider.ToolResult, 0, len(calls))
	for i, call := range calls {
		if ctx.Err() != nil {
			for _, rc := range calls[i:] {
				out = append(out, provider.ToolResult{
					ID:      rc.ID,
					Name:    rc.Name,
					IsError: true,
					Content: wrapError(fmt.Sprintf("tool dispatch cancelled: %v", ctx.Err())),
				})
			}
			break
		}
		out = append(out, d.dispatch(ctx, env, call))
	}
	return out
}

func (d *Dispatcher) dispatch(ctx context.Context, env *tools.Env, call provider.ToolCall) provider.ToolResult {
	tool, ok := d.find(call.Name)
	if !ok {
		return provider.ToolResult{
			ID:      call.ID,
			Name:    call.Name,
			IsError: true,
			Content: wrapError(fmt.Sprintf("unknown tool %q — available tools: %s", call.Name, tools.Names(d.Tools))),
		}
	}
	args, err := parseArgs(call.Arguments)
	if err != nil {
		return errorResult(call, fmt.Errorf("invalid arguments JSON for %s: %v", call.Name, err))
	}
	if err := validateArgs(tool, args); err != nil {
		return errorResult(call, err)
	}
	data, err := tool.Handler(ctx, env, args)
	if err != nil {
		return errorResult(call, fmt.Errorf("%s failed: %v", call.Name, err))
	}
	return provider.ToolResult{ID: call.ID, Name: call.Name, Content: d.render(data)}
}

func (d *Dispatcher) find(name string) (tools.Tool, bool) {
	for _, t := range d.Tools {
		if t.Name == name {
			return t, true
		}
	}
	return tools.Tool{}, false
}

func errorResult(call provider.ToolCall, err error) provider.ToolResult {
	return provider.ToolResult{
		ID:      call.ID,
		Name:    call.Name,
		IsError: true,
		Content: wrapError(err.Error()),
	}
}

func wrapError(msg string) string {
	b, _ := json.Marshal(map[string]any{"ok": false, "error": msg})
	return string(b)
}

func parseArgs(raw string) (map[string]any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
		return nil, err
	}
	if args == nil {
		args = map[string]any{}
	}
	return args, nil
}

// validateArgs checks that every schema-required argument is present. The
// schema's "required" list may be []string (built in-process) or []any
// (decoded from JSON).
func validateArgs(t tools.Tool, args map[string]any) error {
	switch req := t.Parameters["required"].(type) {
	case []string:
		for _, name := range req {
			if _, ok := args[name]; !ok {
				return fmt.Errorf("missing required argument %q for %s", name, t.Name)
			}
		}
	case []any:
		for _, r := range req {
			if name, ok := r.(string); ok {
				if _, ok := args[name]; !ok {
					return fmt.Errorf("missing required argument %q for %s", name, t.Name)
				}
			}
		}
	}
	return nil
}

// render serializes a handler result for the model. Raw output (source code,
// diffs) passes through verbatim; structured data is wrapped as
// {"ok":true,"data":...}. Oversized results are truncated with the flag
// {"ok":true,"truncated":true,"data_preview":...} so the model knows the
// data was cut and can narrow its query.
func (d *Dispatcher) render(data any) string {
	max := d.maxBytes()
	if raw, ok := data.(tools.Raw); ok {
		s := string(raw)
		if len(s) <= max {
			return s
		}
		cut := runeCut(s, max)
		return s[:cut] + fmt.Sprintf("\n…[truncated: showing %d of %d bytes — narrow the query]", cut, len(s))
	}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return wrapError(fmt.Sprintf("cannot serialize tool result: %v", err))
	}
	if len(dataJSON) <= max {
		return fmt.Sprintf(`{"ok":true,"data":%s}`, dataJSON)
	}
	preview := string(dataJSON)
	preview = preview[:runeCut(preview, max)] + "…"
	wrapped, err := json.Marshal(map[string]any{
		"ok":                true,
		"truncated":         true,
		"data_length_bytes": len(dataJSON),
		"data_preview":      preview,
	})
	if err != nil {
		return wrapError("cannot serialize tool result")
	}
	return string(wrapped)
}

// runeCut returns the largest index <= n that lands on a rune boundary.
func runeCut(s string, n int) int {
	if n >= len(s) {
		return len(s)
	}
	for i := n; i > 0; i-- {
		if utf8.RuneStart(s[i]) {
			return i
		}
	}
	return 0
}
