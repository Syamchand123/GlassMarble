package tools

import (
	"context"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// PatternTool queries detected architectural patterns and anti-pattern smells.
var PatternTool = Tool{
	Name:        "get_architecture_patterns",
	Description: "Get detected architectural patterns and anti-patterns (smells) in the codebase with confidence scores, components, and evidence.",
	Category:    CategoryAKG,
	Parameters: Schema(map[string]Prop{
		"include_smells": {Type: "boolean", Description: "Whether to include architectural smells / anti-patterns (default true)", Default: true},
		"min_confidence": {Type: "number", Description: "Minimum confidence score threshold (0.0 to 1.0, default 0.0)", Default: float64(0.0)},
	}),
	Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
		includeSmells := boolArg(args, "include_smells", true)
		minConf := floatArg(args, "min_confidence", 0.0, 0.0, 1.0)

		var patterns []archmodel.DetectedPattern
		var smells []archmodel.ArchSmell

		if env != nil && env.RootDir != "" {
			snapDir := filepath.Join(env.RootDir, ".glassmarble", "snapshots")
			if store, err := arch_timeline.NewSnapshotStore(snapDir); err == nil {
				if latest, err := store.Latest(); err == nil && latest != nil {
					patterns = latest.Patterns
					if includeSmells {
						smells = latest.Smells
					}
				}
			}
		}

		// Fallback to live AKG summary if no snapshot store is initialized
		if len(patterns) == 0 && env != nil && env.Bridge != nil {
			if snap, err := env.Bridge.Snapshot(); err == nil && snap.Summary != nil {
				for _, p := range snap.Summary.PrimaryPatterns {
					patterns = append(patterns, archmodel.DetectedPattern{
						Kind:        archmodel.PatternKind(p),
						Name:        p,
						Confidence:  1.0,
						Description: "Detected primary architectural pattern from AKG graph analysis",
					})
				}
			}
		}

		filteredPatterns := make([]archmodel.DetectedPattern, 0, len(patterns))
		for _, p := range patterns {
			if p.Confidence >= minConf {
				filteredPatterns = append(filteredPatterns, p)
			}
		}

		if filteredPatterns == nil {
			filteredPatterns = []archmodel.DetectedPattern{}
		}
		if smells == nil {
			smells = []archmodel.ArchSmell{}
		}

		return map[string]any{
			"ok":       true,
			"patterns": filteredPatterns,
			"smells":   smells,
			"count":    len(filteredPatterns),
		}, nil
	},
}
