// Package patcher — merger.go
// Implements Stage 8 3-way merge: BASE (last generated) + OURS (human edits) + THEIRS (new machine content).
//
// Merge contract (P4 Human Sovereignty):
//   - If BASE == OURS  → no human edits → use THEIRS directly.
//   - If BASE != OURS  → human edited the managed zone → line-level 3-way merge.
//     - On conflict: preserve OURS, append THEIRS as a "Doc Update Note" block, warn to stderr.
//
// Plan A2 delegation: MergeSection first tries `git merge-file` (battle-tested
// xdiff backend, 20 years of adversarial exposure) and falls back to the
// built-in LCS line merge when git is missing or fails. The caller-visible
// behavior contract (fast path, human-wins, note block, Conflicted flag) is
// identical on both paths. All inputs are normalized to LF so CRLF can never
// produce mixed line endings.
package patcher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// MergeResult is the output of the 3-way merge operation.
type MergeResult struct {
	// Content is the merged managed-zone body (excluding anchor comment lines).
	Content string
	// Conflicted is true when human edits conflicted with machine content.
	Conflicted bool
	// ConflictNote is the appended machine block on conflict.
	ConflictNote string
}

// MergeSection performs a 3-way merge for a single managed section.
//
//   base  = the machine-generated content from the last engine run (stored in docs_state.json)
//   ours  = the current content between the anchor markers in the live file
//   theirs = the freshly generated content from the renderer
//
// The return value is the resolved content to write into the managed zone body.
//
// Strategy: normalize to LF, take the BASE==OURS fast path, then try
// `git merge-file`, then fall back to the built-in LCS merge.
func MergeSection(base, ours, theirs string) MergeResult {
	// CRLF guard: normalize everything to LF up front so no path below can
	// emit mixed endings.
	baseN := normalizeLF(base)
	oursN := normalizeLF(ours)
	theirsN := normalizeLF(theirs)

	// Fast path: no human edits since last machine run.
	// Compare with trailing newlines normalized to avoid false conflicts
	// from editor-added trailing newlines.
	if strings.TrimRight(baseN, "\n") == strings.TrimRight(oursN, "\n") {
		return MergeResult{Content: theirsN}
	}

	// Preferred path: delegate to git merge-file.
	if merged, conflicted, err := mergeViaGitMergeFile(baseN, oursN, theirsN); err == nil {
		if !conflicted {
			return MergeResult{Content: merged}
		}
		// Conflict: preserve OURS, append THEIRS as informational block.
		note := buildConflictNote(theirsN)
		return MergeResult{
			Content:      oursN + note,
			Conflicted:   true,
			ConflictNote: note,
		}
	}

	// Fallback path: built-in LCS line merge (offline, no external deps).
	merged, conflicted := mergeLCS(
		strings.Split(baseN, "\n"),
		strings.Split(oursN, "\n"),
		strings.Split(theirsN, "\n"),
	)
	if !conflicted {
		return MergeResult{Content: strings.Join(merged, "\n")}
	}

	// Conflict: preserve OURS, append THEIRS as informational block.
	note := buildConflictNote(theirsN)

	return MergeResult{
		Content:      oursN + note,
		Conflicted:   true,
		ConflictNote: note,
	}
}

// normalizeLF converts CRLF and lone CR to LF so merging never produces
// mixed line endings.
func normalizeLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

// buildConflictNote renders THEIRS as an appended machine-output block.
func buildConflictNote(theirs string) string {
	note := "\n\n> **Doc Update Note** (gmb detected a merge conflict in this section):\n>\n"
	for _, line := range strings.Split(theirs, "\n") {
		note += "> " + line + "\n"
	}
	return note
}

// ────────────────────────────────────────────────────────────────────────────
// git merge-file delegation
// ────────────────────────────────────────────────────────────────────────────

// gitMergeTimeout bounds the external git call so a hung git can never hang
// the engine; on timeout the caller falls back to mergeLCS.
const gitMergeTimeout = 10 * time.Second

