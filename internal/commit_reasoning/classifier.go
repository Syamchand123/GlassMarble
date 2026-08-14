package commit_reasoning

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
	"github.com/Syamchand123/GlassMarble/internal/git"
)

// ClassifiedChange is one architectural change inferred from a commit.
// AffectedIDs always carries the canonical ordered IDs for the event kind:
// component IDs for SERVICE_*/DEPENDENCY_* events (so commit reasoning and component inference
// produce identical event IDs), nil for cycle/layer-violation events
// (matching the component inference convention), and node IDs for graph-level events that
// component inference does not produce.
type ClassifiedChange struct {
	Kind        archmodel.EventKind
	AffectedIDs []string
	Names       []string
	Confidence  float64
	Summary     string
	Tags        []string
	Evidence    evidence.Bundle
}

// ClassifyInput carries every piece of evidence the classifier may use. Every
// field is optional: each pass runs only on the evidence it needs, so a
// graph-less run and a snapshot-less run both work.
type ClassifyInput struct {
	Meta      *git.CommitMeta
	BaseSnap  *archmodel.ArchSnapshot
	HeadSnap  *archmodel.ArchSnapshot
	Diff      *arch_timeline.DiffResult
	GraphDiff *akg.GraphDiff
	BaseGraph *akg.CodePropertyGraph
	HeadGraph *akg.CodePropertyGraph
	Layers    []config.DriftLayer
	Forbidden []config.ForbiddenDepRule
}

// cacheTargetRegex matches cache-technology names with word boundaries, so
// "redis" matches "redis" and "redis-cache" but not "redispatcher". The
// junior-era substring match classified any node whose ID merely contained
// "cache" (e.g. "cacheservice" was fine, but "cacheable" was not a cache).
var cacheTargetRegex = regexp.MustCompile(`(?i)\b(redis|memcached|hazelcast|caffeine|cache)\b`)

// ClassifyChange runs the four classification passes over the input:
//
//  1. Component pass  — add/remove/split/merge/dependency events from the
//     snapshot diff (arch_timeline.Diff), with affected IDs identical to
//     component inference's so the memory builder deduplicates the two generators.
//  2. Smell pass      — new LAYER_VIOLATION smell instances → LAYER_VIOLATION
//     events (affected=nil, matching component inference; component inference owns all other smell kinds).
//  3. Graph pass      — node/edge additions from the AKG diff: new modules,
//     datastores, async hops, cache layers, API endpoints, security edges.
//  4. Cycle pass      — SCC comparison of the replayed base/head graphs
//     (fallback: metric delta) → CYCLE_INTRODUCED / CYCLE_RESOLVED.
//
// The result is deterministic: every slice is sorted and no map iteration
// order leaks into output. Rule ownership follows the plan: coupling (R9)
// and dead code (R10) belong to component inference and are deliberately not duplicated.
func ClassifyChange(in ClassifyInput) []ClassifiedChange {
	var changes []ClassifiedChange
	if in.Meta == nil {
		in.Meta = &git.CommitMeta{}
	}
	// The snapshot diff is derived, not given: compute it whenever both
	// snapshots are present and the caller did not already supply one.
	if in.Diff == nil && in.BaseSnap != nil && in.HeadSnap != nil {
		in.Diff = arch_timeline.Diff(in.BaseSnap, in.HeadSnap)
	}
	if in.HeadSnap != nil && in.BaseSnap != nil {
		changes = append(changes, componentPass(in)...)
		changes = append(changes, smellPass(in)...)
	}
	if in.GraphDiff != nil {
		changes = append(changes, graphPass(in)...)
	}
	changes = append(changes, cyclePass(in)...)
	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].Kind != changes[j].Kind {
			return changes[i].Kind < changes[j].Kind
		}
		return changes[i].Summary < changes[j].Summary
	})
	return changes
}

// ---------------------------------------------------------------------------
// Pass 1: component-level evolution
// ---------------------------------------------------------------------------

