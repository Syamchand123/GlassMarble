// Package provider defines the unified LLM provider abstraction used by the
// GlassMarble AI engine. All providers expose the same Provider interface
// with chat completion, tool calling, and a lightweight connectivity ping.
//
// The provider layer speaks one canonical message/tool model; each adapter
// (openai_compat, anthropic, gemini) translates it to its vendor wire format.
package provider

import (
	"context"
	"time"
)

// Role identifies the sender of a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall is a function invocation requested by the model.
type ToolCall struct {
	// ID is the provider-assigned identifier used to correlate results.
	ID string
	// Name is the tool name the model wants to invoke.
	Name string
	// Arguments is the raw JSON object of arguments as produced by the model.
	Arguments string
}

// ToolResult is the outcome of a tool execution, correlated to a ToolCall via ID.
type ToolResult struct {
	// ID matches the originating ToolCall.ID.
	ID string
	// Name matches the originating ToolCall.Name (required by Gemini).
	Name string
	// Content is the tool output text or serialized JSON.
	Content string
	// IsError marks failed tool executions.
	IsError bool
}

// Message is one entry of the conversation history.
type Message struct {
	Role        Role
	Content     string
	ToolCalls   []ToolCall
	ToolResults []ToolResult
}

// Tool declares a callable tool to the model.
type Tool struct {
	Name        string
	Description string
	// Parameters is a JSON Schema (draft-07) object describing the arguments.
	Parameters map[string]any
}

// Usage reports token consumption for a completion.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Request is a single chat completion invocation.
type Request struct {
	Model    string
	System   string
	Messages []Message
	Tools    []Tool
	// Temperature nil means provider default; non-nil may be 0.0 for
	// deterministic sampling.
	Temperature *float64
	// MaxOutputTokens caps the completion length; 0 means provider default.
	MaxOutputTokens int
	// OnStream receives text deltas as they are produced when the provider
	// supports streaming. When nil the completion is fetched in one shot.
	// The final Response still carries the complete text and usage.
	OnStream func(string)
}

// Response is the result of a chat completion.
type Response struct {
	Text      string
	ToolCalls []ToolCall
	Usage     Usage
}

// Provider is the interface every LLM adapter implements.
type Provider interface {
	// Name returns the adapter name, e.g. "openai_compat", "anthropic", "gemini".
	Name() string
	// Complete runs a single chat completion. It may return tool calls
	// instead of (or in addition to) text.
	Complete(ctx context.Context, req Request) (*Response, error)
	// Ping verifies connectivity and authentication with a minimal completion.
	Ping(ctx context.Context, model string) error
}

// DurationFor returns the HTTP client timeout to use for the given request,
// defaulting to 180s.
func DurationFor(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return 180 * time.Second
	}
	return timeout
}

// FloatPtr is a helper for tests and callers constructing explicit temperature values.
func FloatPtr(v float64) *float64 { return &v }
