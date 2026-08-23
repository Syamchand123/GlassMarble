package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/agent"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/akgbridge"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/testutil"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/tools"
)

// scriptedProvider replays canned responses and records every request.
type scriptedProvider struct {
	steps []*provider.Response
	reqs  []provider.Request
}

func (p *scriptedProvider) Name() string { return "scripted" }

func (p *scriptedProvider) Ping(context.Context, string) error { return nil }

func (p *scriptedProvider) Complete(_ context.Context, req provider.Request) (*provider.Response, error) {
	p.reqs = append(p.reqs, req)
	if len(p.steps) == 0 {
		return &provider.Response{Text: "(no more steps)"}, nil
	}
	r := p.steps[0]
	p.steps = p.steps[1:]
	return r, nil
}

func call1(id string) provider.ToolCall {
	return provider.ToolCall{ID: id, Name: "akg_status", Arguments: "{}"}
}

func textResp(text string) *provider.Response {
	return &provider.Response{Text: text, Usage: provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}
}

func newTestAgent(t *testing.T, p provider.Provider, maxTurns int) *agent.Agent {
	t.Helper()
	dir := t.TempDir()
	testutil.SeedAKG(t, dir)
	tmp := 0.2
	return &agent.Agent{
		Provider:        p,
		Model:           "test-model",
		System:          "You are a test agent.",
		Tools:           tools.All(),
		Env:             &tools.Env{RootDir: dir, Bridge: akgbridge.New(dir)},
		MaxTurns:        maxTurns,
		MaxResultBytes:  8192,
		Temperature:     &tmp,
		MaxOutputTokens: 512,
	}
}

