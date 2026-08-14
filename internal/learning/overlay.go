package learning

import (
	"fmt"
	"strconv"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// TargetType identifies which kind of memory item a correction was applied
// to (for the audit trail).
type TargetType string

const (
	TargetClaim     TargetType = "claim"
	TargetEvent     TargetType = "event"
	TargetComponent TargetType = "component"
)

// AppliedCorrection is one audit entry: what was corrected, what was
// displayed before, what is displayed after, and whether the correction had
// an effect on the queried results. This is the "what did the system learn,
// when, from what feedback" answer for the master plan's auditability and
// reversibility requirement.
type AppliedCorrection struct {
	Correction Correction `json:"correction"`
	TargetType TargetType `json:"target_type"`
	// Before is the value displayed before this correction ("" when the
	// target was not found in the results).
	Before string `json:"before"`
	// After is the value displayed after this correction.
	After string `json:"after"`
	// Applied is false when the correction could not take effect (target
	// missing from the results, or kind not applicable to the target
	// type). The correction is still part of the audit log — it was
	// recorded, it just had nothing to act on this time.
	Applied bool `json:"applied"`
	// Note explains a non-applied correction ("target not found in
	// results", "kind not applicable to events", ...).
	Note string `json:"note,omitempty"`
}

// CorrectedResult is the overlay projection of a memory query: the original
// ranked results with every correction applied, plus the audit trail of
// what was applied. The JSON shape embeds the query-result fields directly
// and adds "corrections_applied" — backward compatible for consumers that
// read the pre-Phase-10 shape.
type CorrectedResult struct {
	*developer_memory.MemoryQueryResult
	// CorrectionsApplied lists every correction that targeted an item in
	// the results, in application order (oldest first).
	CorrectionsApplied []AppliedCorrection `json:"corrections_applied,omitempty"`
}

// Apply is the pure overlay engine (master plan §8.3). It takes a query
// result and the correction log and returns a projection in which:
//
//   - REJECT marks the item as rejected (still shown, flagged),
//   - ACCEPT marks the item as confirmed by the developer,
//   - CONFIDENCE overrides the displayed confidence,
//   - STATE overrides the displayed knowledge state,
//   - INTENT overrides an event's displayed intent,
//   - LABEL overrides a displayed name (claim subject, event title,
//     component name).
//
// The source-of-truth result is NEVER mutated: the projection is a fresh
// copy, and corrections are overlaid at query time so they are reflected
// immediately without touching the WALs (master plan §8.3). When several
// corrections target the same item, the last one by timestamp wins, and
// each audit entry records the value that was displayed just before it.
func Apply(result *developer_memory.MemoryQueryResult, corrections []Correction) *CorrectedResult {
	proj := &CorrectedResult{
		MemoryQueryResult: cloneQueryResult(result),
	}
	if len(corrections) == 0 {
		return proj
	}
	ordered := SortedCorrections(corrections)
	for _, c := range ordered {
		proj.applyOne(c)
	}
	return proj
}

// applyOne applies a single correction to the projection, appending its
// audit entry.
func (p *CorrectedResult) applyOne(c Correction) {
	if p == nil || p.MemoryQueryResult == nil {
		return
	}
	if i, ok := claimIndex(p.Claims)[c.TargetID]; ok {
		p.CorrectionsApplied = append(p.CorrectionsApplied, applyClaim(&p.Claims[i], c))
		return
	}
	if i, ok := eventIndex(p.Events)[c.TargetID]; ok {
		p.CorrectionsApplied = append(p.CorrectionsApplied, applyEvent(&p.Events[i], c))
		return
	}
	if i, ok := componentIndex(p.Components)[c.TargetID]; ok {
		p.CorrectionsApplied = append(p.CorrectionsApplied, applyComponent(&p.Components[i], c))
		return
	}
	p.CorrectionsApplied = append(p.CorrectionsApplied, AppliedCorrection{
		Correction: c,
		Applied:    false,
		Note:       "target not found in results",
	})
}

// applyClaim mutates the projected claim in place and returns its audit
// entry.
func applyClaim(claim *developer_memory.KnowledgeClaim, c Correction) AppliedCorrection {
	a := AppliedCorrection{Correction: c, TargetType: TargetClaim, Applied: true}
	switch c.Kind {
	case CorrectionKindReject:
		a.Before, a.After = "shown", "rejected"
	case CorrectionKindAccept:
		a.Before, a.After = "shown", "confirmed"
	case CorrectionKindConfidence:
		a.Before = fmt.Sprintf("%.3f", claim.Evidence.AggConfidence)
		a.After = clampedConfidence(c.CorrectedValue)
		if v, err := strconv.ParseFloat(a.After, 64); err == nil {
			claim.Evidence.AggConfidence = v
		}
	case CorrectionKindState:
		a.Before = string(claim.State)
		a.After = c.CorrectedValue
		claim.State = developer_memory.KnowledgeState(c.CorrectedValue)
	case CorrectionKindLabel:
		a.Before = claim.Subject
		a.After = c.CorrectedValue
		claim.Subject = c.CorrectedValue
	default: // INTENT does not apply to claims
		a.Applied, a.Note = false, "kind not applicable to claims"
	}
	return a
}

// applyEvent mutates the projected event in place and returns its audit
// entry.
func applyEvent(ev *archmodel.ArchEvent, c Correction) AppliedCorrection {
	a := AppliedCorrection{Correction: c, TargetType: TargetEvent, Applied: true}
	switch c.Kind {
	case CorrectionKindReject:
		a.Before, a.After = "shown", "rejected"
	case CorrectionKindAccept:
		a.Before, a.After = "shown", "confirmed"
	case CorrectionKindConfidence:
		a.Before = fmt.Sprintf("%.3f", ev.Evidence.AggConfidence)
		a.After = clampedConfidence(c.CorrectedValue)
		if v, err := strconv.ParseFloat(a.After, 64); err == nil {
			ev.Evidence.AggConfidence = v
		}
	case CorrectionKindIntent:
		a.Before = ev.Intent
		a.After = c.CorrectedValue
		ev.Intent = c.CorrectedValue
	case CorrectionKindLabel:
		a.Before = ev.Title
		a.After = c.CorrectedValue
		ev.Title = c.CorrectedValue
	default: // STATE does not apply to events (they have ValidFrom/Until, not a state)
		a.Applied, a.Note = false, "kind not applicable to events"
	}
	return a
}

// applyComponent mutates the projected component history in place and
// returns its audit entry.
func applyComponent(comp *developer_memory.ComponentHistory, c Correction) AppliedCorrection {
	a := AppliedCorrection{Correction: c, TargetType: TargetComponent, Applied: true}
	switch c.Kind {
	case CorrectionKindReject:
		a.Before, a.After = "shown", "rejected"
	case CorrectionKindAccept:
		a.Before, a.After = "shown", "confirmed"
	case CorrectionKindState:
		a.Before = string(comp.State)
		a.After = c.CorrectedValue
		comp.State = developer_memory.KnowledgeState(c.CorrectedValue)
	case CorrectionKindLabel:
		a.Before = comp.Name
		a.After = c.CorrectedValue
		comp.Name = c.CorrectedValue
	default: // CONFIDENCE/INTENT do not apply to components
		a.Applied, a.Note = false, "kind not applicable to components"
	}
	return a
}

// ApplyToMemory projects a full DeveloperMemory aggregate with corrections
// applied (used by the overview and --component paths, which render from
// the aggregate rather than a ranked query). Returns the projected
// aggregate and the audit trail. The source aggregate is never mutated.
//
// Rejected/confirmed items stay visible with their original values: the
// rejection flag lives in the audit trail, so the CLI can render a
// "(rejected)" marker without corrupting the temporal model.
func ApplyToMemory(mem *developer_memory.DeveloperMemory, corrections []Correction) (*developer_memory.DeveloperMemory, []AppliedCorrection) {
	if mem == nil {
		return nil, nil
	}
	proj := &developer_memory.DeveloperMemory{
		ProjectID:       mem.ProjectID,
		LastUpdated:     mem.LastUpdated,
		TotalEvents:     mem.TotalEvents,
		Timeline:        cloneSlice(mem.Timeline),
		ComponentMemory: make(map[string]developer_memory.ComponentHistory, len(mem.ComponentMemory)),
		GlobalMemory:    cloneSlice(mem.GlobalMemory),
		Events:          cloneSlice(mem.Events),
	}
	for name, h := range mem.ComponentMemory {
		hc := h
		hc.Claims = cloneSlice(h.Claims)
		hc.Events = cloneSlice(h.Events)
		proj.ComponentMemory[name] = hc
	}

	var applied []AppliedCorrection
	if len(corrections) == 0 {
		return proj, nil
	}

	claimIdx := make(map[string]int, len(proj.GlobalMemory))
	for i := range proj.GlobalMemory {
		claimIdx[proj.GlobalMemory[i].ID] = i
	}
	eventIdx := make(map[string]int, len(proj.Events))
	for i := range proj.Events {
		eventIdx[proj.Events[i].ID] = i
	}
	compIdx := make(map[string]struct{}, len(proj.ComponentMemory))
	for name := range proj.ComponentMemory {
		compIdx[name] = struct{}{}
	}

	for _, c := range SortedCorrections(corrections) {
		if i, ok := eventIdx[c.TargetID]; ok {
			applied = append(applied, applyEvent(&proj.Events[i], c))
			continue
		}
		if i, ok := claimIdx[c.TargetID]; ok {
			applied = append(applied, applyClaim(&proj.GlobalMemory[i], c))
			continue
		}
		if hasKey(compIdx, c.TargetID) {
			// Component corrections apply to the history under that
			// name. The name is both the map key and a display value;
			// only the display field is overridden, the key stays
			// canonical so lookups keep working.
			h := proj.ComponentMemory[c.TargetID]
			applied = append(applied, applyComponent(&h, c))
			proj.ComponentMemory[c.TargetID] = h
			continue
		}
		applied = append(applied, AppliedCorrection{
			Correction: c,
			Applied:    false,
			Note:       "target not found in memory",
		})
	}
	return proj, applied
}

// clampedConfidence normalizes a confidence correction into its [0,1]
// display form. The value was validated at record time; this defends
// against corrections constructed bypassing the store.
func clampedConfidence(v string) string {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return v
	}
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return strconv.FormatFloat(f, 'f', 3, 64)
}

