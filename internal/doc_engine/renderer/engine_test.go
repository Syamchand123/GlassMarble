package renderer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
)

func TestOrchestrator_NoLLM(t *testing.T) {
	mock := &mockProvider{}
	orch := NewOrchestrator(OrchestratorOptions{
		NoLLM:    true,
		Provider: mock,
	})

	fs := &config.FactSheet{
		DocID:     "docs/test.md",
		SectionID: "sec",
		GroundTruth: config.GroundTruthPayload{
			Symbols: []config.SymbolFact{
				{FQN: "pkg.Foo", Kind: "func"},
			},
		},
	}

	outcome, err := orch.RenderSection(context.Background(), fs, nil)
	if err != nil {
		t.Fatalf("RenderSection failed: %v", err)
	}

	if outcome.RenderMode != "deterministic" {
		t.Errorf("expected deterministic render mode, got %q", outcome.RenderMode)
	}
	if mock.callCount != 0 {
		t.Errorf("expected 0 LLM calls with NoLLM=true, got %d", mock.callCount)
	}
	if !strings.Contains(outcome.Content, "<!-- gmb:mode:deterministic -->") {
		t.Errorf("missing deterministic mode tag")
	}
}

// TestNewOrchestrator_HonorsConfiguredMaxOutputTokensAndTemperature guards
// against a regression where DefaultLLMActuatorConfig's hardcoded budget
// (300 output tokens, temperature 0.0) was always used regardless of what
// OrchestratorOptions carried — silently discarding whatever the user
// configured in their AI provider's ai.yaml (commonly 8192+ tokens). A
// budget that small is routinely exhausted by a reasoning model's own
// chain-of-thought before it ever reaches a final answer, producing
// responses truncated mid-thought.
func TestNewOrchestrator_HonorsConfiguredMaxOutputTokensAndTemperature(t *testing.T) {
	mock := &mockProvider{}
	temp := 0.7
	orch := NewOrchestrator(OrchestratorOptions{
		Provider:        mock,
		MaxOutputTokens: 8192,
		Temperature:     &temp,
	})

	fs := &config.FactSheet{DocID: "d", SectionID: "s"}
	if _, err := orch.RenderSection(context.Background(), fs, nil); err != nil {
		t.Fatalf("RenderSection failed: %v", err)
	}

	if mock.lastRequest.MaxOutputTokens != 8192 {
		t.Errorf("expected MaxOutputTokens 8192, got %d", mock.lastRequest.MaxOutputTokens)
	}
	if mock.lastRequest.Temperature == nil || *mock.lastRequest.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", mock.lastRequest.Temperature)
	}
}

// TestNewOrchestrator_DefaultsWhenUnconfigured confirms omitting
// MaxOutputTokens/Temperature in OrchestratorOptions still falls back to
// DefaultLLMActuatorConfig's built-in defaults, not zero values.
func TestNewOrchestrator_DefaultsWhenUnconfigured(t *testing.T) {
	mock := &mockProvider{}
	orch := NewOrchestrator(OrchestratorOptions{Provider: mock})

	fs := &config.FactSheet{DocID: "d", SectionID: "s"}
	if _, err := orch.RenderSection(context.Background(), fs, nil); err != nil {
		t.Fatalf("RenderSection failed: %v", err)
	}

	defaults := DefaultLLMActuatorConfig(mock, "")
	if mock.lastRequest.MaxOutputTokens != defaults.MaxOutputTokens {
		t.Errorf("expected default MaxOutputTokens %d, got %d", defaults.MaxOutputTokens, mock.lastRequest.MaxOutputTokens)
	}
	if mock.lastRequest.Temperature == nil || *mock.lastRequest.Temperature != defaults.Temperature {
		t.Errorf("expected default temperature %v, got %v", defaults.Temperature, mock.lastRequest.Temperature)
	}
}

