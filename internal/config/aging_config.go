package config

// AgingConfig controls the knowledge aging layer
// (v2_master_implementaion_plan.md §9). It is the single canonical
// definition of the "aging:" section in .glassmarble/config.yaml.
//
// It lives in the config package (not in knowledge_aging) for the same
// reason LearningConfig, FusionConfig and IntelligenceConfig do: the config
// package is a leaf that every phase imports; phase packages consume config
// types, they do not define them.
//
// All fields are optional; zero values fall back to DefaultAgingConfig via
// ApplyDefaults.
type AgingConfig struct {
	// Enabled turns the aging pass on/off. Tri-state: nil means the
	// default (true) — a plain bool cannot distinguish "unset" from an
	// explicit "false".
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`

	// CodeHalfLifeDays is the freshness half-life for claims whose primary
	// evidence source is code or a user correction — the most durable
	// knowledge (default 365).
	CodeHalfLifeDays int `json:"code_half_life_days,omitempty" yaml:"code_half_life_days,omitempty"`

	// DocsHalfLifeDays is the half-life for documentation-sourced claims
	// (docs, PR descriptions, issues) — durable but drifts as docs rot
	// (default 270).
	DocsHalfLifeDays int `json:"docs_half_life_days,omitempty" yaml:"docs_half_life_days,omitempty"`

	// GitHalfLifeDays is the half-life for git-history and
	// rule/heuristic-derived claims (default 180).
	GitHalfLifeDays int `json:"git_half_life_days,omitempty" yaml:"git_half_life_days,omitempty"`

	// LLMHalfLifeDays is the half-life for LLM-inferred claims — the
	// fastest-decaying bucket (default 90).
	LLMHalfLifeDays int `json:"llm_half_life_days,omitempty" yaml:"llm_half_life_days,omitempty"`

	// DefaultHalfLifeDays is the fallback half-life for evidence sources
	// with no explicit bucket (default 180).
	DefaultHalfLifeDays int `json:"default_half_life_days,omitempty" yaml:"default_half_life_days,omitempty"`

	// DeprecationToHistoricalDays is how long a component must stay
	// DEPRECATED (no observation in memory) before it transitions to
	// HISTORICAL (default 180).
	DeprecationToHistoricalDays int `json:"deprecation_to_historical_days,omitempty" yaml:"deprecation_to_historical_days,omitempty"`

	// ExperimentalPromotionEvents is the minimum number of confirming
	// events an EXPERIMENTAL component needs before it is promoted to
	// CURRENT (default 3).
	ExperimentalPromotionEvents int `json:"experimental_promotion_events,omitempty" yaml:"experimental_promotion_events,omitempty"`

	// StaleGraceDays is how long a component absent from the current
	// architecture snapshot must stay unobserved in memory before aging
	// may transition it away from CURRENT / EXPERIMENTAL. It is the
	// deterministic guard against transient absence (e.g. a watch-mode
	// analysis mid-refactor) — the plan's "two consecutive snapshots"
	// requirement expressed in time. Tri-state: nil means the default
	// (7 days); an explicit 0 disables the grace period and transitions
	// on the first absence. A plain int cannot distinguish "unset" from
	// an explicit 0, so this is a pointer (same convention as Enabled).
	StaleGraceDays *int `json:"stale_grace_days,omitempty" yaml:"stale_grace_days,omitempty"`
}

// StaleGraceDaysValue returns the effective stale-grace period in days:
// the configured value, the default (7) when unset, or 0 when explicitly
// disabled.
func (c *AgingConfig) StaleGraceDaysValue() int {
	if c == nil || c.StaleGraceDays == nil {
		return *DefaultAgingConfig().StaleGraceDays
	}
	return *c.StaleGraceDays
}

// DefaultAgingConfig returns the built-in defaults.
func DefaultAgingConfig() *AgingConfig {
	enabled := true
	return &AgingConfig{
		Enabled:                     &enabled,
		CodeHalfLifeDays:            365,
		DocsHalfLifeDays:            270,
		GitHalfLifeDays:             180,
		LLMHalfLifeDays:             90,
		DefaultHalfLifeDays:         180,
		DeprecationToHistoricalDays: 180,
		ExperimentalPromotionEvents: 3,
		StaleGraceDays:              intPtr(7),
	}
}

// ApplyDefaults fills every unset field with the default value. It is the
// single place config merging happens, so a partially-populated "aging:"
// section in config.yaml still behaves sensibly.
func (c *AgingConfig) ApplyDefaults() {
	if c == nil {
		return
	}
	d := DefaultAgingConfig()
	if c.Enabled == nil {
		c.Enabled = d.Enabled
	}
	if c.CodeHalfLifeDays == 0 {
		c.CodeHalfLifeDays = d.CodeHalfLifeDays
	}
	if c.DocsHalfLifeDays == 0 {
		c.DocsHalfLifeDays = d.DocsHalfLifeDays
	}
	if c.GitHalfLifeDays == 0 {
		c.GitHalfLifeDays = d.GitHalfLifeDays
	}
	if c.LLMHalfLifeDays == 0 {
		c.LLMHalfLifeDays = d.LLMHalfLifeDays
	}
	if c.DefaultHalfLifeDays == 0 {
		c.DefaultHalfLifeDays = d.DefaultHalfLifeDays
	}
	if c.DeprecationToHistoricalDays == 0 {
		c.DeprecationToHistoricalDays = d.DeprecationToHistoricalDays
	}
	if c.ExperimentalPromotionEvents == 0 {
		c.ExperimentalPromotionEvents = d.ExperimentalPromotionEvents
	}
	if c.StaleGraceDays == nil {
		c.StaleGraceDays = d.StaleGraceDays
	}
}

// intPtr returns a pointer to v — the tri-state constructor for pointer
// fields whose zero value must remain distinguishable from "unset".
func intPtr(v int) *int {
	return &v
}

// AgingEnabled reports whether the aging pass is enabled, honoring the
// tri-state Enabled (nil = default true).
func (c *AgingConfig) AgingEnabled() bool {
	if c == nil || c.Enabled == nil {
		return true
	}
	return *c.Enabled
}
