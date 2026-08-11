package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// MockLLM is a scriptable OpenAI-compatible chat-completions server used to
// give the ai/why commands a deterministic "LLM" without any real network.
//
// The GlassMarble provider adapters speak the OpenAI wire format
// (provider/openai_compat.go): POST {baseURL}/chat/completions with
// {"model","messages","stream",...} and either a JSON body or an SSE stream
// of chunks ending in data: [DONE].
type MockLLM struct {
	t *testing.T

	mu sync.Mutex
	// script is the per-request response plan. When empty, the default
	// answer is returned.
	script []MockResponse
	// defaultText is returned for requests beyond the script.
	defaultText string
	// recorded holds every request body received, in order.
	recorded [][]byte
	// failNext makes the next request return a 500 (provider fault tests).
	failNext bool
	// requestCount counts completions served.
	requestCount int

	// Server is the running mock; callers start it with Start.
	Server *httptest.Server
}

// MockResponse is one scripted completion response.
type MockResponse struct {
	// Text is the assistant answer (plain or first chunk).
	Text string
	// ToolCalls are emitted instead of (or before) final text. When set,
	// the provider will surface them as tool calls and the agent loop will
	// run tools; a subsequent script entry supplies the tool-result answer.
	ToolCalls []MockToolCall
	// Stream forces an SSE response even when the request didn't ask to
	// stream (and vice versa is not possible — the client decides).
	Stream bool
	// ErrHTTP makes the mock return this status code (0 = 200).
	ErrHTTP int
}

// MockToolCall is a scripted function call from the model.
type MockToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// NewMockLLM creates a mock; call Start to bind the server.
func NewMockLLM(t *testing.T) *MockLLM {
	return &MockLLM{t: t}
}

// Start launches the HTTP server. The returned base URL must be written into
// the sandbox AI config (SeedAIConfig) or passed via --base-url.
func (m *MockLLM) Start() string {
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m.Server.URL
}

// Close shuts the server down (safe to call multiple times).
func (m *MockLLM) Close() {
	if m.Server != nil {
		m.Server.Close()
		m.Server = nil
	}
}

// Script sets the per-request response plan for the next len(responses)
// requests. Extra requests beyond the script get the default answer.
func (m *MockLLM) Script(responses ...MockResponse) *MockLLM {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.script = responses
	m.failNext = false
	return m
}

// DefaultText is returned for any request beyond the script, making single-
// answer tests one-liners.
func (m *MockLLM) DefaultText(text string) *MockLLM {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.script = nil
	m.defaultText = text
	return m
}

// FailNext makes the next completion return HTTP 500, then recovers.
func (m *MockLLM) FailNext() *MockLLM {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failNext = true
	return m
}

// Requests returns the raw JSON bodies of all received completions.
func (m *MockLLM) Requests() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, len(m.recorded))
	copy(out, m.recorded)
	return out
}

// LastRequest returns the most recent request body as a decoded generic map
// (or nil when none arrived).
func (m *MockLLM) LastRequest() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.recorded) == 0 {
		return nil
	}
	var out map[string]any
	_ = json.Unmarshal(m.recorded[len(m.recorded)-1], &out)
	return out
}

// Count returns how many completions have been served.
func (m *MockLLM) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requestCount
}

// WaitFor waits until at least n completions have been served (streaming
// responses may arrive after the command printed). For use with --no-stream
// the command is synchronous and this is not needed.
func (m *MockLLM) WaitFor(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if m.Count() >= n {
			return
		}
	}
}

func (m *MockLLM) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/chat/completions" {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	m.recorded = append(m.recorded, body)
	m.requestCount++

	if m.failNext {
		m.failNext = false
		m.mu.Unlock()
		http.Error(w, `{"error":{"message":"mock provider failure"}}`, http.StatusInternalServerError)
		return
	}
	var resp MockResponse
	if len(m.script) > 0 {
		resp = m.script[0]
		m.script = m.script[1:]
	} else {
		resp = MockResponse{Text: m.defaultText}
	}
	m.mu.Unlock()

	if resp.ErrHTTP != 0 {
		http.Error(w, `{"error":{"message":"scripted failure"}}`, resp.ErrHTTP)
		return
	}

	var req struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(body, &req)

	if req.Stream {
		writeSSE(w, resp)
		return
	}
	writeJSON(w, resp)
}

// --- wire writers (OpenAI-compatible shapes) ---

func writeJSON(w http.ResponseWriter, resp MockResponse) {
	msg := struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		ToolCalls any    `json:"tool_calls,omitempty"`
	}{Role: "assistant", Content: resp.Text}
	if len(resp.ToolCalls) > 0 {
		tcs := make([]map[string]any, 0, len(resp.ToolCalls))
		for _, tc := range resp.ToolCalls {
			tcs = append(tcs, map[string]any{
				"id":   tc.ID,
				"type": "function",
				"function": map[string]string{
					"name":      tc.Name,
					"arguments": tc.Arguments,
				},
			})
		}
		msg.ToolCalls = tcs
	}
	payload := map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": "stop"}},
		"usage": map[string]int{
			"prompt_tokens":     12,
			"completion_tokens": 8,
			"total_tokens":      20,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func writeSSE(w http.ResponseWriter, resp MockResponse) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, resp)
		return
	}
	writeChunk := func(delta map[string]any) {
		chunk := map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion.chunk",
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
	// Emit one token-sized chunk per 8 characters so streaming sinks see
	// multiple deltas.
	for i := 0; i < len(resp.Text); i += 8 {
		end := i + 8
		if end > len(resp.Text) {
			end = len(resp.Text)
		}
		writeChunk(map[string]any{"content": resp.Text[i:end]})
	}
	for _, tc := range resp.ToolCalls {
		writeChunk(map[string]any{
			"tool_calls": []any{map[string]any{
				"index": 0,
				"id":    tc.ID,
				"type":  "function",
				"function": map[string]string{
					"name":      tc.Name,
					"arguments": tc.Arguments,
				},
			}},
		})
	}
	usage := map[string]any{
		"prompt_tokens":     12,
		"completion_tokens": 8,
		"total_tokens":      20,
	}
	data, _ := json.Marshal(map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion.chunk",
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
		"usage":   usage,
	})
	fmt.Fprintf(w, "data: %s\n\n", data)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
