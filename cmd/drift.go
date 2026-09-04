package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/drift"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var driftCmd = &cobra.Command{
	Use:     "drift",
	GroupID: GroupGovern.ID,
	Short:   "Detect architecture drift against declared layering and cycle budgets",
	Long: `Compares the committed AKG against the invariants declared in
.glassmarble/config.yaml under the "drift" key.

Reports forbidden cross-layer dependencies and layer cycles, and exits non-zero
when declared cycle budgets or forbidden dependencies are breached (suitable for CI gates).

With --since, the report becomes a comparison against a stored snapshot and
reports movement rather than state: which violations are new, which were fixed,
and which have simply always been there. In that mode the exit code tracks
newly introduced violations instead of the total, so a codebase carrying a
known backlog can gate on "do not make it worse" without first having to reach
zero.`,
	Example: `  # Check current architecture for layer drift and cycles
  gmb drift

  # Output drift report as JSON for CI gating
  gmb drift --json

  # Run drift checks on a specific directory
  gmb drift --dir ./backend

  # What has drifted since a given commit
  gmb drift --since a1b2c3d

  # What has drifted in the last week (ratchet gate: fails only on new breaches)
  gmb drift --since 7d`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolveDir(cmd)
		asJSON, _ := cmd.Flags().GetBool("json")

		storageDir := filepath.Join(dir, ".glassmarble")
		tm, err := newAKGManager(storageDir, cmd)
		if err != nil {
			return fmt.Errorf("failed to open AKG database: %w — try 'gmb analyze'", err)
		}

		graph := tm.GetActiveSnapshot()
		if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
			if asJSON {
				out, _ := json.MarshalIndent(map[string]string{"error": "no active AKG database"}, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			return producterrs.Tagged("AKG database is empty — try 'gmb analyze' first", producterrs.ErrEmptySubgraph)
		}

		// A malformed config must fail loudly. Swallowing the parse error left
		// cfg empty, so drift checked nothing and reported success - the worst
		// possible outcome for a governance gate wired into CI.
		cfg := config.Config{}
		cfgPath := filepath.Join(storageDir, "config.yaml")
		if data, rerr := os.ReadFile(cfgPath); rerr == nil {
			var local config.Config
			if yerr := yaml.Unmarshal(data, &local); yerr != nil {
				return producterrs.Tagged(
					fmt.Sprintf("%s is not valid YAML: %v — fix the file, or remove it to fall back to the global config", cfgPath, yerr),
					producterrs.ErrEntryMissing)
			}
			cfg = local
		} else if !os.IsNotExist(rerr) {
			return fmt.Errorf("failed to read %s: %w", cfgPath, rerr)
		}

		// Each drift setting falls back to the global config independently.
		// Gating the whole fallback on "both Layers and ForbiddenDeps are nil"
		// meant a repo that declared layers locally could never inherit the
		// global CycleBudget, which then stayed 0 — so any cycle at all
		// "exceeded budget" and failed CI.
		if global, gerr := config.Load(config.Config{}); gerr == nil {
			if cfg.Drift.Layers == nil {
				cfg.Drift.Layers = global.Drift.Layers
			}
			if cfg.Drift.ForbiddenDeps == nil {
				cfg.Drift.ForbiddenDeps = global.Drift.ForbiddenDeps
			}
			if cfg.Drift.CycleBudget == 0 {
				cfg.Drift.CycleBudget = global.Drift.CycleBudget
			}
		}

		rep := drift.Analyze(graph, cfg.Drift)

		if since, _ := cmd.Flags().GetString("since"); since != "" {
			return runDriftTrend(cmd, storageDir, since, rep, cfg, asJSON)
		}

		if asJSON {
			out, _ := json.MarshalIndent(rep, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
		} else {
			tui.Fprintln(cmd.OutOrStdout(), views.RenderDrift(rep))
		}

		if rep.ExceedsBudget() || rep.ForbiddenEdges > 0 {
			return producterrs.Tagged(fmt.Sprintf(
				"architecture drift detected: %d forbidden dependency(ies), %d cycle(s) exceed budget %d — try inspecting violations with 'gmb inspect'",
				rep.ForbiddenEdges, rep.CycleCount, rep.CycleBudget), producterrs.ErrPolicyViolation)
		}
		return nil
	},
}

func init() {
	driftCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	driftCmd.Flags().String("since", "", "Compare against a stored snapshot and report movement: a commit hash prefix, or an age such as 7d/24h/30m")
	rootCmd.AddCommand(driftCmd)
}

// resolveDriftBaseline finds the snapshot to compare against. since is either a
// commit hash prefix or an age ("7d", "24h", "30m"); an age resolves to the
// snapshot nearest that point in history, so the caller does not have to know
// which commits happen to have been analyzed.
func resolveDriftBaseline(store *arch_timeline.SnapshotStore, since string) (*archmodel.ArchSnapshot, error) {
	if d, ok := parseAge(since); ok {
		return store.NearestAt(time.Now().Add(-d))
	}
	return store.Get(since)
}

