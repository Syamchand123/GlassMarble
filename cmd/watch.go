package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

var watchInterval time.Duration

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Continuously watch repository for source changes and update AKG",
	Long: `Watches the repository for file-system events (fsnotify) and triggers
incremental delta analysis whenever source files change. The git working-tree
fingerprint is re-checked before every rebuild so branch switches, rebases and
changes made outside the watcher's scope are also picked up.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir, _ := cmd.Flags().GetString("dir")
		if targetDir == "" {
			targetDir = "."
		}
		commitHash, _ := cmd.Flags().GetString("commit")
		workers, _ := cmd.Flags().GetInt("workers")
		verbose, _ := cmd.Flags().GetBool("verbose")
		linkLevel, _ := cmd.Flags().GetString("link-level")
		macroInference, _ := cmd.Flags().GetString("macro-inference")
		maxNodes, _ := cmd.Flags().GetInt("max-nodes")
		abortOnLimit, _ := cmd.Flags().GetBool("abort-on-limit")

		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}
		if _, err := os.Stat(filepath.Join(absDir, ".git")); err != nil {
			return fmt.Errorf("watch requires a git repository (no .git found at %s)", absDir)
		}

		fmt.Printf("GlassMarble Watcher active on '%s' (fsnotify, debounce: %s)\nPress Ctrl+C to stop.\n\n", absDir, watchInterval)

		opts := runAnalysisOptions{
			targetDir:      absDir,
			commitHash:     commitHash,
			workers:        workers,
			verbose:        verbose,
			linkLevel:      linkLevel,
			macroInference: macroInference,
			maxNodes:       maxNodes,
			abortOnLimit:   abortOnLimit,
		}

		// Run an initial analysis so the AKG is current before watching.
		fmt.Printf("[%s] Initial analysis...\n", time.Now().Format("15:04:05"))
		if err := runAnalysis(opts); err != nil {
			fmt.Printf("[%s] Initial analysis failed: %v\n", time.Now().Format("15:04:05"), err)
		}

		lastFingerprint := workingTreeFingerprint(absDir, commitHash)

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
		var pending chan struct{}
		runNow := func() {
			if pending != nil {
				close(pending)
				pending = nil
			}
			pending = make(chan struct{})
			go func(done chan struct{}) {
				select {
				case <-time.After(debounce):
				case <-done:
					return
				}
				fp := workingTreeFingerprint(absDir, commitHash)
				if fp == lastFingerprint {
					return
				}
				lastFingerprint = fp
				fmt.Printf("[%s] Repository changes detected, running analysis...\n", time.Now().Format("15:04:05"))
				if err := runAnalysis(opts); err != nil {
					fmt.Printf("[%s] Analysis failed: %v\n", time.Now().Format("15:04:05"), err)
				}
			}(pending)
		}

		for {
			select {
			case <-cmd.Context().Done():
				fmt.Println("\nWatcher stopped.")
				return nil
			case ev, ok := <-watcher.Events:
				if !ok {
					return nil
				}
				if watchEventRelevant(watcher, ev) {
					runNow()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return nil
				}
				fmt.Printf("[%s] watcher error: %v\n", time.Now().Format("15:04:05"), err)
			}
		}
	},
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
	if strings.HasSuffix(p, ".ttl") || strings.HasSuffix(p, ".wal") || strings.HasSuffix(p, ".lock") {
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
func addWatchTree(w *fsnotify.Watcher, root string) error {
	if w == nil {
		return nil
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
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
	if out, err := stage1.GitCommandOutput(absDir, "rev-parse", "HEAD"); err == nil && out != "" {
		head = out
	}
	status := ""
	if out, err := stage1.GitCommandOutput(absDir, "status", "--porcelain"); err == nil {
		status = out
	}
	return head + "\x00" + status
}

func init() {
	watchCmd.Flags().String("dir", ".", "Target repository directory")
	watchCmd.Flags().String("commit", "HEAD", "Git commit hash to tag the analysis")
	watchCmd.Flags().Int("workers", 0, "Number of parallel workers (default: CPUs)")
	watchCmd.Flags().String("link-level", "architecture", "Linker detail level: architecture, standard, full")
	watchCmd.Flags().String("macro-inference", "all", "Macro inference mode: disabled, structural, all")
	watchCmd.Flags().Int("max-nodes", 0, "Max total CPG nodes before warning/abort (0 = unlimited)")
	watchCmd.Flags().Bool("abort-on-limit", false, "Abort analysis if --max-nodes is exceeded (otherwise warn)")
	watchCmd.Flags().Bool("verbose", false, "Enable verbose output")
	watchCmd.Flags().DurationVar(&watchInterval, "interval", 500*time.Millisecond, "Debounce interval for file-system events")
	rootCmd.AddCommand(watchCmd)
}
