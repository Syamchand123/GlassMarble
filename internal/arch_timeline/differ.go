package arch_timeline

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// ComponentRename is a component whose identity was preserved across a
// snapshot boundary while its name changed. Evidence: name-token similarity
// and/or shared node coverage between the old and new membership.
type ComponentRename struct {
	OldName    string  `json:"old_name"`
	NewName    string  `json:"new_name"`
	Similarity float64 `json:"similarity"`
	NodeOverlap int    `json:"node_overlap"`
}

// ServiceSplit is one component in the base that became two or more in the
// head, with the head components jointly covering the old node membership.
type ServiceSplit struct {
	Source  string   `json:"source"`
	Targets []string `json:"targets"`
}

// ServiceMerge is the inverse of a split: two or more base components were
// consolidated into one head component.
type ServiceMerge struct {
	Target  string   `json:"target"`
	Sources []string `json:"sources"`
}

// DependencyChange is a component-level dependency that appeared or vanished
// between snapshots (based on DetectedComponent.Dependencies).
type DependencyChange struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Added  bool   `json:"added"`
}

// DiffResult is the complete architectural evolution between two snapshots:
// the archmodel SnapshotDelta (shared vocabulary with event generation), the
// structural AKG diff when both snapshots embed graphs, and the enriched
// rename/split/merge/dependency analysis. Deterministically ordered for
// stable output and CI use.
type DiffResult struct {
	Delta             *archmodel.SnapshotDelta `json:"delta"`
	Graph             *akg.GraphDiff            `json:"graph,omitempty"`
	Renames           []ComponentRename        `json:"renames,omitempty"`
	Splits            []ServiceSplit           `json:"splits,omitempty"`
	Merges            []ServiceMerge           `json:"merges,omitempty"`
	DependencyChanges []DependencyChange       `json:"dependency_changes,omitempty"`
}

// diffComponentKey identifies a component across snapshots by ID, falling
// back to its name for callers that omit IDs.
func diffComponentKey(c archmodel.DetectedComponent) string {
	if c.ID != "" {
		return c.ID
	}
	return c.Name
}

// componentKeys maps a snapshot's components to their identity keys.
func componentKeys(snap *archmodel.ArchSnapshot) map[string]archmodel.DetectedComponent {
	out := make(map[string]archmodel.DetectedComponent)
	if snap == nil {
		return out
	}
	for _, c := range snap.Components {
		out[diffComponentKey(c)] = c
	}
	return out
}

// patternKey identifies a pattern instance unambiguously: canonical JSON of
// (kind, name, sorted components) — safe against separators appearing in
// names, which the old "|"-joined keys were not.
func patternKey(p archmodel.DetectedPattern) string {
	return canonicalJSON(struct {
		Kind       archmodel.PatternKind
		Name       string
		Components []string
	}{p.Kind, p.Name, sortedStrings(p.Components)})
}

// smellKey identifies a smell instance unambiguously (canonical JSON of
// kind, title, sorted affected IDs).
func smellKey(s archmodel.ArchSmell) string {
	return canonicalJSON(struct {
		Kind        archmodel.SmellKind
		Title       string
		AffectedIDs []string
	}{s.Kind, s.Title, sortedStrings(s.AffectedIDs)})
}

var tokenPattern = regexp.MustCompile(`[a-z0-9]+`)

// nameTokens splits a component name into lowercase alphanumeric tokens
// ("PaymentService" → [payment service], "auth-api" → [auth api]).
func nameTokens(name string) []string {
	return tokenPattern.FindAllString(strings.ToLower(name), -1)
}

// nameSimilarity is the Jaccard similarity of the names' token sets,
// boosted to 1.0 for exact matches and degraded to 0 for empty token sets.
func nameSimilarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	ta, tb := nameTokens(a), nameTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0.0
	}
	setA := make(map[string]bool, len(ta))
	for _, t := range ta {
		setA[t] = true
	}
	intersection := 0
	unionSet := make(map[string]bool, len(ta)+len(tb))
	for _, t := range ta {
		unionSet[t] = true
	}
	for _, t := range tb {
		if setA[t] {
			intersection++
		}
		unionSet[t] = true
	}
	return float64(intersection) / float64(len(unionSet))
}

// nodeOverlapRatio is the fraction of the larger membership shared by both
// components. 0 when either side has no node membership.
func nodeOverlapRatio(a, b archmodel.DetectedComponent) int {
	setB := make(map[string]bool, len(b.NodeIDs))
	for _, id := range b.NodeIDs {
		setB[id] = true
	}
	overlap := 0
	for _, id := range a.NodeIDs {
		if setB[id] {
			overlap++
		}
	}
	return overlap
}

