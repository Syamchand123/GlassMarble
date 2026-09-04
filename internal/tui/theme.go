// Package tui is the GlassMarble terminal-UI layer built on the Charm stack
// (BubbleTea, Bubbles, Lip Gloss, Huh, Fang). It provides the shared design
// tokens, layout helpers, reusable components, and per-command programs/views
// used by the Cobra commands in cmd/. Business logic stays in internal/;
// this package only renders display state.
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// === Color Palette (OKLCH-derived AdaptiveColor pairs) ===
// Primary brand: violet (OKLCH chroma balanced).
// Accent: cyan (electric).
// Semantic: success emerald, warning amber, error rose, info blue.
// Neutrals: slate/zinc ramp with >=4.5:1 WCAG AA contrast in both light and dark.
var (
	// Brand
	ColorPrimary = lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#7C3AED"} // violet
	ColorAccent  = lipgloss.AdaptiveColor{Light: "#0891B2", Dark: "#06B6D4"} // cyan
	ColorDim     = lipgloss.AdaptiveColor{Light: "#64748B", Dark: "#9CA3AF"} // slate
	ColorSubtle  = lipgloss.AdaptiveColor{Light: "#CBD5E1", Dark: "#374151"} // border/subtle

	// Semantic
	ColorSuccess   = lipgloss.AdaptiveColor{Light: "#059669", Dark: "#10B981"} // emerald
	ColorWarning   = lipgloss.AdaptiveColor{Light: "#D97706", Dark: "#F59E0B"} // amber
	ColorWarningBg = lipgloss.AdaptiveColor{Light: "#FEF3C7", Dark: "#78350F"} // amber bg
	ColorError     = lipgloss.AdaptiveColor{Light: "#E11D48", Dark: "#F43F5E"} // rose
	ColorInfo      = lipgloss.AdaptiveColor{Light: "#2563EB", Dark: "#3B82F6"} // blue
	ColorDisabled  = lipgloss.AdaptiveColor{Light: "#94A3B8", Dark: "#4B5563"} // disabled text

	// Surface
	ColorSurfaceDark = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#111827"} // base background
	ColorSurfaceCard = lipgloss.AdaptiveColor{Light: "#F8FAFC", Dark: "#1F2937"} // card background
	ColorSurfaceCode = lipgloss.AdaptiveColor{Light: "#F1F5F9", Dark: "#111827"} // code span background
	ColorBorder      = lipgloss.AdaptiveColor{Light: "#CBD5E1", Dark: "#374151"} // default borders
	ColorBorderFocus = ColorPrimary                                              // focused border

	// Text (Checked for >=4.5:1 WCAG AA in both modes)
	ColorTextPrimary   = lipgloss.AdaptiveColor{Light: "#0F172A", Dark: "#F9FAFB"} // primary text
	ColorTextSecondary = lipgloss.AdaptiveColor{Light: "#334155", Dark: "#D1D5DB"} // secondary text
	ColorTextMuted     = lipgloss.AdaptiveColor{Light: "#64748B", Dark: "#9CA3AF"} // muted (AA compliant)
)

// === Typography ===
var (
	StyleH1 = R.NewStyle().
		Bold(true).
		Foreground(ColorTextPrimary).
		MarginBottom(1)

	StyleH2 = R.NewStyle().
		Bold(true).
		Foreground(ColorAccent)

	StyleLabel = R.NewStyle().
			Foreground(ColorTextSecondary)

	StyleMuted = R.NewStyle().
			Foreground(ColorTextMuted)

	StyleCode = R.NewStyle().
			Foreground(ColorAccent).
			Background(ColorSurfaceCode).
			Padding(0, 1)
)

