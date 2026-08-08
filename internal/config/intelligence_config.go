// File: internal/config/intelligence_config.go
//
// WHY: Stage 5 pattern/smell detection requires configurable thresholds.
// Hard-coded values would cause false positives on large repos and false
// negatives on small ones. IntelligenceConfig holds all thresholds with
// sensible defaults that will be refined after Phase 3 calibration.
//
// Calibration requirement: run 'gmb patterns --smells' on GlassMarble +
// traefik + caddy after Phase 3 implementation. Record false positives.
// Update DefaultIntelligenceConfig() accordingly.
package config

// IntelligenceConfig holds threshold values for Stage 5 pattern/smell detection.
type IntelligenceConfig struct {
	GodObjectFanInThreshold     int     `json:"god_object_fan_in_threshold" yaml:"god_object_fan_in_threshold"`
	GodObjectMethodThreshold    int     `json:"god_object_method_threshold" yaml:"god_object_method_threshold"`
	SmallCycleThreshold         int     `json:"small_cycle_threshold" yaml:"small_cycle_threshold"`
	LargeCycleThreshold         int     `json:"large_cycle_threshold" yaml:"large_cycle_threshold"`
	GodPackageTrafficPct        float64 `json:"god_package_traffic_pct" yaml:"god_package_traffic_pct"`
	LayeredConsistencyThreshold float64 `json:"layered_consistency_threshold" yaml:"layered_consistency_threshold"`
	EventEdgePct                float64 `json:"event_edge_pct" yaml:"event_edge_pct"`
	CouplingChangePct           float64 `json:"coupling_change_pct" yaml:"coupling_change_pct"`
	LLMIntentEnabled            bool    `json:"llm_intent_enabled" yaml:"llm_intent_enabled"`
	SnapshotNoGraph             bool    `json:"snapshot_no_graph" yaml:"snapshot_no_graph"`
	PageRankIterations          int     `json:"page_rank_iterations" yaml:"page_rank_iterations"`
	PageRankDamping             float64 `json:"page_rank_damping" yaml:"page_rank_damping"`

	// ArchLayers defines the optional layering used by the architectural
	// consistency pattern (PR-07) and by --arch stats output. When empty the
	// layer index degrades to root-level grouping.
	ArchLayers []DriftLayer `json:"arch_layers,omitempty" yaml:"arch_layers"`

	// ArchExcludedDirs lists directory prefixes skipped by component
	// inference (e.g. vendored or generated code).
	ArchExcludedDirs []string `json:"arch_excluded_dirs,omitempty" yaml:"arch_excluded_dirs"`

	// NodeCountThreshold is the graph size above which analytics switch to
	// iterative algorithms (iterative Tarjan, cap on Louvain passes).
	NodeCountThreshold int `json:"node_count_threshold" yaml:"node_count_threshold"`

	// UnstableThreshold marks a component unstable when Instability > value.
	UnstableThreshold float64 `json:"unstable_threshold" yaml:"unstable_threshold"`

	// StableComponentsThreshold is the share of component weight a
	// snapshot must have in stable components to be reported as stable.
	StableComponentsThreshold float64 `json:"stable_components_threshold" yaml:"stable_components_threshold"`

	// SnapshotTTLSeconds is how long cached snapshots stay valid.
	SnapshotTTLSeconds int `json:"snapshot_ttl_seconds" yaml:"snapshot_ttl_seconds"`

	// SnapshotNumPages is how many recent graph pages (per snapshot source)
	// are retained for drift analysis.
	SnapshotNumPages int `json:"snapshot_num_pages" yaml:"snapshot_num_pages"`

	// RunRules selects which rule families run in Stage 5: any subset of
	// "patterns", "smells", "events". Empty means all.
	RunRules []string `json:"run_rules,omitempty" yaml:"run_rules"`
}

// DefaultIntelligenceConfig returns conservative default thresholds.
func DefaultIntelligenceConfig() *IntelligenceConfig {
	return &IntelligenceConfig{
		GodObjectFanInThreshold:     15,
		GodObjectMethodThreshold:    30,
		SmallCycleThreshold:         3,
		LargeCycleThreshold:         5,
		GodPackageTrafficPct:        40.0,
		LayeredConsistencyThreshold: 0.80,
		EventEdgePct:                15.0,
		CouplingChangePct:           0.20,
		LLMIntentEnabled:            false,
		SnapshotNoGraph:             false,
		PageRankIterations:          100,
		PageRankDamping:             0.85,
		NodeCountThreshold:          2000,
		UnstableThreshold:           0.8,
		StableComponentsThreshold:   0.9,
		SnapshotTTLSeconds:          3600,
		SnapshotNumPages:            10,
		RunRules:                    nil,
	}
}
