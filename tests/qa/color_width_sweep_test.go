package qa_test

import (
	"fmt"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/charmbracelet/lipgloss"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
)

// TestColorModeNoColor verifies that when NO_COLOR=1 is set, rendered output
// does not emit ANSI color escape sequences (QA-05).
func TestColorModeNoColor(t *testing.T) {
	orig := os.Getenv("NO_COLOR")
	defer os.Setenv("NO_COLOR", orig)

	os.Setenv("NO_COLOR", "1")

	status := views.RenderStatus(views.StatusData{
		Initialized:   true,
		StorageDir:    "/test/.glassmarble",
		SchemaVersion: 3,
		GraphVersion:  1,
		CommitHash:    "1234567890ab",
		NodeCount:     10,
		EdgeCount:     20,
	})

	if strings.Contains(status, "\x1b[38;2;") {
		t.Errorf("NO_COLOR=1 produced 24-bit truecolor ANSI sequences in status view:\n%s", status)
	}
}

// TestNarrowWideRenderSweep renders the static card views at several terminal
// widths and asserts every emitted line actually fits.
//
// The previous version looped over widths but never applied them: it called the
// same zero-argument renderers five times and only checked the output was
// non-empty, so it could not detect overflow at any width. Widths are now
// applied through COLUMNS, which tui.OutputWidth honours.
func TestNarrowWideRenderSweep(t *testing.T) {
	widths := []int{40, 60, 80, 120, 200}

	longRows := []views.HotspotRow{
		{Rank: 1, Name: "github.com/organization/deeply/nested/package/service.go::VeryLongFunctionName", Kind: "FUNCTION", InDegree: 42, OutDegree: 5, Primitive: "DATABASE"},
		{Rank: 2, Name: "github.com/organization/deeply/nested/package/handler.go::HandleIncomingRequest", Kind: "METHOD", InDegree: 25, OutDegree: 12, Primitive: "NETWORK_IO"},
		{Rank: 3, Name: "pkg/util.go::Helper", Kind: "FUNCTION", InDegree: 10, OutDegree: 0, Primitive: "NONE"},
	}

	for _, w := range widths {
		w := w
		t.Run(fmt.Sprintf("width_%d", w), func(t *testing.T) {
			t.Setenv("COLUMNS", strconv.Itoa(w))
			tui.ResetOutputWidthForTest()
			defer tui.ResetOutputWidthForTest()

			got, ok := tui.OutputWidth()
			if !ok || got != w {
				t.Fatalf("width not applied: OutputWidth()=%d,%v want %d", got, ok, w)
			}

			// A card may not exceed the terminal, and must not collapse to
			// nothing on a narrow one.
			checkFits := func(label, out string) {
				t.Helper()
				if strings.TrimSpace(out) == "" {
					t.Errorf("width %d: %s output empty", w, label)
					return
				}
				for i, line := range strings.Split(out, "\n") {
					if lw := lipgloss.Width(line); lw > w {
						t.Errorf("width %d: %s line %d overflows by %d cells:\n%q", w, label, i, lw-w, line)
						return
					}
				}
			}

			checkFits("doctor", views.RenderDoctor(&akg.DoctorReport{
				StorageDir:    "/long/path/to/project/deeply/nested/.glassmarble",
				SchemaVersion: 3,
				GraphVersion:  10,
				LoadOK:        true,
				NodeCount:     150,
				EdgeCount:     400,
			}))
			checkFits("hotspot", views.RenderHotspot(3, longRows))
			checkFits("init", views.RenderInitSuccess("/test/.glassmarble", true))
		})
	}
}