// renameScore combines name similarity and node-overlap evidence, requiring
// both components to have at least one source of identity.
func renameScore(a, b archmodel.DetectedComponent) (score float64, overlap int) {
	overlap = nodeOverlapRatio(a, b)
	hasNodes := len(a.NodeIDs) > 0 && len(b.NodeIDs) > 0
	var overlapRatio float64
	if hasNodes {
		maxLen := len(a.NodeIDs)
		if len(b.NodeIDs) > maxLen {
			maxLen = len(b.NodeIDs)
		}
		overlapRatio = float64(overlap) / float64(maxLen)
	}
	ns := nameSimilarity(a.Name, b.Name)
	score = ns
	if overlapRatio > score {
		score = overlapRatio
	}
	return score, overlap
}

const renameThreshold = 0.5

// Diff computes the architectural evolution between two snapshots. It is
// deterministic (every slice is sorted) and safe for nil inputs (returns nil,
// never panics). The structural Graph diff is computed when both snapshots
// embed AKGJSON; it is nil when either side was captured with --no-graph.
func Diff(base, head *archmodel.ArchSnapshot) *DiffResult {
	if base == nil || head == nil {
		return nil
	}

	result := &DiffResult{}
	delta := &archmodel.SnapshotDelta{
		BaseSnapshot: base.ID,
		HeadSnapshot: head.ID,
	}
	result.Delta = delta

	baseComps := componentKeys(base)
	headComps := componentKeys(head)

	// 1. Added/removed components, keyed by ID-with-name-fallback.
	var added, removed []archmodel.DetectedComponent
	for _, c := range head.Components {
		if _, ok := baseComps[diffComponentKey(c)]; !ok {
			added = append(added, c)
		}
	}
	for _, c := range base.Components {
		if _, ok := headComps[diffComponentKey(c)]; !ok {
			removed = append(removed, c)
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i].Name < added[j].Name })
	sort.Slice(removed, func(i, j int) bool { return removed[i].Name < removed[j].Name })

	// 2. Split/merge detection runs BEFORE rename matching: node-membership
	// coverage is stronger evidence than name similarity, and a split target
	// often shares the source's name tokens ("billing" → "billing-core" +
	// "billing-web" is a split, not a rename).
	addedAfter := append([]archmodel.DetectedComponent(nil), added...)
	removedAfter := append([]archmodel.DetectedComponent(nil), removed...)

	// 2a. Splits: one removed component whose nodes are jointly covered by
	// two or more added components.
	for i := 0; i < len(removedAfter); {
		r := removedAfter[i]
		if len(r.NodeIDs) == 0 {
			i++
			continue
		}
		// Coverage must measure how much of the REMOVED component the targets
		// account for: |r.NodeIDs ∩ ⋃targets| / |r.NodeIDs|. Counting every
		// node of every target instead let the ratio exceed 1.0, so the 0.6
		// gate below passed for any removed component that shared a single
		// node with two added ones.
		source := make(map[string]bool, len(r.NodeIDs))
		for _, id := range r.NodeIDs {
			source[id] = true
		}
		var targets []archmodel.DetectedComponent
		covered := make(map[string]bool, len(r.NodeIDs))
		for _, a := range addedAfter {
			if len(a.NodeIDs) == 0 || nodeOverlapRatio(r, a) == 0 {
				continue
			}
			targets = append(targets, a)
			for _, id := range a.NodeIDs {
				if source[id] {
					covered[id] = true
				}
			}
		}
		if len(targets) >= 2 && float64(len(covered))/float64(len(r.NodeIDs)) >= 0.6 {
			names := make([]string, 0, len(targets))
			for _, a := range targets {
				names = append(names, a.Name)
			}
			sort.Strings(names)
			result.Splits = append(result.Splits, ServiceSplit{Source: r.Name, Targets: names})
			removedAfter = removeComponents(removedAfter, r)
			addedAfter = removeComponents(addedAfter, targets...)
			continue
		}
		i++
	}
	sort.Slice(result.Splits, func(i, j int) bool { return result.Splits[i].Source < result.Splits[j].Source })

	// 2b. Merges: two or more removed components jointly covered by one
	// added component.
	for i := 0; i < len(addedAfter); {
		a := addedAfter[i]
		if len(a.NodeIDs) == 0 {
			i++
			continue
		}
		// Same correction as the split case, in the other direction:
		// |a.NodeIDs ∩ ⋃sources| / |a.NodeIDs|.
		target := make(map[string]bool, len(a.NodeIDs))
		for _, id := range a.NodeIDs {
			target[id] = true
		}
		var sources []archmodel.DetectedComponent
		covered := make(map[string]bool, len(a.NodeIDs))
		for _, r := range removedAfter {
			if len(r.NodeIDs) == 0 || nodeOverlapRatio(a, r) == 0 {
				continue
			}
			sources = append(sources, r)
			for _, id := range r.NodeIDs {
				if target[id] {
					covered[id] = true
				}
			}
		}
		if len(sources) >= 2 && float64(len(covered))/float64(len(a.NodeIDs)) >= 0.6 {
			names := make([]string, 0, len(sources))
			for _, r := range sources {
				names = append(names, r.Name)
			}
			sort.Strings(names)
			result.Merges = append(result.Merges, ServiceMerge{Target: a.Name, Sources: names})
			addedAfter = removeComponents(addedAfter, a)
			removedAfter = removeComponents(removedAfter, sources...)
			continue
		}
		i++
	}
	sort.Slice(result.Merges, func(i, j int) bool { return result.Merges[i].Target < result.Merges[j].Target })

	// 2c. Rename detection: greedy best-match between the remaining removed
	// and added components (name similarity and/or node overlap ≥ threshold).
	matchedRemoved := make(map[int]bool, len(removedAfter))
	matchedAdded := make(map[int]bool, len(addedAfter))
	for ai := range addedAfter {
		bestRi, bestScore, bestOverlap := -1, renameThreshold, 0
		for ri := range removedAfter {
			if matchedRemoved[ri] {
				continue
			}
			score, overlap := renameScore(removedAfter[ri], addedAfter[ai])
			if score >= bestScore && score >= renameThreshold {
				if score == bestScore && bestRi >= 0 && removedAfter[ri].Name >= removedAfter[bestRi].Name {
					continue
				}
				bestRi, bestScore, bestOverlap = ri, score, overlap
			}
		}
		if bestRi >= 0 {
			matchedRemoved[bestRi] = true
			matchedAdded[ai] = true
			result.Renames = append(result.Renames, ComponentRename{
				OldName:     removedAfter[bestRi].Name,
				NewName:     addedAfter[ai].Name,
				Similarity:  bestScore,
				NodeOverlap: bestOverlap,
			})
		}
	}
	sort.Slice(result.Renames, func(i, j int) bool { return result.Renames[i].OldName < result.Renames[j].OldName })

	// 2d. Raw membership delta from everything not reconciled as a
	// rename/split/merge.
	renamedAdded := remainingComponents(addedAfter, matchedAdded)
	renamedRemoved := remainingComponents(removedAfter, matchedRemoved)

	// 5. Raw membership delta (after rename/split/merge reconciliation).
	for _, c := range renamedAdded {
		delta.AddedComponents = append(delta.AddedComponents, c.Name)
	}
	for _, c := range renamedRemoved {
		delta.RemovedComponents = append(delta.RemovedComponents, c.Name)
	}
	sort.Strings(delta.AddedComponents)
	sort.Strings(delta.RemovedComponents)

	// 6. Dependency changes on components present in both snapshots.
	result.DependencyChanges = dependencyChanges(baseComps, headComps)

	// 7. Pattern changes (canonical keys → unambiguous).
	basePat := make(map[string]archmodel.DetectedPattern)
	for _, p := range base.Patterns {
		basePat[patternKey(p)] = p
	}
	headPat := make(map[string]archmodel.DetectedPattern)
	for _, p := range head.Patterns {
		headPat[patternKey(p)] = p
	}
	addedPats, removedPats := 0, 0
	for key, p := range headPat {
		if _, exists := basePat[key]; !exists {
			delta.PatternChanges = append(delta.PatternChanges, fmt.Sprintf("Added %s: %s", p.Kind, p.Name))
			addedPats++
		}
	}
	for key, p := range basePat {
		if _, exists := headPat[key]; !exists {
			delta.PatternChanges = append(delta.PatternChanges, fmt.Sprintf("Removed %s: %s", p.Kind, p.Name))
			removedPats++
		}
	}
	sort.Strings(delta.PatternChanges)

	// 8. Smell changes.
	baseSmells := make(map[string]archmodel.ArchSmell)
	for _, s := range base.Smells {
		baseSmells[smellKey(s)] = s
	}
	headSmells := make(map[string]archmodel.ArchSmell)
	for _, s := range head.Smells {
		headSmells[smellKey(s)] = s
	}
	addedSmells, removedSmells := 0, 0
	for key, s := range headSmells {
		if _, exists := baseSmells[key]; !exists {
			delta.SmellChanges = append(delta.SmellChanges, fmt.Sprintf("Introduced %s: %s", s.Kind, s.Title))
			addedSmells++
		}
	}
	for key, s := range baseSmells {
		if _, exists := headSmells[key]; !exists {
			delta.SmellChanges = append(delta.SmellChanges, fmt.Sprintf("Resolved %s: %s", s.Kind, s.Title))
			removedSmells++
		}
	}
	sort.Strings(delta.SmellChanges)

	// 9. Metrics delta.
	delta.MetricDelta.DensityDelta = head.Metrics.GraphDensity - base.Metrics.GraphDensity
	delta.MetricDelta.CycleDelta = head.Metrics.CycleCount - base.Metrics.CycleCount
	delta.MetricDelta.ViolationDelta = head.Metrics.LayerViolationCount - base.Metrics.LayerViolationCount

	switch {
	case head.Metrics.AvgFanIn > base.Metrics.AvgFanIn:
		delta.MetricDelta.CouplingTrend = "INCREASING"
	case head.Metrics.AvgFanIn < base.Metrics.AvgFanIn:
		delta.MetricDelta.CouplingTrend = "DECREASING"
	default:
		delta.MetricDelta.CouplingTrend = "STABLE"
	}
	// LCOM4 measures non-cohesion: a falling LCOM4 means cohesion improved.
	switch {
	case head.Metrics.LCOM4 < base.Metrics.LCOM4:
		delta.MetricDelta.CohesionTrend = "IMPROVING"
	case head.Metrics.LCOM4 > base.Metrics.LCOM4:
		delta.MetricDelta.CohesionTrend = "DEGRADING"
	default:
		delta.MetricDelta.CohesionTrend = "STABLE"
	}
	delta.MetricDelta.SummaryLine = fmt.Sprintf(
		"Components +%d/-%d (%d rename, %d split, %d merge); patterns +%d/-%d; smells +%d/-%d; cycles %+d; coupling %s; cohesion %s",
		len(delta.AddedComponents), len(delta.RemovedComponents),
		len(result.Renames), len(result.Splits), len(result.Merges),
		addedPats, removedPats, addedSmells, removedSmells,
		delta.MetricDelta.CycleDelta, delta.MetricDelta.CouplingTrend, delta.MetricDelta.CohesionTrend)

	// 10. Structural diff — only when both snapshots embed graphs.
	if len(base.AKGJSON) > 0 && len(head.AKGJSON) > 0 {
		baseGraph, berr := Replay(base)
		headGraph, herr := Replay(head)
		if berr == nil && herr == nil {
			result.Graph = akg.DiffGraphs(baseGraph, headGraph)
		}
	}

	return result
}

