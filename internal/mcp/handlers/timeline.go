package handlers

import (
	"strings"
	"time"
)

// Timeline tool name (Master Plan §6 memory & timeline).
const ToolArchTimeline = "gmb_arch_timeline"

// TimelineToolNames returns the timeline tool set.
func TimelineToolNames() []string {
	return []string{ToolArchTimeline}
}

// TimelineArgs holds validated args for gmb_arch_timeline.
type TimelineArgs struct {
	From      string
	To        string
	Component string
	FromTime  time.Time
	ToTime    time.Time
}

// ValidateTimelineArgs validates gmb_arch_timeline args.
// It accepts RFC3339 timestamps or empty strings; unparsable values are kept as raw strings
// and ignored for time filtering (caller can decide to return all).
func ValidateTimelineArgs(args map[string]any) TimelineArgs {
	from, _ := args["from"].(string)
	to, _ := args["to"].(string)
	comp, _ := args["component"].(string)
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	comp = strings.TrimSpace(comp)
	var fromT, toT time.Time
	if from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			fromT = t
		}
	}
	if to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			toT = t
		}
	}
	return TimelineArgs{
		From:      from,
		To:        to,
		Component: comp,
		FromTime:  fromT,
		ToTime:    toT,
	}
}
