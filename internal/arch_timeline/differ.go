package arch_timeline

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// patternKey generates a unique identifier for a pattern instance
func patternKey(p archmodel.DetectedPattern) string {
	comps := append([]string{}, p.Components...)
	return fmt.Sprintf("%s|%s|%s", p.Kind, p.Name, strings.Join(comps, ","))
}

// smellKey generates a unique identifier for a smell instance
func smellKey(s archmodel.ArchSmell) string {
	aff := append([]string{}, s.AffectedIDs...)
	return fmt.Sprintf("%s|%s|%s", s.Kind, s.Title, strings.Join(aff, ","))
}

// Diff computes the architectural evolution between two snapshots.
func Diff(base, head *archmodel.ArchSnapshot) *archmodel.SnapshotDelta {
	delta := &archmodel.SnapshotDelta{
		BaseSnapshot: base.ID,
		HeadSnapshot: head.ID,
	}

	baseComps := make(map[string]bool)
	for _, c := range base.Components {
		baseComps[c.Name] = true
	}

	headComps := make(map[string]bool)
	for _, c := range head.Components {
		headComps[c.Name] = true
	}

	for name := range headComps {
		if !baseComps[name] {
			delta.AddedComponents = append(delta.AddedComponents, name)
		}
	}
	for name := range baseComps {
		if !headComps[name] {
			delta.RemovedComponents = append(delta.RemovedComponents, name)
		}
	}

	// Compare Patterns accurately by exact signature
	basePat := make(map[string]archmodel.DetectedPattern)
	for _, p := range base.Patterns {
		basePat[patternKey(p)] = p
	}
	headPat := make(map[string]archmodel.DetectedPattern)
	for _, p := range head.Patterns {
		headPat[patternKey(p)] = p
	}
	for key, p := range headPat {
		if _, exists := basePat[key]; !exists {
			delta.PatternChanges = append(delta.PatternChanges, fmt.Sprintf("Added %s: %s", p.Kind, p.Name))
		}
	}
	for key, p := range basePat {
		if _, exists := headPat[key]; !exists {
			delta.PatternChanges = append(delta.PatternChanges, fmt.Sprintf("Removed %s: %s", p.Kind, p.Name))
		}
	}

	// Compare Smells accurately by exact signature
	baseSmells := make(map[string]archmodel.ArchSmell)
	for _, s := range base.Smells {
		baseSmells[smellKey(s)] = s
	}
	headSmells := make(map[string]archmodel.ArchSmell)
	for _, s := range head.Smells {
		headSmells[smellKey(s)] = s
	}
	for key, s := range headSmells {
		if _, exists := baseSmells[key]; !exists {
			delta.SmellChanges = append(delta.SmellChanges, fmt.Sprintf("Introduced %s: %s", s.Kind, s.Title))
		}
	}
	for key, s := range baseSmells {
		if _, exists := headSmells[key]; !exists {
			delta.SmellChanges = append(delta.SmellChanges, fmt.Sprintf("Resolved %s: %s", s.Kind, s.Title))
		}
	}

	// Metrics delta
	delta.MetricDelta.DensityDelta = head.Metrics.GraphDensity - base.Metrics.GraphDensity
	delta.MetricDelta.CycleDelta = head.Metrics.CycleCount - base.Metrics.CycleCount
	delta.MetricDelta.ViolationDelta = head.Metrics.LayerViolationCount - base.Metrics.LayerViolationCount
	
	if head.Metrics.AvgFanIn > base.Metrics.AvgFanIn {
		delta.MetricDelta.CouplingTrend = "INCREASING"
	} else if head.Metrics.AvgFanIn < base.Metrics.AvgFanIn {
		delta.MetricDelta.CouplingTrend = "DECREASING"
	} else {
		delta.MetricDelta.CouplingTrend = "STABLE"
	}

	delta.MetricDelta.SummaryLine = fmt.Sprintf("Components added: %d, removed: %d. Cycles diff: %d", 
		len(delta.AddedComponents), len(delta.RemovedComponents), delta.MetricDelta.CycleDelta)

	return delta
}
