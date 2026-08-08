package arch_timeline

import (
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

func TestRenderTimeline(t *testing.T) {
	entries := []archmodel.TimelineEntry{
		{
			Timestamp:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Title:       "Authentication service introduced",
			Description: "Basic JWT",
			Intent:      "Required for login",
		},
		{
			Timestamp: time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
			Title:     "Redis caching added",
		},
	}

	output := RenderTimeline(entries)
	
	if !strings.Contains(output, "Jan 2025  Authentication service introduced - Basic JWT") {
		t.Errorf("Output missing expected Jan 2025 entry")
	}
	if !strings.Contains(output, "Reason: Required for login") {
		t.Errorf("Output missing intent")
	}
	if !strings.Contains(output, "Mar 2025  Redis caching added") {
		t.Errorf("Output missing Mar 2025 entry")
	}
}
