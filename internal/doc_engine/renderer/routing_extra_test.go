// Package renderer — routing_extra_test.go
//
// Whitebox coverage for v1 heuristic routing boundaries, the MaxWords
// repair path (stub provider, mirroring the mockProvider pattern from
// llm_actuator_test.go), and RenderMode defaulting.
package renderer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// ────────────────────────────────────────────────────────────────────────────
// RouteForFactSheet boundary cases
// ────────────────────────────────────────────────────────────────────────────

func TestExtraRouteEmptyFactSheet(t *testing.T) {
	// Zero-value sheet: no instruction, no payload → no deterministic
	// signal → preserve current behavior (LLM).
	if got := RouteForFactSheet(&config.FactSheet{}); got != RouteLLM {
		t.Errorf("empty FactSheet routed %q, want llm", got)
	}
	if est := EstimateTokens(&config.FactSheet{}); est != 0 {
		t.Errorf("empty FactSheet estimates %d tokens, want 0", est)
	}
}

func TestExtraRouteHugePayload(t *testing.T) {
	// A payload inflated across every GroundTruth dimension must trip the
	// deterministic floor even with a neutral instruction.
	var syms []config.SymbolFact
	for i := 0; i < 120; i++ {
		syms = append(syms, config.SymbolFact{
			FQN:       "pkg/mod.go::SymbolWithAVeryLongNameToInflateTheEstimate",
			Signature: "func SymbolWithAVeryLongNameToInflateTheEstimate(a int, b string) (string, error)",
			Doc:       "Doc comment padding that inflates the token estimate past the routing floor.",
		})
	}
	comments := map[string]string{}
	for i := 0; i < 40; i++ {
		comments["k"] = strings.Repeat("c", 200)
	}
	fs := &config.FactSheet{
		SectionInstruction:   "Current state of the module",
		PriorSectionMarkdown: strings.Repeat("prior ", 500),
		GroundTruth: config.GroundTruthPayload{
			AllSymbols:     syms,
			Symbols:        syms,
			DiagramMermaid: strings.Repeat("x", 4000),
			DocComments:    comments,
		},
	}
	if est := EstimateTokens(fs); est <= routingDeterministicTokenFloor {
		t.Fatalf("fixture too small to exercise the floor: %d", est)
	}
	if got := RouteForFactSheet(fs); got != RouteDeterministic {
		t.Errorf("huge neutral payload routed %q, want deterministic", got)
	}
}