// parseAge accepts the git-ish shorthand people actually type.
// time.ParseDuration handles h/m/s but rejects days, which is the unit that
// matters most for this question.
func parseAge(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if rest, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(rest)
		if err != nil || n < 0 {
			return 0, false
		}
		return time.Duration(n) * 24 * time.Hour, true
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, false
	}
	return d, true
}

// runDriftTrend renders the comparison against a historical snapshot.
//
// The exit code deliberately differs from plain `gmb drift`: it tracks newly
// introduced violations, not the total. A team adopting this on an existing
// codebase cannot reach zero violations on day one, but they can commit to not
// adding any — and a gate they can actually pass is a gate they will keep.
func runDriftTrend(cmd *cobra.Command, storageDir, since string, head *drift.Report, cfg config.Config, asJSON bool) error {
	store, err := arch_timeline.NewSnapshotStore(filepath.Join(storageDir, "snapshots"))
	if err != nil {
		return fmt.Errorf("failed to open the snapshot store: %w", err)
	}

	baseSnap, err := resolveDriftBaseline(store, since)
	if err != nil {
		return producterrs.Tagged(fmt.Sprintf(
			"no stored snapshot matches --since %q: %v — 'gmb snapshot list' shows what is available",
			since, err), producterrs.ErrEntryNotFound)
	}

	// Comparing conformance requires re-running the layer rules over the old
	// graph, so a snapshot stored without one cannot answer the question. This
	// is the common case on large repositories rather than an exotic failure —
	// the snapshot size auto-threshold drops the graph at 15k nodes — so say
	// what to change instead of surfacing a bare replay error.
	baseGraph, rerr := arch_timeline.Replay(baseSnap)
	if rerr != nil {
		return producterrs.Tagged(fmt.Sprintf(
			"cannot compare against snapshot %s: %v", shortHash(baseSnap.CommitHash), rerr),
			producterrs.ErrEntryMissing)
	}

	// The baseline is evaluated against TODAY's rules, not the rules in force
	// when it was taken. Otherwise relaxing a rule would retroactively "fix"
	// violations and tightening one would invent a backlog, and neither is a
	// change in the architecture.
	baseRep := drift.Analyze(baseGraph, cfg.Drift)

	trend := drift.AnalyzeTrend(baseRep, head)
	trend.BaseCommit = baseSnap.CommitHash
	if !baseSnap.Timestamp.IsZero() {
		trend.BaseAt = baseSnap.Timestamp.UTC().Format(time.RFC3339)
	}

	if asJSON {
		out, _ := json.MarshalIndent(trend, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
	} else {
		renderDriftTrend(cmd, trend, baseSnap.CommitHash, since)
	}

	if trend.Worsened() {
		return producterrs.Tagged(fmt.Sprintf(
			"architecture drifted since %s: %d newly introduced violation(s), cycles %d → %d",
			shortHash(baseSnap.CommitHash), len(trend.Introduced),
			trend.BaseCycleCount, trend.HeadCycleCount), producterrs.ErrPolicyViolation)
	}
	return nil
}

func shortHash(h string) string {
	if h == "" {
		return "(unknown)"
	}
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

func renderDriftTrend(cmd *cobra.Command, t *drift.TrendReport, baseHash, since string) {
	w := cmd.OutOrStdout()
	width, _ := tui.OutputWidth()

	label := t.BaseAt
	if label == "" {
		label = since
	}

	tui.Fprintln(w, tui.Divider(fmt.Sprintf("Architecture drift since %s", shortHash(baseHash)), width))
	tui.Fprintln(w, tui.KV("Baseline", fmt.Sprintf("%s (%s)", shortHash(baseHash), label)))
	tui.Fprintln(w, tui.KV("Introduced", fmt.Sprintf("%d", len(t.Introduced))))
	tui.Fprintln(w, tui.KV("Resolved", fmt.Sprintf("%d", len(t.Resolved))))
	tui.Fprintln(w, tui.KV("Pre-existing", fmt.Sprintf("%d", len(t.Persisting))))
	tui.Fprintln(w, tui.KV("Cycles", fmt.Sprintf("%d → %d (budget %d)",
		t.BaseCycleCount, t.HeadCycleCount, t.CycleBudget)))
	tui.Fprintln(w, "")

	if len(t.Introduced) > 0 {
		tui.Fprintln(w, "New violations (introduced since the baseline):")
		for _, v := range t.Introduced {
			tui.Fprintf(w, "  + %s\n", v.Message)
		}
		tui.Fprintln(w, "")
	}
	if len(t.Resolved) > 0 {
		tui.Fprintln(w, "Resolved since the baseline:")
		for _, v := range t.Resolved {
			tui.Fprintf(w, "  - %s\n", v.Message)
		}
		tui.Fprintln(w, "")
	}

	switch {
	case t.Worsened():
		tui.Fprintln(w, "Verdict: the architecture moved away from its declared intent.")
	case t.Improved():
		tui.Fprintln(w, "Verdict: the architecture moved toward its declared intent.")
	case len(t.Persisting) > 0:
		tui.Fprintln(w, "Verdict: no drift — the outstanding violations pre-date the baseline.")
	default:
		tui.Fprintln(w, "Verdict: no drift, and no outstanding violations.")
	}
}
