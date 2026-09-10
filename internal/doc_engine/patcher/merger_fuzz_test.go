package patcher

import (
	"strings"
	"testing"
)

// FuzzMergeSection fuzzes the 3-way merge over random (base, ours, theirs)
// triplets. Invariants (must hold on both the git path and the LCS fallback):
//
//  1. Never panics; output never contains mixed (CR) line endings.
//  2. When Conflicted, Content contains OURS verbatim (LF-normalized) plus
//     the machine note block.
//  3. Identity P1: merge(x, x, y) == y (LF-normalized), never conflicted.
//  4. Identity P2 (human-wins): merge(x, y, x) == y, never conflicted.
//
// Seed corpus covers: empty strings, CRLF, unicode, no-trailing-newline,
// and conflicting adjacent inserts.
func FuzzMergeSection(f *testing.F) {
	// Seed corpus.
	f.Add("", "", "")
	f.Add("Base.\n", "Base.\n", "Theirs.\n")                          // fast path
	f.Add("Base.\n", "Ours.\n", "Base.\n")                            // human-wins
	f.Add("Original.\n", "Human rewrite.\n", "Machine rewrite.\n")    // clean conflict
	f.Add("A\r\nB\r\n", "A\r\nB\r\n", "C\r\nD\r\n")                   // CRLF fast path
	f.Add("A\r\nB\r\n", "A\r\nB-human\r\n", "A-updated\r\nB\r\n")     // CRLF both-edit
	f.Add("Héllo 🌍\n", "Héllo ✏️\n", "Héllo 🤖\n")                     // unicode conflict
	f.Add("A\nB", "A\nB-human", "A-updated\nB")                       // no trailing newline
	f.Add("A\nB\n", "A\nhuman-insert\nB\n", "A\nmachine-insert\nB\n") // adjacent inserts
	f.Add("A\nB\nC\n", "A\nB\nC-human\n", "A-updated\nB\nC\n")        // non-overlapping
	f.Add("line1\nline2\nline3\n", "", "other\n")                     // empty ours
	f.Add("\n\n\n", "\n", "\n\n")                                     // blank-line soup
	f.Add("a", "b", "c")                                              // single chars, no newline

	f.Fuzz(func(t *testing.T, base, ours, theirs string) {
		// Primary merge must never panic (a panic fails the fuzz run).
		got := MergeSection(base, ours, theirs)

		// Invariant 1: CRLF normalization — no CR may leak into output.
		if strings.Contains(got.Content, "\r") {
			t.Errorf("output contains CR: base=%q ours=%q theirs=%q got=%q",
				base, ours, theirs, got.Content)
		}
		if strings.Contains(got.ConflictNote, "\r") {
			t.Errorf("conflict note contains CR: %q", got.ConflictNote)
		}

		// Invariant 2: conflict ⇒ OURS verbatim + note block.
		if got.Conflicted {
			oursN := normalizeLF(ours)
			if !strings.Contains(got.Content, oursN) {
				t.Errorf("conflict lost OURS verbatim: ours=%q got=%q",
					oursN, got.Content)
			}
			if !strings.Contains(got.Content, "Doc Update Note") {
				t.Errorf("conflict missing note block: got=%q", got.Content)
			}
			if got.ConflictNote == "" || !strings.Contains(got.Content, got.ConflictNote) {
				t.Errorf("ConflictNote must be embedded in Content: got=%q note=%q",
					got.Content, got.ConflictNote)
			}
		}

		// Invariant 3 (P1): merge(x, x, y) == normalizeLF(y), no conflict.
		p1 := MergeSection(base, base, theirs)
		if p1.Conflicted {
			t.Errorf("P1 merge(x,x,y) conflicted: x=%q y=%q", base, theirs)
		}
		if p1.Content != normalizeLF(theirs) {
			t.Errorf("P1 merge(x,x,y)==y failed: x=%q y=%q got=%q",
				base, theirs, p1.Content)
		}

		// Invariant 4 (P2 human-wins): merge(x, y, x) == normalizeLF(y),
		// modulo trailing newlines (the BASE==OURS fast path normalizes
		// trailing newlines so editor-added newlines are not human edits).
		p2 := MergeSection(base, ours, base)
		if p2.Conflicted {
			t.Errorf("P2 merge(x,y,x) conflicted: x=%q y=%q", base, ours)
		}
		wantY := normalizeLF(ours)
		if p2.Content != wantY &&
			strings.TrimRight(p2.Content, "\n") != strings.TrimRight(wantY, "\n") {
			t.Errorf("P2 merge(x,y,x)==y failed: x=%q y=%q got=%q",
				base, ours, p2.Content)
		}
	})
}
