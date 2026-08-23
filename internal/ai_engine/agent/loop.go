// Package agent implements the GlassMarble AI agent: the tool-calling loop
// that lets the model query the AKG knowledge graph, read source code, and
// answer grounded questions about a repository.
package agent

import (
	"context"
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/tools"
)

// Event is emitted to the OnEvent callback as the loop makes progress.
type Event struct {
	Turn        int
	Type        string // "stream" | "tool_call" | "tool_result" | "answer"
	ToolName    string
	ToolArgs    string
	OK          bool
	ResultBytes int
	// Delta carries a streamed text fragment for Type "stream".
	Delta string
}

// ToolTrace records one executed tool call for reporting.
type ToolTrace struct {
	ID    string
	Name  string
	Args  string
	OK    bool
	Bytes int
}

// Result is the outcome of an agent run.
type Result struct {
	Text             string
	Usage            provider.Usage
	CostUSD          float64
	CostEstimated    bool
	Turns            int
	ReachedTurnLimit bool
	// StoppedReason is why the loop ended early: "" (answered),
	// "turn_limit", "token_budget", or "cost_budget".
	StoppedReason string
	ToolCalls     []ToolTrace
	// Messages is the full conversation transcript (including tool rounds),
	// suitable for continuing the conversation in a later call.
	Messages []provider.Message
}

// Agent runs the tool-calling loop against a provider.
type Agent struct {
	Provider        provider.Provider
	Model           string
	System          string
	Tools           []tools.Tool
	Env             *tools.Env
	MaxTurns        int
	MaxResultBytes  int
	Temperature     *float64
	MaxOutputTokens int
	OnEvent         func(Event)
	// OnStream receives text deltas of every completion (tool rounds and the
	// final answer alike) when the provider streams.
	OnStream func(string)
	// MaxTotalTokens caps the summed prompt+completion tokens across the
	// whole run; 0 means unlimited.
	MaxTotalTokens int
	// MaxCostUSD caps the estimated spend of the whole run; 0 means
	// unlimited. The cap is enforced only for priced models.
	MaxCostUSD float64
}

func (a *Agent) emit(ev Event) {
	if a.OnEvent != nil {
		a.OnEvent(ev)
	}
}

func (a *Agent) stream(delta string) {
	if a.OnStream != nil {
		a.OnStream(delta)
	}
	a.emit(Event{Type: "stream", Delta: delta})
}

