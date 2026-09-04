package arch_timeline

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// TestDiff_SplitRequiresRealCoverage distinguishes the correct split formula
// from the broken one.
//
// Regression: coverage was computed as |⋃targets| / |source|, counting every
// node of every overlapping target — including nodes the source never had — so
// the ratio routinely exceeded 1.0 and the 0.6 gate passed for any removed
// component sharing a single node with two added ones. The correct measure is
// |source ∩ ⋃targets| / |source|.
//
// Here "legacy" has 10 nodes and the two added components together cover only
// 2 of them (n1, n2) while carrying 16 unrelated nodes. Broken formula:
// 18/10 = 1.8 → split. Correct formula: 2/10 = 0.2 → not a split.
func TestDiff_SplitRequiresRealCoverage(t *testing.T) {
	base := diffSnapshot(
		[]archmodel.DetectedComponent{{
			ID:      "l",
			Name:    "legacy",
			NodeIDs: []string{"n1", "n2", "n3", "n4", "n5", "n6", "n7", "n8", "n9", "n10"},
		}},
		nil, nil, archmodel.ArchMetrics{}, "b")

	head := diffSnapshot(
		[]archmodel.DetectedComponent{
			{ID: "a", Name: "alpha", NodeIDs: []string{"n1", "x1", "x2", "x3", "x4", "x5", "x6", "x7"}},
			{ID: "z", Name: "zeta", NodeIDs: []string{"n2", "y1", "y2", "y3", "y4", "y5", "y6", "y7"}},
		},
		nil, nil, archmodel.ArchMetrics{}, "h")

	res := Diff(base, head)
	if len(res.Splits) != 0 {
		t.Errorf("two components covering 2 of 10 source nodes must not be reported as a split, got %+v", res.Splits)
	}

	// The inverse must not be reported as a merge either.
	res2 := Diff(head, base)
	if len(res2.Merges) != 0 {
		t.Errorf("inverse must not be reported as a merge, got %+v", res2.Merges)
	}
}

// TestDiff_SplitStillDetectedOnGenuineCoverage guards against over-correcting:
// a real split, where the targets do account for most of the source, must
// still be detected.
func TestDiff_SplitStillDetectedOnGenuineCoverage(t *testing.T) {
	base := diffSnapshot(
		[]archmodel.DetectedComponent{{
			ID: "b", Name: "billing", NodeIDs: []string{"n1", "n2", "n3", "n4", "n5"},
		}},
		nil, nil, archmodel.ArchMetrics{}, "b")
	head := diffSnapshot(
		[]archmodel.DetectedComponent{
			{ID: "c", Name: "billing-core", NodeIDs: []string{"n1", "n2", "n3"}},
			{ID: "w", Name: "billing-web", NodeIDs: []string{"n4", "extra"}},
		},
		nil, nil, archmodel.ArchMetrics{}, "h")

	res := Diff(base, head)
	if len(res.Splits) != 1 {
		t.Fatalf("genuine split (4 of 5 source nodes covered) not detected: %+v", res.Splits)
	}
}
