package views

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// UpdateData carries the fields for rendering update result cards.
type UpdateData struct {
	CurrentVersion string
	LatestVersion  string
	BinaryPath     string
	OS             string
	Arch           string
	ReleaseURL     string
	ReleaseNotes   string
	AlreadyLatest  bool
	CheckOnly      bool
}

// RenderUpdateAlreadyLatest renders the card when the binary is already up to date.
func RenderUpdateAlreadyLatest(version, binaryPath string) string {
	badge := tui.BadgeOK.Render("  LATEST  ")
	rows := []string{
		tui.StyleH2.Render("  GlassMarble is Up to Date"),
		"",
		"  Current Version: " + tui.StyleCode.Render(version) + "  " + badge,
		"  Executable:      " + binaryPath,
		"",
		"  " + tui.StyleMuted.Render("You are already running the latest release of GlassMarble."),
		"  " + tui.StyleMuted.Render("To force reinstall, run: ") + tui.StyleCode.Render("gmb update --force"),
	}
	return tui.StyleCard.Render("  " + joinLines(rows))
}

// RenderUpdateCheckAvailable renders the card when a new version is available during check.
func RenderUpdateCheckAvailable(current, latest, releaseURL string) string {
	badge := tui.BadgeWarn.Render("  UPDATE AVAILABLE  ")
	rows := []string{
		tui.StyleH2.Render("  New GlassMarble Release Available"),
		"",
		"  Current Version: " + current,
		"  Latest Version:  " + tui.StyleOK.Render(latest) + "  " + badge,
		"  Release Page:    " + releaseURL,
		"",
		"  " + tui.StyleAccent.Render("To install this update, run:"),
		"  " + tui.StyleCode.Render("gmb update"),
	}
	return tui.StyleCard.Render("  " + joinLines(rows))
}

// RenderUpdateSuccess renders the success card after a new release is installed.
func RenderUpdateSuccess(d UpdateData) string {
	badge := tui.BadgeOK.Render("  SUCCESS  ")
	rows := []string{
		tui.StyleH2.Render("  GlassMarble Successfully Updated"),
		"",
		"  Status:          " + badge,
		"  Upgraded:        " + d.CurrentVersion + " → " + tui.StyleOK.Render(d.LatestVersion),
		"  Platform:        " + fmt.Sprintf("%s / %s", d.OS, d.Arch),
		"  Binary Path:     " + d.BinaryPath,
		"  Release Notes:   " + d.ReleaseURL,
	}

	if strings.TrimSpace(d.ReleaseNotes) != "" {
		rows = append(rows, "", "  "+tui.Divider("Release Highlights", 56))
		for _, line := range wrapText(d.ReleaseNotes, 70) {
			rows = append(rows, "  "+line)
		}
	}

	return tui.StyleCard.Render("  " + joinLines(rows))
}
