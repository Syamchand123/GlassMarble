// Package renderer — llm_actuator.go
// Implements Track A LLM Prose Actuator using the unified internal/ai_engine/provider interface.
package renderer

import (
	"context"
	"fmt"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// LLMActuatorConfig controls LLM completion parameters.
type LLMActuatorConfig struct {
	Provider        provider.Provider
	Model           string
	MaxOutputTokens int
	Temperature     float64
	BackoffSchedule []time.Duration
}

// DefaultLLMActuatorConfig returns sensible production defaults for Track A.
func DefaultLLMActuatorConfig(p provider.Provider, model string) LLMActuatorConfig {
	return LLMActuatorConfig{
		Provider:        p,
		Model:           model,
		MaxOutputTokens: 300,
		Temperature:     0.0, // Hard temperature 0.0 per architectural principle P1
		BackoffSchedule: []time.Duration{
			100 * time.Millisecond,
			500 * time.Millisecond,
			2000 * time.Millisecond,
		},
	}
}

// LLMActuator executes LLM calls with exponential backoff and token tracking.
type LLMActuator struct {
	cfg LLMActuatorConfig
}

// NewLLMActuator creates a new LLM prose actuator.
func NewLLMActuator(cfg LLMActuatorConfig) *LLMActuator {
	if cfg.MaxOutputTokens <= 0 {
		cfg.MaxOutputTokens = 300
	}
	if len(cfg.BackoffSchedule) == 0 {
		cfg.BackoffSchedule = []time.Duration{
			100 * time.Millisecond,
			500 * time.Millisecond,
			2000 * time.Millisecond,
		}
	}
	return &LLMActuator{cfg: cfg}
}

// RenderResult contains the generated text and token accounting.
type RenderResult struct {
	Text             string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Duration         time.Duration
}

// Render runs Track A prose generation for the given FactSheet.
// Retries up to 3 times with exponential backoff (100ms -> 500ms -> 2000ms).
func (a *LLMActuator) Render(ctx context.Context, fs *config.FactSheet) (*RenderResult, error) {
	if a.cfg.Provider == nil {
		return nil, fmt.Errorf("doc_engine/llm_actuator: no AI provider configured")
	}

	systemPrompt := BuildSystemPrompt(&fs.Style, 0)
	userPrompt, err := BuildUserPrompt(fs)
	if err != nil {
		return nil, err
	}

	req := provider.Request{
		Model:       a.cfg.Model,
		System:      systemPrompt,
		Temperature: &a.cfg.Temperature,
		MaxOutputTokens: a.cfg.MaxOutputTokens,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: userPrompt},
		},
	}

	return a.completeWithBackoff(ctx, req)
}

// Repair runs a 1-turn repair prompt when the initial LLM output failed a quality gate.
func (a *LLMActuator) Repair(ctx context.Context, fs *config.FactSheet, prevOutput string, gateErr error) (*RenderResult, error) {
	if a.cfg.Provider == nil {
		return nil, fmt.Errorf("doc_engine/llm_actuator: no AI provider configured")
	}

	systemPrompt := BuildSystemPrompt(&fs.Style, 0)
	userPrompt, err := BuildUserPrompt(fs)
	if err != nil {
		return nil, err
	}

	repairPrompt := BuildRepairPrompt(fs, prevOutput, gateErr)

	req := provider.Request{
		Model:       a.cfg.Model,
		System:      systemPrompt,
		Temperature: &a.cfg.Temperature,
		MaxOutputTokens: a.cfg.MaxOutputTokens,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: userPrompt},
			{Role: provider.RoleAssistant, Content: prevOutput},
			{Role: provider.RoleUser, Content: repairPrompt},
		},
	}

	return a.completeWithBackoff(ctx, req)
}

// completeWithBackoff attempts completion across backoff intervals.
func (a *LLMActuator) completeWithBackoff(ctx context.Context, req provider.Request) (*RenderResult, error) {
	start := time.Now()
	var lastErr error

	attempts := len(a.cfg.BackoffSchedule)
	if attempts == 0 {
		attempts = 1
	}

	for i := 0; i < attempts; i++ {
		resp, err := a.cfg.Provider.Complete(ctx, req)
		if err == nil && resp != nil {
			return &RenderResult{
				Text:             resp.Text,
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				TotalTokens:      resp.Usage.TotalTokens,
				Duration:         time.Since(start),
			}, nil
		}

		lastErr = err

		// Check if context canceled before sleeping
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Backoff sleep before next attempt (if not last)
		if i < len(a.cfg.BackoffSchedule) {
			sleepDur := a.cfg.BackoffSchedule[i]
			select {
			case <-time.After(sleepDur):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	return nil, fmt.Errorf("doc_engine/llm_actuator: completion failed after %d attempts: %w", attempts, lastErr)
}
