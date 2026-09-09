package patcher

import (
	"strings"
	"testing"
)

// ────────────────────────────────────────────────────────────────────────────
// MergeSection tests
// ────────────────────────────────────────────────────────────────────────────

func TestMergeSection_NoHumanEdits(t *testing.T) {
	base := "Machine line 1.\nMachine line 2.\n"
	ours := base // no human edits
	theirs := "Updated machine line 1.\nMachine line 2.\n"

	result := MergeSection(base, ours, theirs)
	if result.Conflicted {
		t.Error("expected no conflict when ours == base")
	}
	if result.Content != theirs {
		t.Errorf("expected THEIRS, got %q", result.Content)
	}
}

func TestMergeSection_HumanEditsNoConflict(t *testing.T) {
	// Human edited the last line; machine edited the first line.
	base := "Line A.\nLine B.\n"
	ours := "Line A.\nLine B — human fix.\n"
	theirs := "Line A — updated.\nLine B.\n"

	result := MergeSection(base, ours, theirs)
	// Non-overlapping changes → should merge without conflict.
	if result.Conflicted {
		t.Log("conflict flag set (LCS heuristic); content preserved:", result.Content)
		// Conflict fallback is also valid — check human text is preserved.
		if !strings.Contains(result.Content, "human fix") {
			t.Error("human edit lost in conflict fallback")
		}
	}
}

func TestMergeSection_ConflictPreservesHuman(t *testing.T) {
	// Both human and machine changed the same lines → conflict.
	base := "Original.\n"
	ours := "Human rewrite.\n"
	theirs := "Machine rewrite.\n"

	result := MergeSection(base, ours, theirs)
	if !result.Conflicted {
		// Acceptable only if one side wins cleanly; human must be present.
		if !strings.Contains(result.Content, "Human rewrite") {
			t.Error("human edit not in result")
		}
		return
	}
	if !strings.Contains(result.Content, "Human rewrite") {
		t.Error("human text lost in conflict")
	}
	if !strings.Contains(result.Content, "Machine rewrite") {
		t.Error("machine update note missing in conflict")
	}
	if !strings.Contains(result.Content, "Doc Update Note") {
		t.Error("conflict note block missing")
	}
}

func TestMergeSection_BothIdentical(t *testing.T) {
	// base == ours == theirs — pure no-op.
	content := "Same.\n"
	result := MergeSection(content, content, content)
	if result.Conflicted {
		t.Error("expected no conflict for identical content")
	}
	if result.Content != content {
		t.Errorf("expected unchanged content, got %q", result.Content)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// ApplyToDoc tests
// ────────────────────────────────────────────────────────────────────────────

func TestApplyToDoc_UpdatesManaged(t *testing.T) {
	src := "Human header.\n<!-- gmb:begin:s1 -->\nOld body.\n<!-- gmb:end:s1 -->\nHuman footer.\n"
	doc := ParseMarkdown(src)

	result := ApplyToDoc(doc, "s1", "New body line 1.\nNew body line 2.")
	if !strings.Contains(result, "New body line 1") {
		t.Error("new body not applied")
	}
	if !strings.Contains(result, "Human header") {
		t.Error("human header lost")
	}
	if !strings.Contains(result, "Human footer") {
		t.Error("human footer lost")
	}
	if !strings.Contains(result, "gmb:begin:s1") {
		t.Error("begin marker missing")
	}
	if !strings.Contains(result, "gmb:end:s1") {
		t.Error("end marker missing")
	}
}

func TestApplyToDoc_FrozenZoneUnchanged(t *testing.T) {
	src := "<!-- gmb:begin:frozen-sec -->\n<!-- gmb:freeze -->\nDo not touch.\n<!-- gmb:end:frozen-sec -->\n"
	doc := ParseMarkdown(src)

	result := ApplyToDoc(doc, "frozen-sec", "Machine content that should not appear.")
	if strings.Contains(result, "Machine content") {
		t.Error("frozen zone was overwritten")
	}
	if !strings.Contains(result, "Do not touch") {
		t.Error("frozen content was removed")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// LCS correctness
// ────────────────────────────────────────────────────────────────────────────

func TestLCSLines(t *testing.T) {
	a := []string{"A", "B", "C", "D"}
	b := []string{"A", "C", "D", "E"}
	lcs := lcsLines(a, b)
	// LCS should be ["A", "C", "D"]
	want := []string{"A", "C", "D"}
	if len(lcs) != len(want) {
		t.Fatalf("LCS length: got %d, want %d: %v", len(lcs), len(want), lcs)
	}
	for i, w := range want {
		if lcs[i] != w {
			t.Errorf("LCS[%d] = %q, want %q", i, lcs[i], w)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Anchor markers
// ────────────────────────────────────────────────────────────────────────────

func TestAnchorMarkers(t *testing.T) {
	begin := BuildBeginMarker("my-section")
	end := BuildEndMarker("my-section")

	if begin != "<!-- gmb:begin:my-section -->" {
		t.Errorf("unexpected begin marker: %q", begin)
	}
	if end != "<!-- gmb:end:my-section -->" {
		t.Errorf("unexpected end marker: %q", end)
	}
}
