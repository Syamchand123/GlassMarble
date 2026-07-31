package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/agent"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
)

func TestAgentStreamForwarding(t *testing.T) {
	p := &streamingScriptedProvider{steps: []*provider.Response{
		textResp("Final streamed answer."),
	}}
	a := newTestAgent(t, p, 5)

	var deltas []string
	var events []agent.Event
	a.OnStream = func(d string) { deltas = append(deltas, d) }
	a.OnEvent = func(ev agent.Event) { events = append(events, ev) }

	res, err := a.Run(context.Background(), "stream?", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Join(deltas, "") != "Final streamed answer." {
		t.Errorf("deltas = %q", strings.Join(deltas, ""))
	}
	if res.Text != "Final streamed answer." {
		t.Errorf("text = %q", res.Text)
	}
	// Each delta also surfaces as a "stream" event (3 chunks from the
	// scripted provider).
	streams := 0
	for _, ev := range events {
		if ev.Type == "stream" && ev.Delta != "" {
			streams++
		}
	}
	if streams != 3 {
		t.Errorf("stream events = %d, want 3", streams)
	}
}

// streamingScriptedProvider replays responses and emits request.OnStream
// deltas as it would a real provider.
type streamingScriptedProvider struct {
	steps []*provider.Response
}

func (p *streamingScriptedProvider) Name() string                       { return "streaming-scripted" }
func (p *streamingScriptedProvider) Ping(context.Context, string) error { return nil }

func (p *streamingScriptedProvider) Complete(_ context.Context, req provider.Request) (*provider.Response, error) {
	if len(p.steps) == 0 {
		return &provider.Response{Text: "(no more steps)"}, nil
	}
	r := p.steps[0]
	p.steps = p.steps[1:]
	if req.OnStream != nil {
		for _, chunk := range []string{"Final ", "streamed ", "answer."} {
			req.OnStream(chunk)
		}
	}
	return r, nil
}

func TestAgentTokenBudgetGuardrail(t *testing.T) {
	p := &scriptedProvider{steps: []*provider.Response{
		{ToolCalls: []provider.ToolCall{call1("c1")}, Usage: provider.Usage{PromptTokens: 2000, CompletionTokens: 1000, TotalTokens: 3000}},
		{ToolCalls: []provider.ToolCall{call1("c2")}, Usage: provider.Usage{PromptTokens: 2000, CompletionTokens: 1000, TotalTokens: 3000}},
		textResp("never reached"),
	}}
	a := newTestAgent(t, p, 5)
	a.MaxTotalTokens = 5000

	res, err := a.Run(context.Background(), "spend?", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StoppedReason != "token_budget" {
		t.Errorf("stopped reason = %q, want token_budget", res.StoppedReason)
	}
	if res.ReachedTurnLimit {
		t.Error("token-budget stop must not set turn-limit flag")
	}
	if res.Usage.TotalTokens != 6000 {
		t.Errorf("usage = %+v", res.Usage)
	}
	// The second completion was spent before the overrun was visible; the
	// tool round it requested must never execute, and a third call must
	// never be sent.
	if len(res.ToolCalls) != 1 {
		t.Errorf("tool calls = %d, want 1 (second round must not execute)", len(res.ToolCalls))
	}
	if len(p.reqs) != 2 {
		t.Errorf("requests = %d, want 2 (third completion must not run)", len(p.reqs))
	}
}

func TestAgentCostBudgetGuardrail(t *testing.T) {
	p := &scriptedProvider{steps: []*provider.Response{
		{ToolCalls: []provider.ToolCall{call1("c1")}, Usage: provider.Usage{PromptTokens: 100000, CompletionTokens: 100000, TotalTokens: 200000}},
		textResp("never reached"),
	}}
	a := newTestAgent(t, p, 5)
	a.Model = "gpt-4o" // 2.50/10.00 per M
	a.MaxCostUSD = 0.50

	res, err := a.Run(context.Background(), "expensive?", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StoppedReason != "cost_budget" {
		t.Errorf("stopped reason = %q, want cost_budget", res.StoppedReason)
	}
	// 100k * 2.5/M + 100k * 10/M = 1.25 USD > 0.50 cap.
	if res.CostUSD < 1.0 {
		t.Errorf("cost = %v, want >= 1.0", res.CostUSD)
	}
	if !res.CostEstimated {
		t.Error("cost should be marked estimated")
	}
}

func TestAgentUnpricedModelSkipsCostCap(t *testing.T) {
	p := &scriptedProvider{steps: []*provider.Response{
		textResp("Free as a bird."),
	}}
	a := newTestAgent(t, p, 5)
	a.Model = "some-local-model"
	a.MaxCostUSD = 0.0001 // tiny cap, but unpriced => no enforcement

	res, err := a.Run(context.Background(), "cheap?", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StoppedReason != "" {
		t.Errorf("stopped reason = %q, want none", res.StoppedReason)
	}
	if res.CostEstimated {
		t.Error("unpriced model must not be marked estimated")
	}
	if res.Text != "Free as a bird." {
		t.Errorf("text = %q", res.Text)
	}
}

func TestAgentGuardrailStopIsNotAnError(t *testing.T) {
	// A budget stop is a normal result, not an error the caller must handle.
	p := &scriptedProvider{steps: []*provider.Response{textResp("should not run")}}
	a := newTestAgent(t, p, 5)
	a.MaxTotalTokens = 1 // pre-flight check stops before the first call

	res, err := a.Run(context.Background(), "tiny budget", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StoppedReason != "token_budget" {
		t.Errorf("stopped reason = %q, want token_budget", res.StoppedReason)
	}
	if len(p.reqs) != 0 {
		t.Errorf("requests = %d, want 0 (provider must not be called)", len(p.reqs))
	}
}