func claimIndex(claims []developer_memory.KnowledgeClaim) map[string]int {
	idx := make(map[string]int, len(claims))
	for i, c := range claims {
		idx[c.ID] = i
	}
	return idx
}

func eventIndex(events []archmodel.ArchEvent) map[string]int {
	idx := make(map[string]int, len(events))
	for i, e := range events {
		idx[e.ID] = i
	}
	return idx
}

func componentIndex(comps []developer_memory.ComponentHistory) map[string]int {
	idx := make(map[string]int, len(comps))
	for i, c := range comps {
		idx[c.Name] = i
	}
	return idx
}

// hasKey reports whether m contains key.
func hasKey(m map[string]struct{}, key string) bool {
	_, ok := m[key]
	return ok
}

// cloneQueryResult deep-copies a query result so the overlay never mutates
// the caller's data.
func cloneQueryResult(res *developer_memory.MemoryQueryResult) *developer_memory.MemoryQueryResult {
	if res == nil {
		return &developer_memory.MemoryQueryResult{}
	}
	clone := *res
	clone.Components = cloneSlice(res.Components)
	clone.Claims = cloneSlice(res.Claims)
	clone.Events = cloneSlice(res.Events)
	clone.Timeline = cloneSlice(res.Timeline)
	return &clone
}

// cloneSlice copies a slice, preserving nil so an untouched result stays
// byte-identical to the pre-Phase-10 query output.
func cloneSlice[T any](in []T) []T {
	if in == nil {
		return nil
	}
	out := make([]T, len(in))
	copy(out, in)
	return out
}
