package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func openAICompatServer(t *testing.T, responseBody string, onRequest func(r *http.Request, body []byte)) *httptest.Server {
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

func TestOpenAICompatComplete(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody []byte

	srv := openAICompatServer(t, `{
		"choices": [{
			"message": {
				"content": "The answer is 42.",
				"tool_calls": [{
					"id": "call_abc",
					"type": "function",
					"function": {"name": "akg_search", "arguments": "{\"kind\":\"FUNCTION\"}"}
				}]
			}
		}],
		"usage": {"prompt_tokens": 12, "completion_tokens": 7, "total_tokens": 19}
	}`, func(r *http.Request, body []byte) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotBody = body
	})
	defer srv.Close()

	p := NewOpenAICompatProvider("sk-test", srv.URL, 30*time.Second)
	resp, err := p.Complete(context.Background(), Request{
		Model:  "test-model",
		System: "You are a helper.",
		Messages: []Message{
			{Role: RoleUser, Content: "What is 6*7?"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_1", Name: "akg_search", Arguments: `{"kind":"FUNCTION"}`}}},
			{Role: RoleTool, ToolResults: []ToolResult{{ID: "call_1", Content: `{"nodes":[]}`}}},
		},
		Tools:       []Tool{{Name: "akg_search", Description: "Search the AKG", Parameters: map[string]any{"type": "object"}}},
		Temperature: FloatPtr(0.5),
	})
	if err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}

	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", gotAuth)
	}

	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("request body is not JSON: %v\n%s", err, gotBody)
	}
	if payload["model"] != "test-model" {
		t.Errorf("model = %v", payload["model"])
	}
	if payload["temperature"] != 0.5 {
		t.Errorf("temperature = %v, want 0.5", payload["temperature"])
	}

	msgs, _ := payload["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("messages = %d, want 4 (system + 3)", len(msgs))
	}
	// system
	s := msgs[0].(map[string]any)
	if s["role"] != "system" || s["content"] != "You are a helper." {
		t.Errorf("system message = %v", s)
	}
	// user
	u := msgs[1].(map[string]any)
	if u["role"] != "user" || u["content"] != "What is 6*7?" {
		t.Errorf("user message = %v", u)
	}
	// assistant with tool call
	a := msgs[2].(map[string]any)
	if a["role"] != "assistant" {
		t.Errorf("assistant role = %v", a["role"])
	}
	tcs, _ := a["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("tool_calls = %d, want 1", len(tcs))
	}
	tc := tcs[0].(map[string]any)
	fn := tc["function"].(map[string]any)
	if tc["id"] != "call_1" || fn["name"] != "akg_search" || fn["arguments"] != `{"kind":"FUNCTION"}` {
		t.Errorf("tool call = %v", tc)
	}
	if _, hasContent := a["content"]; hasContent {
		t.Errorf("assistant content should be omitted when tool_calls present")
	}
	// tool result
	tr := msgs[3].(map[string]any)
	if tr["role"] != "tool" || tr["tool_call_id"] != "call_1" || tr["content"] != `{"nodes":[]}` {
		t.Errorf("tool result message = %v", tr)
	}

	// tools serialization
	tools, _ := payload["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %d, want 1", len(tools))
	}
	toolFn := tools[0].(map[string]any)["function"].(map[string]any)
	if toolFn["name"] != "akg_search" {
		t.Errorf("tool function name = %v", toolFn["name"])
	}

	// response mapping
	if resp.Text != "The answer is 42." {
		t.Errorf("text = %q", resp.Text)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_abc" || resp.ToolCalls[0].Name != "akg_search" {
		t.Errorf("tool call = %+v", resp.ToolCalls[0])
	}
	if resp.ToolCalls[0].Arguments != `{"kind":"FUNCTION"}` {
		t.Errorf("arguments = %q", resp.ToolCalls[0].Arguments)
	}
	if resp.Usage.TotalTokens != 19 || resp.Usage.PromptTokens != 12 || resp.Usage.CompletionTokens != 7 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestOpenAICompatNoTools(t *testing.T) {
	var gotBody []byte
	srv := openAICompatServer(t, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`, func(r *http.Request, body []byte) {
		gotBody = body
	})
	defer srv.Close()

	p := NewOpenAICompatProvider("", srv.URL, 30*time.Second)
	if _, err := p.Complete(context.Background(), Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}}); err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if _, hasTools := payload["tools"]; hasTools {
		t.Errorf("tools should be omitted when empty")
	}
	if _, hasTemp := payload["temperature"]; hasTemp {
		t.Errorf("temperature should be omitted when nil")
	}
}

func TestOpenAICompatMaxOutputTokens(t *testing.T) {
	var gotBody []byte
	srv := openAICompatServer(t, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`, func(r *http.Request, body []byte) {
		gotBody = body
	})
	defer srv.Close()

	p := NewOpenAICompatProvider("", srv.URL, 30*time.Second)
	if _, err := p.Complete(context.Background(), Request{
		Model:           "m",
		MaxOutputTokens: 256,
		Messages:        []Message{{Role: RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if payload["max_tokens"] != float64(256) {
		t.Errorf("max_tokens = %v, want 256", payload["max_tokens"])
	}
}

func TestOpenAICompatMaxOutputTokensZero(t *testing.T) {
	var gotBody []byte
	srv := openAICompatServer(t, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`, func(r *http.Request, body []byte) {
		gotBody = body
	})
	defer srv.Close()

	p := NewOpenAICompatProvider("", srv.URL, 30*time.Second)
	if _, err := p.Complete(context.Background(), Request{
		Model:    "m",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if _, has := payload["max_tokens"]; has {
		t.Errorf("max_tokens should be omitted when 0")
	}
}

func TestOpenAICompatPing(t *testing.T) {
	var gotBody []byte
	srv := openAICompatServer(t, `{"choices":[{"message":{"content":"OK"}}],"usage":{}}`, func(r *http.Request, body []byte) {
		gotBody = body
	})
	defer srv.Close()

	p := NewOpenAICompatProvider("sk-test", srv.URL, 30*time.Second)
	if err := p.Ping(context.Background(), "gpt-4o"); err != nil {
		t.Fatalf("Ping() failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("ping body is not JSON: %v", err)
	}
	if payload["max_tokens"] != float64(5) {
		t.Errorf("max_tokens = %v, want 5", payload["max_tokens"])
	}
	msgs, _ := payload["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("ping messages = %d, want 1", len(msgs))
	}
	content := msgs[0].(map[string]any)["content"]
	if !strings.Contains(fmt.Sprint(content), "OK") {
		t.Errorf("ping prompt = %v", content)
	}
}

func TestOpenAICompatPingMissingModel(t *testing.T) {
	srv := openAICompatServer(t, `{}`, nil)
	defer srv.Close()
	p := NewOpenAICompatProvider("", srv.URL, 30*time.Second)
	if err := p.Ping(context.Background(), ""); err == nil {
		t.Error("Ping() with empty model should fail")
	}
}

func TestOpenAICompatEmptyBaseURL(t *testing.T) {
	p := NewOpenAICompatProvider("", "", 30*time.Second)
	if _, err := p.Complete(context.Background(), Request{Model: "m"}); err == nil {
		t.Error("Complete() with empty base URL should fail")
	}
	if err := p.Ping(context.Background(), "m"); err == nil {
		t.Error("Ping() with empty base URL should fail")
	}
}

func TestOpenAICompatHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer srv.Close()

	p := NewOpenAICompatProvider("bad-key", srv.URL, 30*time.Second)
	_, err := p.Complete(context.Background(), Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error for HTTP 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error should include provider detail: %v", err)
	}
}
