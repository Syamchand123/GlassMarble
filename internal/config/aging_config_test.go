package config

import "testing"

// TestAgingConfigDefaults pins the built-in knowledge aging defaults.
func TestAgingConfigDefaults(t *testing.T) {
	c := DefaultAgingConfig()
	if !c.AgingEnabled() {
		t.Errorf("aging should default to enabled")
	}
	if c.CodeHalfLifeDays != 365 || c.DocsHalfLifeDays != 270 ||
		c.GitHalfLifeDays != 180 || c.LLMHalfLifeDays != 90 || c.DefaultHalfLifeDays != 180 {
		t.Errorf("unexpected half-life defaults: %+v", c)
	}
	if c.DeprecationToHistoricalDays != 180 || c.ExperimentalPromotionEvents != 3 {
		t.Errorf("unexpected transition thresholds: %+v", c)
	}
	if c.StaleGraceDaysValue() != 7 {
		t.Errorf("stale grace days = %d, want 7", c.StaleGraceDaysValue())
	}
}

// TestAgingConfigApplyDefaults verifies that a partially-populated "aging:"
// section falls back to defaults for the unset fields.
func TestAgingConfigApplyDefaults(t *testing.T) {
	disabled := false
	c := &AgingConfig{Enabled: &disabled, LLMHalfLifeDays: 45}
	c.ApplyDefaults()
	if c.AgingEnabled() {
		t.Errorf("explicit disabled must stay disabled")
	}
	if c.LLMHalfLifeDays != 45 {
		t.Errorf("explicit llm half-life = %d, want 45", c.LLMHalfLifeDays)
	}
	if c.CodeHalfLifeDays != DefaultAgingConfig().CodeHalfLifeDays {
		t.Errorf("code half-life = %d, want default", c.CodeHalfLifeDays)
	}
	if c.DeprecationToHistoricalDays != DefaultAgingConfig().DeprecationToHistoricalDays {
		t.Errorf("deprecation threshold = %d, want default", c.DeprecationToHistoricalDays)
	}
}

// TestAgingEnabledTriState covers the nil-vs-explicit semantics.
func TestAgingEnabledTriState(t *testing.T) {
	if !(*AgingConfig)(nil).AgingEnabled() {
		t.Errorf("nil config must default to enabled")
	}
	c := DefaultAgingConfig()
	if !c.AgingEnabled() {
		t.Errorf("default config must be enabled")
	}
	off := false
	c.Enabled = &off
	if c.AgingEnabled() {
		t.Errorf("explicit false must disable")
	}
}

// TestAgingConfigZeroValueFieldsAreSafe pins that a config constructed
// directly (bypassing ApplyDefaults) still cannot produce zero half-lives —
// the scorer guards against division by zero by falling back.
func TestAgingConfigZeroValueFieldsAreSafe(t *testing.T) {
	c := &AgingConfig{Enabled: boolPtr(true)}
	if c.CodeHalfLifeDays != 0 {
		t.Fatalf("precondition: code half-life should be zero")
	}
	// ApplyDefaults is the contract: every consumer must call it.
	c.ApplyDefaults()
	if c.CodeHalfLifeDays == 0 || c.GitHalfLifeDays == 0 || c.LLMHalfLifeDays == 0 {
		t.Errorf("ApplyDefaults must fill every half-life: %+v", c)
	}
}

// TestStaleGraceTriState pins the nil-vs-explicit-zero semantics: nil falls
// back to the default (7), an explicit 0 means "transition immediately".
func TestStaleGraceTriState(t *testing.T) {
	c := DefaultAgingConfig()
	if c.StaleGraceDaysValue() != 7 {
		t.Errorf("default stale grace = %d, want 7", c.StaleGraceDaysValue())
	}

	c.ApplyDefaults()
	if c.StaleGraceDaysValue() != 7 {
		t.Errorf("after ApplyDefaults stale grace = %d, want 7 (nil -> default)", c.StaleGraceDaysValue())
	}

	immediate := 0
	c.StaleGraceDays = &immediate
	if c.StaleGraceDaysValue() != 0 {
		t.Errorf("explicit 0 must stay 0 (immediate transitions), got %d", c.StaleGraceDaysValue())
	}

	if got := (*AgingConfig)(nil).StaleGraceDaysValue(); got != 7 {
		t.Errorf("nil config stale grace = %d, want 7", got)
	}
}

func boolPtr(b bool) *bool {
	return &b
}
