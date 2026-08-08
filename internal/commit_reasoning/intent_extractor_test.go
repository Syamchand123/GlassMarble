package commit_reasoning

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/evidence"
	"github.com/Syamchand123/GlassMarble/internal/git"
)

func TestExtract_KeywordLevels(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		body    string
		want    Intent
	}{
		{"fix bug", "Fix broken login redirect", "", IntentFixBug},
		{"fix singular", "fixes #42 crash in parser", "", IntentFixBug},
		{"add feature", "Add payment webhook endpoint", "", IntentAddFeature},
		{"implement feature", "Implement user search API", "", IntentAddFeature},
		{"refactor", "Refactor auth module", "", IntentRefactor},
		{"performance", "Optimize query performance", "", IntentPerformance},
		{"security", "Sanitize user input against injection", "", IntentSecurity},
		{"tests", "Add unit tests for billing", "", IntentTest},
		{"docs", "Document the CLI commands", "", IntentDocs},
		{"infra", "Bump CI pipeline to Go 1.26", "", IntentInfrastructure},
		{"unknown", "Rotate left on Wednesday", "", IntentUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewIntentExtractor().Extract(context.Background(), &git.CommitMeta{Subject: tt.subject, Body: tt.body}, "")
			if got.Intent != tt.want {
				t.Errorf("Extract(%q) = %v, want %v", tt.subject, got.Intent, tt.want)
			}
		})
	}
}

func TestExtract_KeywordSourceAndConfidence(t *testing.T) {
	got := NewIntentExtractor().Extract(context.Background(), &git.CommitMeta{Subject: "Fix auth bug"}, "")
	if got.Source != evidence.SourceHeuristic {
		t.Errorf("Source = %v, want %v (keyword intent must be INFERENCE, not explicit)", got.Source, evidence.SourceHeuristic)
	}
	if got.Level != IntentLevelKeyword {
		t.Errorf("Level = %v, want keyword", got.Level)
	}
	if got.Confidence != 0.75 {
		t.Errorf("Confidence = %v, want 0.75", got.Confidence)
	}
	if got.Excerpt == "" {
		t.Error("Excerpt must name the matching evidence")
	}
}

func TestExtract_StructuralFileLevel(t *testing.T) {
	meta := &git.CommitMeta{
		Subject: "wip",
		Files:   []string{"go.mod", "go.sum"},
	}
	got := NewIntentExtractor().Extract(context.Background(), meta, "")
	if got.Intent != IntentDependencyUpdate {
		t.Errorf("Intent = %v, want DEPENDENCY_UPDATE", got.Intent)
	}
	if got.Source != evidence.SourceGit {
		t.Errorf("Source = %v, want git (structural = explicit)", got.Source)
	}
	if got.Level != IntentLevelStructural {
		t.Errorf("Level = %v, want structural", got.Level)
	}
}

func TestExtract_StructuralRequiresAllFilesMatch(t *testing.T) {
	meta := &git.CommitMeta{
		Subject: "bump deps",
		Files:   []string{"go.mod", "internal/pay/pay.go"},
	}
	got := NewIntentExtractor().Extract(context.Background(), meta, "")
	if got.Intent == IntentDependencyUpdate {
		t.Error("mixed change must not classify as DEPENDENCY_UPDATE")
	}
}

func TestExtract_CasePreservedExcerpt(t *testing.T) {
	meta := &git.CommitMeta{Subject: "Sanitize the OAuth token flow"}
	got := NewIntentExtractor().Extract(context.Background(), meta, "")
	if !strings.Contains(got.Excerpt, "Sanitize") {
		t.Errorf("excerpt must preserve original casing, got %q", got.Excerpt)
	}
	if got.Intent != IntentSecurity {
		t.Errorf("Intent = %v, want SECURITY (OAuth)", got.Intent)
	}
}

func TestExtract_DeterministicWinner(t *testing.T) {
	meta := &git.CommitMeta{Subject: "Refactor and fix the auth service performance"}
	first := NewIntentExtractor().Extract(context.Background(), meta, "")
	for i := 0; i < 20; i++ {
		got := NewIntentExtractor().Extract(context.Background(), meta, "")
		if got.Intent != first.Intent || got.Excerpt != first.Excerpt {
			t.Fatalf("run %d disagreed: %+v vs %+v", i, got, first)
		}
	}
}

func TestExtract_LLMLevel(t *testing.T) {
	llmCalled := false
	ext := NewIntentExtractor(WithLLM(func(ctx context.Context, subject, body, pr string) (IntentResult, error) {
		llmCalled = true
		if subject != "bizarrely worded change" {
			t.Errorf("LLM got subject %q", subject)
		}
		return IntentResult{Intent: IntentRefactor, Confidence: 0.8, Excerpt: "llm said refactor"}, nil
	}))
	got := ext.Extract(context.Background(), &git.CommitMeta{Subject: "bizarrely worded change"}, "")
	if !llmCalled {
		t.Fatal("LLM backend was not called")
	}
	if got.Intent != IntentRefactor {
		t.Errorf("Intent = %v, want REFACTOR from LLM", got.Intent)
	}
	if got.Source != evidence.SourceLLM {
		t.Errorf("Source = %v, want llm", got.Source)
	}
	if got.Level != IntentLevelLLM || got.Confidence != 0.6 {
		t.Errorf("LLM level/confidence = %v/%v, want 3/0.6", got.Level, got.Confidence)
	}
}

func TestExtract_LLMFallbackOnError(t *testing.T) {
	ext := NewIntentExtractor(WithLLM(func(ctx context.Context, subject, body, pr string) (IntentResult, error) {
		return IntentResult{}, errors.New("backend down")
	}))
	got := ext.Extract(context.Background(), &git.CommitMeta{Subject: "Fix the bug"}, "")
	if got.Intent != IntentFixBug {
		t.Errorf("must fall back to keyword level on LLM error, got %v", got.Intent)
	}
	if got.Source != evidence.SourceHeuristic {
		t.Errorf("fallback source = %v, want heuristic", got.Source)
	}
}

func TestExtract_NilMeta(t *testing.T) {
	got := NewIntentExtractor().Extract(context.Background(), nil, "")
	if got.Intent != IntentUnknown {
		t.Errorf("nil meta must classify UNKNOWN, got %v", got.Intent)
	}
}
