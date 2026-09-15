// Package renderer — llm_actuator.go
// Implements Track A LLM Prose Actuator using the unified internal/ai_engine/provider interface.
package renderer

import (
	"context"
	"fmt"
	"regexp"
	"strings"
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
//
// Every section goes through Track A now — there is no "this section is
// tabular, skip straight to the deterministic renderer" routing anymore:
// the whole point of the LLM path is a real explanation for every section,
// with the deterministic renderer's tables appended underneath as a
// reference appendix (see engine.go's renderTrackA and deterministic.go's
// RenderReferenceAppendix), never instead of one.
func (a *LLMActuator) Render(ctx context.Context, fs *config.FactSheet) (*RenderResult, error) {
	if a.cfg.Provider == nil {
		return nil, fmt.Errorf("doc_engine/llm_actuator: no AI provider configured")
	}

	systemPrompt := BuildSystemPrompt(&fs.Style, fs.MaxWords)
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

	systemPrompt := BuildSystemPrompt(&fs.Style, fs.MaxWords)
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

// reasoningTagPattern matches an explicit chain-of-thought block some
// reasoning-model APIs/gateways inline directly into the message content
// (no separate "reasoning_content" field exists anywhere in this codebase's
// provider structs to catch it before it reaches Text) — e.g. <think>...
// </think>, <thinking>...</thinking>, <reasoning>...</reasoning>. An
// unclosed opening tag (response truncated mid-thought) matches to the end
// of the string, since everything after it is still reasoning, not answer.
var reasoningTagPattern = regexp.MustCompile(`(?is)<(think|thinking|reasoning)>.*?(</(think|thinking|reasoning)>|$)`)

// reasoningLeakOpeners are phrase patterns that overwhelmingly indicate the
// model is narrating its own deliberation about the task rather than
// answering it — real documentation prose essentially never opens a section
// with "we need to produce markdown for..." or similar meta-commentary
// about the prompt/ground_truth/instruction itself. Matched only against
// the START of the (tag-stripped) text, where a genuine answer for these
// sections would instead open by describing the actual code.
var reasoningLeakOpeners = regexp.MustCompile(`(?i)^\s*(we need to|we must|we should|we can|we have\b|let'?s (think|figure|produce|write)|i need to|i'll|the (task|instruction|rule)s?\s+(is|are|says?|:)|given the instruction|okay,?\s+(so|let)|first,?\s+(we|let)|to (answer|produce) this|the ground_truth|the fact_sheet)`)

// stripReasoningArtifacts removes explicit <think>/<thinking>/<reasoning>
// blocks from an LLM response, returning the cleaned text.
func stripReasoningArtifacts(text string) string {
	return strings.TrimSpace(reasoningTagPattern.ReplaceAllString(text, ""))
}

// looksLikeReasoningLeak reports whether text (already tag-stripped) reads
// as narrated chain-of-thought rather than a finished markdown answer. This
// is a deliberately narrow, high-precision check — it flags real observed
// failures (a reasoning model dumping "We need to produce markdown for the
// section... The rule: we must not reference..." verbatim, often truncated
// mid-sentence, into a shipped document) without rejecting ordinary
// documentation prose, which does not open sections by talking about the
// task itself.
func looksLikeReasoningLeak(text string) bool {
	return reasoningLeakOpeners.MatchString(strings.TrimSpace(text))
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
		var cleaned string
		if err == nil && resp != nil && strings.TrimSpace(resp.Text) != "" {
			cleaned = stripReasoningArtifacts(resp.Text)
			if cleaned != "" && !looksLikeReasoningLeak(cleaned) {
				return &RenderResult{
					Text:             cleaned,
					PromptTokens:     resp.Usage.PromptTokens,
					CompletionTokens: resp.Usage.CompletionTokens,
					TotalTokens:      resp.Usage.TotalTokens,
					Duration:         time.Since(start),
				}, nil
			}
		}

		switch {
		case err != nil:
			lastErr = err
		case cleaned == "" && resp != nil && strings.TrimSpace(resp.Text) == "":
			// A 200-ok response with empty/whitespace-only text (a safety
			// filter, finish_reason=length with zero output, a provider
			// bug) is not a successful render — retry it rather than
			// shipping blank content as if the LLM had produced prose.
			lastErr = fmt.Errorf("empty completion text")
		default:
			// Either nothing survived tag-stripping (the whole response was
			// a <think> block) or what's left still reads as deliberation
			// about the task, not an answer — treat exactly like an empty
			// completion: retry, and if every attempt leaks, Render/Repair
			// return an error and RunGates never even sees it, so the
			// caller's existing "Track A failed... falling back to
			// deterministic renderer" path ships the correct, grounded
			// Track B content instead of a chain-of-thought transcript.
			lastErr = fmt.Errorf("completion looked like leaked reasoning/chain-of-thought, not a finished answer")
		}

		// Check if context canceled before sleeping
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Backoff sleep before the next attempt — never after the last one,
		// which would otherwise block the caller for the final backoff
		// interval only to return the same error afterward.
		if i < attempts-1 {
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
