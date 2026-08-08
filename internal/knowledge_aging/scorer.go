package knowledge_aging

import (
	"math"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// FreshnessScore computes a 0.0-1.0 freshness score for a knowledge claim.
// Score = 1.0 at creation, decays based on claim type and elapsed time.
func FreshnessScore(claim developer_memory.KnowledgeClaim, now time.Time) float64 {
	if claim.State == developer_memory.StateRemoved {
		return 0.0 // no freshness if explicitly removed
	}

	age := now.Sub(claim.ValidFrom)
	if age < 0 {
		return 1.0 // Future claims (or clock drift) are fresh
	}

	var halfLifeDays float64

	switch claim.Evidence.PrimarySource {
	case evidence.SourceCode:
		// Code-backed claims: slow decay (12-month half-life)
		halfLifeDays = 365.0
	case evidence.SourceGit:
		// Git-backed claims: medium decay (6-month half-life)
		halfLifeDays = 180.0
	case evidence.SourceLLM:
		// LLM inferences: fast decay (3-month half-life)
		halfLifeDays = 90.0
	default:
		// Default decay (6-month half-life)
		halfLifeDays = 180.0
	}

	return math.Pow(0.5, age.Hours()/(24*halfLifeDays))
}
