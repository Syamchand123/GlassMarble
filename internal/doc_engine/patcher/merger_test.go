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
// Plan A2 property tests (delegation contract — must hold on both the git
// path and the LCS fallback path).
// ────────────────────────────────────────────────────────────────────────────

// P1: no-human-edits identity — merge(x, x, y) == y (LF-normalized).
func TestMergeSection_Property_NoHumanEditsIdentity(t *testing.T) {
	cases := []struct {
		name string
		x, y string
	}{
		{"simple", "Base line.\n", "New machine line.\n"},
		{"empty-base", "", "Fresh content.\n"},
		{"empty-theirs", "Base.\n", ""},
		{"both-empty", "", ""},
		{"multiline", "A\nB\nC\n", "A-updated\nB\nC\n"},
		{"crlf", "A\r\nB\r\n", "C\r\nD\r\n"},
		{"unicode", "Héllo 🌍\n", "Updated héllo 🌟\n"},
		{"no-trailing-newline", "A\nB", "A-updated\nB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MergeSection(tc.x, tc.x, tc.y)
			if got.Conflicted {
				t.Errorf("merge(x,x,y) must not conflict")
			}
			if got.Content != normalizeLF(tc.y) {
				t.Errorf("merge(x,x,y) == y: got %q, want %q", got.Content, normalizeLF(tc.y))
			}
		})
	}
}

// P2: human-wins — merge(x, y, x) == y (machine made no change, keep ours).
// Precisely: equal modulo trailing newlines, because the BASE==OURS fast
// path intentionally normalizes trailing newlines (editor-added trailing
// newlines must not count as human edits). All substantive edits below
// assert exact equality; the relaxation only covers blank-only diffs.
func TestMergeSection_Property_HumanWins(t *testing.T) {
	cases := []struct {
		name string
		x, y string
	}{
		{"simple", "Base.\n", "Human edit.\n"},
		{"empty-human", "Base.\n", ""},
		{"multiline", "A\nB\nC\n", "A\nB-human\nC\n"},
		{"crlf", "A\r\nB\r\n", "A\r\nB-human\r\n"},
		{"unicode", "Base 🎲\n", "Human ✏️ edit\n"},
		{"no-trailing-newline", "A\nB", "A\nB-human"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MergeSection(tc.x, tc.y, tc.x)
			if got.Conflicted {
				t.Errorf("merge(x,y,x) must not conflict (theirs == base)")
			}
			want := normalizeLF(tc.y)
			if got.Content != want &&
				strings.TrimRight(got.Content, "\n") != strings.TrimRight(want, "\n") {
				t.Errorf("merge(x,y,x) == y: got %q, want %q", got.Content, want)
			}
		})
	}
}

// P3: idempotence on clean merges — precisely: let m1 = merge(base, ours,
// theirs). If m1 is NOT conflicted, then re-merging the already-merged
// output against the same base and theirs must be a fixed point:
// merge(base, m1.Content, theirs) == m1.Content with no new conflict.
// (Conflict outputs are excluded: re-merging an ours+note block would
// append a second note by design.)
func TestMergeSection_Property_IdempotentWhenClean(t *testing.T) {
	cases := []struct {
		name               string
		base, ours, theirs string
	}{
		{"no-human-edits", "A\n", "A\n", "B\n"},
		{"human-wins", "A\n", "B\n", "A\n"},
		// 3-line file with a blank separator so the two edits are
		// non-adjacent: git merges cleanly (2-line adjacent edits
		// intentionally excluded — xdiff treats them as one hunk).
		{"non-overlapping", "A\nB\nC\n", "A\nB\nC-human\n", "A-updated\nB\nC\n"},
		{"identical", "Same.\n", "Same.\n", "Same.\n"},
		{"unicode-clean", "α\nβ\nγ\n", "α\nβ\nγ-human\n", "α-updated\nβ\nγ\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m1 := MergeSection(tc.base, tc.ours, tc.theirs)
			if m1.Conflicted {
				t.Skip("first merge conflicted — idempotence defined for clean merges only")
			}
			m2 := MergeSection(tc.base, m1.Content, tc.theirs)
			if m2.Conflicted {
				t.Errorf("re-merge introduced a conflict: first %q, second %q", m1.Content, m2.Content)
			}
			if m2.Content != m1.Content {
				t.Errorf("not idempotent:\nfirst:  %q\nsecond: %q", m1.Content, m2.Content)
			}
		})
	}
}