// === Semantic text styles ===
var (
	// StyleOK colors text success-emerald (used for inline ✓ markers).
	StyleOK = R.NewStyle().Foreground(ColorSuccess).Bold(true)

	// StyleError colors text error-rose (used for inline ✗ markers).
	StyleError = R.NewStyle().Foreground(ColorError).Bold(true)

	// StyleWarningText colors text warning-amber.
	StyleWarningText = R.NewStyle().Foreground(ColorWarning).Bold(true)

	// StyleAccent colors text accent-cyan.
	StyleAccent = R.NewStyle().Foreground(ColorAccent)

	// StylePrimaryText colors text primary-violet.
	StylePrimaryText = R.NewStyle().Foreground(ColorPrimary)

	// StyleInfoText colors text info-blue.
	StyleInfoText = R.NewStyle().Foreground(ColorInfo)

	// StyleTextSecondary colors text secondary-slate.
	StyleTextSecondary = R.NewStyle().Foreground(ColorTextSecondary)
)

// === Badges / Status Pills ===
var (
	BadgeOK = R.NewStyle().
		Bold(true).
		Foreground(ColorSurfaceDark).
		Background(ColorSuccess).
		Padding(0, 1)

	BadgeWarn = R.NewStyle().
			Bold(true).
			Foreground(ColorSurfaceDark).
			Background(ColorWarning).
			Padding(0, 1)

	BadgeError = R.NewStyle().
			Bold(true).
			Foreground(ColorSurfaceDark).
			Background(ColorError).
			Padding(0, 1)

	BadgeInfo = R.NewStyle().
			Bold(true).
			Foreground(ColorSurfaceDark).
			Background(ColorInfo).
			Padding(0, 1)
)

// === Borders ===
var (
	StyleCard = R.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(1, 2)

	StyleCardFocused = R.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorBorderFocus).
				Padding(1, 2)

	StyleDivider = R.NewStyle().
			Foreground(ColorBorder)

	StylePanel = R.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(ColorPrimary).
			PaddingLeft(1)
)

// === Spinner frames (Charm-style dots) ===
var SpinnerFrames = []string{"◐", "◓", "◑", "◒"}

// === Logo Banner ===
const LogoBanner = `
  ██████╗ ██╗      █████╗ ███████╗███████╗
 ██╔════╝ ██║     ██╔══██╗██╔════╝██╔════╝
 ██║  ███╗██║     ███████║███████╗███████╗
 ██║   ██║██║     ██╔══██║╚════██║╚════██║
 ╚██████╔╝███████╗██║  ██║███████║███████║
  ╚═════╝ ╚══════╝╚═╝  ╚═╝╚══════╝╚══════╝
     ███╗   ███╗ █████╗ ██████╗ ██████╗██╗     ███████╗
     ████╗ ████║██╔══██╗██╔══██╗██╔══██╗██║     ██╔════╝
     ██╔████╔██║███████║██████╔╝██████╔╝██║     █████╗
     ██║╚██╔╝██║██╔══██║██╔══██╗██╔══██╗██║     ██╔══╝
     ██║ ╚═╝ ██║██║  ██║██║  ██║██████╔╝███████╗███████║
     ╚═╝     ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝ ╚══════╝╚══════╝`

// RenderLogoBanner returns the GlassMarble ASCII art logo banner styled in brand colors.
func RenderLogoBanner() string {
	// The ASCII logo has a fixed width. On a terminal too narrow to hold it
	// the art wraps into noise, so fall back to a single styled line.
	widest := 0
	for _, line := range strings.Split(LogoBanner, "\n") {
		if w := lipgloss.Width(line); w > widest {
			widest = w
		}
	}
	if w, ok := OutputWidth(); ok && w < widest {
		return R.NewStyle().Foreground(ColorPrimary).Bold(true).Render("GlassMarble") + "\n" +
			StyleMuted.Render("AI Architecture Intelligence Platform") + "\n"
	}

	banner := R.NewStyle().Foreground(ColorPrimary).Bold(true).Render(LogoBanner)
	tagline := StyleMuted.Render("       AI Architecture Intelligence Platform")
	return banner + "\n" + tagline + "\n"
}
