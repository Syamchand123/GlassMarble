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

func geminiServer(t *testing.T, responseBody string, onRequest func(r *http.Request, body []byte)) *httptest.Server {
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

func TestGeminiComplete(t *testing.T) {
	var gotPath string
	var gotKey string
	var gotBody []byte

	srv := geminiServer(t, `{
		"candidates": [{
			"content": {
				"parts": [
					{"text": "I will check the graph."},
					{"functionCall": {"name": "akg_get_node", "args": {"id": "src/db.go::DBStore"}}}
				]
			}
		}],
		"usageMetadata": {"promptTokenCount": 20, "candidatesTokenCount": 8, "totalTokenCount": 28}
	}`, func(r *http.Request, body []byte) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-goog-api-key")
		gotBody = body
	})
	defer srv.Close()

	p := NewGeminiProvider("gem-key", srv.URL, 30*time.Second)
	resp, err := p.Complete(context.Background(), Request{
		Model:  "gemini-2.5-flash",
		System: "You are an architect.",
		Messages: []Message{
			{Role: RoleUser, Content: "Explain this node"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_0", Name: "akg_get_node", Arguments: `{"id":"src/db.go::DBStore"}`}}},
			{Role: RoleTool, ToolResults: []ToolResult{{ID: "call_0", Name: "akg_get_node", Content: `{"kind":"STRUCT"}`}}},
		},
		Tools: []Tool{{Name: "akg_get_node", Description: "Get a node", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}

	if gotPath != "/models/gemini-2.5-flash:generateContent" {
		t.Errorf("path = %q", gotPath)
	}
	if gotKey != "gem-key" {
		t.Errorf("x-goog-api-key = %q", gotKey)
	}

	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	si := payload["systemInstruction"].(map[string]any)
	siParts := si["parts"].([]any)
	if siParts[0].(map[string]any)["text"] != "You are an architect." {
		t.Errorf("systemInstruction = %v", si)
	}

	contents, _ := payload["contents"].([]any)
	if len(contents) != 3 {
		t.Fatalf("contents = %d, want 3", len(contents))
	}
	// user
	u := contents[0].(map[string]any)
	if u["role"] != "user" {
		t.Errorf("first role = %v", u["role"])
	}
	// assistant -> model with functionCall
	a := contents[1].(map[string]any)
	if a["role"] != "model" {
		t.Errorf("assistant role = %v, want model", a["role"])
	}
	aParts := a["parts"].([]any)
	fc := aParts[0].(map[string]any)["functionCall"].(map[string]any)
	if fc["name"] != "akg_get_node" {
		t.Errorf("functionCall = %v", fc)
	}
	// tool result -> user with functionResponse
	tr := contents[2].(map[string]any)
	if tr["role"] != "user" {
		t.Errorf("tool result role = %v, want user", tr["role"])
	}
	trParts := tr["parts"].([]any)
	fr := trParts[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr["name"] != "akg_get_node" {
		t.Errorf("functionResponse name = %v", fr["name"])
	}
	if fr["response"].(map[string]any)["kind"] != "STRUCT" {
		t.Errorf("functionResponse response = %v", fr["response"])
	}

	// tools
	tools, _ := payload["tools"].([]any)
	fns := tools[0].(map[string]any)["functionDeclarations"].([]any)
	fn := fns[0].(map[string]any)
	if fn["name"] != "akg_get_node" {
		t.Errorf("functionDeclaration = %v", fn)
	}

	// response mapping
	if resp.Text != "I will check the graph." {
		t.Errorf("text = %q", resp.Text)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_0" || tc.Name != "akg_get_node" {
		t.Errorf("tool call = %+v", tc)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		t.Fatalf("arguments not JSON: %q", tc.Arguments)
	}
	if args["id"] != "src/db.go::DBStore" {
		t.Errorf("arguments = %q", tc.Arguments)
	}
	if resp.Usage.PromptTokens != 20 || resp.Usage.CompletionTokens != 8 || resp.Usage.TotalTokens != 28 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestGeminiConsecutiveToolResultsMerged(t *testing.T) {
	var gotBody []byte
	srv := geminiServer(t, `{"candidates":[{"content":{"parts":[{"text":"done"}]}}],"usageMetadata":{}}`, func(r *http.Request, body []byte) {
		gotBody = body
	})
	defer srv.Close()

	p := NewGeminiProvider("", srv.URL, 30*time.Second)
	_, err := p.Complete(context.Background(), Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleTool, ToolResults: []ToolResult{{ID: "call_0", Name: "a", Content: `{"x":1}`}}},
			{Role: RoleTool, ToolResults: []ToolResult{{ID: "call_1", Name: "b", Content: `{"y":2}`}}},
		},
	})
	if err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}

	var payload map[string]any
	_ = json.Unmarshal(gotBody, &payload)
	contents, _ := payload["contents"].([]any)
	if len(contents) != 1 {
		t.Fatalf("contents = %d, want 1 merged user block", len(contents))
	}
	parts := contents[0].(map[string]any)["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want 2 functionResponse parts", len(parts))
	}
}

func TestGeminiToolResultRawText(t *testing.T) {
	var gotBody []byte
	srv := geminiServer(t, `{"candidates":[{"content":{"parts":[{"text":"done"}]}}],"usageMetadata":{}}`, func(r *http.Request, body []byte) {
		gotBody = body
	})
	defer srv.Close()

	p := NewGeminiProvider("", srv.URL, 30*time.Second)
	_, err := p.Complete(context.Background(), Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleTool, ToolResults: []ToolResult{{ID: "call_0", Name: "a", Content: "plain text output"}}},
		},
	})
	if err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}

	var payload map[string]any
	_ = json.Unmarshal(gotBody, &payload)
	parts := payload["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	fr := parts[0].(map[string]any)["functionResponse"].(map[string]any)
	resp := fr["response"].(map[string]any)
	if resp["result"] != "plain text output" {
		t.Errorf("response = %v, want wrapped raw text", resp)
	}
}

func TestGeminiPing(t *testing.T) {
	var gotPath string
	var gotKey string
	var gotBody []byte
	srv := geminiServer(t, `{"candidates":[{"content":{"parts":[{"text":"OK"}]}}],"usageMetadata":{}}`, func(r *http.Request, body []byte) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-goog-api-key")
		gotBody = body
	})
	defer srv.Close()

	p := NewGeminiProvider("gem-key", srv.URL, 30*time.Second)
	if err := p.Ping(context.Background(), "gemini-2.5-flash-lite"); err != nil {
		t.Fatalf("Ping() failed: %v", err)
	}

	if !strings.Contains(gotPath, "gemini-2.5-flash-lite:generateContent") {
		t.Errorf("ping path = %q", gotPath)
	}
	if gotKey != "gem-key" {
		t.Errorf("x-goog-api-key = %q", gotKey)
	}
	var payload map[string]any
	_ = json.Unmarshal(gotBody, &payload)
	gc := payload["generationConfig"].(map[string]any)
	if gc["maxOutputTokens"] != float64(5) {
		t.Errorf("ping maxOutputTokens = %v, want 5", gc["maxOutputTokens"])
	}
}

func TestGeminiHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":{"message":"API key not valid"}}`)
	}))
	defer srv.Close()

	p := NewGeminiProvider("bad", srv.URL, 30*time.Second)
	_, err := p.Complete(context.Background(), Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error for HTTP 403")
	}
	if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "API key not valid") {
		t.Errorf("error = %v", err)
	}
}