// componentPass maps the reconciled snapshot diff to SERVICE_*/DEPENDENCY_*
// events. affected IDs replicate arch_intelligence.GenerateEvents exactly
// (component IDs; dependency pairs source-first), so deduplication in the
// memory builder keeps the enriched commit reasoning event and drops the component inference twin.
// Components (the memory key space) are likewise the canonical component
// IDs — one component, one key — matching the component inference convention.
func componentPass(in ClassifyInput) []ClassifiedChange {
	var changes []ClassifiedChange
	diff := in.Diff
	if diff == nil || diff.Delta == nil {
		return changes
	}
	baseByName := componentsByName(in.BaseSnap)
	headByName := componentsByName(in.HeadSnap)
	t := in.Meta.Timestamp

	// Added / removed components — only the members the differ did NOT
	// reconcile as a rename, split or merge.
	for _, name := range diff.Delta.AddedComponents {
		c, ok := headByName[name]
		if !ok {
			continue
		}
		changes = append(changes, newClassified(in, archmodel.EventServiceAdded,
			[]string{c.ID}, []string{c.ID},
			"Component "+c.Name+" first appeared in the architecture.", 0.95,
			evItem(evidence.SourceGit, in.Meta.Hash, "added component: "+c.Name, 0.95, t)))
	}
	for _, name := range diff.Delta.RemovedComponents {
		c, ok := baseByName[name]
		if !ok {
			continue
		}
		changes = append(changes, newClassified(in, archmodel.EventServiceRemoved,
			[]string{c.ID}, []string{c.ID},
			"Component "+c.Name+" no longer exists in the architecture.", 0.95,
			evItem(evidence.SourceGit, in.Meta.Hash, "removed component: "+c.Name, 0.95, t)))
	}

	// Splits and merges — beyond the component inference vocabulary (the plan's SERVICE_SPLIT
	// and SERVICE_MERGED kinds).
	for _, s := range diff.Splits {
		srcID := componentIDByName(baseByName, s.Source)
		names := append([]string{s.Source}, s.Targets...)
		changes = append(changes, newClassified(in, archmodel.EventServiceSplit,
			[]string{srcID}, names,
			"Component "+s.Source+" split into: "+strings.Join(s.Targets, ", "), 0.9,
			evItem(evidence.SourceRule, "arch_timeline.Diff:split", "split "+s.Source+" -> "+strings.Join(s.Targets, ", "), 0.9, t)))
	}
	for _, m := range diff.Merges {
		tgtID := componentIDByName(headByName, m.Target)
		names := append([]string{m.Target}, m.Sources...)
		changes = append(changes, newClassified(in, archmodel.EventServiceMerged,
			[]string{tgtID}, names,
			"Components "+strings.Join(m.Sources, ", ")+" merged into "+m.Target, 0.9,
			evItem(evidence.SourceRule, "arch_timeline.Diff:merge", "merge "+strings.Join(m.Sources, ", ")+" -> "+m.Target, 0.9, t)))
	}

	// Component-level dependency changes — identical tuple to component inference's
	// (source component ID first, then target component ID).
	changes = append(changes, dependencyPass(in)...)
	return changes
}

