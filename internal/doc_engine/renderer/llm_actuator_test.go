package renderer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

type mockProvider struct {
	name         string
	completeFunc func(ctx context.Context, req provider.Request) (*provider.Response, error)
	callCount    int
	lastRequest  provider.Request
}

func (m *mockProvider) Name() string {
	if m.name == "" {
		return "mock"
	}
	return m.name
}

func (m *mockProvider) Complete(ctx context.Context, req provider.Request) (*provider.Response, error) {
	m.callCount++
	m.lastRequest = req
	if m.completeFunc != nil {
		return m.completeFunc(ctx, req)
	}
	return &provider.Response{
		Text: "Generated prose.",
		Usage: provider.Usage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
	}, nil
}

func (m *mockProvider) Ping(ctx context.Context, model string) error {
	return nil
}

func TestLLMActuator_Success(t *testing.T) {
	mock := &mockProvider{}
	cfg := DefaultLLMActuatorConfig(mock, "gpt-4o")
	cfg.BackoffSchedule = []time.Duration{1 * time.Millisecond}

	act := NewLLMActuator(cfg)
	fs := &config.FactSheet{
		DocID:              "docs/test.md",
		SectionID:          "sec",
		SectionInstruction: "Document functions.",
		GroundTruth: config.GroundTruthPayload{
			Symbols: []config.SymbolFact{
				{FQN: "pkg.Foo", Kind: "func"},
			},
		},
	}

	res, err := act.Render(context.Background(), fs)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	if res.Text != "Generated prose." {
		t.Errorf("unexpected text: %q", res.Text)
	}
	if res.TotalTokens != 150 {
		t.Errorf("unexpected token count: %d", res.TotalTokens)
	}
	if mock.callCount != 1 {
		t.Errorf("expected 1 call, got %d", mock.callCount)
	}
	if mock.lastRequest.Temperature == nil || *mock.lastRequest.Temperature != 0.0 {
		t.Errorf("expected temperature 0.0")
	}
}

func TestLLMActuator_RetryBackoff(t *testing.T) {
	attempts := 0
	mock := &mockProvider{
		completeFunc: func(ctx context.Context, req provider.Request) (*provider.Response, error) {
			attempts++
			if attempts < 3 {
				return nil, errors.New("rate limited")
			}
			return &provider.Response{
				Text:  "Succeeded on attempt 3.",
				Usage: provider.Usage{TotalTokens: 200},
			}, nil
		},
	}

	cfg := DefaultLLMActuatorConfig(mock, "claude-3-5-sonnet")
	// Use fast backoff for tests
	cfg.BackoffSchedule = []time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond}

	act := NewLLMActuator(cfg)
	fs := &config.FactSheet{DocID: "d", SectionID: "s"}

	res, err := act.Render(context.Background(), fs)
	if err != nil {
		t.Fatalf("expected success on 3rd attempt, got error: %v", err)
	}
	if res.Text != "Succeeded on attempt 3." {
		t.Errorf("unexpected text: %q", res.Text)
	}
	if mock.callCount != 3 {
		t.Errorf("expected 3 calls, got %d", mock.callCount)
	}
}

func TestLLMActuator_FailureExhausted(t *testing.T) {
	mock := &mockProvider{
		completeFunc: func(ctx context.Context, req provider.Request) (*provider.Response, error) {
			return nil, errors.New("permanent failure")
		},
	}

	cfg := DefaultLLMActuatorConfig(mock, "claude-3-5-sonnet")
	cfg.BackoffSchedule = []time.Duration{1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond}

	act := NewLLMActuator(cfg)
	fs := &config.FactSheet{DocID: "d", SectionID: "s"}

	_, err := act.Render(context.Background(), fs)
	if err == nil {
		t.Fatalf("expected error after exhausted retries")
	}
	if mock.callCount != 3 {
		t.Errorf("expected 3 calls, got %d", mock.callCount)
	}
}

