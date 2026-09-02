package handlers

import (
	"fmt"
	"strings"
)

// Impact tool names (Master Plan §6 impact category).
const (
	ToolImpactAnalysis  = "gmb_impact_analysis"
	ToolHotspotRankings = "gmb_hotspot_rankings"
)

// ImpactToolNames returns the impact tool set.
func ImpactToolNames() []string {
	return []string{ToolImpactAnalysis, ToolHotspotRankings}
}

// ImpactArgs are validated args for gmb_impact_analysis.
type ImpactArgs struct {
	Target string
	Depth  int
}

// ValidateImpactArgs validates gmb_impact_analysis args.
func ValidateImpactArgs(args map[string]any) (ImpactArgs, error) {
	target, _ := args["target"].(string)
	target = strings.TrimSpace(target)
	if target == "" {
		return ImpactArgs{}, fmt.Errorf("missing required parameter \"target\"")
	}
	if len(target) > 1000 {
		return ImpactArgs{}, fmt.Errorf("target too long (%d chars, max 1000)", len(target))
	}
	depth := 0
	if v, ok := args["depth"]; ok {
		switch n := v.(type) {
		case float64:
			depth = int(n)
		case int:
			depth = n
		}
		if depth < 0 {
			depth = 0
		}
		if depth > 20 {
			depth = 20
		}
	}
	return ImpactArgs{Target: target, Depth: depth}, nil
}

// HotspotArgs are validated args for gmb_hotspot_rankings.
type HotspotArgs struct {
	Top int
}

// ValidateHotspotArgs validates gmb_hotspot_rankings args.
func ValidateHotspotArgs(args map[string]any) HotspotArgs {
	top := 10
	if v, ok := args["top"]; ok {
		switch n := v.(type) {
		case float64:
			top = int(n)
		case int:
			top = n
		}
	}
	if top < 1 {
		top = 10
	}
	if top > 100 {
		top = 100
	}
	return HotspotArgs{Top: top}
}

// RiskLevel maps risk_score 0-100 to LOW/MEDIUM/HIGH/CRITICAL (mirrors impact_analyzer).
func RiskLevel(score int) string {
	switch {
	case score >= 80:
		return "CRITICAL"
	case score >= 50:
		return "HIGH"
	case score >= 20:
		return "MEDIUM"
	default:
		return "LOW"
	}
}
