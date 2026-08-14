package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/git"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
	"github.com/spf13/cobra"
)

// snapshotCmd manages architecture snapshots (master plan §5.5 / architecture timeline).
// Exactly one mode per invocation:
//
//	--create            run architecture intelligence at HEAD and store a new snapshot
//	--list              list indexed snapshots
//	--at <ref>          show the state at a commit/ref (nearest snapshot)
//	--diff <ref> <ref>  architectural diff between two snapshots
//	--replay <ref>      render a diagram from the restored graph
//
// Refs are commit hashes, prefixes, tags, branches or HEAD. The snapshot
// store content-addresses files by snapshot ID, skip-writes unchanged
// topologies, and self-heals a missing/corrupt index (D2).
var snapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Create and query point-in-time architecture snapshots",
	Long: `Snapshots capture the full architecture state (graph + Architecture Intelligence analysis)
at a commit. This command creates and inspects them.

  gmb snapshot --create                     # snapshot at HEAD
  gmb snapshot --list
  gmb snapshot --at HEAD
  gmb snapshot --at <commit>                # nearest snapshot at/before the commit
  gmb snapshot --diff <base> <head>
  gmb snapshot --replay <commit> --diagram dependency

Refs are resolved as stored commit prefixes first, then as git refs (tags,
branches, HEAD) via their author timestamp.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		if dir == "" {
			dir = "."
		}
		create, _ := cmd.Flags().GetBool("create")
		list, _ := cmd.Flags().GetBool("list")
		at, _ := cmd.Flags().GetString("at")
		replay, _ := cmd.Flags().GetString("replay")
		diffStr, _ := cmd.Flags().GetString("diff")
		diffRefs := parseRefList(diffStr)
		// `--diff <base> <head>` reads the second ref as a positional arg
		// (flags consume only one token; also accepts comma/space-separated).
		diffRefs = append(diffRefs, parseRefList(strings.Join(args, " "))...)
		noGraph, _ := cmd.Flags().GetBool("no-graph")
		asJSON, _ := cmd.Flags().GetBool("json")
		diagram, _ := cmd.Flags().GetString("diagram")

		modes := 0
		for _, on := range []bool{create, list} {
			if on {
				modes++
			}
		}
		for _, s := range []string{at, replay} {
			if s != "" {
				modes++
			}
		}
		if len(diffRefs) > 0 {
			modes++
		}
		if modes == 0 {
			return fmt.Errorf("no mode selected: use --create, --list, --at, --diff or --replay")
		}
		if modes > 1 {
			return fmt.Errorf("only one mode may be used at a time")
		}

		absDir, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}
		storageDir := filepath.Join(absDir, ".glassmarble")
		store, err := arch_timeline.NewSnapshotStore(filepath.Join(storageDir, "snapshots"))
		if err != nil {
			return err
		}

		switch {
		case create:
			return runSnapshotCreate(cmd, store, storageDir, absDir, noGraph, asJSON)
		case list:
			return runSnapshotList(cmd, store)
		case at != "":
			return runSnapshotAt(cmd, store, absDir, at, asJSON)
		case len(diffRefs) > 0:
			if len(diffRefs) != 2 {
				return fmt.Errorf("--diff requires exactly two refs: --diff <base> <head>")
			}
			return runSnapshotDiff(cmd, store, absDir, diffRefs[0], diffRefs[1], asJSON)
		default: // replay
			return runSnapshotReplay(cmd, store, absDir, replay, diagram)
		}
	},
}

// runSnapshotCreate runs architecture intelligence on the committed graph at HEAD and stores a
// new snapshot (skip-writing when the topology is unchanged).
func runSnapshotCreate(cmd *cobra.Command, store *arch_timeline.SnapshotStore, storageDir, absDir string, noGraph, asJSON bool) error {
	commitHash, err := git.GetHEADCommitHash(absDir)
	if err != nil {
		commitHash = ""
	}

	tm, err := newAKGManager(storageDir, cmd)
	if err != nil {
		return fmt.Errorf("failed to open AKG: %w", err)
	}
	graph := tm.GetActiveGraph()
	if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
		return fmt.Errorf("AKG database is empty -- run 'gmb analyze' first")
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	res := runIntelligence(graph, storageDir, verbose)

	snap, wrote, err := buildAndStoreSnapshot(absDir, graph, commitHash, res, store, noGraph)
	if err != nil {
		return fmt.Errorf("snapshot failed: %w", err)
	}

	if asJSON {
		out, _ := json.MarshalIndent(snap, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	if commitHash == "" {
		fmt.Fprintln(cmd.OutOrStdout(), "note: not a git repository (or no commits yet) — snapshot recorded without a commit reference")
	}
	if !wrote {
		fmt.Fprintf(cmd.OutOrStdout(), "Snapshot unchanged (topology identical to the latest snapshot): %s\n", snap.ID)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Snapshot created: %s at %s (%s | %d nodes, %d edges | %d components, %d patterns, %d smells)\n",
		snap.ID, snap.Timestamp.Format("2006-01-02 15:04"), shortCommit(snap.CommitHash),
		snap.NodeCount, snap.EdgeCount, len(snap.Components), len(snap.Patterns), len(snap.Smells))
	return nil
}

// runSnapshotList prints the snapshot index as an aligned table.
func runSnapshotList(cmd *cobra.Command, store *arch_timeline.SnapshotStore) error {
	entries := store.List()
	if len(entries) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No snapshots yet. Run 'gmb snapshot --create' or 'gmb analyze'.")
		return nil
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SNAPSHOT ID\tCOMMIT\tCAPTURED\tTOPOLOGY HASH\tPATS\tSMELLS")
	for _, e := range entries {
		hash := e.TopologyHash
		if len(hash) > 12 {
			hash = hash[:12] + "…"
		}
		if hash == "" {
			hash = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\n",
			e.SnapshotID, shortCommit(e.CommitHash), e.Timestamp.Format("2006-01-02 15:04:05"), hash, e.PatternCount, e.SmellCount)
	}
	w.Flush()
	return nil
}

// runSnapshotAt prints the state at a ref: the exact snapshot for a stored
// commit prefix, else the snapshot nearest to (not after) the ref's author
// timestamp.
func runSnapshotAt(cmd *cobra.Command, store *arch_timeline.SnapshotStore, absDir, ref string, asJSON bool) error {
	snap, err := resolveSnapshot(store, absDir, ref)
	if err != nil {
		return err
	}
	if asJSON {
		out, _ := json.MarshalIndent(snap, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Snapshot %s\n", snap.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  commit:     %s\n", shortCommit(snap.CommitHash))
	fmt.Fprintf(cmd.OutOrStdout(), "  captured:   %s\n", snap.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(cmd.OutOrStdout(), "  graph:      %d nodes, %d edges (embedded: %v)\n", snap.NodeCount, snap.EdgeCount, len(snap.AKGJSON) > 0)
	fmt.Fprintf(cmd.OutOrStdout(), "  components: %d\n", len(snap.Components))
	for _, c := range snap.Components {
		fmt.Fprintf(cmd.OutOrStdout(), "    - %s (%s, confidence %.2f)\n", c.Name, c.Kind, c.Confidence)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "  patterns:   %d | smells: %d | cycles: %d | layer violations: %d\n",
		len(snap.Patterns), len(snap.Smells), snap.Metrics.CycleCount, snap.Metrics.LayerViolationCount)
	return nil
}

// runSnapshotDiff prints the architectural evolution between two refs.
func runSnapshotDiff(cmd *cobra.Command, store *arch_timeline.SnapshotStore, absDir, baseRef, headRef string, asJSON bool) error {
	base, err := resolveSnapshot(store, absDir, baseRef)
	if err != nil {
		return fmt.Errorf("cannot resolve base ref %q: %w", baseRef, err)
	}
	head, err := resolveSnapshot(store, absDir, headRef)
	if err != nil {
		return fmt.Errorf("cannot resolve head ref %q: %w", headRef, err)
	}
	result := arch_timeline.Diff(base, head)

	if asJSON {
		out, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Architecture diff  %s (%s) → %s (%s)\n",
		base.ID, shortCommit(base.CommitHash), head.ID, shortCommit(head.CommitHash))
	fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", result.Delta.MetricDelta.SummaryLine)

	if len(result.Delta.AddedComponents) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "\nComponents added:")
		for _, name := range result.Delta.AddedComponents {
			fmt.Fprintf(cmd.OutOrStdout(), "  + %s\n", name)
		}
	}
	if len(result.Delta.RemovedComponents) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "\nComponents removed:")
		for _, name := range result.Delta.RemovedComponents {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", name)
		}
	}
	for _, r := range result.Renames {
		fmt.Fprintf(cmd.OutOrStdout(), "\nRenamed: %s → %s (similarity %.2f, %d shared nodes)\n", r.OldName, r.NewName, r.Similarity, r.NodeOverlap)
	}
	for _, s := range result.Splits {
		fmt.Fprintf(cmd.OutOrStdout(), "\nSplit: %s → %s\n", s.Source, strings.Join(s.Targets, ", "))
	}
	for _, m := range result.Merges {
		fmt.Fprintf(cmd.OutOrStdout(), "\nMerged: %s → %s\n", strings.Join(m.Sources, ", "), m.Target)
	}
	if len(result.DependencyChanges) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "\nDependency changes:")
		for _, d := range result.DependencyChanges {
			marker := "+"
			if !d.Added {
				marker = "-"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  %s %s → %s\n", marker, d.Source, d.Target)
		}
	}
	if len(result.Delta.PatternChanges) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "\nPattern changes:")
		for _, p := range result.Delta.PatternChanges {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", p)
		}
	}
	if len(result.Delta.SmellChanges) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "\nSmell changes:")
		for _, s := range result.Delta.SmellChanges {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", s)
		}
	}

	if result.Graph != nil {
		fmt.Fprintln(cmd.OutOrStdout(), "\nStructural changes:")
		fmt.Fprint(cmd.OutOrStdout(), views.RenderCompare(result.Graph))
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "\n(no structural diff: one of the snapshots has no embedded graph)")
	}
	return nil
}

// runSnapshotReplay restores the graph embedded in a snapshot and renders it
// as a diagram via the visualization engine (D4: composition happens here,
// arch_timeline stays free of the engine).
func runSnapshotReplay(cmd *cobra.Command, store *arch_timeline.SnapshotStore, absDir, ref, diagram string) error {
	snap, err := resolveSnapshot(store, absDir, ref)
	if err != nil {
		return err
	}
	graph, err := arch_timeline.Replay(snap)
	if err != nil {
		return err
	}
	diagType, err := parseDiagramTypeByName(diagram)
	if err != nil {
		return err
	}
	format, _ := cmd.Flags().GetString("format")
	if format == "" {
		format = "mermaid"
	}
	opts := types.QueryOptions{Format: format}
	out, err := visualization_engine.ProjectDiagramFromGraph(graph.ToNativeGraph(), diagType, opts)
	if err != nil {
		return fmt.Errorf("failed to render %s diagram from %s: %w", diagram, snap.ID, err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), out)
	return nil
}

// resolveSnapshot maps a user-supplied ref to a stored snapshot: exact
// snapshot ID first, then a stored commit-prefix match, then the ref resolved
// through git (exact snapshot for that commit), and finally a nearest-
// preceding fallback by author timestamp.
func resolveSnapshot(store *arch_timeline.SnapshotStore, absDir, ref string) (*archmodel.ArchSnapshot, error) {
	if snap, err := store.GetBySnapshotID(ref); err == nil {
		return snap, nil
	}
	if snap, err := store.Get(ref); err == nil {
		return snap, nil
	}
	if full, err := git.ResolveRef(absDir, ref); err == nil {
		if snap, err := store.Get(full); err == nil {
			return snap, nil
		}
		// An exact snapshot for this commit does not exist; fall back to the
		// snapshot nearest to (not after) the commit's author timestamp.
		if ts, terr := git.GetCommitTimestamp(absDir, full); terr == nil {
			if snap, serr := store.NearestAt(ts); serr == nil {
				return snap, nil
			}
		}
	}
	return nil, fmt.Errorf("no snapshot for %q and it is not a resolvable git ref", ref)
}

// parseRefList splits a --diff value into refs on commas and whitespace,
// dropping empties ("--diff 'HEAD~1 HEAD'", "--diff HEAD~1,HEAD" and
// "--diff HEAD~1 HEAD" all yield two refs).
func parseRefList(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		if f != "" && f != "[]" {
			out = append(out, f)
		}
	}
	return out
}

func init() {
	snapshotCmd.Flags().String("dir", ".", "Directory containing the .glassmarble/ database")
	snapshotCmd.Flags().Bool("create", false, "Run architecture intelligence at HEAD and store a new snapshot")
	snapshotCmd.Flags().Bool("list", false, "List indexed snapshots")
	snapshotCmd.Flags().String("at", "", "Show the state at a commit/ref (nearest snapshot at or before it)")
	snapshotCmd.Flags().String("diff", "", "Architectural diff between two refs: --diff '<base> <head>'")
	snapshotCmd.Flags().String("replay", "", "Restore the embedded graph at a ref and render a diagram")
	snapshotCmd.Flags().String("diagram", "dependency", "Diagram type for --replay (e.g. dependency, class, layered, c4container)")
	snapshotCmd.Flags().String("format", "mermaid", "Diagram markup format for --replay: mermaid, plantuml or dot")
	snapshotCmd.Flags().Bool("no-graph", false, "Skip embedding the full graph (smaller files; disables --replay and structural diffs)")
	snapshotCmd.Flags().Bool("json", false, "Emit machine-readable JSON")
	rootCmd.AddCommand(snapshotCmd)
}
