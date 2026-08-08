package arch_timeline

import (
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

func timelineEntries() []archmodel.TimelineEntry {
	return []archmodel.TimelineEntry{
		{
			Timestamp:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			CommitHash:  "c1",
			Title:       "Authentication service introduced",
			Description: "Basic JWT",
			Intent:      "Required for login",
			EventKind:   archmodel.EventServiceAdded,
			Components:  []string{"auth"},
			Tags:        []string{"security"},
		},
		{
			Timestamp:  time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
			CommitHash: "c2",
			Title:      "Redis caching added",
		},
	}
}

func TestRenderTimeline(t *testing.T) {
	output := RenderTimeline(timelineEntries())

	if !strings.Contains(output, "Jan 2025  Authentication service introduced - Basic JWT") {
		t.Errorf("Output missing expected Jan 2025 entry:\n%s", output)
	}
	if !strings.Contains(output, "Reason: Required for login") {
		t.Errorf("Output missing intent:\n%s", output)
	}
	if !strings.Contains(output, "Mar 2025  Redis caching added") {
		t.Errorf("Output missing Mar 2025 entry:\n%s", output)
	}
}

// TestRenderTimeline_Ordering: entries must be rendered oldest-first even
// when the input is unordered.
func TestRenderTimeline_Ordering(t *testing.T) {
	entries := timelineEntries()
	entries[0], entries[1] = entries[1], entries[0] // now newest first
	out := RenderTimeline(entries)
	if strings.Index(out, "Jan 2025") > strings.Index(out, "Mar 2025") {
		t.Errorf("entries must be ordered oldest-first:\n%s", out)
	}
}

func TestRenderTimelineFull(t *testing.T) {
	out := RenderTimelineFull(timelineEntries())
	for _, want := range []string{
		"2025-01-01 00:00", "Authentication service introduced", "Basic JWT",
		"commit: c1", "kind:   SERVICE_ADDED", "components: auth", "reason: Required for login", "tags:   security",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("full render missing %q:\n%s", want, out)
		}
	}
}

func TestRenderTimelineMermaid(t *testing.T) {
	out := RenderTimelineMermaid(timelineEntries())

	if !strings.HasPrefix(out, "timeline\n") {
		t.Errorf("mermaid output must start with the timeline directive:\n%s", out)
	}
	if !strings.Contains(out, "title Architecture Evolution") {
		t.Errorf("mermaid output missing title:\n%s", out)
	}
	if !strings.Contains(out, "section 2025-01") || !strings.Contains(out, "section 2025-03") {
		t.Errorf("mermaid output missing month sections:\n%s", out)
	}
	if !strings.Contains(out, "Authentication service introduced : Basic JWT") {
		t.Errorf("mermaid item with description missing:\n%s", out)
	}
	if !strings.Contains(out, "Redis caching added") {
		t.Errorf("mermaid item without description missing:\n%s", out)
	}
}

// TestRenderTimelineMermaid_ColonSanitization: colons in titles must not
// break the "text : description" item syntax.
func TestRenderTimelineMermaid_ColonSanitization(t *testing.T) {
	entries := []archmodel.TimelineEntry{{
		Timestamp:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:       "CI: add build step",
		Description: "fix: cache",
	}}
	out := RenderTimelineMermaid(entries)
	if strings.Contains(out, "CI: add build step : fix: cache") {
		t.Errorf("colon must be sanitized in titles/descriptions:\n%s", out)
	}
	if !strings.Contains(out, "CI- add build step : fix- cache") {
		t.Errorf("sanitized text missing:\n%s", out)
	}
}

func TestRenderTimeline_Empty(t *testing.T) {
	if out := RenderTimeline(nil); out != "" {
		t.Errorf("empty timeline must render nothing, got %q", out)
	}
	if out := RenderTimelineFull(nil); out != "" {
		t.Errorf("empty full timeline must render nothing, got %q", out)
	}
	if out := RenderTimelineMermaid(nil); out != "timeline\n    title Architecture Evolution\n" {
		t.Errorf("empty mermaid timeline must render just the header, got %q", out)
	}
}
