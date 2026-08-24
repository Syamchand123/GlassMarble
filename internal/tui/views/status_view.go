package views

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// StatusData carries the values needed to render the `gmb status` dashboard.
type StatusData struct {
	Initialized   bool
	StorageDir    string
	SchemaVersion int
	GraphVersion  uint64
	CommitHash    string
	LastAnalysis  string
	NodeCount     int
	EdgeCount     int
	IndexedFiles  int
	Entrypoints   int
	VirtualCount  int
	VirtualShare  float64
	Dangling      int
	JSONBytes     int64
	Verified      bool
}

// RenderStatusUninitialized renders the `gmb status` dashboard for an
// uninitialized database directory.
func RenderStatusUninitialized(StatePath string) string {
	return tui.StyleCard.Render(joinLines([]string{
		"  GlassMarble Status: Uninitialized",
		"  No active AKG database found at " + tui.StyleCode.Render(StatePath) + ". Run 'gmb analyze' first.",
	}))
}

// RenderStatus renders the `gmb status` dashboard. Line prefixes asserted by
// the CLI tests (Schema Version:, Graph Version:, Entrypoints:, Virtual Nodes:,
// Nodes Count:) are preserved verbatim.
func RenderStatus(s StatusData) string {
	health := tui.BadgeOK.Render("  ● HEALTHY  ")
	verified := tui.BadgeOK.Render("  verified  ")
	if s.Dangling > 0 {
		health = tui.BadgeWarn.Render("  ● WARNING  ")
		verified = tui.BadgeWarn.Render(fmt.Sprintf("  UNVERIFIED — %d dangling  ", s.Dangling))
	}

	rows := []string{
		tui.StyleH2.Render("  GlassMarble AKG Status"),
		"",
		"  Storage Dir:   " + s.StorageDir + "   " + health,
		"  Schema Version: " + itoa(s.SchemaVersion),
		"  Graph Version: " + itoa(int(s.GraphVersion)),
		"  Commit Hash:   " + s.CommitHash,
		"  Last Analysis: " + s.LastAnalysis,
		"",
		"  " + tui.Divider("Graph", 56),
		"  Nodes Count:   " + itoa(s.NodeCount),
		"  Outbound Edges:" + itoa(s.EdgeCount),
		"  Inbound Edges: " + itoa(s.EdgeCount),
		"  Indexed Files: " + itoa(s.IndexedFiles),
		"  Entrypoints:   " + itoa(s.Entrypoints),
		"  Virtual Nodes: " + fmt.Sprintf("%d (%.1f%%)", s.VirtualCount, s.VirtualShare),
		"  Health Errors: " + fmt.Sprintf("%d dangling reference(s)", s.Dangling),
		"",
		"  " + tui.Divider("Storage", 56),
		"  Storage:       State " + humanBytes(s.JSONBytes),
		"  Verification:  " + strings.TrimSpace(verified),
	}
	return tui.StyleCard.Render("  " + joinLines(rows))
}
