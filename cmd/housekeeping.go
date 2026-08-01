package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

// housekeepingCmd reports and prunes the .glassmarble working set
// (AUDIT Issue 4 Phase 4B-8): saved marbles, AI chat sessions, and WAL
// segments. The AKG state file itself is never touched here — it is guarded
// by the post-write verification and the --max-ttl-mb budget.
var housekeepingCmd = &cobra.Command{
	Use:   "housekeeping",
	Short: "Report and prune .glassmarble working-set storage (marbles, sessions, WAL)",
	Long: `Scans .glassmarble/ and reports the bytes held by each working-set area
(wal/, marbles/, ai/), then optionally prunes saved diagrams and chat
sessions older than --older-than days. The AKG state file (akg_state.ttl)
is never pruned by this command.

  gmb housekeeping                 # report sizes only
  gmb housekeeping --prune         # delete marbles/sessions older than 30 days
  gmb housekeeping --prune --older-than 7   # 7-day retention

WAL segments are truncated automatically after every successful load; the
--prune run also truncates the WAL if a healthy state file exists.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		if dir == "" {
			dir = "."
		}
		prune, _ := cmd.Flags().GetBool("prune")
		olderThan, _ := cmd.Flags().GetInt("older-than")
		if olderThan <= 0 {
			olderThan = 30
		}

		storageDir := filepath.Join(dir, ".glassmarble")
		cutoff := time.Now().Add(-time.Duration(olderThan) * 24 * time.Hour)

		type area struct {
			name  string
			path  string
			bytes int64
			files int
		}
		areas := []*area{
			{name: "state (akg_state.ttl)", path: filepath.Join(storageDir, "akg_state.ttl")},
			{name: "wal/", path: filepath.Join(storageDir, "wal")},
			{name: "marbles/", path: filepath.Join(storageDir, "marbles")},
			{name: "ai/", path: filepath.Join(storageDir, "ai")},
		}

		var totalBytes int64
		var totalFiles int
		fmt.Println("=== .glassmarble Working Set ===")
		for _, a := range areas {
			info, err := os.Stat(a.path)
			if err != nil {
				continue
			}
			var size int64
			var count int
			if info.IsDir() {
				filepath.Walk(a.path, func(_ string, fi os.FileInfo, _ error) error {
					if fi != nil && fi.Mode().IsRegular() {
						size += fi.Size()
						count++
					}
					return nil
				})
			} else {
				size = info.Size()
				count = 1
			}
			a.bytes = size
			a.files = count
			totalBytes += size
			totalFiles += count
			fmt.Printf("  %-24s %8s  %d file(s)\n", a.name, humanBytes(size), count)
		}
		fmt.Printf("  %-24s %8s  %d file(s)\n", "total", humanBytes(totalBytes), totalFiles)

		if !prune {
			fmt.Println("\nRun `gmb housekeeping --prune` to delete marbles/sessions older than the retention window.")
			return nil
		}

		// Prune only derived working-set areas, never the AKG state.
		prunedBytes := int64(0)
		prunedFiles := 0
		for _, a := range areas {
			if a.name != "marbles/" && a.name != "ai/" {
				continue
			}
			prunedBytes += pruneArea(a.path, cutoff, &prunedFiles)
		}

		// WAL truncation: safe after a successful load of a healthy state file.
		if ttlStat, err := os.Stat(filepath.Join(storageDir, "akg_state.ttl")); err == nil && ttlStat.Size() > 0 {
			if walDirStat, err := os.Stat(filepath.Join(storageDir, "wal")); err == nil && walDirStat.IsDir() {
				if old, err := walDirSize(filepath.Join(storageDir, "wal")); err == nil && old > 0 {
					if tm, err := newAKGManager(storageDir, cmd); err == nil {
						tm.Close() // load + recovery truncate the WAL on exit path
						if after, _ := walDirSize(filepath.Join(storageDir, "wal")); after < old {
							prunedBytes += old - after
							prunedFiles++
						}
					}
				}
			}
		}

		fmt.Printf("\nPruned %d file(s), %s reclaimed (retention: %d days).\n", prunedFiles, humanBytes(prunedBytes), olderThan)
		return nil
	},
}

// pruneArea deletes regular files under dir whose mtime predates cutoff.
func pruneArea(dir string, cutoff time.Time, prunedFiles *int) int64 {
	var reclaimed int64
	filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi == nil || !fi.Mode().IsRegular() {
			return nil
		}
		if fi.ModTime().Before(cutoff) {
			if os.Remove(path) == nil {
				reclaimed += fi.Size()
				*prunedFiles++
			}
		}
		return nil
	})
	return reclaimed
}

// walDirSize sums the WAL segment chain bytes.
func walDirSize(walDir string) (int64, error) {
	var size int64
	entries, err := os.ReadDir(walDir)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil {
			size += info.Size()
		}
	}
	return size, nil
}

func init() {
	housekeepingCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ folder")
	housekeepingCmd.Flags().Bool("prune", false, "Delete marbles/sessions older than the retention window and truncate the WAL")
	housekeepingCmd.Flags().Int("older-than", 30, "Retention window in days for pruned working-set files")
	rootCmd.AddCommand(housekeepingCmd)
}
