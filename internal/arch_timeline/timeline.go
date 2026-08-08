package arch_timeline

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// sortTimeline orders entries deterministically (oldest first, then commit,
// then title) regardless of the order they arrived in.
func sortTimeline(entries []archmodel.TimelineEntry) []archmodel.TimelineEntry {
	out := append([]archmodel.TimelineEntry(nil), entries...)
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].Timestamp.Before(out[j].Timestamp)
		}
		if out[i].CommitHash != out[j].CommitHash {
			return out[i].CommitHash < out[j].CommitHash
		}
		return out[i].Title < out[j].Title
	})
	return out
}

// RenderTimeline generates a compact human-readable timeline view: one line
// per entry with the month-year, title (and description when present), and
// the reason line when an intent is known.
func RenderTimeline(entries []archmodel.TimelineEntry) string {
	var builder strings.Builder
	for _, entry := range sortTimeline(entries) {
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

// RenderTimelineFull is the verbose variant: it adds the exact timestamp,
// commit hash, event kind, affected components and tags for every entry.
func RenderTimelineFull(entries []archmodel.TimelineEntry) string {
	var builder strings.Builder
	for _, entry := range sortTimeline(entries) {
		builder.WriteString(fmt.Sprintf("[%s] %s\n", entry.Timestamp.Format("2006-01-02 15:04"), entry.Title))
		if entry.Description != "" {
			builder.WriteString(fmt.Sprintf("    %s\n", entry.Description))
		}
		if entry.CommitHash != "" {
			builder.WriteString(fmt.Sprintf("    commit: %s\n", entry.CommitHash))
		}
		if entry.EventKind != "" {
			builder.WriteString(fmt.Sprintf("    kind:   %s\n", entry.EventKind))
		}
		if len(entry.Components) > 0 {
			builder.WriteString(fmt.Sprintf("    components: %s\n", strings.Join(entry.Components, ", ")))
		}
		if entry.Intent != "" {
			builder.WriteString(fmt.Sprintf("    reason: %s\n", entry.Intent))
		}
		if len(entry.Tags) > 0 {
			builder.WriteString(fmt.Sprintf("    tags:   %s\n", strings.Join(entry.Tags, ", ")))
		}
	}
	return builder.String()
}

// RenderTimelineMermaid renders the timeline as a Mermaid timeline diagram,
// grouped into month sections. Titles are sanitized so a colon in a title
// cannot break the "text : description" item syntax.
func RenderTimelineMermaid(entries []archmodel.TimelineEntry) string {
	var builder strings.Builder
	builder.WriteString("timeline\n")
	builder.WriteString("    title Architecture Evolution\n")

	currentSection := ""
	for _, entry := range sortTimeline(entries) {
		section := entry.Timestamp.Format("2006-01")
		if section != currentSection {
			builder.WriteString(fmt.Sprintf("    section %s\n", section))
			currentSection = section
		}
		item := sanitizeMermaidText(entry.Title)
		if entry.Description != "" {
			item += " : " + sanitizeMermaidText(entry.Description)
		}
		builder.WriteString(fmt.Sprintf("        %s\n", item))
	}
	return builder.String()
}

// sanitizeMermaidText removes characters that break Mermaid syntax within a
// timeline item (colons delimit the optional description).
func sanitizeMermaidText(s string) string {
	s = strings.ReplaceAll(s, ":", "-")
	s = strings.TrimSpace(s)
	return s
}
