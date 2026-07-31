// Package tools defines the unified tool registry for the GlassMarble AI
// agent: JSON-Schema declarations plus Go handlers. Tools are grouped into
// four categories (system, akg, code, diagram) so the CLI can restrict the set.
//
// Every tool is read-only with respect to the repository. Structured results
// are wrapped by the dispatcher as {"ok": true, "data": ...}; handlers that
// must reach the model verbatim (source code, diffs) return tools.Raw.
package tools

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/akgbridge"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
)

// Tool categories, used by the --tools CLI flag for restricting the set.
const (
	CategorySystem  = "system"
	CategoryAKG     = "akg"
	CategoryCode    = "code"
	CategoryDiagram = "diagram"
)

// Env carries the runtime environment every tool handler needs.
type Env struct {
	// RootDir is the repository root the agent answers questions about.
	RootDir string
	// Bridge provides lazy access to the AKG snapshot.
	Bridge *akgbridge.Bridge
	// ArtifactDir is where save_artifact writes files; created on demand.
	ArtifactDir string
}

// Handler executes a tool call. The returned value is serialized to JSON for
// the model; Raw values pass through verbatim.
type Handler func(ctx context.Context, env *Env, args map[string]any) (any, error)

// Tool is one agent capability: a JSON-Schema declaration plus a handler.
type Tool struct {
	Name        string
	Description string
	Category    string
	Parameters  map[string]any
	Handler     Handler
}

// Decl converts the tool to the provider wire declaration.
func (t *Tool) Decl() provider.Tool {
	return provider.Tool{Name: t.Name, Description: t.Description, Parameters: t.Parameters}
}

// Raw marks handler output that must reach the model verbatim (code blocks,
// diffs) instead of being embedded in a JSON object.
type Raw string

// Prop describes one JSON-Schema property of a tool's parameters.
type Prop struct {
	Type        string
	Description string
	Enum        []string
	Default     any
	Required    bool
}

// Schema builds a JSON-Schema (draft-07) object from property descriptions.
func Schema(props map[string]Prop) map[string]any {
	properties := make(map[string]any, len(props))
	var required []string
	for name, p := range props {
		spec := map[string]any{"type": p.Type, "description": p.Description}
		if len(p.Enum) > 0 {
			spec["enum"] = p.Enum
		}
		if p.Default != nil {
			spec["default"] = p.Default
		}
		properties[name] = spec
		if p.Required {
			required = append(required, name)
		}
	}
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// All returns the full ordered tool registry.
func All() []Tool {
	return append(systemTools(), append(akgTools(), append(codeTools(), diagramTools()...)...)...)
}

// Select restricts the registry to the requested tool categories and exact
// tool names. An empty restrict list returns every tool. Unknown names
// produce an error listing the available choices.
func Select(all []Tool, restrict []string) ([]Tool, error) {
	if len(restrict) == 0 {
		return all, nil
	}
	want := make(map[string]bool, len(restrict))
	var invalid []string
	for _, r := range restrict {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if r != CategorySystem && r != CategoryAKG && r != CategoryCode && r != CategoryDiagram && !hasName(all, r) {
			invalid = append(invalid, r)
		}
		want[r] = true
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("unknown tools %s — use categories (%s, %s, %s, %s) or names: %s",
			strings.Join(invalid, ", "), CategorySystem, CategoryAKG, CategoryCode, CategoryDiagram, Names(all))
	}
	var out []Tool
	for _, t := range all {
		if want[t.Category] || want[t.Name] {
			out = append(out, t)
		}
	}
	return out, nil
}

// Names returns the tool names, sorted, joined with ", ".
func Names(ts []Tool) string {
	names := make([]string, 0, len(ts))
	for _, t := range ts {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func hasName(ts []Tool, name string) bool {
	for _, t := range ts {
		if t.Name == name {
			return true
		}
	}
	return false
}

// ---- argument helpers ----

// strArg returns the string value of an argument, or def when absent.
func strArg(args map[string]any, name, def string) string {
	if v, ok := args[name]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

// intArg returns the integer value of an argument (JSON number, int, or
// numeric string), clamped to [lo, hi], or def when absent.
func intArg(args map[string]any, name string, def, lo, hi int) int {
	v := def
	if raw, ok := args[name]; ok {
		switch n := raw.(type) {
		case float64:
			v = int(n)
		case int:
			v = n
		case string:
			if p, err := strconv.Atoi(n); err == nil {
				v = p
			}
		}
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// floatArg returns the float value of an argument, clamped to [lo, hi], or
// def when absent.
func floatArg(args map[string]any, name string, def, lo, hi float64) float64 {
	v := def
	if raw, ok := args[name]; ok {
		switch n := raw.(type) {
		case float64:
			v = n
		case int:
			v = float64(n)
		case string:
			if p, err := strconv.ParseFloat(n, 64); err == nil {
				v = p
			}
		}
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// boolArg returns the boolean value of an argument.
func boolArg(args map[string]any, name string, def bool) bool {
	if raw, ok := args[name]; ok {
		switch n := raw.(type) {
		case bool:
			return n
		case string:
			if p, err := strconv.ParseBool(n); err == nil {
				return p
			}
		}
	}
	return def
}