// remainingComponents returns the components whose indices were not matched.
func remainingComponents(all []archmodel.DetectedComponent, matched map[int]bool) []archmodel.DetectedComponent {
	out := make([]archmodel.DetectedComponent, 0, len(all))
	for i, c := range all {
		if !matched[i] {
			out = append(out, c)
		}
	}
	return out
}

// removeComponents removes the given components from a working pool, matching
// on ID-with-name-fallback identity. Deterministic and safe when a component
// is absent.
func removeComponents(pool []archmodel.DetectedComponent, drops ...archmodel.DetectedComponent) []archmodel.DetectedComponent {
	out := pool[:0]
	for _, c := range pool {
		drop := false
		for _, d := range drops {
			if diffComponentKey(c) == diffComponentKey(d) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, c)
		}
	}
	return out
}

// dependencyChanges diffs DetectedComponent.Dependencies for components
// present on both sides, keyed by ID-with-name-fallback. Deterministic order.
func dependencyChanges(baseComps, headComps map[string]archmodel.DetectedComponent) []DependencyChange {
	var changes []DependencyChange
	keys := make([]string, 0, len(baseComps))
	for k := range baseComps {
		if _, ok := headComps[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		baseDeps := stringSet(baseComps[k].Dependencies)
		headDeps := stringSet(headComps[k].Dependencies)
		for _, dep := range sortedStrings(headComps[k].Dependencies) {
			if !baseDeps[dep] {
				changes = append(changes, DependencyChange{Source: k, Target: dep, Added: true})
			}
		}
		for _, dep := range sortedStrings(baseComps[k].Dependencies) {
			if !headDeps[dep] {
				changes = append(changes, DependencyChange{Source: k, Target: dep, Added: false})
			}
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Source != changes[j].Source {
			return changes[i].Source < changes[j].Source
		}
		if changes[i].Target != changes[j].Target {
			return changes[i].Target < changes[j].Target
		}
		return changes[i].Added && !changes[j].Added
	})
	return changes
}

func stringSet(ss []string) map[string]bool {
	out := make(map[string]bool, len(ss))
	for _, s := range ss {
		out[s] = true
	}
	return out
}
