package arch_intelligence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// CommitMeta holds metadata about a git commit for event generation.
type CommitMeta struct {
	Hash      string
	Timestamp time.Time
	// BaseHash is the commit of the snapshot being diffed against, stamped
	// onto every generated event so consumers can tell a single-commit change
	// from a change accumulated over a range.
	BaseHash string
}

// defaultCouplingChangePct is the instability delta that counts as a
// coupling change when no config is available (config.CouplingChangePct).
const defaultCouplingChangePct = 0.2

// eventID derives a deterministic 16-hex event id from the commit, kind and
// affected ids, so identical changes produce identical events.
func eventID(commit string, kind archmodel.EventKind, affected []string) string {
	parts := append([]string{commit, string(kind)}, affected...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "evt_" + hex.EncodeToString(sum[:8])
}

// EventID is the exported form of the deterministic event-ID scheme shared by
// every event producer (component inference snapshot diff and commit reasoning).
//
// commit reasoning must produce the exact same ID for the same (commit, kind, affected)
// tuple so the developer memory memory builder can deduplicate the two generators: the
// commit reasoning event (enriched with intent, PR refs and impact) is appended first
// and the identical component inference event is dropped. Producers therefore must agree
// on the canonical ordering of affected IDs — see the classifier contract in
// internal/commit_reasoning.
func EventID(commit string, kind archmodel.EventKind, affected []string) string {
	return eventID(commit, kind, affected)
}

// newEvent builds an ArchEvent with a deterministic ID and populated evidence.
func newEvent(meta CommitMeta, kind archmodel.EventKind, title string, components, affected []string, excerpt string, confidence float64) archmodel.ArchEvent {
	b := evidence.Bundle{PrimarySource: evidence.SourceGit}
	b.Add(evidence.EvidenceItem{
		Source:     evidence.SourceGit,
		Reference:  meta.Hash,
		Excerpt:    excerpt,
		Confidence: confidence,
		Timestamp:  meta.Timestamp,
	})
	return archmodel.ArchEvent{
		ID:             eventID(meta.Hash, kind, affected),
		Kind:           kind,
		CommitHash:     meta.Hash,
		BaseCommitHash: meta.BaseHash,
		Timestamp:      meta.Timestamp,
		Title:          title,
		Components:     components,
		AffectedIDs:    affected,
		Evidence:       b,
		IntentSrc:      evidence.SourceGit,
		ValidFrom:      meta.Timestamp,
	}
}

// snapshotHasComponent checks whether a component exists in a snapshot by ID,
// falling back to name for snapshots built before stable IDs existed.
func snapshotHasComponent(snap *archmodel.ArchSnapshot, comp archmodel.DetectedComponent) bool {
	if snap == nil {
		return false
	}
	for _, c := range snap.Components {
		if comp.ID != "" && c.ID == comp.ID {
			return true
		}
		if comp.ID == "" && c.Name != "" && c.Name == comp.Name {
			return true
		}
	}
	return false
}

// componentByName finds a component in a snapshot by ID, falling back to name.
func componentByName(snap *archmodel.ArchSnapshot, comp archmodel.DetectedComponent) (archmodel.DetectedComponent, bool) {
	if snap == nil {
		return archmodel.DetectedComponent{}, false
	}
	for _, c := range snap.Components {
		if comp.ID != "" && c.ID == comp.ID {
			return c, true
		}
		if comp.ID == "" && c.Name != "" && c.Name == comp.Name {
			return c, true
		}
	}
	return archmodel.DetectedComponent{}, false
}

// GenerateEvents produces ArchEvents by comparing two snapshots. Event IDs
// are deterministic (sha256 of commit, kind, affected ids). The comparison
// covers components, component dependencies, patterns, smells, cycles, dead
// code, per-component coupling and layer violations.
func GenerateEvents(
	base *archmodel.ArchSnapshot,
	head *archmodel.ArchSnapshot,
	graphDiff *akg.GraphDiff,
	commitMeta CommitMeta,
) []archmodel.ArchEvent {
	var events []archmodel.ArchEvent
	if head == nil {
		return events
	}
	if commitMeta.Timestamp.IsZero() {
		commitMeta.Timestamp = time.Now()
	}
	// The caller already has the baseline; take the commit from it rather than
	// making every call site remember to pass it.
	if commitMeta.BaseHash == "" && base != nil {
		commitMeta.BaseHash = base.CommitHash
	}
	// head is guarded above but base was dereferenced in nine places, so a nil
	// baseline panicked instead of meaning anything. It has an obvious meaning:
	// the first analysis diffs against an empty architecture, which yields
	// additions and no removals. Substituting the empty snapshot says that once
	// rather than threading a nil check through every comparison below.
	if base == nil {
		base = &archmodel.ArchSnapshot{}
	}

	// 1. Component additions/removals.
	baseComps := make(map[string]bool)
	for _, c := range head.Components {
		if snapshotHasComponent(base, c) {
			baseComps[c.ID] = true
		}
	}
	added := make([]archmodel.DetectedComponent, 0)
	removed := make([]archmodel.DetectedComponent, 0)
	for _, c := range head.Components {
		if !baseComps[c.ID] {
			added = append(added, c)
		}
	}
	for _, c := range base.Components {
		if _, ok := componentByName(head, c); !ok {
			removed = append(removed, c)
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i].Name < added[j].Name })
	sort.Slice(removed, func(i, j int) bool { return removed[i].Name < removed[j].Name })
	for _, c := range added {
		events = append(events, newEvent(commitMeta, archmodel.EventServiceAdded,
			"Component Added: "+c.Name, []string{compKey(c)}, []string{c.ID},
			"Component "+c.Name+" first appeared in the architecture.", 0.95))
	}
	for _, c := range removed {
		events = append(events, newEvent(commitMeta, archmodel.EventServiceRemoved,
			"Component Removed: "+c.Name, []string{compKey(c)}, []string{c.ID},
			"Component "+c.Name+" no longer exists in the architecture.", 0.95))
	}

	// 2. Component-level dependency changes.
	depKey := func(src, tgt string) string { return src + "\x00" + tgt }
	baseDeps := make(map[string]bool)
	for _, c := range base.Components {
		for _, d := range c.Dependencies {
			baseDeps[depKey(c.ID, d)] = true
		}
	}
	depEvents := make(map[string]archmodel.EventKind)
	for _, c := range head.Components {
		for _, d := range c.Dependencies {
			if !baseDeps[depKey(c.ID, d)] {
				depEvents[depKey(c.ID, d)] = archmodel.EventDependencyAdded
			}
		}
	}
	for _, c := range base.Components {
		for _, d := range c.Dependencies {
			found := false
			for _, hc := range head.Components {
				if hc.ID == c.ID {
					for _, hd := range hc.Dependencies {
						if hd == d {
							found = true
							break
						}
					}
				}
			}
			if !found {
				depEvents[depKey(c.ID, d)] = archmodel.EventDependencyRemoved
			}
		}
	}
	keys := make([]string, 0, len(depEvents))
	for k := range depEvents {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts := strings.SplitN(k, "\x00", 2)
		kind := depEvents[k]
		events = append(events, newEvent(commitMeta, kind,
			string(kind)+": "+parts[0]+" <-> "+parts[1], []string{parts[0], parts[1]},
			[]string{parts[0], parts[1]},
			"Component dependency between "+parts[0]+" and "+parts[1]+" changed.", 0.9))
	}

	// 3. Pattern changes.
	basePatterns := make(map[archmodel.PatternKind]bool)
	for _, p := range base.Patterns {
		basePatterns[p.Kind] = true
	}
	headPatterns := make(map[archmodel.PatternKind]bool)
	for _, p := range head.Patterns {
		headPatterns[p.Kind] = true
	}
	var patternKinds []archmodel.PatternKind
	for k := range headPatterns {
		patternKinds = append(patternKinds, k)
	}
	for k := range basePatterns {
		if _, ok := headPatterns[k]; !ok {
			patternKinds = append(patternKinds, k)
		}
	}
	sort.Slice(patternKinds, func(i, j int) bool { return patternKinds[i] < patternKinds[j] })
	for _, k := range patternKinds {
		if !basePatterns[k] {
			events = append(events, newEvent(commitMeta, archmodel.EventPatternDetected,
				"Pattern Detected: "+string(k), []string{string(k)}, []string{string(k)},
				"Architecture pattern "+string(k)+" was detected.", 0.9))
		} else if !headPatterns[k] {
			events = append(events, newEvent(commitMeta, archmodel.EventPatternLost,
				"Pattern Lost: "+string(k), []string{string(k)}, []string{string(k)},
				"Architecture pattern "+string(k)+" is no longer detected.", 0.9))
		}
	}

	// 4. Smell changes.
	baseSmells := make(map[archmodel.SmellKind]int)
	for _, s := range base.Smells {
		baseSmells[s.Kind]++
	}
	headSmells := make(map[archmodel.SmellKind]int)
	for _, s := range head.Smells {
		headSmells[s.Kind]++
	}
	var smellKinds []archmodel.SmellKind
	for k := range headSmells {
		smellKinds = append(smellKinds, k)
	}
	for k := range baseSmells {
		if _, ok := headSmells[k]; !ok {
			smellKinds = append(smellKinds, k)
		}
	}
	sort.Slice(smellKinds, func(i, j int) bool { return smellKinds[i] < smellKinds[j] })
	for _, k := range smellKinds {
		bc, hc := baseSmells[k], headSmells[k]
		if hc > 0 && bc == 0 {
			events = append(events, newEvent(commitMeta, archmodel.EventSmellDetected,
				"Smell Detected: "+string(k), nil, []string{string(k)},
				"Architecture smell "+string(k)+" was introduced.", 0.9))
		} else if hc == 0 && bc > 0 {
			events = append(events, newEvent(commitMeta, archmodel.EventSmellResolved,
				"Smell Resolved: "+string(k), nil, []string{string(k)},
				"Architecture smell "+string(k)+" was resolved.", 0.9))
		}
	}

	// 5. Dead code, cycles, layer violations.
	if head.Metrics.DeadCodeNodeCount > 0 && base.Metrics.DeadCodeNodeCount == 0 {
		events = append(events, newEvent(commitMeta, archmodel.EventDeadCodeDetected,
			"Dead Code Detected", nil, nil,
			"Unreachable code nodes appeared in the architecture.", 0.85))
	}
	switch {
	case head.Metrics.CycleCount > base.Metrics.CycleCount:
		events = append(events, newEvent(commitMeta, archmodel.EventCycleIntroduced,
			"Cycle Introduced", nil, nil,
			"New circular dependencies appeared (now "+itoa(head.Metrics.CycleCount)+" cycles).", 0.9))
	case head.Metrics.CycleCount < base.Metrics.CycleCount:
		events = append(events, newEvent(commitMeta, archmodel.EventCycleResolved,
			"Cycle Resolved", nil, nil,
			"Circular dependencies were reduced (now "+itoa(head.Metrics.CycleCount)+" cycles).", 0.9))
	}
	if head.Metrics.LayerViolationCount > 0 && base.Metrics.LayerViolationCount == 0 {
		events = append(events, newEvent(commitMeta, archmodel.EventLayerViolation,
			"Layer Violation", nil, nil,
			"Edges violating the declared layering appeared.", 0.9))
	}

	// 6. Per-component coupling changes beyond the threshold.
	for _, hc := range head.Components {
		baseComp, ok := componentByName(base, hc)
		if !ok {
			continue
		}
		headInst := componentInstability(hc)
		baseInst := componentInstability(baseComp)
		delta := headInst - baseInst
		if delta > defaultCouplingChangePct {
			events = append(events, newEvent(commitMeta, archmodel.EventCouplingIncreased,
				"Coupling Increased: "+hc.Name, []string{compKey(hc)}, []string{hc.ID},
				"Instability of "+hc.Name+" increased by "+fmtFloat(delta), 0.85))
		} else if delta < -defaultCouplingChangePct {
			events = append(events, newEvent(commitMeta, archmodel.EventCouplingDecreased,
				"Coupling Decreased: "+hc.Name, []string{compKey(hc)}, []string{hc.ID},
				"Instability of "+hc.Name+" decreased by "+fmtFloat(-delta), 0.85))
		}
	}

	// Deterministic global ordering: kind, then title.
	sort.Slice(events, func(i, j int) bool {
		if events[i].Kind != events[j].Kind {
			return events[i].Kind < events[j].Kind
		}
		return events[i].Title < events[j].Title
	})
	return events
}

// compKey returns the canonical component key for memory events: the stable
// component ID (the single key space memory uses — dependency, coupling,
// added and removed events must all reference the same space or one
// component splits into several memory entries), falling back to the name
// for snapshots built before stable IDs existed.
func compKey(c archmodel.DetectedComponent) string {
	if c.ID != "" {
		return c.ID
	}
	return c.Name
}

// componentInstability returns the instability recorded on a component,
// falling back to an approximation from its dependency lists.
func componentInstability(c archmodel.DetectedComponent) float64 {
	if c.Instability != 0 || c.Ca+c.Ce == 0 {
		return c.Instability
	}
	if c.Ca+c.Ce > 0 {
		return float64(c.Ce) / float64(c.Ca+c.Ce)
	}
	return 0
}

func fmtFloat(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", v), "0"), ".")
}

func itoa(v int) string {
	return fmt.Sprintf("%d", v)
}
