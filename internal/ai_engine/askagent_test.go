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

// scriptedCompletionsServer replays responses by request order and records
// the request bodies so tests can assert on the wire format.
func scriptedCompletionsServer(t *testing.T, responses []string) (*httptest.Server, *[]map[string]any, *sync.Mutex) {
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
		w.Header().Set("Content-Type", "application/json")
		if i < len(responses) {
			fmt.Fprint(w, responses[i])
		} else {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"(fallback)"}}],"usage":{}}`)
		}
	}))
	return srv, &bodies, &mu
}

func testEngine(t *testing.T, srv *httptest.Server, rootDir string) *Engine {
	t.Helper()
	tmp := 0.2
	cfg := &aiconfig.Config{
		Provider:           "custom",
		Model:              "test-model",
		APIKey:             "sk-test",
		BaseURL:            srv.URL,
		Temperature:        &tmp,
		MaxTurns:           5,
		MaxToolResultBytes: 4096,
		MaxOutputTokens:    512,
		TimeoutSec:         30,
	}
	e, err := New(cfg, rootDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

func TestAskAgentToolFlowE2E(t *testing.T) {
	srv, bodies, mu := scriptedCompletionsServer(t, []string{
		`{"choices":[{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"akg_status","arguments":"{}"}}]}}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`,
		`{"choices":[{"message":{"content":"The AKG is missing — run gmb analyze."}}],"usage":{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13}}`,
	})
	defer srv.Close()

	e := testEngine(t, srv, t.TempDir())
	res, err := e.AskAgent(context.Background(), "what is the state of the AKG?", AgentOptions{EnableTools: true})
	if err != nil {
		t.Fatalf("AskAgent: %v", err)
	}
	if res.Text != "The AKG is missing — run gmb analyze." {
		t.Errorf("text = %q", res.Text)
	}
	if res.Turns != 2 {
		t.Errorf("turns = %d", res.Turns)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "akg_status" || res.ToolCalls[0].OK {
		t.Errorf("tool calls = %+v", res.ToolCalls)
	}
	if res.Usage.TotalTokens != 20 {
		t.Errorf("usage = %+v", res.Usage)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(*bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(*bodies))
	}
	first := (*bodies)[0]
	toolsDecl, _ := first["tools"].([]any)
	if len(toolsDecl) == 0 {
		t.Error("first request must declare tools")
	}
	// The system prompt carries the repo context header.
	sys := first["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(sys, "Repository context") {
		t.Error("system prompt missing repository context header")
	}
	// The second request carries the tool result.
	second := (*bodies)[1]
	msgs := second["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	if last["role"] != "tool" {
		t.Errorf("last message role = %v", last["role"])
	}
}

func TestAskAgentNoToolsMode(t *testing.T) {
	srv, bodies, mu := scriptedCompletionsServer(t, []string{
		`{"choices":[{"message":{"content":"A purely textual opinion."}}],"usage":{"prompt_tokens":3,"completion_tokens":6,"total_tokens":9}}`,
	})
	defer srv.Close()

	e := testEngine(t, srv, t.TempDir())
	res, err := e.AskAgent(context.Background(), "opinion?", AgentOptions{EnableTools: false})
	if err != nil {
		t.Fatalf("AskAgent: %v", err)
	}
	if res.Text != "A purely textual opinion." || res.Turns != 1 {
		t.Errorf("res = %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*bodies) != 1 {
		t.Fatalf("requests = %d", len(*bodies))
	}
	if _, ok := (*bodies)[0]["tools"]; ok {
		t.Error("no-tools mode must not declare tools")
	}
}

func TestAskAgentToolRestriction(t *testing.T) {
	e := testEngine(t, httptest.NewServer(http.NotFoundHandler()), t.TempDir())
	if _, err := e.AskAgent(context.Background(), "x", AgentOptions{EnableTools: true, Tools: []string{"bogus_tool"}}); err == nil {
		t.Fatal("expected error for unknown tool name")
	} else if !strings.Contains(err.Error(), "bogus_tool") {
		t.Errorf("err = %v", err)
	}
}

func TestAskAgentEmptyQuestion(t *testing.T) {
	e := testEngine(t, httptest.NewServer(http.NotFoundHandler()), t.TempDir())
	if _, err := e.AskAgent(context.Background(), "", AgentOptions{EnableTools: true}); err == nil {
		t.Fatal("expected empty-question error")
	}
}
