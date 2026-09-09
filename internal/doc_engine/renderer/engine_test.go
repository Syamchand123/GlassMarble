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

func TestOrchestrator_FallbackOnLLMFailure(t *testing.T) {
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
	if err != nil {
		t.Fatalf("expected fallback to succeed, got error: %v", err)
	}

	if outcome.RenderMode != "deterministic" {
		t.Errorf("expected fallback to deterministic, got %q", outcome.RenderMode)
	}
	if !outcome.FallbackUsed {
		t.Errorf("expected FallbackUsed=true")
	}
	if !strings.Contains(outcome.Content, "`Bar`") {
		t.Errorf("fallback output missing symbol")
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

func TestOrchestrator_Gate4HardFailFallback(t *testing.T) {
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
	if err != nil {
		t.Fatalf("expected fallback to deterministic on Gate 4, got error: %v", err)
	}

	if outcome.RenderMode != "deterministic" {
		t.Errorf("expected fallback to deterministic mode, got %q", outcome.RenderMode)
	}
	if !outcome.FallbackUsed {
		t.Errorf("expected FallbackUsed=true on Gate 4 failure")
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
