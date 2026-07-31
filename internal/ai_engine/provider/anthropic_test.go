package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func anthropicServer(t *testing.T, responseBody string, onRequest func(r *http.Request, body []byte)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if onRequest != nil {
			onRequest(r, body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, responseBody)
	}))
}

func TestAnthropicComplete(t *testing.T) {
	var gotPath string
	var gotKey string
	var gotVersion string
	var gotBody []byte

	srv := anthropicServer(t, `{
		"content": [
			{"type": "text", "text": "Here is the answer."},
			{"type": "tool_use", "id": "toolu_01", "name": "akg_get_node", "input": {"id": "src/db.go::DBStore"}}
		],
		"usage": {"input_tokens": 15, "output_tokens": 9}
	}`, func(r *http.Request, body []byte) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotBody = body
	})
	defer srv.Close()

	p := NewAnthropicProvider("sk-ant-test", srv.URL, 30*time.Second)
	resp, err := p.Complete(context.Background(), Request{
		Model:  "claude-sonnet-4-5",
		System: "You are an architect.",
		Messages: []Message{
			{Role: RoleUser, Content: "Explain this node"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "toolu_00", Name: "akg_get_node", Arguments: `{"id":"src/db.go::DBStore"}`}}},
			{Role: RoleTool, ToolResults: []ToolResult{{ID: "toolu_00", Name: "akg_get_node", Content: `{"kind":"STRUCT"}`}}},
		},
		Tools: []Tool{{Name: "akg_get_node", Description: "Get a node", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}

	if gotPath != "/messages" {
		t.Errorf("path = %q, want /messages", gotPath)
	}
	if gotKey != "sk-ant-test" {
		t.Errorf("x-api-key = %q", gotKey)
	}
	if gotVersion != AnthropicVersion {
		t.Errorf("anthropic-version = %q, want %q", gotVersion, AnthropicVersion)
	}

	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if payload["model"] != "claude-sonnet-4-5" {
		t.Errorf("model = %v", payload["model"])
	}
	if payload["system"] != "You are an architect." {
		t.Errorf("system = %v", payload["system"])
	}
	if payload["max_tokens"] != float64(8192) {
		t.Errorf("max_tokens = %v, want default 8192", payload["max_tokens"])
	}

	msgs, _ := payload["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	// assistant tool_use block
	a := msgs[1].(map[string]any)
	blocks := a["content"].([]any)
	if a["role"] != "assistant" || len(blocks) != 1 {
		t.Fatalf("assistant message = %v", a)
	}
	block := blocks[0].(map[string]any)
	if block["type"] != "tool_use" || block["name"] != "akg_get_node" || block["id"] != "toolu_00" {
		t.Errorf("tool_use block = %v", block)
	}
	if input, ok := block["input"].(map[string]any); !ok || input["id"] != "src/db.go::DBStore" {
		t.Errorf("tool_use input = %v", block["input"])
	}
	// tool_result block
	tr := msgs[2].(map[string]any)
	trBlocks := tr["content"].([]any)
	if tr["role"] != "user" || len(trBlocks) != 1 {
		t.Fatalf("tool result message = %v", tr)
	}
	trBlock := trBlocks[0].(map[string]any)
	if trBlock["type"] != "tool_result" || trBlock["tool_use_id"] != "toolu_00" {
		t.Errorf("tool_result block = %v", trBlock)
	}

	// tools with input_schema
	tools, _ := payload["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %d, want 1", len(tools))
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "akg_get_node" {
		t.Errorf("tool name = %v", tool["name"])
	}
	if _, ok := tool["input_schema"].(map[string]any); !ok {
		t.Errorf("input_schema missing: %v", tool)
	}

	// response mapping
	if resp.Text != "Here is the answer." {
		t.Errorf("text = %q", resp.Text)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "toolu_01" || tc.Name != "akg_get_node" {
		t.Errorf("tool call = %+v", tc)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		t.Fatalf("arguments not JSON: %q", tc.Arguments)
	}
	if args["id"] != "src/db.go::DBStore" {
		t.Errorf("arguments = %q", tc.Arguments)
	}
	if resp.Usage.PromptTokens != 15 || resp.Usage.CompletionTokens != 9 || resp.Usage.TotalTokens != 24 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestAnthropicMaxOutputTokens(t *testing.T) {
	var gotBody []byte
	srv := anthropicServer(t, `{"content":[],"usage":{}}`, func(r *http.Request, body []byte) {
		gotBody = body
	})
	defer srv.Close()

	p := NewAnthropicProvider("k", srv.URL, 30*time.Second)
	if _, err := p.Complete(context.Background(), Request{
		Model:           "m",
		Messages:        []Message{{Role: RoleUser, Content: "hi"}},
		MaxOutputTokens: 256,
	}); err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}

	var payload map[string]any
	_ = json.Unmarshal(gotBody, &payload)
	if payload["max_tokens"] != float64(256) {
		t.Errorf("max_tokens = %v, want 256", payload["max_tokens"])
	}
}

func TestAnthropicPing(t *testing.T) {
	var gotKey string
	var gotBody []byte
	srv := anthropicServer(t, `{"content":[{"type":"text","text":"OK"}],"usage":{}}`, func(r *http.Request, body []byte) {
		gotKey = r.Header.Get("x-api-key")
		gotBody = body
	})
	defer srv.Close()

	p := NewAnthropicProvider("sk-ant-test", srv.URL, 30*time.Second)
	if err := p.Ping(context.Background(), "claude-haiku-4-5"); err != nil {
		t.Fatalf("Ping() failed: %v", err)
	}

	if gotKey != "sk-ant-test" {
		t.Errorf("x-api-key = %q", gotKey)
	}
	var payload map[string]any
	_ = json.Unmarshal(gotBody, &payload)
	if payload["max_tokens"] != float64(5) {
		t.Errorf("ping max_tokens = %v, want 5", payload["max_tokens"])
	}
	if payload["model"] != "claude-haiku-4-5" {
		t.Errorf("ping model = %v", payload["model"])
	}
}

func TestAnthropicToolResultError(t *testing.T) {
	var gotBody []byte
	srv := anthropicServer(t, `{"content":[],"usage":{}}`, func(r *http.Request, body []byte) {
		gotBody = body
	})
	defer srv.Close()

	p := NewAnthropicProvider("k", srv.URL, 30*time.Second)
	_, err := p.Complete(context.Background(), Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleTool, ToolResults: []ToolResult{{ID: "toolu_01", Name: "x", Content: "boom", IsError: true}}},
		},
	})
	if err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}

	var payload map[string]any
	_ = json.Unmarshal(gotBody, &payload)
	msgs, _ := payload["messages"].([]any)
	block := msgs[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if block["is_error"] != true {
		t.Errorf("is_error = %v, want true", block["is_error"])
	}
	if block["content"] != "boom" {
		t.Errorf("content = %v", block["content"])
	}
}