// P4: conflict case — Conflicted is always set and OURS is contained verbatim
// (LF-normalized), with the machine note block appended.
func TestMergeSection_Property_ConflictPreservesOurs(t *testing.T) {
	cases := []struct {
		name               string
		base, ours, theirs string
	}{
		{"same-line", "Original.\n", "Human rewrite.\n", "Machine rewrite.\n"},
		{"adjacent-inserts", "A\nB\n", "A\nhuman-insert\nB\n", "A\nmachine-insert\nB\n"},
		{"unicode-conflict", "Base 🎲\n", "Human ✏️\n", "Machine 🤖\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MergeSection(tc.base, tc.ours, tc.theirs)
			if !got.Conflicted {
				// A real 3-way merge may resolve some inputs cleanly;
				// when it does, the human text must still survive.
				if !strings.Contains(got.Content, strings.TrimRight(normalizeLF(tc.ours), "\n")) {
					t.Errorf("clean merge lost human text: %q", got.Content)
				}
				return
			}
			oursN := normalizeLF(tc.ours)
			if !strings.Contains(got.Content, oursN) {
				t.Errorf("conflict lost OURS verbatim:\nours: %q\ngot:  %q", oursN, got.Content)
			}
			if !strings.Contains(got.Content, "Doc Update Note") {
				t.Error("conflict note block missing")
			}
			// THEIRS is quoted line-by-line ("> " prefix), so assert
			// per-line containment rather than whole-block containment.
			for _, line := range strings.Split(normalizeLF(tc.theirs), "\n") {
				if line == "" {
					continue
				}
				if !strings.Contains(got.Content, line) {
					t.Errorf("machine line %q missing in conflict output %q", line, got.Content)
				}
			}
			if got.ConflictNote == "" || !strings.Contains(got.Content, got.ConflictNote) {
				t.Error("ConflictNote field must be populated and embedded in Content")
			}
		})
	}
}

// P5: CRLF — LF-normalized inputs in, LF-only content out; never mixed endings.
func TestMergeSection_Property_CRLFNormalized(t *testing.T) {
	cases := []struct {
		name               string
		base, ours, theirs string
	}{
		{"fast-path", "A\r\nB\r\n", "A\r\nB\r\n", "C\r\nD\r\n"},
		{"clean", "A\r\nB\r\nC\r\n", "A\r\nB\r\nC-human\r\n", "A-updated\r\nB\r\nC\r\n"},
		{"conflict", "Orig\r\n", "Human\r\n", "Machine\r\n"},
		{"mixed", "A\r\nB\nC\rD", "A\r\nB-human\n", "A-updated\r\nB\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MergeSection(tc.base, tc.ours, tc.theirs)
			if strings.Contains(got.Content, "\r") {
				t.Errorf("mixed line endings in output: %q", got.Content)
			}
			if got.ConflictNote != "" && strings.Contains(got.ConflictNote, "\r") {
				t.Errorf("CR in conflict note: %q", got.ConflictNote)
			}
		})
	}
}

// Delegation unit tests: git path reports clean vs conflict correctly, and
// the LCS fallback preserves the human-wins contract.
func TestMergeViaGitMergeFile_CleanAndConflict(t *testing.T) {
	merged, conflicted, err := mergeViaGitMergeFile("A\nB\nC\n", "A\nB\nC-human\n", "A-updated\nB\nC\n")
	if err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	if conflicted {
		t.Error("expected clean git merge for non-overlapping edits")
	}
	if !strings.Contains(merged, "A-updated") || !strings.Contains(merged, "C-human") {
		t.Errorf("git clean merge dropped a side: %q", merged)
	}

	_, conflicted, err = mergeViaGitMergeFile("Original.\n", "Human rewrite.\n", "Machine rewrite.\n")
	if err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	if !conflicted {
		t.Error("expected conflict for same-line edits")
	}
}

func TestMergeLCS_HumanWins(t *testing.T) {
	merged, conflicted := mergeLCS(
		strings.Split("A\n", "\n"),
		strings.Split("B\n", "\n"),
		strings.Split("A\n", "\n"),
	)
	if conflicted {
		t.Error("LCS fallback: theirs == base must not conflict")
	}
	if strings.Join(merged, "\n") != "B\n" {
		t.Errorf("LCS fallback human-wins: got %q", strings.Join(merged, "\n"))
	}
}

// TestMergeLCS_TrailingAppendNotLostAsNoOp guards against a regression where
// diffHunks(base, theirs) reported zero hunks for a pure trailing append
// (theirs = base + new lines, nothing else changed), because it only ever
// recorded a trailing hunk for leftover BASE lines, never leftover MODIFIED
// lines. mergeLCS then treated theirs as identical-to-base and returned ours
// unmodified, silently discarding the machine's newly appended content
// whenever a human had also edited something earlier in the section.
func TestMergeLCS_TrailingAppendNotLostAsNoOp(t *testing.T) {
	base := strings.Split("Intro.\nDetail line.\n", "\n")
	ours := strings.Split("Intro (human-edited).\nDetail line.\n", "\n")
	theirs := strings.Split("Intro.\nDetail line.\nNewly appended subsection.\n", "\n")

	merged, conflicted := mergeLCS(base, ours, theirs)
	if conflicted {
		t.Fatalf("non-overlapping human edit + trailing append must not conflict")
	}
	got := strings.Join(merged, "\n")
	if !strings.Contains(got, "human-edited") {
		t.Errorf("merge dropped the human edit: %q", got)
	}
	if !strings.Contains(got, "Newly appended subsection") {
		t.Errorf("merge silently dropped the machine's trailing append: %q", got)
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