// Run executes the loop. history carries prior conversation turns; query is
// appended as the new user message. The loop ends when the model answers
// without tool calls, when MaxTurns tool rounds are exhausted, or when a
// configured token/cost budget is exceeded.
func (a *Agent) Run(ctx context.Context, query string, history []provider.Message) (*Result, error) {
	maxTurns := a.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 1
	}

	messages := make([]provider.Message, 0, len(history)+2+maxTurns*2)
	messages = append(messages, history...)
	messages = append(messages, provider.Message{Role: provider.RoleUser, Content: query})

	res := &Result{}
	dispatcher := &Dispatcher{Tools: a.Tools, MaxResultBytes: a.MaxResultBytes}

	for turn := 1; turn <= maxTurns; turn++ {
		req := provider.Request{
			Model:           a.Model,
			System:          a.System,
			Messages:        messages,
			Temperature:     a.Temperature,
			MaxOutputTokens: a.MaxOutputTokens,
		}
		if a.OnStream != nil {
			req.OnStream = a.stream
		}
		if len(a.Tools) > 0 {
			req.Tools = make([]provider.Tool, 0, len(a.Tools))
			for i := range a.Tools {
				req.Tools = append(req.Tools, a.Tools[i].Decl())
			}
		}

		// Pre-flight budget check: estimate the prompt tokens of the next
		// request and stop before spending if a cap would be exceeded.
		if !a.budgetAllows(res, req) {
			break
		}

		resp, err := a.Provider.Complete(ctx, req)
		if err != nil {
			return res, fmt.Errorf("completion failed on turn %d: %w", turn, err)
		}
		res.Usage = addUsage(res.Usage, resp.Usage)
		if cost, known := provider.EstimateCost(a.Model, resp.Usage); known {
			res.CostUSD += cost
			res.CostEstimated = true
		}
		res.Turns = turn

		// Post-hoc budget check on the actual provider-reported usage.
		if !a.budgetAllows(res, provider.Request{}) {
			break
		}

		if len(resp.ToolCalls) == 0 {
			messages = append(messages, provider.Message{Role: provider.RoleAssistant, Content: resp.Text})
			res.Text = resp.Text
			res.Messages = messages
			a.emit(Event{Turn: turn, Type: "answer"})
			return res, nil
		}

		// Tool round: record the assistant turn, execute, append results.
		messages = append(messages, provider.Message{
			Role:      provider.RoleAssistant,
			Content:   resp.Text,
			ToolCalls: resp.ToolCalls,
		})
		traceByID := make(map[string]int, len(resp.ToolCalls))
		for _, tc := range resp.ToolCalls {
			traceByID[tc.ID] = len(res.ToolCalls)
			res.ToolCalls = append(res.ToolCalls, ToolTrace{ID: tc.ID, Name: tc.Name, Args: tc.Arguments})
			a.emit(Event{Turn: turn, Type: "tool_call", ToolName: tc.Name, ToolArgs: tc.Arguments})
		}
		results := dispatcher.Dispatch(ctx, a.Env, resp.ToolCalls)
		for _, r := range results {
			if i, ok := traceByID[r.ID]; ok {
				res.ToolCalls[i].OK = !r.IsError
				res.ToolCalls[i].Bytes = len(r.Content)
			}
			a.emit(Event{Turn: turn, Type: "tool_result", ToolName: r.Name, OK: !r.IsError, ResultBytes: len(r.Content)})
		}
		messages = append(messages, provider.Message{Role: provider.RoleTool, ToolResults: results})
	}

	if res.StoppedReason == "" {
		res.StoppedReason = "turn_limit"
		res.ReachedTurnLimit = true
	}
	res.Messages = messages
	a.emit(Event{Turn: maxTurns, Type: "answer"})
	return res, nil
}

// budgetAllows reports whether a run may continue under the configured
// token and cost caps, given the usage so far and the next request. With a
// zero-value request only the already-accumulated usage is checked. When the
// check fails the run's StoppedReason is set and false is returned.
func (a *Agent) budgetAllows(res *Result, next provider.Request) bool {
	if a.MaxTotalTokens > 0 && res.Usage.TotalTokens > a.MaxTotalTokens {
		res.StoppedReason = "token_budget"
		return false
	}
	if a.MaxCostUSD > 0 && res.CostEstimated && res.CostUSD > a.MaxCostUSD {
		res.StoppedReason = "cost_budget"
		return false
	}
	if a.MaxTotalTokens > 0 {
		if projected := res.Usage.TotalTokens + estimatePromptTokens(next); projected > a.MaxTotalTokens {
			res.StoppedReason = "token_budget"
			return false
		}
	}
	if a.MaxCostUSD > 0 {
		if price, known := provider.PricingFor(a.Model); known {
			promptEst := estimatePromptTokens(next)
			if res.CostUSD+float64(promptEst)/1e6*price.InputPerM > a.MaxCostUSD {
				res.StoppedReason = "cost_budget"
				return false
			}
		}
	}
	return true
}

// estimatePromptTokens approximates the prompt size of a request at ~4 chars
// per token. It is deliberately rough: guardrails treat it as a floor for the
// pre-flight check, and the authoritative counts come from the provider.
func estimatePromptTokens(req provider.Request) int {
	n := len(req.Model) + len(req.System)
	for _, m := range req.Messages {
		n += len(m.Content)
		for _, tc := range m.ToolCalls {
			n += len(tc.Name) + len(tc.Arguments)
		}
		for _, tr := range m.ToolResults {
			n += len(tr.Content)
		}
	}
	return n / 4
}

func addUsage(a, b provider.Usage) provider.Usage {
	return provider.Usage{
		PromptTokens:     a.PromptTokens + b.PromptTokens,
		CompletionTokens: a.CompletionTokens + b.CompletionTokens,
		TotalTokens:      a.TotalTokens + b.TotalTokens,
	}
}
