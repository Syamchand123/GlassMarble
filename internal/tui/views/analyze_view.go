package views

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// PatternSummary holds detected architecture pattern info for the summary view.
type PatternSummary struct {
	Name       string
	Kind       string
	Confidence float64
}

// SmellSummary holds detected architectural smell info for the summary view.
type SmellSummary struct {
	Title    string
	Severity string
}

// AnalysisCardData contains the metrics and intelligence to render the styled
// analysis result card.
type AnalysisCardData struct {
	TargetDir          string
	IsIncremental      bool
	FilesAnalyzed      int
	Nodes              int
	NodesDelta         int
	Edges              int
	EdgesDelta         int
	VirtualNodes       int
	VirtualDelta       int
	DanglingEdges      int
	StateBytes         int64
	DurationSec        float64
	ComponentsCount    int
	Patterns           []PatternSummary
	Smells             []SmellSummary
	CyclesCount        int
	LayerViolations    int
	ReasonedChanges    int
	MemoryEventsCount  int
	SkippedFiles       []string
	IngestionWarnings  []string
}

// RenderAnalysisStart renders the branded starting banner.
func RenderAnalysisStart(targetDir string, isIncremental bool) string {
	modeBadge := tui.BadgeInfo.Render("  ⚡ INCREMENTAL  ")
	if !isIncremental {
		modeBadge = tui.BadgeWarn.Render("  🔄 FULL RESCAN  ")
	}
	title := tui.StyleAccent.Render("GlassMarble Architecture Analysis")
	target := tui.StyleMuted.Render(targetDir)
	return fmt.Sprintf("  %s %s\n  %s\n", title, modeBadge, target)
}

// RenderAnalysisSummary renders the comprehensive Lip Gloss analysis card.
func RenderAnalysisSummary(d AnalysisCardData) string {
	health := tui.BadgeOK.Render("  ● HEALTHY  ")
	if d.DanglingEdges > 0 {
		health = tui.BadgeWarn.Render(fmt.Sprintf("  ● %d DANGLING  ", d.DanglingEdges))
	}

	modeStr := "Incremental (git delta)"
	if !d.IsIncremental {
		modeStr = "Full rescan"
	}

	// Exact summary string preserved for test assertion compatibility
	exactSummaryLine := fmt.Sprintf("Analyzed %d files | %d nodes (+%d) | %d edges (+%d) | %d virtual (+%d) | %d dangling | state=%s | %.1fs",
		d.FilesAnalyzed, d.Nodes, d.NodesDelta,
		d.Edges, d.EdgesDelta,
		d.VirtualNodes, d.VirtualDelta,
		d.DanglingEdges, humanBytes(d.StateBytes), d.DurationSec)

	rows := []string{
		tui.StyleH2.Render("  GlassMarble Architecture Analysis Complete"),
		"",
		"  Target:        " + d.TargetDir + "   " + health,
		"  Mode:          " + modeStr,
		"  State Size:    " + humanBytes(d.StateBytes),
		"  Duration:      " + fmt.Sprintf("%.1fs", d.DurationSec),
		"",
		"  " + tui.Divider("Graph Metrics", maxCardLine()),
		"  " + exactSummaryLine,
		"  Files Analyzed: " + itoa(d.FilesAnalyzed),
		"  Total Nodes:    " + itoa(d.Nodes) + fmt.Sprintf(" (+%d)", d.NodesDelta),
		"  Total Edges:    " + itoa(d.Edges) + fmt.Sprintf(" (+%d)", d.EdgesDelta),
		"  Virtual Nodes:  " + itoa(d.VirtualNodes) + fmt.Sprintf(" (+%d)", d.VirtualDelta),
	}

	// Architectural Intelligence section
	if d.ComponentsCount > 0 || len(d.Patterns) > 0 || len(d.Smells) > 0 || d.CyclesCount > 0 || d.LayerViolations > 0 {
		rows = append(rows, "", "  "+tui.Divider("Architectural Intelligence", maxCardLine()))
		intelHeader := fmt.Sprintf("Intelligence: %d components | %d patterns | %d smells | %d cycles | %d layer violations",
			d.ComponentsCount, len(d.Patterns), len(d.Smells), d.CyclesCount, d.LayerViolations)
		rows = append(rows, "  "+intelHeader)

		for _, p := range d.Patterns {
			confBadge := tui.BadgeInfo.Render(fmt.Sprintf(" %.0f%% ", p.Confidence*100))
			rows = append(rows, fmt.Sprintf("  pattern: %s (confidence %.2f)  %s", p.Name, p.Confidence, confBadge))
		}

		for _, s := range d.Smells {
			badge := tui.BadgeInfo.Render(" LOW ")
			switch strings.ToUpper(s.Severity) {
			case "HIGH", "CRITICAL":
				badge = tui.BadgeError.Render(" " + strings.ToUpper(s.Severity) + " ")
			case "MEDIUM":
				badge = tui.BadgeWarn.Render(" MEDIUM ")
			}
			rows = append(rows, fmt.Sprintf("  smell: [%s] %s  %s", s.Severity, s.Title, badge))
		}

		if d.CyclesCount == 0 && d.LayerViolations == 0 {
			rows = append(rows, "  "+tui.StyleOK.Render("✓ 0 circular dependencies  ✓ 0 layer violations"))
		}
	}

	// Developer Memory & Commit Reasoning
	if d.ReasonedChanges > 0 || d.MemoryEventsCount > 0 {
		rows = append(rows, "", "  "+tui.Divider("Developer Memory & Evolution", maxCardLine()))
		if d.ReasonedChanges > 0 {
			rows = append(rows, fmt.Sprintf("  Commit reasoning: reasoned %d architectural change(s)", d.ReasonedChanges))
		}
		if d.MemoryEventsCount > 0 {
			rows = append(rows, fmt.Sprintf("  Memory: recorded %d architectural event(s) into developer memory", d.MemoryEventsCount))
		}
	}

	return tui.StyleCard.Render("  " + joinLines(rows))
}
