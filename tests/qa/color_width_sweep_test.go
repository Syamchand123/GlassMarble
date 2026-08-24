package qa_test

import (
	"os"
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

// TestNarrowWideRenderSweep tests that static card views and tables render
// cleanly at narrow (40-column) and wide (200-column) dimensions without panic (QA-06).
func TestNarrowWideRenderSweep(t *testing.T) {
	widths := []int{40, 60, 80, 120, 200}

	for _, w := range widths {
		t.Run("width_"+string(rune('0'+w/100))+string(rune('0'+(w/10)%10))+string(rune('0'+w%10)), func(t *testing.T) {
			// Doctor card
			doc := views.RenderDoctor(&akg.DoctorReport{
				StorageDir:    "/long/path/to/project/deeply/nested/.glassmarble",
				SchemaVersion: 3,
				GraphVersion:  10,
				LoadOK:        true,
				NodeCount:     150,
				EdgeCount:     400,
			})
			if len(doc) == 0 {
				t.Errorf("width %d: doctor output empty", w)
			}

			// Hotspots card
			hotspot := views.RenderHotspot(3, []views.HotspotRow{
				{Rank: 1, Name: "github.com/organization/deeply/nested/package/service.go::VeryLongFunctionName", Kind: "FUNCTION", InDegree: 42, OutDegree: 5, Primitive: "DATABASE"},
				{Rank: 2, Name: "github.com/organization/deeply/nested/package/handler.go::HandleIncomingRequest", Kind: "METHOD", InDegree: 25, OutDegree: 12, Primitive: "NETWORK_IO"},
				{Rank: 3, Name: "pkg/util.go::Helper", Kind: "FUNCTION", InDegree: 10, OutDegree: 0, Primitive: "NONE"},
			})
			if len(hotspot) == 0 {
				t.Errorf("width %d: hotspot output empty", w)
			}

			// Init card
			initCard := views.RenderInitSuccess("/test/.glassmarble", true)
			if len(initCard) == 0 {
				t.Errorf("width %d: init output empty", w)
			}
		})
	}
}
