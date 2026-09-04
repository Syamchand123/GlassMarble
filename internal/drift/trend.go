package drift

import "sort"

// The rest of this package answers "does the architecture violate its declared
// invariants right now" — a conformance lint against a single snapshot. That
// is worth having, but it is not drift: a repository that has been equally
// non-conformant for two years and one that broke its layering yesterday
// produce identical reports, and `gmb drift` could not tell you which you were
// looking at.
//
// Drift is movement. AnalyzeTrend supplies the missing axis by comparing two
// conformance reports taken at different points in history, so the question
// becomes "what has changed, and in which direction".

// ViolationKey identifies a violation across snapshots. Two findings are the
// same finding when they are the same kind of breach between the same two
// nodes; the human-readable message and the layer labels are derived from the
// configuration in force at the time and must not participate, or a reworded
// layer name would read as a violation being fixed and an identical one
// introduced.
type ViolationKey struct {
	Kind     ViolationKind
	SourceID string
	TargetID string
}

// Key returns the cross-snapshot identity of a violation.
func (v Violation) Key() ViolationKey {
	return ViolationKey{Kind: v.Kind, SourceID: v.SourceID, TargetID: v.TargetID}
}

// TrendReport is the architectural movement between a baseline and a head.
//
// Introduced is the number that matters: violations that exist now and did not
// exist at the baseline. That is drift in the ordinary sense of the word, and
// it is the set a CI gate should fail on, because it is the set this change is
// responsible for. Resolved is the same measurement pointed the other way, and
// is what makes a cleanup visible instead of merely less bad.
type TrendReport struct {
	BaseCommit string `json:"base_commit"`
	HeadCommit string `json:"head_commit"`
	BaseAt     string `json:"base_at,omitempty"`
	HeadAt     string `json:"head_at,omitempty"`

	Introduced []Violation `json:"introduced"`
	Resolved   []Violation `json:"resolved"`
	Persisting []Violation `json:"persisting"`

	BaseViolations int `json:"base_violations"`
	HeadViolations int `json:"head_violations"`

	BaseCycleCount int `json:"base_cycle_count"`
	HeadCycleCount int `json:"head_cycle_count"`
	CycleBudget    int `json:"cycle_budget"`
}

// Worsened reports whether the architecture moved away from its declared
// intent. Note that this is not "head has violations": a repository can carry
// a long-standing backlog of accepted violations and still not be drifting.
// Only newly introduced breaches, or a cycle count that grew, count as
// movement in the wrong direction.
func (t *TrendReport) Worsened() bool {
	if t == nil {
		return false
	}
	return len(t.Introduced) > 0 || t.HeadCycleCount > t.BaseCycleCount
}

// Improved reports whether the architecture moved toward its declared intent
// with no offsetting regression.
func (t *TrendReport) Improved() bool {
	if t == nil {
		return false
	}
	return !t.Worsened() && (len(t.Resolved) > 0 || t.HeadCycleCount < t.BaseCycleCount)
}

// NetViolationDelta is the signed change in total violation count. It is
// reported alongside Introduced/Resolved rather than instead of them: a change
// that fixes three violations and introduces three others nets to zero while
// having drifted three violations' worth.
func (t *TrendReport) NetViolationDelta() int {
	if t == nil {
		return 0
	}
	return t.HeadViolations - t.BaseViolations
}

// AnalyzeTrend diffs two conformance reports. base may be nil, which is the
// honest representation of "no history to compare against" — the first ever
// analysis — and yields every current violation as Introduced only when the
// caller has a baseline to speak of; with a nil base nothing is attributed as
// newly introduced, because with no earlier observation there is no evidence
// any of it is new.
func AnalyzeTrend(base, head *Report) *TrendReport {
	t := &TrendReport{}
	if head == nil {
		return t
	}

	t.HeadViolations = len(head.Violations)
	t.HeadCycleCount = head.CycleCount
	t.CycleBudget = head.CycleBudget

	if base == nil {
		// Everything currently failing is "persisting": it is the state of the
		// world, not a regression this comparison can attribute to anyone.
		t.Persisting = append(t.Persisting, head.Violations...)
		sortViolations(t.Persisting)
		t.BaseCycleCount = head.CycleCount
		t.BaseViolations = len(head.Violations)
		return t
	}

	t.BaseViolations = len(base.Violations)
	t.BaseCycleCount = base.CycleCount

	baseSet := make(map[ViolationKey]Violation, len(base.Violations))
	for _, v := range base.Violations {
		baseSet[v.Key()] = v
	}
	headSet := make(map[ViolationKey]bool, len(head.Violations))

	for _, v := range head.Violations {
		headSet[v.Key()] = true
		if _, existed := baseSet[v.Key()]; existed {
			t.Persisting = append(t.Persisting, v)
			continue
		}
		t.Introduced = append(t.Introduced, v)
	}
	for _, v := range base.Violations {
		if !headSet[v.Key()] {
			t.Resolved = append(t.Resolved, v)
		}
	}

	sortViolations(t.Introduced)
	sortViolations(t.Resolved)
	sortViolations(t.Persisting)
	return t
}

// sortViolations orders findings deterministically so two runs over the same
// pair of snapshots produce byte-identical output.
func sortViolations(vs []Violation) {
	sort.SliceStable(vs, func(i, j int) bool {
		a, b := vs[i], vs[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.SourceID != b.SourceID {
			return a.SourceID < b.SourceID
		}
		return a.TargetID < b.TargetID
	})
}
