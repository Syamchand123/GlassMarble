package knowledge_aging

import (
	"math"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

var baseNow = time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

func claimWith(source evidence.Source, validFrom time.Time) developer_memory.KnowledgeClaim {
	return developer_memory.KnowledgeClaim{
		ID:        "c1",
		State:     developer_memory.StateActive,
		ValidFrom: validFrom,
		Evidence:  evidence.NewBundle(evidence.EvidenceItem{Source: source, Confidence: 1.0, Timestamp: validFrom}),
	}
}

// TestFreshnessScore_DecayCurves verifies the exponential decay for every
// source bucket at 0, 1 and 2 half-lives.
func TestFreshnessScore_DecayCurves(t *testing.T) {
	tests := []struct {
		name      string
		source    evidence.Source
		halfLife  time.Duration
	}{
		{"code", evidence.SourceCode, 365 * 24 * time.Hour},
		{"user", evidence.SourceUser, 365 * 24 * time.Hour},
		{"docs", evidence.SourceDocs, 270 * 24 * time.Hour},
		{"pr", evidence.SourcePR, 270 * 24 * time.Hour},
		{"issue", evidence.SourceIssue, 270 * 24 * time.Hour},
		{"git", evidence.SourceGit, 180 * 24 * time.Hour},
		{"rule", evidence.SourceRule, 180 * 24 * time.Hour},
		{"heuristic", evidence.SourceHeuristic, 180 * 24 * time.Hour},
		{"llm", evidence.SourceLLM, 90 * 24 * time.Hour},
		{"unknown source falls back", "mystery", 180 * 24 * time.Hour},
		{"empty source falls back", "", 180 * 24 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := claimWith(tt.source, baseNow.Add(-tt.halfLife))

			if got := FreshnessScore(c, baseNow); math.Abs(got-0.5) > 1e-9 {
				t.Errorf("at one half-life: score = %.4f, want 0.5", got)
			}

			// Fresh at creation.
			if got := FreshnessScore(claimWith(tt.source, baseNow), baseNow); got != 1.0 {
				t.Errorf("at creation: score = %.4f, want 1.0", got)
			}

			// Two half-lives.
			c2 := claimWith(tt.source, baseNow.Add(-2*tt.halfLife))
			if got := FreshnessScore(c2, baseNow); math.Abs(got-0.25) > 1e-9 {
				t.Errorf("at two half-lives: score = %.4f, want 0.25", got)
			}

			// Decay is monotonic: older is never fresher.
			old := claimWith(tt.source, baseNow.Add(-3*tt.halfLife))
			if FreshnessScore(old, baseNow) >= FreshnessScore(c2, baseNow) {
				t.Errorf("score must decrease with age")
			}
		})
	}
}

// TestFreshnessScore_BoundaryConditions covers the non-decay edges.
func TestFreshnessScore_BoundaryConditions(t *testing.T) {
	t.Run("removed claims have no freshness", func(t *testing.T) {
		c := claimWith(evidence.SourceCode, baseNow.Add(-24*time.Hour))
		c.State = developer_memory.StateRemoved
		if got := FreshnessScore(c, baseNow); got != 0.0 {
			t.Errorf("score = %.4f, want 0.0 for removed", got)
		}
	})

	t.Run("claims past valid_until have no freshness", func(t *testing.T) {
		c := claimWith(evidence.SourceCode, baseNow.Add(-48*time.Hour))
		until := baseNow.Add(-24 * time.Hour)
		c.ValidUntil = &until
		if got := FreshnessScore(c, baseNow); got != 0.0 {
			t.Errorf("score = %.4f, want 0.0 after valid_until", got)
		}
	})

	t.Run("future valid_from is fresh (clock drift safety)", func(t *testing.T) {
		c := claimWith(evidence.SourceLLM, baseNow.Add(24*time.Hour))
		if got := FreshnessScore(c, baseNow); got != 1.0 {
			t.Errorf("score = %.4f, want 1.0 for future claims", got)
		}
	})

	t.Run("zero valid_from is fresh", func(t *testing.T) {
		c := claimWith(evidence.SourceCode, time.Time{})
		if got := FreshnessScore(c, baseNow); got != 1.0 {
			t.Errorf("score = %.4f, want 1.0 for claims without valid_from", got)
		}
	})

	t.Run("valid_until in the future keeps freshness", func(t *testing.T) {
		c := claimWith(evidence.SourceCode, baseNow.Add(-24*time.Hour))
		until := baseNow.Add(24 * time.Hour)
		c.ValidUntil = &until
		if got := FreshnessScore(c, baseNow); got != FreshnessScore(claimWith(evidence.SourceCode, baseNow.Add(-24*time.Hour)), baseNow) {
			t.Errorf("a future valid_until must not change the score")
		}
	})
}

// TestFreshnessScoreWithConfig verifies config-tuned half-lives take effect.
func TestFreshnessScoreWithConfig(t *testing.T) {
	cfg := config.DefaultAgingConfig()
	cfg.CodeHalfLifeDays = 10

	c := claimWith(evidence.SourceCode, baseNow.Add(-10*24*time.Hour))
	if got := FreshnessScoreWithConfig(c, baseNow, cfg); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("config-tuned score = %.4f, want 0.5 (10-day half-life)", got)
	}
	// Default config keeps 365d: 10 days barely decays.
	if got := FreshnessScore(c, baseNow); got < 0.97 {
		t.Errorf("default score = %.4f, want ~1.0 at 10 days with 365d half-life", got)
	}
}

// TestFreshnessScore_ZeroHalfLifeIsSafe pins the guard: a broken config with
// a zero half-life must fall back to defaults, not divide by zero.
func TestFreshnessScore_ZeroHalfLifeIsSafe(t *testing.T) {
	cfg := &config.AgingConfig{}
	c := claimWith(evidence.SourceCode, baseNow.Add(-30*24*time.Hour))
	got := FreshnessScoreWithConfig(c, baseNow, cfg)
	if math.IsNaN(got) || math.IsInf(got, 0) || got < 0 || got > 1 {
		t.Errorf("score = %v, want a sane 0..1 value", got)
	}
}