// TestLLMActuator_StripsExplicitThinkTags guards against a regression where
// a reasoning model's <think>...</think> block (inlined directly into the
// message content — no provider struct anywhere separates a distinct
// "reasoning_content" field) shipped verbatim as part of the section body.
func TestLLMActuator_StripsExplicitThinkTags(t *testing.T) {
	mock := &mockProvider{
		completeFunc: func(ctx context.Context, req provider.Request) (*provider.Response, error) {
			return &provider.Response{
				Text: "<think>Let me work through what to write here.</think>\n\n## Overview\n\nThe store guards state with a mutex.",
			}, nil
		},
	}
	cfg := DefaultLLMActuatorConfig(mock, "reasoning-model")
	cfg.BackoffSchedule = []time.Duration{1 * time.Millisecond}
	act := NewLLMActuator(cfg)

	res, err := act.Render(context.Background(), &config.FactSheet{DocID: "d", SectionID: "s"})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if strings.Contains(res.Text, "<think>") || strings.Contains(res.Text, "Let me work through") {
		t.Errorf("think block leaked into rendered text: %q", res.Text)
	}
	if !strings.Contains(res.Text, "## Overview") {
		t.Errorf("expected real content to survive stripping, got: %q", res.Text)
	}
}

// TestLLMActuator_NarratedReasoningLeakFallsBackAfterRetries guards against
// the confirmed real-world failure: a reasoning model (no <think> tags,
// just narrated planning prose) returning "We need to produce markdown for
// the section... The rule: we must not reference..." — cut off mid-word —
// as the entire response, with no separate reasoning field to strip. Every
// retry attempt leaking the same way must exhaust the backoff schedule and
// return an error (so the caller's existing fallback-to-deterministic path
// engages), not ship the transcript as if it were the section's content.
func TestLLMActuator_NarratedReasoningLeakFallsBackAfterRetries(t *testing.T) {
	mock := &mockProvider{
		completeFunc: func(ctx context.Context, req provider.Request) (*provider.Response, error) {
			return &provider.Response{
				Text: "We need to produce markdown for the section \"required-optional\" under doc_id \"config\". " +
					"The rule: we must not reference any code entity not listed in ground_truth. " +
					"Thus we can produce a markdown section like: ## Required and Optional Configuration Variables - `config.go` (file) – optional (no required value",
			}, nil
		},
	}
	cfg := DefaultLLMActuatorConfig(mock, "reasoning-model")
	cfg.BackoffSchedule = []time.Duration{1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond}
	act := NewLLMActuator(cfg)

	_, err := act.Render(context.Background(), &config.FactSheet{DocID: "d", SectionID: "s"})
	if err == nil {
		t.Fatalf("expected an error so the caller falls back to the deterministic renderer, got success")
	}
	if mock.callCount != 3 {
		t.Errorf("expected all 3 attempts to be tried (each leaking), got %d calls", mock.callCount)
	}
}

// TestLLMActuator_OrdinaryProseIsNotFlaggedAsLeak is the false-positive
// guard for the heuristic above: real documentation prose — including
// prose that legitimately uses "we"/"you" in second person, per the
// system prompt's own style rule — must render normally.
func TestLLMActuator_OrdinaryProseIsNotFlaggedAsLeak(t *testing.T) {
	mock := &mockProvider{
		completeFunc: func(ctx context.Context, req provider.Request) (*provider.Response, error) {
			return &provider.Response{
				Text: "## Configuration\n\nYou configure the store's port with `SERVER_PORT`. The default is 8080 when unset.",
			}, nil
		},
	}
	cfg := DefaultLLMActuatorConfig(mock, "gpt-4o")
	cfg.BackoffSchedule = []time.Duration{1 * time.Millisecond}
	act := NewLLMActuator(cfg)

	res, err := act.Render(context.Background(), &config.FactSheet{DocID: "d", SectionID: "s"})
	if err != nil {
		t.Fatalf("ordinary prose must not be treated as a reasoning leak: %v", err)
	}
	if !strings.Contains(res.Text, "SERVER_PORT") {
		t.Errorf("unexpected text: %q", res.Text)
	}
}

func TestLLMActuator_RepairTurn(t *testing.T) {
	mock := &mockProvider{
		completeFunc: func(ctx context.Context, req provider.Request) (*provider.Response, error) {
			if len(req.Messages) != 3 {
				t.Errorf("expected 3 messages in repair request, got %d", len(req.Messages))
			}
			return &provider.Response{Text: "Repaired text."}, nil
		},
	}

	cfg := DefaultLLMActuatorConfig(mock, "gpt-4o")
	cfg.BackoffSchedule = []time.Duration{1 * time.Millisecond}
	act := NewLLMActuator(cfg)

	fs := &config.FactSheet{DocID: "d", SectionID: "s"}
	res, err := act.Repair(context.Background(), fs, "Bad output", errors.New("syntax error"))
	if err != nil {
		t.Fatalf("Repair failed: %v", err)
	}
	if res.Text != "Repaired text." {
		t.Errorf("unexpected repaired text: %q", res.Text)
	}
}
