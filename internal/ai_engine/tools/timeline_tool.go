package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// TimelineTool retrieves architectural evolution events across the project timeline.
var TimelineTool = Tool{
	Name:        "get_architecture_timeline",
	Description: "Get the architecture evolution timeline, optionally filtered by component name and date range (ISO 8601 / RFC 3339).",
	Category:    CategoryAKG,
	Parameters: Schema(map[string]Prop{
		"component": {Type: "string", Description: "Optional component name to filter evolution events (e.g. \"redis\", \"auth\")"},
		"from_date": {Type: "string", Description: "Optional ISO 8601 start date (e.g. \"2026-01-01\" or \"2026-01-01T00:00:00Z\")"},
		"to_date":   {Type: "string", Description: "Optional ISO 8601 end date (e.g. \"2026-12-31\" or \"2026-12-31T23:59:59Z\")"},
	}),
	Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
		if env == nil || env.RootDir == "" {
			return nil, fmt.Errorf("workspace root is not configured")
		}

		fromDateStr := strArg(args, "from_date", "")
		toDateStr := strArg(args, "to_date", "")

		fromDate, err := parseTimelineDate(fromDateStr)
		if err != nil {
			return nil, err
		}
		toDate, err := parseTimelineDate(toDateStr)
		if err != nil {
			return nil, err
		}

		store := developer_memory.NewStoreForRepo(env.RootDir)
		component := strArg(args, "component", "")

		var rawTimeline []archmodel.TimelineEntry
		if component != "" {
			rawTimeline = developer_memory.GetComponentTimeline(store, component)
		} else {
			rawTimeline = developer_memory.GetFullTimeline(store, fromDate, toDate)
			if rawTimeline == nil {
				rawTimeline = []archmodel.TimelineEntry{}
			}
			return map[string]any{
				"ok":       true,
				"count":    len(rawTimeline),
				"timeline": rawTimeline,
			}, nil
		}

		// When component is provided, apply date window filtering to the component timeline
		var filtered []archmodel.TimelineEntry
		if toDate.IsZero() {
			toDate = time.Now()
		}
		for _, entry := range rawTimeline {
			if (fromDate.IsZero() || !entry.Timestamp.Before(fromDate)) && !entry.Timestamp.After(toDate) {
				filtered = append(filtered, entry)
			}
		}

		if filtered == nil {
			filtered = []archmodel.TimelineEntry{}
		}

		return map[string]any{
			"ok":        true,
			"component": component,
			"count":     len(filtered),
			"timeline":  filtered,
		}, nil
	},
}

func parseTimelineDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"2006/01/02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date format %q — use ISO 8601 (e.g. 2006-01-02 or 2006-01-02T15:04:05Z)", s)
}