func TestAgentToolCallFlow(t *testing.T) {
	p := &scriptedProvider{steps: []*provider.Response{
		{ToolCalls: []provider.ToolCall{call1("c1")}, Usage: provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
		textResp("The AKG has 4 nodes."),
	}}
	a := newTestAgent(t, p, 5)
	var events []agent.Event
	a.OnEvent = func(ev agent.Event) { events = append(events, ev) }

	res, err := a.Run(context.Background(), "how many nodes?", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "The AKG has 4 nodes." {
		t.Errorf("text = %q", res.Text)
	}
	if res.Turns != 2 {
		t.Errorf("turns = %d, want 2", res.Turns)
	}
	if res.ReachedTurnLimit {
		t.Error("should not report turn limit")
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "akg_status" || !res.ToolCalls[0].OK {
		t.Errorf("tool traces = %+v", res.ToolCalls)
	}
	if res.Usage.TotalTokens != 30 {
		t.Errorf("usage = %+v, want summed totals", res.Usage)
	}

	// The first request declares tools; the second carries the tool result.
	if len(p.reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(p.reqs))
	}
	if len(p.reqs[0].Tools) != len(tools.All()) {
		t.Errorf("tools declared = %d", len(p.reqs[0].Tools))
	}
	last := p.reqs[1].Messages[len(p.reqs[1].Messages)-1]
	if last.Role != provider.RoleTool || len(last.ToolResults) != 1 {
		t.Fatalf("expected tool-result message, got %+v", last)
	}
	tr := last.ToolResults[0]
	if tr.ID != "c1" || tr.IsError {
		t.Errorf("tool result = %+v", tr)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(tr.Content), &parsed); err != nil {
		t.Fatalf("result not JSON: %v", err)
	}
	if parsed["ok"] != true {
		t.Errorf("result = %v", parsed)
	}

	if len(events) != 3 {
		t.Errorf("events = %+v", events)
	}
	if events[0].Type != "tool_call" || events[0].ToolName != "akg_status" {
		t.Errorf("event[0] = %+v", events[0])
	}
	if events[1].Type != "tool_result" || !events[1].OK {
		t.Errorf("event[1] = %+v", events[1])
	}
	if events[2].Type != "answer" {
		t.Errorf("event[2] = %+v", events[2])
	}
}

func TestAgentTurnLimit(t *testing.T) {
	p := &scriptedProvider{steps: []*provider.Response{
		{ToolCalls: []provider.ToolCall{call1("c1")}},
		{ToolCalls: []provider.ToolCall{call1("c2")}},
		{ToolCalls: []provider.ToolCall{call1("c3")}},
	}}
	a := newTestAgent(t, p, 2)
	res, err := a.Run(context.Background(), "loop forever?", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.ReachedTurnLimit {
		t.Error("expected turn-limit flag")
	}
	if res.Turns != 2 {
		t.Errorf("turns = %d, want 2", res.Turns)
	}
	if len(res.ToolCalls) != 2 {
		t.Errorf("tool calls = %d, want 2", len(res.ToolCalls))
	}
}

func TestAgentUnknownToolRecovers(t *testing.T) {
	p := &scriptedProvider{steps: []*provider.Response{
		{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "no_such_tool", Arguments: "{}"}}},
		textResp("That tool does not exist."),
	}}
	a := newTestAgent(t, p, 5)
	res, err := a.Run(context.Background(), "do something", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "That tool does not exist." {
		t.Errorf("text = %q", res.Text)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].OK {
		t.Fatalf("expected failed trace, got %+v", res.ToolCalls)
	}
	last := p.reqs[1].Messages[len(p.reqs[1].Messages)-1]
	tr := last.ToolResults[0]
	if !tr.IsError || !strings.Contains(tr.Content, "unknown tool") {
		t.Errorf("error result = %+v", tr)
	}
}

func TestAgentNoTools(t *testing.T) {
	p := &scriptedProvider{steps: []*provider.Response{textResp("Plain answer.")}}
	a := newTestAgent(t, p, 5)
	a.Tools = nil

	res, err := a.Run(context.Background(), "opinion?", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "Plain answer." || res.Turns != 1 {
		t.Errorf("res = %+v", res)
	}
	if len(p.reqs[0].Tools) != 0 {
		t.Error("no tools should be declared")
	}
	if len(res.ToolCalls) != 0 {
		t.Errorf("unexpected tool calls: %+v", res.ToolCalls)
	}
}

func TestAgentEmptyToolArguments(t *testing.T) {
	p := &scriptedProvider{steps: []*provider.Response{
		{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "akg_status", Arguments: ""}}},
		textResp("done"),
	}}
	a := newTestAgent(t, p, 5)
	res, err := a.Run(context.Background(), "status?", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.ToolCalls) != 1 || !res.ToolCalls[0].OK {
		t.Errorf("empty args should default to {}: %+v", res.ToolCalls)
	}
}

func TestAgentHistoryContinuity(t *testing.T) {
	p := &scriptedProvider{steps: []*provider.Response{
		textResp("First answer."),
		textResp("Second answer."),
	}}
	a := newTestAgent(t, p, 5)
	res1, err := a.Run(context.Background(), "first?", nil)
	if err != nil {
		t.Fatalf("Run 1: %v", err)
	}
	res2, err := a.Run(context.Background(), "second?", res1.Messages)
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	if res2.Text != "Second answer." {
		t.Errorf("text = %q", res2.Text)
	}
	// Turn 2 request must contain both user questions.
	if len(p.reqs) != 2 {
		t.Fatalf("requests = %d", len(p.reqs))
	}
	users := 0
	for _, m := range p.reqs[1].Messages {
		if m.Role == provider.RoleUser {
			users++
		}
	}
	if users != 2 {
		t.Errorf("user messages in second request = %d, want 2", users)
	}
}

// ---- dispatcher unit tests ----

func TestDispatcherTruncation(t *testing.T) {
	dir := t.TempDir()
	testutil.SeedAKG(t, dir)
	env := &tools.Env{RootDir: dir, Bridge: akgbridge.New(dir)}

	d := &agent.Dispatcher{Tools: tools.All(), MaxResultBytes: 100}
	res := d.Dispatch(context.Background(), env, []provider.ToolCall{
		{ID: "c1", Name: "akg_search", Arguments: `{"kind": "FUNCTION"}`},
	})
	if len(res) != 1 || res[0].IsError {
		t.Fatalf("dispatch = %+v", res)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(res[0].Content), &parsed); err != nil {
		t.Fatalf("result not JSON: %v", err)
	}
	if parsed["ok"] != true || parsed["truncated"] != true {
		t.Fatalf("expected truncated wrapper, got %v", parsed)
	}
	if _, ok := parsed["data_preview"].(string); !ok {
		t.Errorf("missing data_preview: %v", parsed)
	}
}

func TestDispatcherRawTruncation(t *testing.T) {
	dir := t.TempDir()
	testutil.SeedAKG(t, dir)
	testutil.WriteFile(t, dir, "src/db.go", testutil.DBStoreSource())
	env := &tools.Env{RootDir: dir, Bridge: akgbridge.New(dir)}

	d := &agent.Dispatcher{Tools: tools.All(), MaxResultBytes: 200}
	res := d.Dispatch(context.Background(), env, []provider.ToolCall{
		{ID: "c1", Name: "code_read_file", Arguments: `{"path": "src/db.go", "start_line": 1, "end_line": 60}`},
	})
	if res[0].IsError {
		t.Fatalf("dispatch error: %s", res[0].Content)
	}
	if !strings.Contains(res[0].Content, "truncated") {
		t.Errorf("missing truncation marker: %s", res[0].Content)
	}
}

func TestDispatcherMissingRequiredArg(t *testing.T) {
	env := &tools.Env{RootDir: t.TempDir()}
	d := &agent.Dispatcher{Tools: tools.All(), MaxResultBytes: 8192}
	res := d.Dispatch(context.Background(), env, []provider.ToolCall{
		{ID: "c1", Name: "code_read_file", Arguments: `{}`},
	})
	if !res[0].IsError || !strings.Contains(res[0].Content, "missing required argument") {
		t.Errorf("expected required-arg error, got %+v", res[0])
	}
}

func TestDispatcherInvalidJSONArgs(t *testing.T) {
	env := &tools.Env{RootDir: t.TempDir()}
	d := &agent.Dispatcher{Tools: tools.All(), MaxResultBytes: 8192}
	res := d.Dispatch(context.Background(), env, []provider.ToolCall{
		{ID: "c1", Name: "akg_status", Arguments: `{not json`},
	})
	if !res[0].IsError || !strings.Contains(res[0].Content, "invalid arguments JSON") {
		t.Errorf("expected args error, got %+v", res[0])
	}
}
