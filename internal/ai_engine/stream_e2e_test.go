package ai_engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/aiconfig"
)

// streamingCompletionsServer replays SSE bodies in request order when the
// request carries "stream": true, and a plain JSON error otherwise. It records
// request bodies for wire-format assertions.
func streamingCompletionsServer(t *testing.T, sseBodies []string) (*httptest.Server, *[]map[string]any, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var bodies []map[string]any
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		var parsed map[string]any
		_ = json.Unmarshal(raw, &parsed)
		bodies = append(bodies, parsed)
		i := idx
		idx++
		mu.Unlock()
		streaming, _ := parsed["stream"].(bool)
		if !streaming {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"choices":[{"message":{"content":"non-streaming fallback"}}],"usage":{}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if i < len(sseBodies) {
			fmt.Fprint(w, sseBodies[i])
		} else {
			fmt.Fprint(w, "data: [DONE]\n\n")
		}
	}))
	return srv, &bodies, &mu
}

func streamTestEngine(t *testing.T, srv *httptest.Server, rootDir string) *Engine {
	t.Helper()
	cfg := &aiconfig.Config{
		Provider:           "custom",
		Model:              "test-model",
		APIKey:             "sk-test",
		BaseURL:            srv.URL,
		Temperature:        0.2,
		MaxTurns:           5,
		MaxToolResultBytes: 4096,
		MaxOutputTokens:    512,
		TimeoutSec:         30,
		Stream:             true,
	}
	e, err := New(cfg, rootDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

func TestAskAgentStreamE2E(t *testing.T) {
	srv, bodies, mu := streamingCompletionsServer(t, []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"The AKG is \"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"missing.\"}}]}\n\ndata: {\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":4,\"total_tokens\":13}}\n\ndata: [DONE]\n\n",
	})
	defer srv.Close()

	e := streamTestEngine(t, srv, t.TempDir())
	var streamed strings.Builder
	res, err := e.AskAgent(context.Background(), "state of the AKG?", AgentOptions{EnableTools: true, OnStream: func(d string) { streamed.WriteString(d) }})
	if err != nil {
		t.Fatalf("AskAgent: %v", err)
	}
	if res.Text != "The AKG is missing." {
		t.Errorf("text = %q", res.Text)
	}
	if streamed.String() != "The AKG is missing." {
		t.Errorf("streamed = %q", streamed.String())
	}
	if res.Usage.TotalTokens != 13 {
		t.Errorf("usage = %+v", res.Usage)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(*bodies) != 1 {
		t.Fatalf("requests = %d, want 1", len(*bodies))
	}
	if stream, _ := (*bodies)[0]["stream"].(bool); !stream {
		t.Error("request must carry stream: true")
	}
}

func TestAskAgentStreamToolCallE2E(t *testing.T) {
	srv, _, _ := streamingCompletionsServer(t, []string{
		// Tool call reassembled from fragments across chunks.
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"type\":\"function\",\"function\":{\"name\":\"akg_status\",\"arguments\":\"\"}}]}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{}\"}}]}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":3,\"total_tokens\":8}}\n\ndata: [DONE]\n\n",
		// Final answer streamed after the tool result round.
		"data: {\"choices\":[{\"delta\":{\"content\":\"The AKG is \"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"missing.\"}}]}\n\ndata: {\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":4,\"total_tokens\":13}}\n\ndata: [DONE]\n\n",
	})
	defer srv.Close()

	e := streamTestEngine(t, srv, t.TempDir())
	var streamed strings.Builder
	res, err := e.AskAgent(context.Background(), "state of the AKG?", AgentOptions{EnableTools: true, OnStream: func(d string) { streamed.WriteString(d) }})
	if err != nil {
		t.Fatalf("AskAgent: %v", err)
	}
	if res.Text != "The AKG is missing." {
		t.Errorf("text = %q", res.Text)
	}
	if streamed.String() != "The AKG is missing." {
		t.Errorf("streamed = %q", streamed.String())
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "akg_status" {
		t.Errorf("tool calls = %+v", res.ToolCalls)
	}
	// First round usage + second round usage.
	if res.Usage.TotalTokens != 21 {
		t.Errorf("usage = %+v", res.Usage)
	}
	if res.Turns != 2 {
		t.Errorf("turns = %d", res.Turns)
	}
}

// TestAskAgentNoStreamBlocksDeltas verifies the buffered mode: when config
// streaming is off, OnStream is never invoked even if the endpoint streams.
func TestAskAgentNoStreamBlocksDeltas(t *testing.T) {
	srv, _, _ := streamingCompletionsServer(t, []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n",
	})
	defer srv.Close()

	cfg := &aiconfig.Config{
		Provider:           "custom",
		Model:              "test-model",
		APIKey:             "sk-test",
		BaseURL:            srv.URL,
		Temperature:        0.2,
		MaxTurns:           5,
		MaxToolResultBytes: 4096,
		MaxOutputTokens:    512,
		TimeoutSec:         30,
		Stream:             false,
	}
	e, err := New(cfg, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	called := false
	res, err := e.AskAgent(context.Background(), "hi", AgentOptions{EnableTools: false, OnStream: func(string) { called = true }})
	if err != nil {
		t.Fatalf("AskAgent: %v", err)
	}
	if res.Text != "non-streaming fallback" {
		t.Errorf("text = %q", res.Text)
	}
	if called {
		t.Error("OnStream must not fire when config.Stream is false")
	}
}
