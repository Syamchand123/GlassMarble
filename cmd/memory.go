package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/spf13/cobra"
)

// memoryCmd answers questions about the project's architectural memory
// (Stage 6). It is a deterministic, read-only view over
// .glassmarble/memory/ — no LLM is involved (master plan §4.4).
var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Query the developer memory: what do we know, when did it change, and why",
	Long: `Reads the Stage 6 developer memory (.glassmarble/memory/) and answers
questions like "what do we know about Redis?" and "why was PaymentService added?".

Modes:
  (default)        project overview: memory stats and current components
  --ask "query"    ranked knowledge retrieval (components, claims, events, timeline)
  --component NAME longitudinal history of one component plus its timeline

Reasons are never invented: claims are labelled by how they were established
(FACT / EXPLICIT_REASON / INFERENCE / SPECULATION).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		if dir == "" {
			dir = "."
		}
		ask, _ := cmd.Flags().GetString("ask")
		component, _ := cmd.Flags().GetString("component")
		asJSON, _ := cmd.Flags().GetBool("json")

		absDir, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}
		store := developer_memory.NewStoreForRepo(absDir)

		switch {
		case ask != "":
			return renderQuery(cmd, store, ask, asJSON)
		case component != "":
			return renderComponent(cmd, store, component, asJSON)
		default:
			return renderOverview(cmd, store, asJSON)
		}
	},
}

func init() {
	memoryCmd.Flags().String("dir", ".", "Directory containing the .glassmarble/ database")
	memoryCmd.Flags().String("ask", "", "Ask the memory a question (deterministic retrieval, no LLM)")
	memoryCmd.Flags().String("component", "", "Show the full history of one component (substring match)")
	memoryCmd.Flags().Bool("json", false, "Emit machine-readable JSON instead of the human report")
	rootCmd.AddCommand(memoryCmd)
}

// renderOverview prints the memory stats and current components. JSON mode
// emits the whole aggregate so machine consumers get everything.
func renderOverview(cmd *cobra.Command, store *developer_memory.MemoryStore, asJSON bool) error {
	mem, err := store.LoadMemory()
	if err != nil {
		return fmt.Errorf("failed to load memory: %w", err)
	}
	if asJSON {
		out, _ := json.MarshalIndent(mem, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}
	if mem.TotalEvents == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Developer memory is empty. Run 'gmb analyze' to start recording architectural events.")
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Developer memory %s | %d event(s) | %d component(s) | last update %s\n",
		mem.ProjectID, mem.TotalEvents, len(mem.ComponentMemory),
		formatTime(mem.LastUpdated))

	names := sortedComponentNames(mem)
	for _, name := range names {
		h := mem.ComponentMemory[name]
		detail := ""
		switch h.State {
		case developer_memory.StateActive:
			detail = "current"
		case developer_memory.StateRemoved:
			detail = "removed " + formatTime(h.LastSeen)
		case developer_memory.StateDeprecated:
			detail = "deprecated"
		case developer_memory.StateExperimental:
			detail = "experimental"
		case developer_memory.StateHistorical:
			detail = "historical"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %-40s %s\n", name, detail)
	}
	return nil
}

// renderQuery runs deterministic ranked retrieval for the user's question.
func renderQuery(cmd *cobra.Command, store *developer_memory.MemoryStore, ask string, asJSON bool) error {
	res := developer_memory.QueryMemory(store, ask)
	if asJSON {
		out, _ := json.MarshalIndent(res, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	out := cmd.OutOrStdout()
	if len(res.Components) == 0 && len(res.Claims) == 0 && len(res.Events) == 0 {
		fmt.Fprintf(out, "The memory holds nothing about %q. Run 'gmb analyze' to build it, or ask about a known component.\n", ask)
		return nil
	}
	fmt.Fprintf(out, "Answering %q — ranked from developer memory:\n\n", ask)

	if len(res.Components) > 0 {
		fmt.Fprintln(out, "Components:")
		for _, c := range res.Components {
			fmt.Fprintf(out, "  %-40s state=%s first=%s last=%s\n",
				c.Name, c.State, formatTime(c.FirstSeen), formatTime(c.LastSeen))
		}
		fmt.Fprintln(out)
	}
	if len(res.Claims) > 0 {
		fmt.Fprintln(out, "Claims (how each was established):")
		for _, c := range res.Claims {
			fmt.Fprintf(out, "  %s %s %s [%s, %.0f%% confidence]\n",
				c.Subject, c.Predicate, quoteObject(c.Object), c.ClaimKind, c.Evidence.AggConfidence*100)
		}
		fmt.Fprintln(out)
	}
	if len(res.Events) > 0 {
		fmt.Fprintln(out, "Events:")
		for _, e := range res.Events {
			fmt.Fprintf(out, "  %s  %s  %s\n", formatTime(e.Timestamp), e.Kind, e.Title)
		}
		fmt.Fprintln(out)
	}
	if len(res.Timeline) > 0 {
		fmt.Fprintln(out, "Related timeline:")
		for _, t := range res.Timeline {
			fmt.Fprintf(out, "  %s  %s  %s\n", formatTime(t.Timestamp), shortCommit(t.CommitHash), t.Title)
		}
	}
	return nil
}

// renderComponent prints the longitudinal history of one component and the
// timeline entries that mention it.
func renderComponent(cmd *cobra.Command, store *developer_memory.MemoryStore, component string, asJSON bool) error {
	mem, err := store.LoadMemory()
	if err != nil {
		return fmt.Errorf("failed to load memory: %w", err)
	}
	history := findComponent(mem, component)

	if asJSON {
		type componentView struct {
			Query    string                             `json:"query"`
			Found    bool                               `json:"found"`
			History  *developer_memory.ComponentHistory `json:"history,omitempty"`
			Timeline []archmodel.TimelineEntry          `json:"timeline"`
		}
		out, _ := json.MarshalIndent(componentView{
			Query:    component,
			Found:    history != nil,
			History:  history,
			Timeline: developer_memory.GetComponentTimelineFromMemory(mem, component),
		}, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	out := cmd.OutOrStdout()
	if history == nil {
		fmt.Fprintf(out, "No component matching %q is in memory. Try 'gmb memory --ask %q'.\n", component, component)
		return nil
	}
	fmt.Fprintf(out, "Component %s | state=%s | first seen %s | last seen %s\n",
		history.Name, history.State, formatTime(history.FirstSeen), formatTime(history.LastSeen))

	for _, id := range history.Events {
		if ev := findEvent(mem, id); ev != nil {
			fmt.Fprintf(out, "  %s  [%s]  %s\n", formatTime(ev.Timestamp), ev.Kind, ev.Title)
		}
	}
	entries := developer_memory.GetComponentTimelineFromMemory(mem, component)
	if len(entries) > 0 {
		fmt.Fprintln(out, "Timeline:")
		for _, t := range entries {
			fmt.Fprintf(out, "  %s  %s  %s\n", formatTime(t.Timestamp), shortCommit(t.CommitHash), t.Title)
		}
	}
	return nil
}

// sortedComponentNames returns the component names in deterministic order.
func sortedComponentNames(mem *developer_memory.DeveloperMemory) []string {
	names := make([]string, 0, len(mem.ComponentMemory))
	for name := range mem.ComponentMemory {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// findComponent looks up a component by exact name, falling back to
// case-insensitive substring match.
func findComponent(mem *developer_memory.DeveloperMemory, name string) *developer_memory.ComponentHistory {
	if h, ok := mem.ComponentMemory[name]; ok {
		return &h
	}
	needle := strings.ToLower(name)
	for n, h := range mem.ComponentMemory {
		if strings.Contains(strings.ToLower(n), needle) {
			return &h
		}
	}
	return nil
}

// findEvent returns the event with the given ID from the aggregate.
func findEvent(mem *developer_memory.DeveloperMemory, id string) *archmodel.ArchEvent {
	for i := range mem.Events {
		if mem.Events[i].ID == id {
			return &mem.Events[i]
		}
	}
	return nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}

func shortCommit(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

func quoteObject(obj string) string {
	if obj == "" {
		return ""
	}
	return "\"" + obj + "\""
}