// mergeViaGitMergeFile delegates the 3-way merge to
// `git merge-file -p -L ours -L base -L theirs <ours> <base> <theirs>`.
//
// Inputs must already be LF-normalized. Returns:
//
//   - (merged, false, nil) on a clean merge (git exit 0): merged is stdout.
//   - ("", true, nil) on conflict (git exit 1 with conflict markers).
//   - ("", false, err) when git is missing, times out, or exits >1:
//     the caller must fall back to mergeLCS.
func mergeViaGitMergeFile(base, ours, theirs string) (string, bool, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", false, fmt.Errorf("git not found: %w", err)
	}

	writeTemp := func(prefix, content string) (string, error) {
		f, err := os.CreateTemp("", prefix)
		if err != nil {
			return "", err
		}
		name := f.Name()
		if _, err := f.WriteString(content); err != nil {
			_ = f.Close()
			_ = os.Remove(name)
			return "", err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(name)
			return "", err
		}
		return name, nil
	}

	baseFile, err := writeTemp("gmb-merge-base-*.txt", base)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = os.Remove(baseFile) }()
	oursFile, err := writeTemp("gmb-merge-ours-*.txt", ours)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = os.Remove(oursFile) }()
	theirsFile, err := writeTemp("gmb-merge-theirs-*.txt", theirs)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = os.Remove(theirsFile) }()

	ctx, cancel := context.WithTimeout(context.Background(), gitMergeTimeout)
	defer cancel()

	// Positional contract: file1=ours (current), orig-file=base (ancestor),
	// file2=theirs (other). Labels are positional: -L ours -L base -L theirs.
	cmd := exec.CommandContext(ctx, "git", "merge-file", "-p",
		"-L", "ours", "-L", "base", "-L", "theirs",
		oursFile, baseFile, theirsFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err == nil {
		// Exit 0: clean merge, stdout is the merged content.
		return normalizeLF(stdout.String()), false, nil
	} else {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			switch exitErr.ExitCode() {
			case 1:
				// Exit 1: conflicts. Confirm the expected marker
				// convention, then report conflict so the caller
				// converts to ours-wins + note block.
				out := normalizeLF(stdout.String())
				if isGitConflictOutput(out) {
					return "", true, nil
				}
				// Exit 1 without recognizable markers is still a
				// conflict signal — never silently return raw output.
				return "", true, nil
			default:
				return "", false, fmt.Errorf("git merge-file exit %d: %s", exitErr.ExitCode(), strings.TrimSpace(stderr.String()))
			}
		}
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
			return "", false, fmt.Errorf("git merge-file timed out: %w", err)
		}
		return "", false, err
	}
}

// isGitConflictOutput reports whether git's stdout uses the expected
// `<<<<<<< ours ... ======= ... >>>>>>> theirs` conflict-marker convention.
func isGitConflictOutput(out string) bool {
	return strings.Contains(out, "<<<<<<< ours") &&
		strings.Contains(out, "=======") &&
		strings.Contains(out, ">>>>>>> theirs")
}

// ApplyToDoc inserts newBody into the managed zone of doc for sectionID and
// returns the full reassembled document. The anchor comment lines are preserved.
// If the zone is Frozen, the content is returned unchanged.
func ApplyToDoc(doc *ParsedDoc, sectionID, newBody string) string {
	zones := make([]Zone, len(doc.Zones))
	copy(zones, doc.Zones)

	for i, z := range zones {
		if z.SectionID != sectionID {
			continue
		}
		if z.Kind == ZoneFrozen {
			break // frozen: never modify
		}
		// Reassemble: begin-marker + newBody + end-marker
		begin := BuildBeginMarker(sectionID)
		end := BuildEndMarker(sectionID)
		body := newBody
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		zones[i].Content = begin + "\n" + body + end + "\n"
		break
	}

	return Reconstruct(zones)
}

// ExtractBody returns the inner content of a managed zone — the text between
// the begin and end anchor comment lines (exclusive).
func ExtractBody(zone *Zone) string {
	if zone == nil {
		return ""
	}
	lines := splitLines(zone.Content)
	if len(lines) < 2 {
		return ""
	}
	// Skip first (begin) and last (end) anchor lines.
	inner := lines[1:]
	if len(inner) > 0 {
		last := inner[len(inner)-1]
		if strings.HasPrefix(strings.TrimSpace(last), "<!-- gmb:end") {
			inner = inner[:len(inner)-1]
		}
	}
	return strings.Join(inner, "\n")
}

