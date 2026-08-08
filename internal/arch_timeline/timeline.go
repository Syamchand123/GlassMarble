package arch_timeline

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// RenderTimeline generates a human-readable timeline view.
func RenderTimeline(entries []archmodel.TimelineEntry) string {
	var builder strings.Builder

	for _, entry := range entries {
		dateStr := entry.Timestamp.Format("Jan 2006")
		
		desc := entry.Title
		if entry.Description != "" {
			desc += " - " + entry.Description
		}

		builder.WriteString(fmt.Sprintf("%s  %s\n", dateStr, desc))
		
		if entry.Intent != "" {
			builder.WriteString(fmt.Sprintf("            Reason: %s\n", entry.Intent))
		}
	}

	return builder.String()
}
