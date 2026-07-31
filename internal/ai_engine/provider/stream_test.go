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

// sseServer serves a pre-built SSE body and records the request.
func sseServer(t *testing.T, body string, onRequest func(r *http.Request, raw []byte)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if onRequest != nil {
			onRequest(r, raw)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, body)
	}))
}

func TestOpenAICompatStream(t *testing.T) {
	var gotBody []byte
	srv := sseServer(t, `data: {"choices":[{"delta":{"content":"Hello "}}]}

data: {"choices":[{"delta":{"content":"world"}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"akg_status","arguments":""}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]}}]}

data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":5,"total_tokens":16}}

data: [DONE]

`, func(r *http.Request, raw []byte) { gotBody = raw })
	defer srv.Close()

	p := NewOpenAICompatProvider("sk-test", srv.URL, 30*time.Second)
	var deltas []string
	resp, err := p.Complete(context.Background(), Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		OnStream: func(d string) { deltas = append(deltas, d) },
	})
	if err != nil {
		t.Fatalf("Complete() stream: %v", err)
	}

	if strings.Join(deltas, "") != "Hello world" {
		t.Errorf("deltas = %q, want %q", strings.Join(deltas, ""), "Hello world")
	}
	if resp.Text != "Hello world" {
		t.Errorf("text = %q", resp.Text)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "akg_status" || tc.Arguments != `{}` {
		t.Errorf("tool call = %+v", tc)
	}
	if resp.Usage.TotalTokens != 16 || resp.Usage.PromptTokens != 11 || resp.Usage.CompletionTokens != 5 {
		t.Errorf("usage = %+v", resp.Usage)
	}

	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if payload["stream"] != true {
		t.Errorf("stream = %v, want true", payload["stream"])
	}
}

func TestOpenAICompatStreamFallbackToJSON(t *testing.T) {
	// Endpoints that ignore stream:true (and plain test doubles) get the
	// one-shot JSON path with the text still delivered through OnStream.
	srv := openAICompatServer(t, `{"choices":[{"message":{"content":"Buffered answer."}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`, nil)
	defer srv.Close()

	p := NewOpenAICompatProvider("", srv.URL, 30*time.Second)
	var deltas []string
	resp, err := p.Complete(context.Background(), Request{
		Model:    "m",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		OnStream: func(d string) { deltas = append(deltas, d) },
	})
	if err != nil {
		t.Fatalf("Complete(): %v", err)
	}
	if resp.Text != "Buffered answer." {
		t.Errorf("text = %q", resp.Text)
	}
	if strings.Join(deltas, "") != "Buffered answer." {
		t.Errorf("deltas = %q", strings.Join(deltas, ""))
	}
	if resp.Usage.TotalTokens != 5 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestAnthropicStream(t *testing.T) {
	srv := sseServer(t, `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":9}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","id":"tb1"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"there"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu1","name":"akg_status"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":7}}

event: message_stop
data: {"type":"message_stop"}

`, nil)
	defer srv.Close()

	p := NewAnthropicProvider("sk-test", srv.URL, 30*time.Second)
	var deltas []string
	resp, err := p.Complete(context.Background(), Request{
		Model:    "claude-sonnet-4-5",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		OnStream: func(d string) { deltas = append(deltas, d) },
	})
	if err != nil {
		t.Fatalf("Complete() stream: %v", err)
	}
	if strings.Join(deltas, "") != "Hello there" {
		t.Errorf("deltas = %q", strings.Join(deltas, ""))
	}
	if resp.Text != "Hello there" {
		t.Errorf("text = %q", resp.Text)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "tu1" || resp.ToolCalls[0].Name != "akg_status" || resp.ToolCalls[0].Arguments != `{}` {
		t.Errorf("tool call = %+v", resp.ToolCalls[0])
	}
	if resp.Usage.PromptTokens != 9 || resp.Usage.CompletionTokens != 7 || resp.Usage.TotalTokens != 16 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestGeminiStream(t *testing.T) {
	var gotPath string
	srv := sseServer(t, `data: {"candidates":[{"content":{"parts":[{"text":"Gem"}]}}]}

data: {"candidates":[{"content":{"parts":[{"text":"ini!"}]}}]}

data: {"candidates":[],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3,"totalTokenCount":8}}

`, func(r *http.Request, _ []byte) { gotPath = r.URL.Path })
	defer srv.Close()

	p := NewGeminiProvider("sk-test", srv.URL, 30*time.Second)
	var deltas []string
	resp, err := p.Complete(context.Background(), Request{
		Model:    "gemini-2.5-flash",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		OnStream: func(d string) { deltas = append(deltas, d) },
	})
	if err != nil {
		t.Fatalf("Complete() stream: %v", err)
	}
	if strings.Join(deltas, "") != "Gemini!" {
		t.Errorf("deltas = %q", strings.Join(deltas, ""))
	}
	if resp.Text != "Gemini!" {
		t.Errorf("text = %q", resp.Text)
	}
	if !strings.Contains(gotPath, "streamGenerateContent") {
		t.Errorf("path = %q, want streamGenerateContent", gotPath)
	}
	if resp.Usage.TotalTokens != 8 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestGeminiStreamFunctionCall(t *testing.T) {
	srv := sseServer(t, `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"akg_status","args":{}}}]}}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2,"totalTokenCount":6}}

`, nil)
	defer srv.Close()

	p := NewGeminiProvider("", srv.URL, 30*time.Second)
	resp, err := p.Complete(context.Background(), Request{
		Model:    "gemini-2.5-flash",
		Messages: []Message{{Role: RoleUser, Content: "status"}},
		OnStream: func(string) {},
	})
	if err != nil {
		t.Fatalf("Complete(): %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "akg_status" {
		t.Errorf("tool calls = %+v", resp.ToolCalls)
	}
}

func TestSSEScanner(t *testing.T) {
	var got []string
	scanSSE(strings.NewReader(": keep-alive\n\nevent: ping\ndata: {\"a\": 1}\n\ndata: line1\ndata: line2\n\n"), func(event, data string) {
		got = append(got, event+"|"+data)
	})
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2 (%v)", len(got), got)
	}
	if got[0] != "ping|{\"a\": 1}" {
		t.Errorf("event[0] = %q", got[0])
	}
	if got[1] != "|line1\nline2" {
		t.Errorf("event[1] = %q", got[1])
	}
}
