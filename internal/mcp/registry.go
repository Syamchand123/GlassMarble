package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// ToolHandler is the registry-level handler signature used by BuildRegistry.
// It mirrors the master-plan registry.go ToolHandler definition and wraps MCP tool execution
// with panic recovery and structured errors before delegating to the underlying transport-agnostic logic.
type ToolHandler func(ctx context.Context, args map[string]any) (*ToolResult, error)

// registeredTool binds a Tool definition to its handler.
type registeredTool struct {
	Tool    Tool
	Handler ToolHandler
}

// Registry is the central MCP registry that wires tools, resources, and prompts
// (Master Plan §10 Implementation Phases / registry.go). It is populated by
// BuildRegistry and consumed by Server when binding to the underlying mcp-go server.
type Registry struct {
	tools          []registeredTool
	resources      []Resource
	prompts        []Prompt
	promptHandlers map[string]func(map[string]string) string
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		promptHandlers: make(map[string]func(map[string]string) string),
	}
}

// RegisterTool adds a tool to the registry.
func (r *Registry) RegisterTool(tool Tool, handler ToolHandler) {
	r.tools = append(r.tools, registeredTool{Tool: tool, Handler: handler})
}

// RegisterResource adds a resource descriptor to the registry.
func (r *Registry) RegisterResource(res Resource) {
	r.resources = append(r.resources, res)
}

// RegisterPrompt adds a prompt descriptor to the registry.
func (r *Registry) RegisterPrompt(prompt Prompt, handler func(map[string]string) string) {
	r.prompts = append(r.prompts, prompt)
	if handler != nil {
		r.promptHandlers[prompt.Name] = handler
	}
}

// Tools returns the list of registered tool definitions.
func (r *Registry) Tools() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, rt := range r.tools {
		out = append(out, rt.Tool)
	}
	return out
}

// Resources returns the list of registered resources.
func (r *Registry) Resources() []Resource {
	return append([]Resource(nil), r.resources...)
}

// Prompts returns the list of registered prompts.
func (r *Registry) Prompts() []Prompt {
	return append([]Prompt(nil), r.prompts...)
}

// CallTool invokes a registered tool by name with the given arguments.
// It wraps panics and returns a structured ToolResult with IsError=true on failure.
func (r *Registry) CallTool(ctx context.Context, name string, args map[string]any) (result *ToolResult, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			result = ErrorToolResult(fmt.Sprintf("tool %q panic recovered: %v", name, rec))
			err = nil
		}
	}()
	for _, rt := range r.tools {
		if rt.Tool.Name == name {
			return rt.Handler(ctx, args)
		}
	}
	return ErrorToolResult(fmt.Sprintf("tool %q not found", name)), nil
}

// BuildRegistry constructs a Registry wired to the given Bridge and server identity.
// This mirrors Master Plan §10 registry.go BuildRegistry which delegates to
// category-specific registration helpers (system, AKG, inspect, impact, etc.).
// In the current flat-layout coexistence model, the Server itself registers
// tools directly via server.go; this registry is the "new layout" counterpart
// that can be used by alternative server bootstraps and tests.
func BuildRegistry(bridge *Bridge, info ServerInfo) *Registry {
	r := NewRegistry()

	// System tools
	r.RegisterTool(Tool{
		Name:        "gmb_status",
		Description: "Get the active GlassMarble repository status, node/edge/file counts, analysis freshness, and storage health.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, func(ctx context.Context, args map[string]any) (*ToolResult, error) {
		storageDir := bridge.StorageDir()
		payload := map[string]any{
			"storage_dir": storageDir,
			"root_dir":    bridge.RootDir(),
			"server":      info.Name,
			"version":     info.Version,
		}
		b, _ := json.MarshalIndent(payload, "", "  ")
		return TextToolResult(string(b)), nil
	})

	r.RegisterTool(Tool{
		Name:        "gmb_server_info",
		Description: "Get GlassMarble MCP server metadata, version, active repository root, and supported capabilities.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, func(ctx context.Context, args map[string]any) (*ToolResult, error) {
		payload := map[string]any{
			"name":             info.Name,
			"version":          info.Version,
			"protocol_version": ProtocolVersion,
			"root_dir":         bridge.RootDir(),
			"storage_dir":      bridge.StorageDir(),
			"has_akg":          bridge.HasAKG(),
		}
		b, _ := json.MarshalIndent(payload, "", "  ")
		return TextToolResult(string(b)), nil
	})

	// Resources — mirror the glassmarble:// set from resources.go
	for _, res := range []Resource{
		{URI: "glassmarble://status", Name: "AKG Status", Description: "Real-time metadata of the active AKG", MimeType: "application/json"},
		{URI: "glassmarble://intelligence", Name: "Architecture Intelligence", Description: "Latest architecture intelligence report", MimeType: "application/json"},
		{URI: "glassmarble://memory", Name: "Developer Memory Overview", Description: "Developer memory summary", MimeType: "application/json"},
		{URI: "glassmarble://timeline", Name: "Architecture Timeline", Description: "Architecture timeline file", MimeType: "application/json"},
		{URI: "glassmarble://conventions", Name: "Learned Project Conventions", Description: "Learned architecture conventions", MimeType: "application/json"},
		{URI: "glassmarble://telemetry", Name: "Pipeline Telemetry", Description: "GlassMarble pipeline performance telemetry", MimeType: "application/json"},
	} {
		r.RegisterResource(res)
	}

	// Prompts — mirror prompts.go builtin set
	for _, p := range []Prompt{
		{Name: "explain_architecture", Description: "Comprehensive architecture explanation"},
		{Name: "analyze_impact", Description: "Blast radius of a proposed change", Arguments: []PromptArgument{{Name: "symbol", Required: true}}},
		{Name: "find_technical_debt", Description: "Identify smells, dead code, God objects"},
		{Name: "explain_component", Description: "Deep dive into a component", Arguments: []PromptArgument{{Name: "component", Required: true}}},
		{Name: "generate_diagram", Description: "Generate a specific diagram type", Arguments: []PromptArgument{{Name: "diagram_type", Required: true}}},
		{Name: "ci_gate_check", Description: "All governance checks for CI"},
	} {
		p := p
		r.RegisterPrompt(p, func(args map[string]string) string {
			return fmt.Sprintf("Prompt %q invoked with args %v", p.Name, args)
		})
	}

	return r
}
