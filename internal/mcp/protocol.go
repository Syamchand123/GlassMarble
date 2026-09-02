package mcp

import (
	"encoding/json"
)

// MCP protocol version pinned for GlassMarble (Master Plan §5.3).
// The constant itself lives in server.go (ProtocolVersion = "2024-11-05") to
// avoid duplicate definition; this file provides the complementary JSON-RPC
// and capability types that the transport and registry layers depend on.

// JSON-RPC error codes per spec (Master Plan §10 / Protocol).
const (
	ErrParse          = -32700 // Parse error
	ErrInvalidRequest = -32600 // Invalid Request
	ErrMethodNotFound = -32601 // Method not found
	ErrInvalidParams  = -32602 // Invalid params
	ErrInternal       = -32603 // Internal error
)

// Request is the JSON-RPC 2.0 request envelope used on stdio transport.
// This mirrors the MCP spec and is used by StdioTransport for decoding.
type Request struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

// Response is the JSON-RPC 2.0 response envelope.
type Response struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  any              `json:"result,omitempty"`
	Error   *RPCError        `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Tool describes an MCP tool definition (Master Plan §10 protocol.go).
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolResult is the MCP tool call result envelope.
type ToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Content is a single MCP content block (text only for GlassMarble).
type Content struct {
	Type string `json:"type"` // "text"
	Text string `json:"text,omitempty"`
}

// Resource describes an MCP resource.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// Prompt describes an MCP prompt template.
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument describes a prompt argument.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// ServerInfo describes the MCP server identity.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServerCapabilities advertises MCP capabilities.
type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
}

// ToolsCapability indicates tool support.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability indicates resource support.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability indicates prompt support.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// NewResponse builds a successful JSON-RPC response.
func NewResponse(id *json.RawMessage, result any) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

// NewErrorResponse builds an error JSON-RPC response.
func NewErrorResponse(id *json.RawMessage, code int, message string, data any) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// ErrorToolResult builds a ToolResult with isError=true.
func ErrorToolResult(msg string) *ToolResult {
	return &ToolResult{
		Content: []Content{{Type: "text", Text: msg}},
		IsError: true,
	}
}

// TextToolResult builds a successful text ToolResult.
func TextToolResult(text string) *ToolResult {
	return &ToolResult{
		Content: []Content{{Type: "text", Text: text}},
	}
}