// TestOrchestrator_LLMFailureReturnsErrorNoFallback guards the mandatory-LLM
// contract: when Track A fails (after retries), RenderSection must return
// an error and produce no content at all — never silently substitute the
// deterministic renderer's raw fact tables as if they were the finished,
// human-written documentation. That silent substitution was the original
// design; it is deliberately gone. The explicit opts.NoLLM opt-out (a
// separate, already-passing path — see TestOrchestrator_NoLLM) is the only
// way to get the deterministic renderer's output now.
func TestOrchestrator_LLMFailureReturnsErrorNoFallback(t *testing.T) {
	mock := &mockProvider{
		completeFunc: func(ctx context.Context, req provider.Request) (*provider.Response, error) {
			return nil, errors.New("503 Service Unavailable")
		},
	}

	orch := NewOrchestrator(OrchestratorOptions{
		NoLLM:    false,
		Provider: mock,
		Model:    "gpt-4o",
	})
	// Fast backoff for test
	if orch.actuator != nil {
		orch.actuator.cfg.BackoffSchedule = nil
	}

	fs := &config.FactSheet{
		DocID:     "docs/test.md",
		SectionID: "sec",
		GroundTruth: config.GroundTruthPayload{
			Symbols: []config.SymbolFact{
				{FQN: "pkg.Bar", Kind: "func"},
			},
		},
	}

	outcome, err := orch.RenderSection(context.Background(), fs, nil)
	if err == nil {
		t.Fatalf("expected an error on LLM failure, got success with outcome: %+v", outcome)
	}
	if outcome.Content != "" {
		t.Errorf("expected no content on failure, got: %q", outcome.Content)
	}
}

// TestOrchestrator_NoProviderConfiguredReturnsError guards the other half
// of the mandatory-LLM contract: with no actuator at all (no provider
// resolved) and NoLLM not set, RenderSection must refuse rather than
// silently rendering the deterministic tables — this is the engine-level
// safety net behind cmd/doc.go's own upfront ensureLLMReady check, for any
// caller that reaches the orchestrator directly.
func TestOrchestrator_NoProviderConfiguredReturnsError(t *testing.T) {
	orch := NewOrchestrator(OrchestratorOptions{NoLLM: false, Provider: nil})
	fs := &config.FactSheet{DocID: "d", SectionID: "s"}

	_, err := orch.RenderSection(context.Background(), fs, nil)
	if err == nil {
		t.Fatal("expected an error with no LLM provider configured and NoLLM unset")
	}
}

func TestOrchestrator_RepairRetryOnGateFailure(t *testing.T) {
	callIdx := 0
	mock := &mockProvider{
		completeFunc: func(ctx context.Context, req provider.Request) (*provider.Response, error) {
			callIdx++
			if callIdx == 1 {
				// Initial call returns unclosed code block (Gate 1 fails)
				return &provider.Response{
					Text:  "```go\nfunc Hallucinated() {\n",
					Usage: provider.Usage{TotalTokens: 50},
				}, nil
			}
			// Repair turn returns valid clean markdown referencing known symbol
			return &provider.Response{
				Text:  "Use `Foo` to execute operations.",
				Usage: provider.Usage{TotalTokens: 60},
			}, nil
		},
	}

	orch := NewOrchestrator(OrchestratorOptions{
		NoLLM:    false,
		Provider: mock,
		Model:    "gpt-4o",
	})

	fs := &config.FactSheet{
		DocID:     "docs/test.md",
		SectionID: "sec",
		GroundTruth: config.GroundTruthPayload{
			Symbols: []config.SymbolFact{
				{FQN: "pkg.Foo", Kind: "func"},
			},
		},
	}

	outcome, err := orch.RenderSection(context.Background(), fs, nil)
	if err != nil {
		t.Fatalf("RenderSection failed: %v", err)
	}

	if outcome.RenderMode != "llm" {
		t.Errorf("expected llm mode after successful repair, got %q", outcome.RenderMode)
	}
	if !outcome.RepairUsed {
		t.Errorf("expected RepairUsed=true")
	}
	if outcome.TokensUsed != 110 {
		t.Errorf("expected combined tokens 110, got %d", outcome.TokensUsed)
	}
}

