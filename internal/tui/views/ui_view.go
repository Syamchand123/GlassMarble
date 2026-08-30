package views

import (
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderUIServerStart renders the branded visualizer server dashboard.
func RenderUIServerStart(host string, port int, nodeCount, edgeCount int) string {
	url := fmt.Sprintf("http://%s:%d", host, port)
	badge := tui.BadgeOK.Render("  RUNNING  ")

	header := tui.StyleH2.Render("  GlassMarble Live Architecture Visualizer")

	rows := []string{
		"  Status:         " + badge,
		"  Server URL:     " + tui.StyleAccent.Render(url),
		fmt.Sprintf("  Loaded Graph:   %d nodes, %d edges", nodeCount, edgeCount),
		"",
		"  " + tui.Divider("Interactive Features", 52),
		"  • " + tui.StyleCode.Render("2D/3D Force Graph") + "  Zoom, pan, and drag code components",
		"  • " + tui.StyleCode.Render("Node Inspection") + "   View callers, dependencies, & source coordinates",
		"  • " + tui.StyleCode.Render("Blast Radius") + "     Simulate refactoring impact in real time",
		"  • " + tui.StyleCode.Render("REST API Active") + "  /api/graph, /api/status, /api/impact, /api/search",
		"",
		"  " + tui.StyleMuted.Render("Press Ctrl+C to gracefully shut down the visualizer server."),
	}

	return tui.StyleCard.Render("  " + header + "\n\n" + joinLines(rows))
}
