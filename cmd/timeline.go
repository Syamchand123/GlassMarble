package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/git"
	"github.com/spf13/cobra"
)

// timelineCmd shows the architecture evolution timeline (master plan §5.5 /
// Stage 7). It reads the Stage 6 memory fast path (.glassmarble/memory/)
// directly — no AKG replay, so the command stays well under the 200ms budget.
var timelineCmd = &cobra.Command{
	Use:   "timeline",
	Short: "Show the architecture evolution timeline",
	Long: `Renders the chronological story of the architecture, taken from the
developer memory (.glassmarble/memory/timeline.json). Reasons are never
invented: entries only appear when an architectural event was actually
recorded by an analysis run.

Flags:
  --component NAME  only entries touching a component (substring match)
  --from <date|ref> start of the window (ISO date, or a git ref whose author
                    timestamp is used; default: 6 months ago)
  --to   <date|ref> end of the window (default: now)
  --format text|json|mermaid
  --full            verbose text output (commit, kind, components, tags)

Run 'gmb analyze' (or 'gmb watch') to populate the timeline.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		if dir == "" {
			dir = "."
		}
		fromStr, _ := cmd.Flags().GetString("from")
		toStr, _ := cmd.Flags().GetString("to")
		component, _ := cmd.Flags().GetString("component")
		format, _ := cmd.Flags().GetString("format")
		full, _ := cmd.Flags().GetBool("full")

		absDir, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}
		store := developer_memory.NewStoreForRepo(absDir)
		mem, err := store.LoadMemory()
		if err != nil {
			return fmt.Errorf("failed to load developer memory: %w", err)
		}
		if mem == nil || mem.TotalEvents == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "Developer memory is empty. Run 'gmb analyze' to start recording architectural events.")
			return nil
		}

		// Default window: the last six months (master plan §5.5).
		from := time.Now().UTC().AddDate(0, -6, 0)
		if fromStr != "" {
			f, err := parseTimeArg(absDir, fromStr)
			if err != nil {
				return err
			}
			from = f
		}
		to := time.Time{}
		if toStr != "" {
			t, err := parseTimeArg(absDir, toStr)
			if err != nil {
				return err
			}
			to = t
		}

		var entries []archmodel.TimelineEntry
		if component != "" {
			entries = developer_memory.GetComponentTimelineFromMemory(mem, component)
			entries = filterTimelineWindow(entries, from, to)
		} else {
			entries = developer_memory.GetFullTimelineFromMemory(mem, from, to)
		}

		switch strings.ToLower(format) {
		case "json":
			out, merr := json.MarshalIndent(entries, "", "  ")
			if merr != nil {
				return fmt.Errorf("failed to encode timeline: %w", merr)
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
		case "mermaid":
			fmt.Fprint(cmd.OutOrStdout(), arch_timeline.RenderTimelineMermaid(entries))
		case "text", "":
			if full {
				fmt.Fprint(cmd.OutOrStdout(), arch_timeline.RenderTimelineFull(entries))
			} else {
				fmt.Fprint(cmd.OutOrStdout(), arch_timeline.RenderTimeline(entries))
			}
		default:
			return fmt.Errorf("unknown format %q (want text, json or mermaid)", format)
		}
		return nil
	},
}

// parseTimeArg interprets an argument as an ISO date (RFC3339, "2006-01-02
// 15:04:05", "2006-01-02 15:04" or "2006-01-02") or as a git ref resolved to
// its author timestamp (D4).
func parseTimeArg(repoDir, s string) (time.Time, error) {
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	if t, err := git.GetCommitTimestamp(repoDir, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("cannot parse %q as an ISO date or a git ref", s)
}

// filterTimelineWindow keeps the entries within [from, to]; a zero from
// means the beginning, a zero to means now.
func filterTimelineWindow(entries []archmodel.TimelineEntry, from, to time.Time) []archmodel.TimelineEntry {
	if to.IsZero() {
		to = time.Now()
	}
	out := entries[:0]
	for _, e := range entries {
		if (from.IsZero() || !e.Timestamp.Before(from)) && !e.Timestamp.After(to) {
			out = append(out, e)
		}
	}
	return out
}

func init() {
	timelineCmd.Flags().String("dir", ".", "Directory containing the .glassmarble/ database")
	timelineCmd.Flags().String("component", "", "Only entries touching a component (substring match)")
	timelineCmd.Flags().String("from", "", "Window start: ISO date or git ref (default: 6 months ago)")
	timelineCmd.Flags().String("to", "", "Window end: ISO date or git ref (default: now)")
	timelineCmd.Flags().String("format", "text", "Output format: text, json or mermaid")
	timelineCmd.Flags().Bool("full", false, "Verbose text output with commit, kind, components and tags")
	rootCmd.AddCommand(timelineCmd)
}
