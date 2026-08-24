package views

import (
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderDoctor renders the `gmb doctor` health report. The original line
// prefixes (Schema:, Parse-back:, Dangling:, DOCTOR: OK) are preserved
// verbatim so CLI tests keep passing; pass/fail styling is added as a leading
// status pill per check row.
func RenderDoctor(rep *akg.DoctorReport) string {
	rows := []string{
		"  Storage:       " + rep.StorageDir,
		"  State size:    " + formatBytes(rep.StateBytes) + fmt.Sprintf(" (%d bytes)", rep.StateBytes),
		"  State modif.:  " + rep.StateModTime.Format("2006-01-02T15:04:05Z07:00"),
		"  Schema:        v" + itoa(rep.SchemaVersion),
		"  Graph version: " + itoa(int(rep.GraphVersion)),
		"  Commit:        " + shortHash(rep.CommitHash),
		"",
		"  " + tui.Divider("Checks", 56),
	}

	if !rep.LoadOK {
		rows = append(rows, tui.BadgeError.Render(" FAIL ")+"  Parse-back:    FAILED")
		rows = append(rows, "                  "+rep.LoadError)
	} else {
		rows = append(rows, tui.BadgeOK.Render(" PASS ")+fmt.Sprintf("  Parse-back:    ok (%d nodes, %d edges)", rep.NodeCount, rep.EdgeCount))
	}

	if rep.Dangling > 0 {
		rows = append(rows, tui.BadgeError.Render(" FAIL ")+fmt.Sprintf("  Dangling:      %d", rep.Dangling))
	} else {
		rows = append(rows, tui.BadgeOK.Render(" PASS ")+"  Dangling:      0")
	}

	if len(rep.DuplicateIDs) > 0 {
		rows = append(rows, tui.BadgeError.Render(" FAIL ")+fmt.Sprintf("  Duplicate IDs: %d", len(rep.DuplicateIDs)))
		for _, id := range rep.DuplicateIDs {
			rows = append(rows, "                    "+id)
		}
	} else {
		rows = append(rows, tui.BadgeOK.Render(" PASS ")+"  Duplicate IDs: 0")
	}

	rows = append(rows, "")

	failures := 0
	if !rep.LoadOK {
		failures++
	}
	if rep.Dangling > 0 {
		failures++
	}
	if len(rep.DuplicateIDs) > 0 {
		failures++
	}
	if failures == 0 {
		rows = append(rows, tui.BadgeOK.Render("  DOCTOR: OK — all checks passed  "))
	} else {
		rows = append(rows, tui.BadgeError.Render(fmt.Sprintf("  DOCTOR: FAILED — %d issue(s) found  ", failures)))
	}

	return tui.StyleCard.Render("  " + joinLines(rows))
}

// RenderDoctorUninitialized renders the uninitialized-state message.
func RenderDoctorUninitialized(statePath string) string {
	return tui.StyleCard.Render(joinLines([]string{
		"  GlassMarble Doctor: Uninitialized",
		"  No active AKG database found at " + tui.StyleCode.Render(statePath) + ". Run 'gmb analyze' first.",
	}))
}