// ────────────────────────────────────────────────────────────────────────────
// Line-level 3-way merge (LCS-based, no external diff library)
// Offline fallback behind `git merge-file`.
// ────────────────────────────────────────────────────────────────────────────

// mergeLCS merges base→ours and base→theirs at line granularity.
// Returns (merged lines, hadConflict).
//
// ponytail: naive O(n²) LCS. Upgrade to Myers diff if sections exceed ~1000 lines.
func mergeLCS(base, ours, theirs []string) ([]string, bool) {
	// Find hunks changed in ours vs base and theirs vs base.
	oursChanges := diffHunks(base, ours)
	theirsChanges := diffHunks(base, theirs)

	// If either side is identical to base, use the other.
	if len(oursChanges) == 0 {
		return theirs, false
	}
	if len(theirsChanges) == 0 {
		return ours, false
	}

	// Both sides changed. Check if the change regions overlap.
	oursRange := hunkRange(oursChanges)
	theirsRange := hunkRange(theirsChanges)

	if !rangesOverlap(oursRange, theirsRange) {
		// Non-overlapping: combine — apply theirs changes to ours baseline.
		result := applyNonOverlapping(base, ours, theirs, oursRange, theirsRange)
		return result, false
	}

	// Overlapping changes → conflict.
	return ours, true
}

type lineRange struct{ start, end int }

func diffHunks(base, modified []string) []lineRange {
	var hunks []lineRange
	lcs := lcsLines(base, modified)
	// Walk both sequences; positions not in LCS are changed.
	bi, mi := 0, 0
	li := 0
	for bi < len(base) && mi < len(modified) {
		if li < len(lcs) && base[bi] == lcs[li] && modified[mi] == lcs[li] {
			bi++
			mi++
			li++
		} else {
			start := bi
			for bi < len(base) && (li >= len(lcs) || base[bi] != lcs[li]) {
				bi++
			}
			hunks = append(hunks, lineRange{start, bi})
			// advance modified past its edits
			for mi < len(modified) && (li >= len(lcs) || modified[mi] != lcs[li]) {
				mi++
			}
		}
	}
	// Trailing additions/deletions
	if bi < len(base) {
		hunks = append(hunks, lineRange{bi, len(base)})
	}
	return hunks
}

func hunkRange(hunks []lineRange) lineRange {
	if len(hunks) == 0 {
		return lineRange{-1, -1}
	}
	r := hunks[0]
	for _, h := range hunks[1:] {
		if h.start < r.start {
			r.start = h.start
		}
		if h.end > r.end {
			r.end = h.end
		}
	}
	return r
}

func rangesOverlap(a, b lineRange) bool {
	return a.start < b.end && b.start < a.end
}

// applyNonOverlapping applies theirs changes to ours, assuming no overlap.
func applyNonOverlapping(base, ours, theirs []string, _, theirsRange lineRange) []string {
	// Insert theirs changes into ours at the corresponding positions.
	// Simple strategy: replace the theirs-changed base lines in ours.
	if theirsRange.start < 0 {
		return ours
	}

	// Locate ours lines that correspond to the base lines in theirs' range
	// by finding the LCS anchors. For non-overlapping case, prepend/append suffices.
	result := make([]string, 0, len(ours)+len(theirs))
	result = append(result, ours...)

	// If theirs added lines at the end, append them.
	if theirsRange.start >= len(base) {
		result = append(result, theirs[theirsRange.start:]...)
	}
	return result
}

// lcsLines returns the Longest Common Subsequence of two string slices.
// ponytail: O(n*m) DP. Fine for typical doc sections (< 200 lines).
func lcsLines(a, b []string) []string {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] > dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	// Backtrack
	result := make([]string, 0, dp[n][m])
	i, j := n, m
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			result = append(result, a[i-1])
			i--
			j--
		} else if dp[i-1][j] > dp[i][j-1] {
			i--
		} else {
			j--
		}
	}
	// Reverse
	for l, r := 0, len(result)-1; l < r; l, r = l+1, r-1 {
		result[l], result[r] = result[r], result[l]
	}
	return result
}

// WarnConflict writes a conflict warning to w (typically os.Stderr).
func WarnConflict(w io.Writer, docPath, sectionID string) {
	fmt.Fprintf(w, "doc_engine: merge conflict in %s section %q — human edits preserved, machine update appended\n",
		docPath, sectionID)
}
