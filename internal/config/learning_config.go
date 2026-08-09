package config

// LearningConfig controls the Stage 10 learning layer
// (v2_master_implementaion_plan.md §8). It is the single canonical
// definition of the "learning:" section in .glassmarble/config.yaml.
//
// It lives in the config package (not in learning) for the same reason
// FusionConfig and IntelligenceConfig do: the config package is a leaf that
// every stage imports; stage packages consume config types, they do not
// define them.
//
// All fields are optional; zero values fall back to DefaultLearningConfig
// via ApplyDefaults.
type LearningConfig struct {
	// ApplyOnQuery controls whether recorded corrections are overlaid onto
	// memory query results. Corrections are always persisted regardless;
	// this only toggles their effect on displayed results. Tri-state: nil
	// means the default (true) — a plain bool cannot distinguish "unset"
	// from an explicit "false".
	ApplyOnQuery *bool `json:"apply_on_query,omitempty" yaml:"apply_on_query,omitempty"`

	// ConventionsEnabled controls whether deterministic project-convention
	// extraction runs during `gmb analyze` (naming patterns, layer
	// directories, ADR location → conventions.json). Tri-state: nil means
	// the default (true).
	ConventionsEnabled *bool `json:"conventions_enabled,omitempty" yaml:"conventions_enabled,omitempty"`

	// MinConventionEvidence is the minimum number of occurrences a
	// convention must have before it is reported (0 = the built-in default
	// of 2). Prevents single-file accidents from becoming "conventions".
	MinConventionEvidence int `json:"min_convention_evidence,omitempty" yaml:"min_convention_evidence,omitempty"`
}

// DefaultLearningConfig returns the built-in defaults.
func DefaultLearningConfig() *LearningConfig {
	apply := true
	enabled := true
	return &LearningConfig{
		ApplyOnQuery:          &apply,
		ConventionsEnabled:    &enabled,
		MinConventionEvidence: 2,
	}
}

// ApplyDefaults fills every unset field with the default value. It is the
// single place config merging happens, so a partially-populated "learning:"
// section in config.yaml still behaves sensibly.
func (c *LearningConfig) ApplyDefaults() {
	if c == nil {
		return
	}
	d := DefaultLearningConfig()
	if c.ApplyOnQuery == nil {
		c.ApplyOnQuery = d.ApplyOnQuery
	}
	if c.ConventionsEnabled == nil {
		c.ConventionsEnabled = d.ConventionsEnabled
	}
	if c.MinConventionEvidence == 0 {
		c.MinConventionEvidence = d.MinConventionEvidence
	}
}

// CorrectionsApplyOnQuery reports whether the learner overlay is enabled,
// honoring the tri-state ApplyOnQuery (nil = default true).
func (c *LearningConfig) CorrectionsApplyOnQuery() bool {
	if c == nil || c.ApplyOnQuery == nil {
		return true
	}
	return *c.ApplyOnQuery
}

// LearnConventionsEnabled reports whether deterministic convention
// extraction is enabled, honoring the tri-state ConventionsEnabled
// (nil = default true).
func (c *LearningConfig) LearnConventionsEnabled() bool {
	if c == nil || c.ConventionsEnabled == nil {
		return true
	}
	return *c.ConventionsEnabled
}

// ConventionEvidenceThreshold returns the minimum occurrence count a
// convention needs before it is reported.
func (c *LearningConfig) ConventionEvidenceThreshold() int {
	if c == nil || c.MinConventionEvidence == 0 {
		return DefaultLearningConfig().MinConventionEvidence
	}
	return c.MinConventionEvidence
}
