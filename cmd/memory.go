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
	"github.com/Syamchand123/GlassMarble/internal/learning"
	"github.com/spf13/cobra"
)

// memoryCmd answers questions about the project's architectural memory
// (Stage 6) and records developer corrections to it (Stage 10). It is a
// deterministic, read-only view over .glassmarble/memory/ — no LLM is
// involved (master plan §4.4). Corrections recorded with --correct are
// overlaid onto every query result immediately (master plan §8.3).
var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Query the developer memory: what do we know, when did it change, and why",
	Long: `Reads the Stage 6 developer memory (.glassmarble/memory/) and answers
questions like "what do we know about Redis?" and "why was PaymentService added?".

Modes:
  (default)        project overview: memory stats and current components
  --ask "query"    ranked knowledge retrieval (components, claims, events, timeline)
  --component NAME longitudinal history of one component plus its timeline
  --correct ID     record a developer correction (Stage 10 learning layer):
                   --kind INTENT|LABEL|STATE|CONFIDENCE|REJECT|ACCEPT --value ...
  --corrections    list the correction audit log (append-only, reversible)

Corrections never modify the underlying memory: they are an auditable
overlay applied to query results. Reasons are never invented: claims are
labelled by how they were established (FACT / EXPLICIT_REASON / INFERENCE
/ SPECULATION).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		if dir == "" {
			dir = "."
		}
		ask, _ := cmd.Flags().GetString("ask")
		component, _ := cmd.Flags().GetString("component")
		asJSON, _ := cmd.Flags().GetBool("json")
		correct, _ := cmd.Flags().GetString("correct")
		kind, _ := cmd.Flags().GetString("kind")
		value, _ := cmd.Flags().GetString("value")
		reason, _ := cmd.Flags().GetString("reason")
		author, _ := cmd.Flags().GetString("author")
		listCorrections, _ := cmd.Flags().GetBool("corrections")

		absDir, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}
		store := developer_memory.NewStoreForRepo(absDir)
		lcfg, err := loadLearningConfig(filepath.Join(absDir, ".glassmarble"))
		if err != nil {
			// A broken learning config must not break memory queries —
			// fall back to defaults (mirrors fusion config behavior).
			lcfg = nil
		}
		learner := learning.NewLearnerForRepo(absDir, learning.WithConfig(lcfg))

		switch {
		case listCorrections:
			return renderCorrections(cmd, learner, asJSON)
		case correct != "":
			return recordCorrection(cmd, learner, store, correct, kind, value, reason, author, asJSON)
		case ask != "":
			return renderQuery(cmd, learner, store, ask, asJSON)
		case component != "":
			return renderComponent(cmd, learner, store, component, asJSON)
		default:
			return renderOverview(cmd, learner, store, asJSON)
		}
	},
}

func init() {
	memoryCmd.Flags().String("dir", ".", "Directory containing the .glassmarble/ database")
	memoryCmd.Flags().String("ask", "", "Ask the memory a question (deterministic retrieval, no LLM)")
	memoryCmd.Flags().String("component", "", "Show the full history of one component (substring match)")
	memoryCmd.Flags().Bool("json", false, "Emit machine-readable JSON instead of the human report")
	memoryCmd.Flags().String("correct", "", "Record a developer correction for a memory item (target ID or component name)")
	memoryCmd.Flags().String("kind", string(learning.CorrectionKindIntent),
		"Correction kind: INTENT, LABEL, STATE, CONFIDENCE, REJECT, ACCEPT (used with --correct)")
	memoryCmd.Flags().String("value", "", "The corrected value (required for INTENT, LABEL, STATE, CONFIDENCE; ignored for REJECT/ACCEPT)")
	memoryCmd.Flags().String("reason", "", "Why the correction was made (audit trail)")
	memoryCmd.Flags().String("author", "", "Who made the correction (audit trail, optional)")
	memoryCmd.Flags().Bool("corrections", false, "List the correction audit log instead of querying")
	rootCmd.AddCommand(memoryCmd)
}

// renderOverview prints the memory stats and current components. JSON mode
// emits the whole aggregate so machine consumers get everything. Stage 10
// corrections (e.g. a STATE override) are reflected in the projection.
func renderOverview(cmd *cobra.Command, learner *learning.Learner, store *developer_memory.MemoryStore, asJSON bool) error {
	mem, err := store.LoadMemory()
	if err != nil {
		return fmt.Errorf("failed to load memory: %w", err)
	}
	proj, applied, err := learner.OverlayMemory(mem)
	if err != nil {
		return fmt.Errorf("failed to apply corrections: %w", err)
	}
	if asJSON {
		out, _ := json.MarshalIndent(proj, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}
	if proj.TotalEvents == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Developer memory is empty. Run 'gmb analyze' to start recording architectural events.")
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Developer memory %s | %d event(s) | %d component(s) | last update %s\n",
		proj.ProjectID, proj.TotalEvents, len(proj.ComponentMemory),
		formatTime(proj.LastUpdated))

	names := sortedComponentNames(proj)
	flags := flagIndex(applied)
	for _, name := range names {
		h := proj.ComponentMemory[name]
		detail := componentStateDetail(h)
		if flags.isRejected(h.Name) {
			detail += " (rejected)"
		} else if flags.isConfirmed(h.Name) {
			detail += " (confirmed)"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %-40s %s\n", name, detail)
	}
	if len(applied) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "\n%d correction(s) applied to this view — 'gmb memory --corrections' for the audit trail.\n", appliedCount(applied))
	}
	return nil
}

// appliedCount returns how many audit entries actually took effect (targets
// that existed in the projected data).
func appliedCount(applied []learning.AppliedCorrection) int {
	n := 0
	for _, a := range applied {
		if a.Applied {
			n++
		}
	}
	return n
}

// renderQuery runs deterministic ranked retrieval for the user's question,
// with the Stage 10 correction overlay applied (master plan §8.3).
func renderQuery(cmd *cobra.Command, learner *learning.Learner, store *developer_memory.MemoryStore, ask string, asJSON bool) error {
	proj, err := learner.Query(store, ask)
	if err != nil {
		return fmt.Errorf("failed to apply corrections: %w", err)
	}
	if asJSON {
		out, _ := json.MarshalIndent(proj, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	out := cmd.OutOrStdout()
	if len(proj.Components) == 0 && len(proj.Claims) == 0 && len(proj.Events) == 0 {
		fmt.Fprintf(out, "The memory holds nothing about %q. Run 'gmb analyze' to build it, or ask about a known component.\n", ask)
		return nil
	}
	fmt.Fprintf(out, "Answering %q — ranked from developer memory:\n\n", ask)
	flags := flagIndex(proj.CorrectionsApplied)

	if len(proj.Components) > 0 {
		fmt.Fprintln(out, "Components:")
		for _, c := range proj.Components {
			fmt.Fprintf(out, "  %-40s state=%s first=%s last=%s%s\n",
				c.Name, c.State, formatTime(c.FirstSeen), formatTime(c.LastSeen),
				flagSuffix(flags, c.Name))
		}
		fmt.Fprintln(out)
	}
	if len(proj.Claims) > 0 {
		fmt.Fprintln(out, "Claims (how each was established):")
		for _, c := range proj.Claims {
			fmt.Fprintf(out, "  %s %s %s [%s, %.0f%% confidence, state=%s]%s\n",
				c.Subject, c.Predicate, quoteObject(c.Object), c.ClaimKind, c.Evidence.AggConfidence*100,
				c.State, flagSuffix(flags, c.ID))
		}
		fmt.Fprintln(out)
	}
	if len(proj.Events) > 0 {
		fmt.Fprintln(out, "Events:")
		for _, e := range proj.Events {
			fmt.Fprintf(out, "  %s  %s  %s%s\n", formatTime(e.Timestamp), e.Kind, e.Title,
				flagSuffix(flags, e.ID))
		}
		fmt.Fprintln(out)
	}
	if len(proj.Timeline) > 0 {
		fmt.Fprintln(out, "Related timeline:")
		for _, t := range proj.Timeline {
			fmt.Fprintf(out, "  %s  %s  %s\n", formatTime(t.Timestamp), shortCommit(t.CommitHash), t.Title)
		}
	}
	if len(proj.CorrectionsApplied) > 0 {
		fmt.Fprintf(out, "%d correction(s) applied to this view — 'gmb memory --corrections' for the audit trail.\n",
			appliedCount(proj.CorrectionsApplied))
	}
	return nil
}

// renderComponent prints the longitudinal history of one component and the
// timeline entries that mention it, with corrections applied.
func renderComponent(cmd *cobra.Command, learner *learning.Learner, store *developer_memory.MemoryStore, component string, asJSON bool) error {
	mem, err := store.LoadMemory()
	if err != nil {
		return fmt.Errorf("failed to load memory: %w", err)
	}
	proj, applied, err := learner.OverlayMemory(mem)
	if err != nil {
		return fmt.Errorf("failed to apply corrections: %w", err)
	}
	history := findComponent(proj, component)

	if asJSON {
		type componentView struct {
			Query              string                             `json:"query"`
			Found              bool                               `json:"found"`
			History            *developer_memory.ComponentHistory `json:"history,omitempty"`
			Timeline           []archmodel.TimelineEntry          `json:"timeline"`
			CorrectionsApplied []learning.AppliedCorrection       `json:"corrections_applied,omitempty"`
		}
		out, _ := json.MarshalIndent(componentView{
			Query:              component,
			Found:              history != nil,
			History:            history,
			Timeline:           developer_memory.GetComponentTimelineFromMemory(proj, component),
			CorrectionsApplied: applied,
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
		if ev := findEvent(proj, id); ev != nil {
			fmt.Fprintf(out, "  %s  [%s]  %s\n", formatTime(ev.Timestamp), ev.Kind, ev.Title)
		}
	}
	entries := developer_memory.GetComponentTimelineFromMemory(proj, component)
	if len(entries) > 0 {
		fmt.Fprintln(out, "Timeline:")
		for _, t := range entries {
			fmt.Fprintf(out, "  %s  %s  %s\n", formatTime(t.Timestamp), shortCommit(t.CommitHash), t.Title)
		}
	}
	return nil
}

// recordCorrection appends a developer correction to the learning log
// (.glassmarble/memory/corrections.jsonl, master plan §8.2). The target's
// current displayed value is captured automatically for the audit trail.
func recordCorrection(cmd *cobra.Command, learner *learning.Learner, store *developer_memory.MemoryStore, targetID, kind, value, reason, author string, asJSON bool) error {
	corrKind := learning.CorrectionKind(kind)
	if !corrKind.KnownKinds() {
		return fmt.Errorf("unknown correction kind %q (want one of INTENT, LABEL, STATE, CONFIDENCE, REJECT, ACCEPT)", kind)
	}
	if corrKind.NeedsValue() && value == "" {
		return fmt.Errorf("--value is required for %s corrections", corrKind)
	}

	mem, err := store.LoadMemory()
	if err != nil {
		return fmt.Errorf("failed to load memory: %w", err)
	}
	c, err := learner.Correct(learning.Correction{
		Kind:           corrKind,
		TargetID:       targetID,
		CorrectedValue: value,
		Reason:         reason,
		Author:         author,
	}, mem)
	if err != nil {
		return fmt.Errorf("failed to record correction: %w", err)
	}

	if asJSON {
		out, _ := json.MarshalIndent(c, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Recorded correction %s: %s on %q", c.ID, c.Kind, c.TargetID)
	if c.OriginalValue != "" {
		fmt.Fprintf(cmd.OutOrStdout(), " (%q -> %q)", c.OriginalValue, c.CorrectedValue)
	} else if c.CorrectedValue != "" {
		fmt.Fprintf(cmd.OutOrStdout(), " -> %q", c.CorrectedValue)
	}
	if c.Reason != "" {
		fmt.Fprintf(cmd.OutOrStdout(), " — %s", c.Reason)
	}
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "The correction is reflected in every memory query. The log is append-only; 'gmb memory --corrections' shows the full audit trail.")
	return nil
}

// renderCorrections prints the full correction audit log (Stage 10
// auditability: what was learned, when, by whom).
func renderCorrections(cmd *cobra.Command, learner *learning.Learner, asJSON bool) error {
	corrections, err := learner.List()
	if err != nil {
		return fmt.Errorf("failed to load corrections: %w", err)
	}
	if asJSON {
		if corrections == nil {
			corrections = []learning.Correction{}
		}
		out, _ := json.MarshalIndent(corrections, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}
	out := cmd.OutOrStdout()
	if len(corrections) == 0 {
		fmt.Fprintln(out, "No corrections recorded yet. Use 'gmb memory --correct <id> --kind ... --value ...' to teach GlassMarble.")
		return nil
	}
	fmt.Fprintf(out, "%d correction(s) in the audit log (append-only; reversing = append a compensating correction):\n\n", len(corrections))
	for _, c := range corrections {
		fmt.Fprintf(out, "  %s  %-10s target=%-24s %q -> %q",
			formatTime(c.Timestamp), c.Kind, c.TargetID, c.OriginalValue, c.CorrectedValue)
		if c.Reason != "" {
			fmt.Fprintf(out, "  (%s)", c.Reason)
		}
		if c.Author != "" {
			fmt.Fprintf(out, "  by %s", c.Author)
		}
		fmt.Fprintln(out)
	}
	return nil
}

// correctionFlags indexes applied corrections by rejected/confirmed status
// so human output can flag items without touching their values.
type correctionFlags struct {
	rejected  map[string]bool
	confirmed map[string]bool
}

func flagIndex(applied []learning.AppliedCorrection) correctionFlags {
	f := correctionFlags{rejected: map[string]bool{}, confirmed: map[string]bool{}}
	for _, a := range applied {
		if !a.Applied {
			continue
		}
		switch a.Correction.Kind {
		case learning.CorrectionKindReject:
			f.rejected[a.Correction.TargetID] = true
		case learning.CorrectionKindAccept:
			f.confirmed[a.Correction.TargetID] = true
		}
	}
	return f
}

func (f correctionFlags) isRejected(target string) bool  { return f.rejected[target] }
func (f correctionFlags) isConfirmed(target string) bool { return f.confirmed[target] }

// flagSuffix renders the rejection/confirmation marker for one item.
func flagSuffix(f correctionFlags, target string) string {
	switch {
	case f.isRejected(target):
		return " (rejected)"
	case f.isConfirmed(target):
		return " (confirmed)"
	}
	return ""
}

// componentStateDetail renders a component's state for the overview.
func componentStateDetail(h developer_memory.ComponentHistory) string {
	switch h.State {
	case developer_memory.StateActive:
		return "current"
	case developer_memory.StateRemoved:
		return "removed " + formatTime(h.LastSeen)
	case developer_memory.StateDeprecated:
		return "deprecated"
	case developer_memory.StateExperimental:
		return "experimental"
	case developer_memory.StateHistorical:
		return "historical"
	}
	return string(h.State)
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
