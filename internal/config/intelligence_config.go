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
	GodObjectFanInThreshold     int     `json:"god_object_fan_in_threshold"`
	GodObjectMethodThreshold    int     `json:"god_object_method_threshold"`
	SmallCycleThreshold         int     `json:"small_cycle_threshold"`
	LargeCycleThreshold         int     `json:"large_cycle_threshold"`
	GodPackageTrafficPct        float64 `json:"god_package_traffic_pct"`
	LayeredConsistencyThreshold float64 `json:"layered_consistency_threshold"`
	EventEdgePct                float64 `json:"event_edge_pct"`
	CouplingChangePct           float64 `json:"coupling_change_pct"`
	LLMIntentEnabled            bool    `json:"llm_intent_enabled"`
	SnapshotNoGraph             bool    `json:"snapshot_no_graph"`
	PageRankIterations          int     `json:"page_rank_iterations"`
	PageRankDamping             float64 `json:"page_rank_damping"`
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
	}
}