func TestExtraRouteNarrativeVsTabularTie(t *testing.T) {
	// An instruction carrying BOTH a tabular phrase and a narrative verb
	// must resolve deterministic: tabular phrases are checked first, so
	// explicit table requests always win the tie.
	fs := &config.FactSheet{
		SectionInstruction: "Explain the architecture with a table of all exported types",
	}
	if got := RouteForFactSheet(fs); got != RouteDeterministic {
		t.Errorf("tabular+narrative tie routed %q, want deterministic", got)
	}
	// Case-insensitive tabular match.
	fs = &config.FactSheet{SectionInstruction: "LIST OF every public function"}
	if got := RouteForFactSheet(fs); got != RouteDeterministic {
		t.Errorf("uppercase tabular phrase routed %q, want deterministic", got)
	}
	// A narrative verb shields even a large payload from the floor.
	big := &config.FactSheet{
		SectionInstruction: "Explain why the cache exists",
		GroundTruth: config.GroundTruthPayload{
			CallFlow: make([]string, 500),
		},
	}
	if got := RouteForFactSheet(big); got != RouteLLM {
		t.Errorf("narrative instruction on a large payload routed %q, want llm", got)
	}
	// "why" as a substring must not count as narrative.
	fs = &config.FactSheet{SectionInstruction: "Document the highway fallback path"}
	if got := RouteForFactSheet(fs); got != RouteLLM {
		t.Errorf("substring 'why' routed %q, want llm", got)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// enforceMaxWords repair path (stub provider)
// ────────────────────────────────────────────────────────────────────────────

func extraWordy(n int) string {
	words := make([]string, n)
	for i := range words {
		words[i] = "word"
	}
	return strings.Join(words, " ")
}

func extraRepairOrchestrator(stub *mockProvider) *Orchestrator {
	cfg := DefaultLLMActuatorConfig(stub, "extra-test")
	cfg.BackoffSchedule = []time.Duration{time.Millisecond}
	// NewOrchestrator builds its own actuator; swap the fast-backoff
	// config in so repair retries stay instantaneous.
	orch := NewOrchestrator(OrchestratorOptions{Provider: stub, Model: "extra-test"})
	orch.actuator = NewLLMActuator(cfg)
	return orch
}

func TestExtraEnforceMaxWordsRepairSuccess(t *testing.T) {
	ctx := context.Background()
	sec := &config.SectionSpec{ID: "s", Title: "S", Instruction: "Explain things", Managed: true, MaxWords: 5}
	fs := &config.FactSheet{DocID: "d", SectionID: "s", SectionInstruction: "Explain things"}

	stub := &mockProvider{
		completeFunc: func(_ context.Context, _ provider.Request) (*provider.Response, error) {
			return &provider.Response{Text: "Tiny fixed body."}, nil
		},
	}
	orch := extraRepairOrchestrator(stub)
	outcome := SectionRenderOutcome{Content: extraWordy(10), RenderMode: "llm"}
	var warnings []string
	got := orch.enforceMaxWords(ctx, fs, sec, nil, outcome, &warnings)
	if stub.callCount == 0 {
		t.Fatal("over-cap LLM outcome must attempt one repair call")
	}
	if c := len(strings.Fields(got)); c > sec.MaxWords {
		t.Errorf("repaired content has %d words, want <= %d: %q", c, sec.MaxWords, got)
	}
	if len(warnings) == 0 || !strings.Contains(strings.Join(warnings, "\n"), "shortened to MaxWords cap") {
		t.Errorf("successful repair must warn shortened-to-cap, got %v", warnings)
	}
}

func TestExtraEnforceMaxWordsRepairFailsOpen(t *testing.T) {
	ctx := context.Background()
	sec := &config.SectionSpec{ID: "s", Title: "S", Instruction: "Explain things", Managed: true, MaxWords: 5}
	fs := &config.FactSheet{DocID: "d", SectionID: "s", SectionInstruction: "Explain things"}
	orig := extraWordy(10)

	// Repair answers over-cap anyway: the original ships with a warning —
	// content is never truncated (truncation would corrupt markdown).
	stub := &mockProvider{
		completeFunc: func(_ context.Context, _ provider.Request) (*provider.Response, error) {
			return &provider.Response{Text: extraWordy(30)}, nil
		},
	}
	orch := extraRepairOrchestrator(stub)
	var warnings []string
	got := orch.enforceMaxWords(ctx, fs, sec, nil,
		SectionRenderOutcome{Content: orig, RenderMode: "llm"}, &warnings)
	if got != orig {
		t.Errorf("failed repair must ship the original, got %q", got)
	}
	if len(warnings) == 0 || !strings.Contains(strings.Join(warnings, "\n"), "exceeds MaxWords cap") {
		t.Errorf("failed repair must warn exceeds-cap, got %v", warnings)
	}

	// Deterministic track never attempts repair: warning only, zero calls.
	stub2 := &mockProvider{}
	orch2 := extraRepairOrchestrator(stub2)
	var warnings2 []string
	got2 := orch2.enforceMaxWords(ctx, fs, sec, nil,
		SectionRenderOutcome{Content: orig, RenderMode: "deterministic"}, &warnings2)
	if got2 != orig {
		t.Errorf("deterministic over-cap must ship unchanged, got %q", got2)
	}
	if stub2.callCount != 0 {
		t.Errorf("deterministic track must not call the LLM, got %d calls", stub2.callCount)
	}
	if len(warnings2) == 0 {
		t.Error("deterministic over-cap must warn")
	}

	// Under-cap content is untouched with no warnings and no calls.
	stub3 := &mockProvider{}
	orch3 := extraRepairOrchestrator(stub3)
	var warnings3 []string
	short := "Just three words."
	if got3 := orch3.enforceMaxWords(ctx, fs, sec, nil,
		SectionRenderOutcome{Content: short, RenderMode: "llm"}, &warnings3); got3 != short {
		t.Errorf("under-cap content must pass through, got %q", got3)
	}
	if len(warnings3) != 0 || stub3.callCount != 0 {
		t.Errorf("under-cap content must warn nothing and call nothing: %v / %d calls", warnings3, stub3.callCount)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// RenderMode defaulting
// ────────────────────────────────────────────────────────────────────────────

func extraRenderDoc(target string) *config.DocSpec {
	return &config.DocSpec{
		ID:         "demo",
		TargetPath: target,
		Title:      "Demo",
		Purpose:    "RenderMode fixture",
		Audience:   "Developers",
		Scope:      config.ScopeRule{Paths: []string{"internal/demo/**"}},
	}
}

func extraRenderJob() sectionJob {
	return sectionJob{
		sec: config.SectionSpec{
			ID: "overview", Title: "Overview",
			Instruction: "Explain the demo module", Managed: true,
		},
		priorBody: "Old demo body.",
	}
}

func TestExtraRenderModeDefaults(t *testing.T) {
	ctx := context.Background()

	// With an LLM available, an untagged section defaults to llm.
	stub := &mockProvider{}
	orch := extraRepairOrchestrator(stub)
	res := orch.renderOneSection(ctx, t.TempDir(), extraRenderDoc("docs/demo.md"),
		extraRenderJob(), nil, nil, "hash", nil)
	if res.skipped {
		t.Fatalf("render skipped unexpectedly: %v", res.secWarnings)
	}
	if res.outcome.RenderMode != "llm" {
		t.Errorf("untagged section with provider rendered as %q, want llm", res.outcome.RenderMode)
	}

	// Without an LLM, the same section defaults to deterministic.
	det := NewOrchestrator(OrchestratorOptions{NoLLM: true})
	resDet := det.renderOneSection(ctx, t.TempDir(), extraRenderDoc("docs/demo.md"),
		extraRenderJob(), nil, nil, "hash", nil)
	if resDet.skipped {
		t.Fatalf("deterministic render skipped unexpectedly: %v", resDet.secWarnings)
	}
	if resDet.outcome.RenderMode != "deterministic" {
		t.Errorf("untagged section without provider rendered as %q, want deterministic", resDet.outcome.RenderMode)
	}

	// An explicit deterministic tag pins Track B even with a provider.
	stub2 := &mockProvider{}
	orch2 := extraRepairOrchestrator(stub2)
	pinned := &config.FactSheet{
		DocID: "d", SectionID: "s",
		SectionInstruction: "Explain the demo module",
		RenderMode:         "deterministic",
	}
	out, err := orch2.RenderSection(ctx, pinned, nil)
	if err != nil {
		t.Fatalf("pinned deterministic render failed: %v", err)
	}
	if out.RenderMode != "deterministic" {
		t.Errorf("pinned section rendered as %q, want deterministic", out.RenderMode)
	}
	if stub2.callCount != 0 {
		t.Errorf("pinned deterministic must not consume LLM calls, got %d", stub2.callCount)
	}
}
