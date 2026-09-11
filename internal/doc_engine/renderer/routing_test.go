package renderer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

func TestRouteForFactSheetTabular(t *testing.T) {
	for _, instr := range []string{
		"Table of exported types, methods, and sentinel errors",
		"List of all configuration variables",
		"Enumerate every HTTP handler in scope",
		"API reference for the module",
	} {
		fs := &config.FactSheet{SectionInstruction: instr}
		if got := RouteForFactSheet(fs); got != RouteDeterministic {
			t.Errorf("instruction %q routed %q, want deterministic", instr, got)
		}
	}
}

func TestRouteForFactSheetNarrative(t *testing.T) {
	for _, instr := range []string{
		"Explain why the worker pool exists and guide operators through recovery",
		"Describe the architecture and compare it with the previous design",
		"Tutorial: walk through the onboarding lesson",
	} {
		fs := &config.FactSheet{SectionInstruction: instr}
		if got := RouteForFactSheet(fs); got != RouteLLM {
			t.Errorf("instruction %q routed %q, want llm", instr, got)
		}
	}
}

func TestRouteForFactSheetDefaultsAndNil(t *testing.T) {
	if got := RouteForFactSheet(nil); got != RouteLLM {
		t.Errorf("nil FactSheet routed %q, want llm", got)
	}
	// No signal at all → LLM (preserve current behavior).
	fs := &config.FactSheet{SectionInstruction: "Document functions."}
	if got := RouteForFactSheet(fs); got != RouteLLM {
		t.Errorf("neutral section routed %q, want llm", got)
	}
	// "why" inside other words must not route.
	fs = &config.FactSheet{SectionInstruction: "Document the anyway fallback path."}
	if got := RouteForFactSheet(fs); got != RouteLLM {
		t.Errorf("substring 'why' routed %q, want llm", got)
	}
}

func TestRouteForFactSheetLargeDump(t *testing.T) {
	// Large non-narrative payload → deterministic (tables don't need prose).
	var syms []config.SymbolFact
	for i := 0; i < 200; i++ {
		syms = append(syms, config.SymbolFact{
			FQN:       "pkg.Symbol000000000000000000000000000000000000",
			Signature: "func Symbol00000000000000000000000000000000() error with extra padding text",
			Doc:       "Doc comment padding to inflate the token estimate well past the floor.",
		})
	}
	fs := &config.FactSheet{
		SectionInstruction: "Current state of the module",
		GroundTruth:        config.GroundTruthPayload{AllSymbols: syms},
	}
	if est := EstimateTokens(fs); est <= routingDeterministicTokenFloor {
		t.Fatalf("fixture too small to exercise the floor: %d", est)
	}
	if got := RouteForFactSheet(fs); got != RouteDeterministic {
		t.Errorf("large dump routed %q, want deterministic", got)
	}
	// Same dump with a narrative instruction stays on the LLM.
	fs.SectionInstruction = "Explain the module architecture"
	if got := RouteForFactSheet(fs); got != RouteLLM {
		t.Errorf("narrative large section routed %q, want llm", got)
	}
}

func TestLLMActuatorRoutesToDeterministic(t *testing.T) {
	mock := &mockProvider{}
	cfg := DefaultLLMActuatorConfig(mock, "gpt-4o")
	cfg.BackoffSchedule = []time.Duration{1 * time.Millisecond}
	act := NewLLMActuator(cfg)

	fs := &config.FactSheet{
		SectionID:          "ref",
		SectionInstruction: "Table of exported types and methods",
	}
	_, err := act.Render(context.Background(), fs)
	if err == nil || !strings.Contains(err.Error(), "routed to deterministic") {
		t.Fatalf("expected routed-to-deterministic error, got %v", err)
	}
	if mock.callCount != 0 {
		t.Errorf("routed sections must not consume LLM calls, got %d", mock.callCount)
	}
}
