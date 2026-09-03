package views

import (
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// HousekeepingArea is one .glassmarble working-set area in the size report.
type HousekeepingArea struct {
	Name  string
	Bytes int64
	Files int
}

// RenderHousekeepingReport renders the `gmb housekeeping` size report as a
// styled card. Area names are preserved so the report stays self-describing.
func RenderHousekeepingReport(areas []HousekeepingArea, totalBytes int64, totalFiles int) string {
	rows := []string{
		tui.StyleH2.Render("  .glassmarble Working Set"),
		"",
		"  " + tui.StyleLabel.Render(pad("Area", 32)+pad("Size", 10)+"Files"),
		"  " + tui.Divider("", maxCardLine()),
	}
	for _, a := range areas {
		if a.Files == 0 && a.Bytes == 0 {
			rows = append(rows, fmt.Sprintf("  %s %s %s",
				tui.StyleMuted.Render(pad(a.Name, 32)), tui.StyleMuted.Render(pad(humanBytes(a.Bytes), 10)), tui.StyleMuted.Render("0")))
			continue
		}
		rows = append(rows, fmt.Sprintf("  %s %s %d",
			tui.StyleTextSecondary.Render(pad(a.Name, 32)),
			tui.StyleTextSecondary.Render(pad(humanBytes(a.Bytes), 10)),
			a.Files))
	}
	rows = append(rows, "  "+tui.Divider("", maxCardLine()))
	rows = append(rows, fmt.Sprintf("  %s %s %d",
		tui.StyleH2.Render(pad("Total", 32)), tui.StyleH2.Render(pad(humanBytes(totalBytes), 10)), totalFiles))
	return tui.StyleCard.Render("  " + joinLines(rows))
}
