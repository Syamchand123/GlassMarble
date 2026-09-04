package tui

import (
	"image/color"

	lipglossv2 "charm.land/lipgloss/v2"
	"github.com/charmbracelet/fang"
	"github.com/charmbracelet/lipgloss"
)

// FangColorScheme maps the GlassMarble palette onto fang's help, error and
// version surfaces.
//
// fang renders `--help`, usage errors and `--version`, and its theme was never
// configured — so the framing around every command used a completely different
// colour scheme from the commands themselves. This makes the shell match the
// app.
//
// fang builds on lipgloss v2 while the rest of this package uses v1, so the
// palette's AdaptiveColor pairs are resolved through the LightDarkFunc fang
// supplies for the detected terminal background.
func FangColorScheme(ld lipglossv2.LightDarkFunc) fang.ColorScheme {
	pick := func(c lipgloss.AdaptiveColor) color.Color {
		return ld(lipglossv2.Color(c.Light), lipglossv2.Color(c.Dark))
	}

	base := pick(ColorTextPrimary)
	muted := pick(ColorTextMuted)
	primary := pick(ColorPrimary)
	accent := pick(ColorAccent)

	return fang.ColorScheme{
		Base:           base,
		Title:          primary,
		Description:    base,
		Codeblock:      pick(ColorSurfaceCode),
		Program:        primary,
		DimmedArgument: muted,
		Comment:        muted,
		Flag:           accent,
		FlagDefault:    muted,
		Command:        primary,
		QuotedString:   pick(ColorSuccess),
		Argument:       base,
		Help:           muted,
		Dash:           muted,
		ErrorHeader:    [2]color.Color{pick(ColorTextPrimary), pick(ColorError)},
		ErrorDetails:   pick(ColorError),
	}
}
