package drift

import "testing"

func v(kind ViolationKind, src, tgt, msg string) Violation {
	return Violation{Kind: kind, SourceID: src, TargetID: tgt, Message: msg}
}

func TestAnalyzeTrend_SplitsIntroducedResolvedPersisting(t *testing.T) {
	base := &Report{
		Violations: []Violation{
			v(KindForbiddenDep, "ui/a.go::A", "db/x.go::X", "ui -> db"),
			v(KindForbiddenDep, "ui/b.go::B", "db/y.go::Y", "ui -> db"),
		},
		CycleCount: 3, CycleBudget: 5,
	}
	head := &Report{
		Violations: []Violation{
			// b->y survives, a->x was fixed, c->z is new.
			v(KindForbiddenDep, "ui/b.go::B", "db/y.go::Y", "ui -> db"),
			v(KindForbiddenDep, "ui/c.go::C", "db/z.go::Z", "ui -> db"),
		},
		CycleCount: 4, CycleBudget: 5,
	}

	tr := AnalyzeTrend(base, head)

	if len(tr.Introduced) != 1 || tr.Introduced[0].SourceID != "ui/c.go::C" {
		t.Errorf("Introduced = %+v, want only ui/c.go::C", tr.Introduced)
	}
	if len(tr.Resolved) != 1 || tr.Resolved[0].SourceID != "ui/a.go::A" {
		t.Errorf("Resolved = %+v, want only ui/a.go::A", tr.Resolved)
	}
	if len(tr.Persisting) != 1 || tr.Persisting[0].SourceID != "ui/b.go::B" {
		t.Errorf("Persisting = %+v, want only ui/b.go::B", tr.Persisting)
	}
	// One fixed, one introduced: the totals are unchanged, but the
	// architecture did drift.
	if tr.NetViolationDelta() != 0 {
		t.Errorf("NetViolationDelta = %d, want 0", tr.NetViolationDelta())
	}
	if !tr.Worsened() {
		t.Error("a newly introduced violation must count as worsening even when the total is flat")
	}
}

// TestAnalyzeTrend_StableBacklogIsNotDrift is the distinction the static lint
// could not make: a repository equally non-conformant at both ends has not
// drifted, however many violations it carries.
func TestAnalyzeTrend_StableBacklogIsNotDrift(t *testing.T) {
	vs := []Violation{
		v(KindForbiddenDep, "ui/a.go::A", "db/x.go::X", "ui -> db"),
		v(KindForbiddenDep, "ui/b.go::B", "db/y.go::Y", "ui -> db"),
	}
	base := &Report{Violations: vs, CycleCount: 2}
	head := &Report{Violations: vs, CycleCount: 2}

	tr := AnalyzeTrend(base, head)
	if tr.Worsened() {
		t.Error("an unchanged backlog is not drift")
	}
	if len(tr.Introduced) != 0 || len(tr.Resolved) != 0 {
		t.Errorf("expected no movement, got introduced=%d resolved=%d",
			len(tr.Introduced), len(tr.Resolved))
	}
	if len(tr.Persisting) != 2 {
		t.Errorf("Persisting = %d, want 2", len(tr.Persisting))
	}
}

func TestAnalyzeTrend_CleanupIsVisible(t *testing.T) {
	base := &Report{
		Violations: []Violation{v(KindForbiddenDep, "ui/a.go::A", "db/x.go::X", "ui -> db")},
		CycleCount: 4,
	}
	head := &Report{CycleCount: 1}

	tr := AnalyzeTrend(base, head)
	if !tr.Improved() {
		t.Error("removing a violation and cutting cycles should register as improvement")
	}
	if tr.Worsened() {
		t.Error("a pure cleanup must not read as worsening")
	}
	if len(tr.Resolved) != 1 {
		t.Errorf("Resolved = %d, want 1", len(tr.Resolved))
	}
}

// TestAnalyzeTrend_GrowingCyclesWorsen: cycles are budgeted rather than
// enumerated as individual violations, so movement has to be read from the
// count.
func TestAnalyzeTrend_GrowingCyclesWorsen(t *testing.T) {
	tr := AnalyzeTrend(&Report{CycleCount: 2}, &Report{CycleCount: 7})
	if !tr.Worsened() {
		t.Error("cycle count growing from 2 to 7 is drift")
	}
}

// TestAnalyzeTrend_NilBaseAttributesNothing: with no earlier observation there
// is no evidence any current violation is new, so nothing may be reported as
// introduced — otherwise the first analysis of an old repository would blame
// whoever ran it for the entire backlog.
func TestAnalyzeTrend_NilBaseAttributesNothing(t *testing.T) {
	head := &Report{
		Violations: []Violation{
			v(KindForbiddenDep, "ui/a.go::A", "db/x.go::X", "ui -> db"),
		},
		CycleCount: 3,
	}
	tr := AnalyzeTrend(nil, head)
	if len(tr.Introduced) != 0 {
		t.Errorf("nil baseline must attribute nothing as introduced, got %+v", tr.Introduced)
	}
	if len(tr.Persisting) != 1 {
		t.Errorf("Persisting = %d, want 1", len(tr.Persisting))
	}
	if tr.Worsened() {
		t.Error("a first analysis cannot be a regression")
	}
}

// TestAnalyzeTrend_MessageChangeIsNotMovement: identity is the breach, not its
// rendering. Renaming a layer in config rewrites every message, and that must
// not read as the whole backlog being fixed and reintroduced.
func TestAnalyzeTrend_MessageChangeIsNotMovement(t *testing.T) {
	base := &Report{Violations: []Violation{{
		Kind: KindForbiddenDep, SourceID: "ui/a.go::A", TargetID: "db/x.go::X",
		SourceLayer: "ui", TargetLayer: "db", Message: "ui must not reach db",
	}}}
	head := &Report{Violations: []Violation{{
		Kind: KindForbiddenDep, SourceID: "ui/a.go::A", TargetID: "db/x.go::X",
		SourceLayer: "presentation", TargetLayer: "persistence",
		Message: "presentation must not reach persistence",
	}}}

	tr := AnalyzeTrend(base, head)
	if len(tr.Introduced) != 0 || len(tr.Resolved) != 0 {
		t.Errorf("relabelling layers is not architectural movement: introduced=%+v resolved=%+v",
			tr.Introduced, tr.Resolved)
	}
	if len(tr.Persisting) != 1 {
		t.Errorf("Persisting = %d, want 1", len(tr.Persisting))
	}
}

func TestAnalyzeTrend_NilHead(t *testing.T) {
	if tr := AnalyzeTrend(nil, nil); tr == nil || tr.Worsened() {
		t.Error("nil head must yield an empty, non-worsening report")
	}
}
