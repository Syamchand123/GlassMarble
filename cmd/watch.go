package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/programs/watch"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

var watchInterval time.Duration

var watchCmd = &cobra.Command{
	Use:     "watch",
	GroupID: GroupAnalyze.ID,
	Short:   "Continuously watch repository for source changes and update AKG",
	Long: `Watches the repository for file-system events (fsnotify) and triggers
incremental delta analysis whenever source files change. The git working-tree
fingerprint is re-checked before every rebuild so branch switches, rebases and
changes made outside the watcher's scope are also picked up.`,
	Example: `  # Start file watcher on current repository
  gmb watch

  # Watch with a custom debounce interval
  gmb watch --interval 1s

  # Watch specific repository path
  gmb watch --dir ./backend

  # Stream watcher events as newline-delimited JSON
  gmb watch --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		commitHash, _ := cmd.Flags().GetString("commit")
		workers, _ := cmd.Flags().GetInt("workers")
		verbose, _ := cmd.Flags().GetBool("verbose")
		linkLevel, _ := cmd.Flags().GetString("link-level")
		macroInference, _ := cmd.Flags().GetString("macro-inference")
		maxNodes, _ := cmd.Flags().GetInt("max-nodes")
		abortOnLimit, _ := cmd.Flags().GetBool("abort-on-limit")
		asJSON, _ := cmd.Flags().GetBool("json")

		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return producterrs.Annotate(fmt.Errorf("failed to resolve path: %w", err), producterrs.ErrValidation)
		}
		if _, err := os.Stat(filepath.Join(absDir, ".git")); err != nil {
			return producterrs.Tagged(fmt.Sprintf("watch requires a git repository (no .git found at %s)", absDir), producterrs.ErrValidation)
		}

		opts := runAnalysisOptions{
			targetDir:      absDir,
			commitHash:     commitHash,
			workers:        workers,
			verbose:        verbose,
			linkLevel:      linkLevel,
			macroInference: macroInference,
			maxNodes:       maxNodes,
			abortOnLimit:   abortOnLimit,
			out:            cmd.OutOrStdout(),
		}

		// --json is machine-readable and must bypass the interactive layer,
		// and the pipeline's own human report must not reach stdout.
		if asJSON {
			opts.out = io.Discard
			return runWatchPlain(cmd, opts, watchReporter{out: cmd.OutOrStdout(), json: true})
		}

		if tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return runWatchTUI(cmd, opts)
		}

		return runWatchPlain(cmd, opts, watchReporter{out: cmd.OutOrStdout()})
	},
}

// watchReporter renders the watcher's lifecycle events.
//
// `watch` never terminates on its own, so there is no single result document
// to emit: the machine-readable shape is a stream of newline-delimited JSON
// objects (one per line, one per event), which stays parseable incrementally
// for as long as the watcher runs. The human reporter keeps the original
// timestamped lines verbatim.
type watchReporter struct {
	out  io.Writer
	json bool
}

// event writes one NDJSON object. Fields are merged into a fresh map so the
// event name and timestamp are always present and always first-class.
func (r watchReporter) event(name string, fields map[string]any) {
	doc := map[string]any{"event": name, "time": time.Now().Format(time.RFC3339)}
	for k, v := range fields {
		doc[k] = v
	}
	// Marshal, not Encode-with-indent: NDJSON requires exactly one line.
	out, err := json.Marshal(doc)
	if err != nil {
		return
	}
	fmt.Fprintln(r.out, string(out))
}

func (r watchReporter) started(dir string, debounce time.Duration) {
	if r.json {
		r.event("watch_started", map[string]any{"target_dir": dir, "debounce_ms": debounce.Milliseconds()})
		return
	}
	fmt.Fprintf(r.out, "GlassMarble Watcher active on '%s' (fsnotify, debounce: %s)\nPress Ctrl+C to stop.\n\n", dir, debounce)
}

func (r watchReporter) analysisStarted(reason string) {
	if r.json {
		r.event("analysis_started", map[string]any{"reason": reason})
		return
	}
	if reason == "initial" {
		fmt.Fprintf(r.out, "[%s] Initial analysis...\n", time.Now().Format("15:04:05"))
		return
	}
	fmt.Fprintf(r.out, "[%s] Repository changes detected, running analysis...\n", time.Now().Format("15:04:05"))
}

// analysisFinished reports the outcome of one rebuild. sum is nil when the run
// failed; err is nil when it succeeded.
func (r watchReporter) analysisFinished(reason string, sum *analysisSummary, err error) {
	if r.json {
		if err != nil {
			r.event("analysis_failed", map[string]any{"reason": reason, "error": err.Error()})
			return
		}
		fields := map[string]any{"reason": reason}
		if sum != nil {
			fields["target_dir"] = sum.targetDir
			fields["files_analyzed"] = sum.filesAnalyzed
			fields["nodes"] = sum.nodes
			fields["edges"] = sum.edges
			fields["virtual_nodes"] = sum.virtualNodes
			fields["dangling_edges"] = sum.danglingEdges
			fields["state_bytes"] = sum.stateBytes
			fields["duration_ms"] = sum.duration.Milliseconds()
			fields["storage_dir"] = sum.storageDir
		}
		r.event("analysis_completed", fields)
		return
	}
	if err != nil {
		label := "Analysis failed"
		if reason == "initial" {
			label = "Initial analysis failed"
		}
		fmt.Fprintf(r.out, "[%s] %s: %v\n", time.Now().Format("15:04:05"), label, err)
	}
	// The success line is the pipeline's own report, already written by
	// runAnalysis to opts.out.
}

func (r watchReporter) watcherError(err error) {
	if r.json {
		r.event("watcher_error", map[string]any{"error": err.Error()})
		return
	}
	fmt.Fprintf(r.out, "[%s] watcher error: %v\n", time.Now().Format("15:04:05"), err)
}

func (r watchReporter) stopped() {
	if r.json {
		r.event("watch_stopped", nil)
		return
	}
	fmt.Fprintln(r.out, "\nWatcher stopped.")
}

// runWatchTUI launches the interactive watcher, handing the fsnotify helpers
// and the analysis pipeline into the program as callbacks (the program must
// not import cmd).
func runWatchTUI(c *cobra.Command, opts runAnalysisOptions) error {
	runFn := func(progress func(step int, name string, current, total int)) error {
		opts.progress = progress
		return runAnalysis(c, opts)
	}
	register := func(w *fsnotify.Watcher) error { return watchTree(w, opts.targetDir) }
	relevant := func(w *fsnotify.Watcher, ev fsnotify.Event) bool { return watchEventRelevant(w, ev) }
	fingerprint := func() string { return workingTreeFingerprint(opts.targetDir, opts.commitHash) }
	return watch.RunWatch(watch.Options{
		TargetDir:  opts.targetDir,
		CommitHash: opts.commitHash,
		Interval:   watchInterval,
	}, register, relevant, fingerprint, runFn, c.InOrStdin(), c.OutOrStdout())
}

// runWatchPlain runs the non-interactive ticker loop, rendering its lifecycle
// through the supplied reporter (plain lines or NDJSON).
// C6-4: pending / lastFingerprint are guarded by sync.Mutex and a single
// serialized worker channel prevents concurrent analyses and data races.
func runWatchPlain(cmd *cobra.Command, opts runAnalysisOptions, rep watchReporter) error {
	absDir := opts.targetDir

	// Capture the pipeline's QA numbers so the JSON reporter can put them in
	// the completion event instead of parsing the human report line.
	var lastSummary analysisSummary
	var haveSummary bool
	opts.onSummary = func(s analysisSummary) {
		lastSummary = s
		haveSummary = true
	}
	runOnce := func(reason string) {
		haveSummary = false
		rep.analysisStarted(reason)
		err := runAnalysis(cmd, opts)
		var sum *analysisSummary
		if haveSummary {
			sum = &lastSummary
		}
		rep.analysisFinished(reason, sum, err)
	}

	debounceForReport := watchInterval
	if debounceForReport <= 0 {
		debounceForReport = 500 * time.Millisecond
	}
	rep.started(absDir, debounceForReport)

	// Run an initial analysis so the AKG is current before watching.
	runOnce("initial")

	var mu sync.Mutex
	lastFingerprint := workingTreeFingerprint(absDir, opts.commitHash)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to start filesystem watcher: %w", err)
	}
	defer watcher.Close()

	// Watch the whole tree (recursively), pruning standard tool directories.
	if err := watchTree(watcher, absDir); err != nil {
		return fmt.Errorf("failed to register watches: %w", err)
	}

	// Debounce: coalesce bursts of filesystem events into a single rebuild.
	debounce := watchInterval
	if debounce <= 0 {
		debounce = 500 * time.Millisecond
	}

	// Single-worker channel: only one analysis runs at a time; bursts are
	// coalesced by the buffered channel (size 1) with non-blocking sends.
	workCh := make(chan struct{}, 1)
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		for range workCh {
			// Debounce window — coalesces rapid bursts into one check.
			select {
			case <-time.After(debounce):
			case <-cmd.Context().Done():
				return
			}
			mu.Lock()
			fp := workingTreeFingerprint(absDir, opts.commitHash)
			if fp == lastFingerprint {
				mu.Unlock()
				continue
			}
			lastFingerprint = fp
			mu.Unlock()
			runOnce("change")
			// Drain any additional coalesced signal that arrived while we were
			// busy, so we immediately loop for another debounced check.
			select {
			case <-workCh:
				// Re-queue one signal so the for loop iterates again.
				select {
				case workCh <- struct{}{}:
				default:
				}
			default:
			}
		}
	}()
	defer func() {
		close(workCh)
		<-doneCh
	}()

	enqueue := func() {
		select {
		case workCh <- struct{}{}:
		default:
			// Already pending — coalesce.
		}
	}

	for {
		select {
		case <-cmd.Context().Done():
			rep.stopped()
			return nil
		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if watchEventRelevant(watcher, ev) {
				enqueue()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			rep.watcherError(err)
		}
	}
}

// watchEventRelevant filters filesystem events to source-like paths and
// ignores bookkeeping directories the watcher itself writes.
func watchEventRelevant(w *fsnotify.Watcher, ev fsnotify.Event) bool {
	// New subdirectories must be registered so nested changes are observed.
	if ev.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
		if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
			// Best-effort; a failed watch registration degrades gracefully to
			// the git-fingerprint check.
			_ = addWatchTree(w, ev.Name)
		}
	}
	p := filepath.ToSlash(ev.Name)
	if hasIgnoredSegment(p) {
		return false
	}
	if strings.HasSuffix(p, ".wal") || strings.HasSuffix(p, ".lock") {
		return false
	}
	return true
}

// hasIgnoredSegment reports whether a slash-normalized path crosses one of the
// tooling/bookkeeping directories the watcher must ignore. Both absolute
// (C:/repo/.git/x) and relative (node_modules/pkg/f.js) paths are handled by
// comparing each path segment.
func hasIgnoredSegment(p string) bool {
	ignored := map[string]bool{
		".git":         true,
		".glassmarble": true,
		"node_modules": true,
		"vendor":       true,
		"dist":         true,
		"build":        true,
		"target":       true,
	}
	for _, seg := range strings.Split(p, "/") {
		if ignored[seg] {
			return true
		}
	}
	return false
}

// watchTree registers recursive watches starting at root, skipping standard
// tooling/ignored directories to keep the watch set small.
func watchTree(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable paths
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "dist" || name == "build" || name == "target" || name == "bin" || name == "obj" || name == "out" || name == "coverage") {
				return filepath.SkipDir
			}
			return w.Add(path)
		}
		return nil
	})
}

// addWatchTree registers watches for a newly-created directory subtree.
// C6-14: filters ignored directories (node_modules/.git/etc) the same way
// watchTree does so ignored trees do not accumulate handles.
func addWatchTree(w *fsnotify.Watcher, root string) error {
	if w == nil {
		return nil
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			// Mirror watchTree ignore list so handles don't accumulate for
			// churny ignored dirs (C6-14).
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "dist" || name == "build" || name == "target" || name == "bin" || name == "obj" || name == "out" || name == "coverage") {
				return filepath.SkipDir
			}
			// Also respect hasIgnoredSegment for absolute paths that cross
			// ignored segments deeper than one level.
			if hasIgnoredSegment(filepath.ToSlash(path)) && path != root {
				return filepath.SkipDir
			}
			return w.Add(path)
		}
		return nil
	})
}

// workingTreeFingerprint summarizes the git working-tree state so the watcher
// can cheaply detect changes without running a full analysis on every event.
// It joins the current HEAD commit plus the set of modified/added/deleted
// files reported by `git status --porcelain`.
func workingTreeFingerprint(absDir, commitHash string) string {
	head := commitHash
	if out, err := ingest.GitCommandOutput(absDir, "rev-parse", "HEAD"); err == nil && out != "" {
		head = out
	}
	status := ""
	if out, err := ingest.GitCommandOutput(absDir, "status", "--porcelain"); err == nil {
		status = out
	}
	return head + "\x00" + status
}

func init() {
	watchCmd.Flags().String("dir", ".", "Target repository directory")
	watchCmd.Flags().String("commit", "", "Git commit hash to tag the analysis. Empty (default) diffs the working tree against HEAD (incremental delta)")
	watchCmd.Flags().Int("workers", 0, "Number of parallel workers (default: CPUs)")
	watchCmd.Flags().String("link-level", "architecture", "Linker detail level: architecture, standard, full")
	watchCmd.Flags().String("macro-inference", "all", "Macro inference mode: disabled, structural, all")
	watchCmd.Flags().Int("max-nodes", 0, "Max total CPG nodes before warning/abort (0 = unlimited)")
	watchCmd.Flags().Bool("abort-on-limit", false, "Abort analysis if --max-nodes is exceeded (otherwise warn)")
	watchCmd.Flags().Bool("verbose", false, "Enable verbose output")
	watchCmd.Flags().DurationVar(&watchInterval, "interval", 500*time.Millisecond, "Debounce interval for file-system events")
	watchCmd.Flags().Bool("json", false, "Stream machine-readable newline-delimited JSON events")
	rootCmd.AddCommand(watchCmd)
}
