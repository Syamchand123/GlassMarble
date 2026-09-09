package renderer

import (
	"context"
	"errors"
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
