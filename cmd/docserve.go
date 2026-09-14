package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	doc_engine "github.com/Syamchand123/GlassMarble/internal/doc_engine"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/daemon"
	"github.com/spf13/cobra"
)

// DocServeCmd is the `gmb docserve` daemon command, exported so the owner can
// wire it with a one-line hook if self-registration is ever disabled:
//
//	rootCmd.AddCommand(cmd.DocServeCmd)
//
// By default the init() below self-registers it (same pattern as watch.go),
// so no root.go edit is required.
var DocServeCmd = &cobra.Command{
	Use:     "docserve",
	GroupID: GroupAI.ID,
	Short:   "Run the doc engine as a daemon, updating docs on every file change",
	Long: `Loads the documentation engine once and watches the repository for
file-system changes (fsnotify). Changes are coalesced through a debounce
window and each batch triggers one doc-engine update run.

Failures inside a batch update are logged as warnings and never abort the
daemon: the next batch retries on fresh state.`,
	Example: `  # Serve docs for the current repository
  gmb docserve

  # Watch a specific directory with a shorter debounce window
  gmb docserve --dir ./backend --debounce-ms 500 --workers 4`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		debounceMs, _ := cmd.Flags().GetInt("debounce-ms")
		workers, _ := cmd.Flags().GetInt("workers")

		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("docserve: %w", err)
		}

		out := cmd.ErrOrStderr()
		fmt.Fprintf(out, "docserve: watching %s (debounce %dms, workers %d)\n", absDir, debounceMs, workers)
		fmt.Fprintln(out, "Press Ctrl+C to stop.")

		return daemon.Run(cmd.Context(), daemon.Config{
			RepoRoot:   absDir,
			DebounceMs: debounceMs,
			Workers:    workers,
		}, func(ctx context.Context, changed []string) {
			// Non-fatal by contract: log warnings, never return an error.
			commit := ""
			if raw, err := exec.CommandContext(ctx, "git", "-C", absDir, "rev-parse", "HEAD").Output(); err == nil {
				commit = strings.TrimSpace(string(raw))
			} else {
				fmt.Fprintf(out, "docserve: warning: git rev-parse HEAD failed (%v); continuing with empty commit\n", err)
			}
			res := doc_engine.Run(absDir, doc_engine.RunOptions{
				CommitHash: commit,
				Force:      false,
				Out:        out,
				// ctx here is daemon.Run's runCtx, which is derived from
				// cmd.Context() below — cancelling that (e.g. SIGINT/SIGTERM
				// during `gmb docserve`) now reaches an in-flight run's LLM
				// calls promptly instead of blocking shutdown until the run
				// finishes on its own.
				Ctx: ctx,
			})
			if res.Err != nil {
				fmt.Fprintf(out, "docserve: warning: engine run failed: %v\n", res.Err)
			}
			for _, w := range res.Warnings {
				fmt.Fprintf(out, "docserve: warning: %s\n", w)
			}
			fmt.Fprintf(out, "docserve: batch of %d changed file(s): %d doc(s) updated, %d section(s) updated\n",
				len(changed), res.DocsUpdated, res.SectionsUpdated)
		})
	},
}

func init() {
	DocServeCmd.Flags().String("dir", ".", "Target repository directory")
	DocServeCmd.Flags().Int("debounce-ms", 2000, "Debounce window in milliseconds for coalescing file events")
	DocServeCmd.Flags().Int("workers", 2, "Number of batch-consumer workers (onChange never runs concurrently)")
	rootCmd.AddCommand(DocServeCmd)
}
