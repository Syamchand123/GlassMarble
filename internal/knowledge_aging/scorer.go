package knowledge_aging

import (
	"math"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// FreshnessScore computes a 0.0-1.0 freshness score for a knowledge claim.
//
// The score is a PURE function of the claim and the current time — it is
// never persisted as the source of truth, only projected into aggregates
// and query results, so it can never drift from the clock or from a WAL
// rebuild. It decays exponentially from 1.0 at ValidFrom with a half-life
// that depends on how the claim was established:
//
//	code / user       365d (12 months) — durable ground truth
//	docs / pr / issue 270d (9 months)  — drifts as documentation rots
//	git / rule / heuristic 180d (6 months) — derived from history/heuristics
//	llm               90d (3 months)   — fastest decay, lowest trust
//
// Boundary conditions:
//
//   - REMOVED claims score 0.0 — the knowledge is explicitly no longer valid.
//   - Claims past their ValidUntil score 0.0 — the temporal window closed.
//   - Claims with a future ValidFrom (clock drift) score 1.0.
//   - Claims with no ValidFrom score 1.0 — nothing to decay from.
//
// The decay parameters come from DefaultAgingConfig; call
// FreshnessScoreWithConfig to honor an "aging:" config section.
func FreshnessScore(claim developer_memory.KnowledgeClaim, now time.Time) float64 {
	return FreshnessScoreWithConfig(claim, now, config.DefaultAgingConfig())
}

// FreshnessScoreWithConfig is FreshnessScore with explicit aging config.
func FreshnessScoreWithConfig(claim developer_memory.KnowledgeClaim, now time.Time, cfg *config.AgingConfig) float64 {
	if claim.State == developer_memory.StateRemoved {
		return 0.0
	}
	if claim.ValidUntil != nil && now.After(*claim.ValidUntil) {
		return 0.0
	}
	if claim.ValidFrom.IsZero() || now.Before(claim.ValidFrom) {
		return 1.0
	}

	halfLifeDays := halfLifeDaysForSource(claim.Evidence.PrimarySource, cfg)
	age := now.Sub(claim.ValidFrom)
	return math.Pow(0.5, age.Hours()/(24*float64(halfLifeDays)))
}

// halfLifeDaysForSource maps a claim's primary evidence source to its
// freshness half-life, honoring the aging config when present.
func halfLifeDaysForSource(src evidence.Source, cfg *config.AgingConfig) int {
	hl := halfLifeDaysBySource(src, config.DefaultAgingConfig())
	if cfg != nil {
		hl = halfLifeDaysBySource(src, cfg)
	}
	if hl <= 0 {
		return config.DefaultAgingConfig().DefaultHalfLifeDays
	}
	return hl
}

// halfLifeDaysBySource buckets evidence sources into the config's half-life
// fields. Sources not listed fall back to the default half-life.
func halfLifeDaysBySource(src evidence.Source, cfg *config.AgingConfig) int {
	switch src {
	case evidence.SourceCode, evidence.SourceUser:
		return cfg.CodeHalfLifeDays
	case evidence.SourceDocs, evidence.SourcePR, evidence.SourceIssue:
		return cfg.DocsHalfLifeDays
	case evidence.SourceGit, evidence.SourceRule, evidence.SourceHeuristic:
		return cfg.GitHalfLifeDays
	case evidence.SourceLLM:
		return cfg.LLMHalfLifeDays
	default:
		return cfg.DefaultHalfLifeDays
	}
}
