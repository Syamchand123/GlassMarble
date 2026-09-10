// Package tui provides the Bubble Tea views for the Documentation
// Intelligence Engine (master plan §14: internal/doc_engine/tui).
//
// The canonical implementations live under internal/tui/programs/doc_view
// (shared Charm infrastructure). This package re-exports them so the
// doc_engine tree matches the plan's package architecture exactly.
package tui

import (
	"github.com/Syamchand123/GlassMarble/internal/tui/programs/doc_view"
)

// Config carries parameters into the doc viewer.
type Config = doc_view.Config

// Run launches the interactive terminal reader for a managed document.
var Run = doc_view.Run
