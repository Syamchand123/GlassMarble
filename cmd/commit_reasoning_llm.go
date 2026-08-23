package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/aiconfig"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/commit_reasoning"
)

// newIntentLLM builds the optional commit reasoning Level-3 intent backend on top of
// the BYOK AI configuration (same config `gmb why` and `gmb ai` use).
// It returns nil when AI is not configured or unusable, so the pipeline
// silently degrades to the deterministic keyword/structural classifier —
// the LLM is a last-resort level, never a hard dependency
// (v2_master_implementaion_plan.md §6.4, evidence source reliability
// SourceLLM = 0.65).
func newIntentLLM(rootDir string) commit_reasoning.IntentLLMFunc {
	cfg, err := aiconfig.LoadForDir(rootDir, aiconfig.Config{})
	if err != nil || cfg == nil {
		return nil
	}
	engine, err := ai_engine.New(cfg, rootDir)
	if err != nil || engine == nil || engine.Provider == nil {
		return nil
	}
	llm := engine.Provider

	const prompt = `Classify the intent of the following software commit into exactly one category:
ADD_FEATURE, FIX_BUG, REFACTOR, PERFORMANCE, SECURITY, TEST, DOCS, INFRASTRUCTURE, DEPENDENCY_UPDATE, or UNKNOWN.
Answer with only the category keyword, nothing else.

Commit subject: %s
Commit body: %s
Pull request description: %s`

	return func(ctx context.Context, subject, body, prDescription string) (commit_reasoning.IntentResult, error) {
		resp, err := llm.Complete(ctx, provider.Request{
			Model:           cfg.Model,
			System:          "You classify commit intents for an architecture intelligence tool. Reply with a single category keyword.",
			Messages:        []provider.Message{{Role: provider.RoleUser, Content: fmt.Sprintf(prompt, subject, body, prDescription)}},
			Temperature:     cfg.Temperature,
			MaxOutputTokens: cfg.MaxOutputTokens,
		})
		if err != nil {
			return commit_reasoning.IntentResult{}, err
		}
		intent := commit_reasoning.Intent(strings.ToUpper(strings.TrimSpace(resp.Text)))
		if !knownIntent(intent) {
			return commit_reasoning.IntentResult{}, fmt.Errorf("LLM returned unrecognized intent %q", resp.Text)
		}
		return commit_reasoning.IntentResult{
			Intent:     intent,
			Confidence: 0.8,
			Excerpt:    strings.TrimSpace(resp.Text),
		}, nil
	}
}

// knownIntent reports whether an LLM response maps onto the intent taxonomy.
func knownIntent(i commit_reasoning.Intent) bool {
	switch i {
	case commit_reasoning.IntentAddFeature, commit_reasoning.IntentFixBug,
		commit_reasoning.IntentRefactor, commit_reasoning.IntentPerformance,
		commit_reasoning.IntentSecurity, commit_reasoning.IntentTest,
		commit_reasoning.IntentDocs, commit_reasoning.IntentInfrastructure,
		commit_reasoning.IntentDependencyUpdate, commit_reasoning.IntentUnknown:
		return true
	}
	return false
}
