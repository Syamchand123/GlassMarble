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

var timelineCmd = &cobra.Command{
	Use:     "timeline",
	GroupID: GroupGovern.ID,
	Short:   "Show the architecture evolution timeline",
	Long: `Renders the chronological story of the architecture, taken from developer memory
(.glassmarble/memory/timeline.json).

Reasons and event entries are recorded automatically during analysis runs when architectural
changes, refactorings, or ADRs occur.`,
	Example: `  # Display full architecture timeline
  gmb timeline

  # Filter timeline by component name
  gmb timeline --component auth

  # Filter by date range or git tag
  gmb timeline --from 2026-01-01 --to HEAD

  # Output timeline as Mermaid diagram or JSON
  gmb timeline --format mermaid
  gmb timeline --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolveDir(cmd)
		fromStr, _ := cmd.Flags().GetString("from")
		toStr, _ := cmd.Flags().GetString("to")
		component, _ := cmd.Flags().GetString("component")
		format, _ := cmd.Flags().GetString("format")
		asJSON, _ := cmd.Flags().GetBool("json")
		full, _ := cmd.Flags().GetBool("full")

		if asJSON {
			format = "json"
		}

		absDir, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}
		store := developer_memory.NewStoreForRepo(absDir)
		mem, err := store.LoadMemory()
		if err != nil {
			return fmt.Errorf("failed to load developer memory: %w — try 'gmb analyze'", err)
		}
		if mem == nil || mem.TotalEvents == 0 {
			if format == "json" {
				out, _ := json.MarshalIndent([]archmodel.TimelineEntry{}, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Developer memory is empty. Run 'gmb analyze' to start recording architectural events.")
			return nil
		}

		// Default window: the last six months
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
			return fmt.Errorf("unknown format %q (want text, json or mermaid) — try 'gmb timeline --format mermaid'", format)
		}
		return nil
	},
}

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
	return time.Time{}, fmt.Errorf("cannot parse %q as an ISO date or git ref — try 'YYYY-MM-DD' or 'HEAD~1'", s)
}

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
	timelineCmd.Flags().String("component", "", "Filter entries touching a specific component (case-insensitive substring)")
	timelineCmd.Flags().String("from", "", "Window start: ISO date or git ref (default: 6 months ago)")
	timelineCmd.Flags().String("to", "", "Window end: ISO date or git ref (default: now)")
	timelineCmd.Flags().String("format", "text", "Output format: text|json|mermaid")
	timelineCmd.Flags().Bool("json", false, "Emit machine-readable JSON output (alias for --format json)")
	timelineCmd.Flags().Bool("full", false, "Verbose text output with commit, kind, components, and tags")

	_ = timelineCmd.RegisterFlagCompletionFunc("format", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"text", "json", "mermaid"}, cobra.ShellCompDirectiveNoFileComp
	})

	rootCmd.AddCommand(timelineCmd)
}
