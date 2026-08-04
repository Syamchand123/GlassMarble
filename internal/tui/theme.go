// Package tui is the GlassMarble terminal-UI layer built on the Charm stack
// (BubbleTea, Bubbles, Lip Gloss, Huh, Fang). It provides the shared design
// tokens, layout helpers, reusable components, and per-command programs/views
// used by the Cobra commands in cmd/. Business logic stays in internal/;
// this package only renders display state.
package tui

import "github.com/charmbracelet/lipgloss"

// === Color Palette ===
// Primary brand color: deep violet-indigo.
// Accent: electric cyan.
// Semantic: success green, warning amber, error rose.
var (
	// Brand
	ColorPrimary = lipgloss.Color("#7C3AED") // violet-600
	ColorAccent  = lipgloss.Color("#06B6D4") // cyan-500
	ColorDim     = lipgloss.Color("#6B7280") // gray-500
	ColorSubtle  = lipgloss.Color("#374151") // gray-700

	// Semantic
	ColorSuccess = lipgloss.Color("#10B981") // emerald-500
	ColorWarning = lipgloss.Color("#F59E0B") // amber-500
	ColorError   = lipgloss.Color("#F43F5E") // rose-500
	ColorInfo    = lipgloss.Color("#3B82F6") // blue-500

	// Surface
	ColorSurfaceDark = lipgloss.Color("#111827") // gray-900
	ColorSurfaceCard = lipgloss.Color("#1F2937") // gray-800
	ColorBorder      = lipgloss.Color("#374151") // gray-700
	ColorBorderFocus = ColorPrimary

	// Text
	ColorTextPrimary   = lipgloss.Color("#F9FAFB") // gray-50
	ColorTextSecondary = lipgloss.Color("#D1D5DB") // gray-300
	ColorTextMuted     = lipgloss.Color("#6B7280") // gray-500
)

// === Typography ===
var (
	StyleH1 = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorTextPrimary).
		MarginBottom(1)

	StyleH2 = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorAccent)

	StyleLabel = lipgloss.NewStyle().
			Foreground(ColorTextSecondary)

	StyleMuted = lipgloss.NewStyle().
			Foreground(ColorTextMuted)

	StyleCode = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Background(ColorSurfaceCard).
			Padding(0, 1)
)

// === Semantic text styles ===
var (
	// StyleOK colors text success-green (used for inline ✓ markers).
	StyleOK = lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true)

	// StyleError colors text error-rose (used for inline ✗ markers).
	StyleError = lipgloss.NewStyle().Foreground(ColorError).Bold(true)

	// StyleWarningText colors text warning-amber.
	StyleWarningText = lipgloss.NewStyle().Foreground(ColorWarning).Bold(true)

	// StyleAccent colors text accent-cyan.
	StyleAccent = lipgloss.NewStyle().Foreground(ColorAccent)

	// StylePrimaryText colors text primary-violet.
	StylePrimaryText = lipgloss.NewStyle().Foreground(ColorPrimary)

	// StyleInfoText colors text info-blue.
	StyleInfoText = lipgloss.NewStyle().Foreground(ColorInfo)

	// StyleTextSecondary colors text secondary-gray.
	StyleTextSecondary = lipgloss.NewStyle().Foreground(ColorTextSecondary)
)

// === Badges / Status Pills ===
var (
	BadgeOK = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorSurfaceDark).
		Background(ColorSuccess).
		Padding(0, 1)

	BadgeWarn = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorSurfaceDark).
			Background(ColorWarning).
			Padding(0, 1)

	BadgeError = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorSurfaceDark).
			Background(ColorError).
			Padding(0, 1)

	BadgeInfo = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorSurfaceDark).
			Background(ColorInfo).
			Padding(0, 1)
)

// === Borders ===
var (
	StyleCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(1, 2)

	StyleCardFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorBorderFocus).
				Padding(1, 2)

	StyleDivider = lipgloss.NewStyle().
			Foreground(ColorBorder)

	StylePanel = lipgloss.NewStyle().
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
     ██║ ╚═╝ ██║██║  ██║██║  ██║██████╔╝███████╗███████╗
     ╚═╝     ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝ ╚══════╝╚══════╝`