// dependencyPass mirrors arch_intelligence.GenerateEvents' dependency diff so
// the affected tuple (and therefore the event ID) is byte-identical.
func dependencyPass(in ClassifyInput) []ClassifiedChange {
	var changes []ClassifiedChange
	t := in.Meta.Timestamp
	depKey := func(src, tgt string) string { return src + "\x00" + tgt }
	baseDeps := make(map[string]bool)
	for _, c := range in.BaseSnap.Components {
		for _, d := range c.Dependencies {
			baseDeps[depKey(c.ID, d)] = true
		}
	}
	headDeps := make(map[string]bool)
	for _, c := range in.HeadSnap.Components {
		for _, d := range c.Dependencies {
			headDeps[depKey(c.ID, d)] = true
		}
	}
	var keys []string
	seen := make(map[string]bool)
	for k := range headDeps {
		if !baseDeps[k] {
			keys = append(keys, k)
		}
		seen[k] = true
	}
	for k := range baseDeps {
		if !seen[k] && !headDeps[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts := strings.SplitN(k, "\x00", 2)
		added := headDeps[k]
		kind := archmodel.EventDependencyAdded
		verb := "added"
		if !added {
			kind = archmodel.EventDependencyRemoved
			verb = "removed"
		}
		changes = append(changes, newClassified(in, kind,
			[]string{parts[0], parts[1]}, []string{parts[0], parts[1]},
			"Component dependency "+verb+": "+parts[0]+" -> "+parts[1], 0.9,
			evItem(evidence.SourceCode, in.Meta.Hash, "dependency "+verb+": "+parts[0]+" -> "+parts[1], 0.9, t)))
	}
	return changes
}

// ---------------------------------------------------------------------------
// Pass 2: smell-level evolution (layer violations)
// ---------------------------------------------------------------------------

// smellPass turns new LAYER_VIOLATION smell instances into events. The
// snapshot's smell list is authoritative — it was computed by smell detection with
// full graph context — so this reuses signed-off analysis instead of
// recomputing it. affected stays nil to match the component inference convention; the smell
// evidence names the violating edges.
func smellPass(in ClassifyInput) []ClassifiedChange {
	base := make(map[string]bool)
	for _, s := range in.BaseSnap.Smells {
		base[smellKeyOf(s)] = true
	}
	var changes []ClassifiedChange
	t := in.Meta.Timestamp
	for _, s := range in.HeadSnap.Smells {
		if s.Kind != archmodel.SmellLayerViolation || base[smellKeyOf(s)] {
			continue
		}
		summary := "Layer violation introduced: " + s.Title
		changes = append(changes, newClassified(in, archmodel.EventLayerViolation,
			nil, s.AffectedIDs, summary, 0.9,
			evItem(evidence.SourceRule, s.Title, s.Suggestion, 0.9, t),
			evItem(evidence.SourceCode, in.Meta.Hash, "violating edges: "+strings.Join(s.AffectedIDs, ", "), 0.9, t)))
	}
	return changes
}

// smellKeyOf identifies a smell instance across snapshots (kind, title,
// sorted affected IDs).
func smellKeyOf(s archmodel.ArchSmell) string {
	aff := append([]string(nil), s.AffectedIDs...)
	sort.Strings(aff)
	return string(s.Kind) + "\x00" + s.Title + "\x00" + strings.Join(aff, ",")
}

// ---------------------------------------------------------------------------
// Pass 3: graph-level additions
// ---------------------------------------------------------------------------

// graphPass maps AKG structural additions to events. Node-based rules skip
// nodes owned by a snapshot component — the component pass already reported
// them (as component events) and a double report would create two IDs for
// one fact.
func graphPass(in ClassifyInput) []ClassifiedChange {
	var changes []ClassifiedChange
	gd := in.GraphDiff
	t := in.Meta.Timestamp
	owned := headOwnedNodes(in)

	// R1/R2: new/removed MODULE nodes not owned by a component.
	for _, n := range gd.NodesAdded {
		if n.Kind != "MODULE" || owned[n.ID] {
			continue
		}
		changes = append(changes, newClassified(in, archmodel.EventServiceAdded,
			[]string{n.ID}, []string{nameOf(n)},
			"Module appeared: "+nameOf(n), 0.85,
			evItem(evidence.SourceCode, n.File, "added MODULE node "+n.ID+" ("+nameOf(n)+")", 0.9, t)))
	}
	for _, n := range gd.NodesRemoved {
		if n.Kind != "MODULE" || owned[n.ID] {
			continue
		}
		changes = append(changes, newClassified(in, archmodel.EventServiceRemoved,
			[]string{n.ID}, []string{nameOf(n)},
			"Module removed: "+nameOf(n), 0.85,
			evItem(evidence.SourceCode, n.File, "removed MODULE node "+n.ID+" ("+nameOf(n)+")", 0.9, t)))
	}

	// R3: datastore edges (QUERIES_DB) grouped per datastore node.
	datastores := groupEdgesByTarget(gd.EdgesAdded, link.EdgeQueriesDB)
	for _, tgt := range sortedKeys(datastores) {
		edges := datastores[tgt]
		names := sourcesOf(edges)
		changes = append(changes, newClassified(in, archmodel.EventDataStoreAdded,
			[]string{tgt}, names,
			"Data store queried: "+tgt, 0.9,
			evItem(evidence.SourceCode, edgeFile(in.HeadGraph, tgt), "new QUERIES_DB edges: "+tgt+" <- "+strings.Join(names, ", "), 0.9, t)))
	}

	// R4: async hops (PUBLISHES / SUBSCRIBES / DISPATCHES_EVENT) grouped per
	// source->target pair.
	async := groupEdgesByPair(gd.EdgesAdded, link.EdgePublishes, link.EdgeSubscribes, link.EdgeDispatchesEvent)
	for _, k := range sortedKeys(async) {
		parts := strings.SplitN(k, "\x00", 2)
		changes = append(changes, newClassified(in, archmodel.EventAsyncIntroduced,
			[]string{parts[0], parts[1]}, []string{parts[0], parts[1]},
			"Async communication introduced: "+parts[0]+" -> "+parts[1], 0.9,
			evItem(evidence.SourceCode, in.Meta.Hash, "new async edge: "+k, 0.9, t)))
	}

	// R5: caching — edges to cache-technology nodes, grouped per cache node.
	cacheGroups := groupEdgesByCacheTarget(gd.EdgesAdded, in.HeadGraph)
	for _, tgt := range sortedKeys(cacheGroups) {
		edges := cacheGroups[tgt]
		names := sourcesOf(edges)
		changes = append(changes, newClassified(in, archmodel.EventCachingAdded,
			[]string{tgt}, names,
			"Cache layer added: "+tgt, 0.85,
			evItem(evidence.SourceHeuristic, "cache-pattern: "+tgt, "new edges to cache target "+tgt+" from: "+strings.Join(names, ", "), 0.85, t)))
	}

	// R11: API endpoints — new EXPOSES_ENDPOINT edges, grouped per API node.
	api := groupEdgesBySource(gd.EdgesAdded, link.EdgeExposesEndpoint)
	for _, src := range sortedKeys(api) {
		edges := api[src]
		names := targetsOf(edges)
		changes = append(changes, newClassified(in, archmodel.EventAPIAdded,
			[]string{src}, append([]string{src}, names...),
			"API endpoints exposed by: "+src, 0.9,
			evItem(evidence.SourceCode, edgeFile(in.HeadGraph, src), "new EXPOSES_ENDPOINT edges: "+src+" -> "+strings.Join(names, ", "), 0.9, t)))
	}

	// R12: security hardening — edges carrying security meaning.
	security := groupEdgesBySource(gd.EdgesAdded, link.EdgeSecuritySink, link.EdgeVulnerable)
	for _, src := range sortedKeys(security) {
		edges := security[src]
		names := targetsOf(edges)
		changes = append(changes, newClassified(in, archmodel.EventSecurityAdded,
			[]string{src}, append([]string{src}, names...),
			"Security layer hardened: "+src, 0.85,
			evItem(evidence.SourceCode, edgeFile(in.HeadGraph, src), "new security edges: "+src+" -> "+strings.Join(names, ", "), 0.85, t)))
	}
	return changes
}

// headOwnedNodes collects every node ID owned by a head component.
func headOwnedNodes(in ClassifyInput) map[string]bool {
	owned := make(map[string]bool)
	if in.HeadSnap == nil {
		return owned
	}
	for _, c := range in.HeadSnap.Components {
		for _, id := range c.NodeIDs {
			owned[id] = true
		}
	}
	return owned
}

// groupEdgesByTarget groups added edges of the given types by target node.
func groupEdgesByTarget(edges []akg.DiffEdge, types ...link.RelationshipType) map[string][]akg.DiffEdge {
	out := make(map[string][]akg.DiffEdge)
	for _, e := range edges {
		if !edgeTypeIn(e.Type, types) {
			continue
		}
		out[e.TargetID] = append(out[e.TargetID], e)
	}
	return out
}

// groupEdgesBySource groups added edges of the given types by source node.
func groupEdgesBySource(edges []akg.DiffEdge, types ...link.RelationshipType) map[string][]akg.DiffEdge {
	out := make(map[string][]akg.DiffEdge)
	for _, e := range edges {
		if !edgeTypeIn(e.Type, types) {
			continue
		}
		out[e.SourceID] = append(out[e.SourceID], e)
	}
	return out
}

// groupEdgesByPair groups added edges of the given types by source-target pair.
func groupEdgesByPair(edges []akg.DiffEdge, types ...link.RelationshipType) map[string][]akg.DiffEdge {
	out := make(map[string][]akg.DiffEdge)
	for _, e := range edges {
		if !edgeTypeIn(e.Type, types) {
			continue
		}
		out[e.SourceID+"\x00"+e.TargetID] = append(out[e.SourceID+"\x00"+e.TargetID], e)
	}
	return out
}

// groupEdgesByCacheTarget groups added edges whose target NAME matches a
// cache-technology pattern (head graph lookup; ID fallback). Word boundaries
// prevent substring false positives.
func groupEdgesByCacheTarget(edges []akg.DiffEdge, head *akg.CodePropertyGraph) map[string][]akg.DiffEdge {
	out := make(map[string][]akg.DiffEdge)
	nodeName := func(id string) string {
		if head != nil && head.Nodes != nil {
			if n, ok := head.Nodes.Get(id); ok && n != nil {
				return n.Name
			}
		}
		return id
	}
	for _, e := range edges {
		if !cacheTargetRegex.MatchString(nodeName(e.TargetID)) {
			continue
		}
		out[e.TargetID] = append(out[e.TargetID], e)
	}
	return out
}

func edgeTypeIn(typ string, types []link.RelationshipType) bool {
	for _, t := range types {
		if string(t) == typ {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Pass 4: cycles via SCC comparison
// ---------------------------------------------------------------------------

// cyclePass compares the strongly connected components of the base and head
// graphs. New multi-node SCCs are introduced cycles; vanished ones are
// resolved cycles. The SCC comparison runs on the replayed graphs when both
// are available and falls back to the metric delta otherwise — both produce
// affected=nil to match the component inference convention, with the cycle members named in
// the evidence.
func cyclePass(in ClassifyInput) []ClassifiedChange {
	var changes []ClassifiedChange
	t := in.Meta.Timestamp

	baseCycles := cycleMembers(in.BaseGraph)
	headCycles := cycleMembers(in.HeadGraph)
	if in.BaseGraph != nil && in.HeadGraph != nil {
		baseSet := cycleSet(baseCycles)
		headSet := cycleSet(headCycles)
		var introduced, resolved [][]string
		for _, c := range headCycles {
			if !baseSet[cycleKey(c)] {
				introduced = append(introduced, c)
			}
		}
		for _, c := range baseCycles {
			if !headSet[cycleKey(c)] {
				resolved = append(resolved, c)
			}
		}
		sortCycles(introduced)
		sortCycles(resolved)
		for _, c := range introduced {
			changes = append(changes, newClassified(in, archmodel.EventCycleIntroduced,
				nil, c, "Circular dependency introduced: "+strings.Join(c, " -> "), 0.9,
				evItem(evidence.SourceCode, in.Meta.Hash, "new cycle members: "+strings.Join(c, ", "), 0.9, t)))
		}
		for _, c := range resolved {
			changes = append(changes, newClassified(in, archmodel.EventCycleResolved,
				nil, c, "Circular dependency resolved: "+strings.Join(c, " -> "), 0.9,
				evItem(evidence.SourceCode, in.Meta.Hash, "cycle no longer present: "+strings.Join(c, ", "), 0.9, t)))
		}
		return changes
	}

	// Fallback: metric delta on snapshots.
	if in.BaseSnap == nil || in.HeadSnap == nil {
		return changes
	}
	b, h := in.BaseSnap.Metrics.CycleCount, in.HeadSnap.Metrics.CycleCount
	switch {
	case h > b:
		changes = append(changes, newClassified(in, archmodel.EventCycleIntroduced,
			nil, nil, "Circular dependencies increased: now "+itoa(h)+" cycles", 0.85,
			evItem(evidence.SourceRule, "metrics.cycle_count", "cycle count "+itoa(b)+" -> "+itoa(h), 0.85, t)))
	case h < b:
		changes = append(changes, newClassified(in, archmodel.EventCycleResolved,
			nil, nil, "Circular dependencies reduced: now "+itoa(h)+" cycles", 0.85,
			evItem(evidence.SourceRule, "metrics.cycle_count", "cycle count "+itoa(b)+" -> "+itoa(h), 0.85, t)))
	}
	return changes
}

// cycleMembers returns the multi-node SCCs (cycles) of a graph via the
// signed-off iterative Tarjan, or nil when the graph is unavailable.
func cycleMembers(graph *akg.CodePropertyGraph) [][]string {
	if graph == nil {
		return nil
	}
	all := arch_intelligence.SCC(graph)
	var out [][]string
	for _, c := range all {
		if len(c) > 1 {
			out = append(out, c)
		}
	}
	return out
}

func cycleKey(c []string) string {
	return strings.Join(c, "\x00")
}

func cycleSet(cycles [][]string) map[string]bool {
	out := make(map[string]bool, len(cycles))
	for _, c := range cycles {
		out[cycleKey(c)] = true
	}
	return out
}

func sortCycles(cycles [][]string) {
	sort.Slice(cycles, func(i, j int) bool {
		return cycleKey(cycles[i]) < cycleKey(cycles[j])
	})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func componentsByName(snap *archmodel.ArchSnapshot) map[string]archmodel.DetectedComponent {
	out := make(map[string]archmodel.DetectedComponent)
	if snap == nil {
		return out
	}
	for _, c := range snap.Components {
		out[c.Name] = c
	}
	return out
}

func componentIDByName(byName map[string]archmodel.DetectedComponent, name string) string {
	if c, ok := byName[name]; ok {
		return c.ID
	}
	return name
}

func newClassified(in ClassifyInput, kind archmodel.EventKind, affected, names []string, summary string, confidence float64, items ...evidence.EvidenceItem) ClassifiedChange {
	b := evidence.Bundle{}
	for _, it := range items {
		b.Add(it)
	}
	if in.Meta != nil && in.Meta.Hash != "" && len(b.Items) == 0 {
		b.Add(evItem(evidence.SourceGit, in.Meta.Hash, summary, confidence, in.Meta.Timestamp))
	}
	return ClassifiedChange{
		Kind:        kind,
		AffectedIDs: affected,
		Names:       names,
		Confidence:  confidence,
		Summary:     summary,
		Evidence:    b,
	}
}

func evItem(src evidence.Source, ref, excerpt string, confidence float64, ts time.Time) evidence.EvidenceItem {
	return evidence.EvidenceItem{
		Source:     src,
		Reference:  ref,
		Excerpt:    excerpt,
		Confidence: confidence,
		Timestamp:  ts,
	}
}

func sortedKeys(m map[string][]akg.DiffEdge) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sourcesOf(edges []akg.DiffEdge) []string {
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		out = append(out, e.SourceID)
	}
	return out
}

func targetsOf(edges []akg.DiffEdge) []string {
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		out = append(out, e.TargetID)
	}
	return out
}

func edgeFile(graph *akg.CodePropertyGraph, id string) string {
	if graph == nil || graph.Nodes == nil {
		return ""
	}
	if n, ok := graph.Nodes.Get(id); ok && n != nil {
		return n.FileSpec.Path
	}
	return ""
}

func nameOf(n akg.DiffNode) string {
	if n.Name != "" {
		return n.Name
	}
	return n.ID
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