// TestOrchestrator_Gate4HardFailReturnsError guards the mandatory-LLM
// contract for the one hard-fail gate: a secret pattern in the model's
// output must never retry (Gate 4 is deliberately not repairable — see
// renderTrackA) and, under the current no-fallback design, must return an
// error rather than silently substituting the deterministic tables.
func TestOrchestrator_Gate4HardFailReturnsError(t *testing.T) {
	mock := &mockProvider{
		completeFunc: func(ctx context.Context, req provider.Request) (*provider.Response, error) {
			// LLM output contains an AWS key (Gate 4 hard fail)
			return &provider.Response{
				Text: "Here is your key: AKIAIOSFODNN7EXAMPLE",
			}, nil
		},
	}

	orch := NewOrchestrator(OrchestratorOptions{
		NoLLM:    false,
		Provider: mock,
		Model:    "gpt-4o",
	})

	fs := &config.FactSheet{
		DocID:     "docs/sec.md",
		SectionID: "sec",
		GroundTruth: config.GroundTruthPayload{
			Symbols: []config.SymbolFact{
				{FQN: "pkg.Safe", Kind: "func"},
			},
		},
	}

	outcome, err := orch.RenderSection(context.Background(), fs, nil)
	if err == nil {
		t.Fatalf("expected an error on Gate 4 secret detection, got success with outcome: %+v", outcome)
	}
	if outcome.Content != "" {
		t.Errorf("expected no content shipped on a secret-scan hard failure, got: %q", outcome.Content)
	}
}

func TestOrchestrator_ProcessDocument_EndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	storageDir := filepath.Join(tempDir, ".glassmarble")
	_ = os.MkdirAll(storageDir, 0755)

	sm := storage.NewStateManager(storageDir)

	doc := &config.DocSpec{
		ID:         "architecture",
		TargetPath: "docs/architecture.md",
		Title:      "System Architecture",
		Purpose:    "Guide to system architecture",
		Audience:   "Core maintainers",
		Mode:       config.ModeManagedSections,
		Sections: []config.SectionSpec{
			{
				ID:          "overview",
				Title:       "Overview",
				Instruction: "System high-level overview.",
				Managed:     true,
			},
			{
				ID:      "human-notes",
				Title:   "Human Notes",
				Managed: false, // Unmanaged section — must not be touched
			},
		},
	}

	orch := NewOrchestrator(OrchestratorOptions{
		NoLLM: true,
	})

	changed, _, warnings, err := orch.ProcessDocument(
		context.Background(),
		tempDir,
		doc,
		[]string{"overview"},
		nil,
		nil,
		sm,
		"commit123",
	)

	if err != nil {
		t.Fatalf("ProcessDocument failed: %v (warnings: %v)", err, warnings)
	}
	if !changed {
		t.Errorf("expected changed=true on initial write")
	}

	// Verify file on disk
	filePath := filepath.Join(tempDir, "docs/architecture.md")
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading created file: %v", err)
	}
	content := string(contentBytes)

	if !strings.Contains(content, "<!-- gmb:begin:overview -->") {
		t.Errorf("missing begin anchor in output")
	}
	if !strings.Contains(content, "<!-- gmb:mode:deterministic -->") {
		t.Errorf("missing deterministic mode tag inside managed zone")
	}
	if !strings.Contains(content, "<!-- gmb:end:overview -->") {
		t.Errorf("missing end anchor in output")
	}

	// Verify state manager updated
	state, err := sm.Load()
	if err != nil {
		t.Fatalf("loading state: %v", err)
	}
	ds, ok := state.Documents["docs/architecture.md"]
	if !ok {
		t.Fatalf("state does not contain docs/architecture.md")
	}
	if ds.LastUpdatedCommit != "commit123" {
		t.Errorf("unexpected commit in state: %s", ds.LastUpdatedCommit)
	}
}
