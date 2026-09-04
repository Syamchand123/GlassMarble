package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/programs/housekeeping"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

type housekeepingAreaJSON struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Files int    `json:"files"`
}

type housekeepingJSON struct {
	Areas       []housekeepingAreaJSON `json:"areas"`
	TotalBytes  int64                  `json:"total_bytes"`
	TotalFiles  int                    `json:"total_files"`
	PrunedBytes int64                  `json:"pruned_bytes,omitempty"`
	PrunedFiles int                    `json:"pruned_files,omitempty"`
	Warning     string                 `json:"warning,omitempty"`
}

var housekeepingCmd = &cobra.Command{
	Use:     "housekeeping",
	GroupID: GroupUtility.ID,
	Short:   "Report and prune .glassmarble working-set storage (marbles, sessions, snapshots, memory)",
	Long: `Scans .glassmarble/ and reports the bytes held by each working-set area
(marbles/, ai/, snapshots/, memory/, intelligence/), then optionally prunes saved diagrams,
chat sessions, and old snapshots.

The AKG state file (akg.json) is never pruned by this command.

Snapshots: use --prune-snapshots --keep N to retain only the N most recent snapshots
(default N=30, matching intelligence.snapshot_max_count). This reclaims the
bulk of snapshot storage (previously 1 GB+ for 29 full-graph snapshots).`,
	Example: `  # Report storage sizes of .glassmarble directory
  gmb housekeeping

  # Prune marbles and AI sessions older than 30 days
  gmb housekeeping --prune

  # Prune snapshots to keep last 10 only
  gmb housekeeping --prune-snapshots --keep 10

  # Prune both marbles/sessions and snapshots
  gmb housekeeping --prune --prune-snapshots --keep 10

  # Output storage usage as JSON
  gmb housekeeping --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolveDir(cmd)
		prune, _ := cmd.Flags().GetBool("prune")
		pruneSnapshots, _ := cmd.Flags().GetBool("prune-snapshots")
		keep, _ := cmd.Flags().GetInt("keep")
		olderThan, _ := cmd.Flags().GetInt("older-than")
		asJSON, _ := cmd.Flags().GetBool("json")

		if olderThan <= 0 {
			olderThan = 30
		}
		if keep <= 0 {
			keep = 30
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
			{name: "state (akg.json)", path: filepath.Join(storageDir, "akg.json")},
			{name: "marbles/", path: filepath.Join(storageDir, "marbles")},
			{name: "ai/", path: filepath.Join(storageDir, "ai")},
			{name: "snapshots/", path: filepath.Join(storageDir, "snapshots")},
			{name: "memory/", path: filepath.Join(storageDir, "memory")},
			{name: "intelligence/", path: filepath.Join(storageDir, "intelligence")},
		}

		var totalBytes int64
		var totalFiles int
		var areaRows []views.HousekeepingArea
		var areaJSONRows []housekeepingAreaJSON
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
			areaRows = append(areaRows, views.HousekeepingArea{Name: a.name, Bytes: size, Files: count})
			areaJSONRows = append(areaJSONRows, housekeepingAreaJSON{Name: a.name, Bytes: size, Files: count})
		}

		warning := ""
		if totalBytes > 500<<20 {
			warning = fmt.Sprintf("Total .glassmarble is %s (>500MB). Run `gmb housekeeping --prune-snapshots --keep 10` to reclaim snapshot storage.", humanBytes(totalBytes))
		}

		// Report only
		if !prune && !pruneSnapshots {
			if asJSON {
				out, _ := json.MarshalIndent(housekeepingJSON{
					Areas:      areaJSONRows,
					TotalBytes: totalBytes,
					TotalFiles: totalFiles,
					Warning:    warning,
				}, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				if warning != "" {
					fmt.Fprintln(os.Stderr, "WARNING: "+warning)
				}
				return nil
			}
			tui.Fprintln(cmd.OutOrStdout(), views.RenderHousekeepingReport(areaRows, totalBytes, totalFiles))
			if warning != "" {
				tui.Fprintf(cmd.ErrOrStderr(), "\nWARNING: %s\n", warning)
			}
			tui.Fprintln(cmd.OutOrStdout(), "\nRun `gmb housekeeping --prune` to delete marbles/sessions older than the retention window.")
			tui.Fprintln(cmd.OutOrStdout(), "Run `gmb housekeeping --prune-snapshots --keep 10` to prune old snapshots.")
			return nil
		}

		// Interactive confirmation
		if !asJSON && tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout()) {
			var toPruneBytes int64
			var toPruneFiles int
			if prune {
				for _, a := range areas {
					if a.name != "marbles/" && a.name != "ai/" {
						continue
					}
					toPruneBytes += prunePreview(a.path, cutoff, &toPruneFiles)
				}
			}
			if pruneSnapshots {
				store, err := arch_timeline.NewSnapshotStore(filepath.Join(storageDir, "snapshots"))
				if err == nil {
					list := store.List()
					if len(list) > keep {
						toPruneFiles += len(list) - keep
						// estimate bytes: sum of oldest files
						for i := 0; i < len(list)-keep; i++ {
							if st, err := os.Stat(filepath.Join(storageDir, "snapshots", list[i].SnapshotFile)); err == nil {
								toPruneBytes += st.Size()
							}
							sidecar := filepath.Join(storageDir, "snapshots", list[i].SnapshotFile+".graph.json.gz")
							if st, err := os.Stat(sidecar); err == nil {
								toPruneBytes += st.Size()
							}
						}
					}
				}
			}
			if toPruneFiles > 0 {
				msg := fmt.Sprintf("Delete %d file(s) and reclaim %s?", toPruneFiles, humanBytes(toPruneBytes))
				if pruneSnapshots {
					msg = fmt.Sprintf("Prune snapshots to keep last %d? This removes %d file(s) and reclaims %s.", keep, toPruneFiles, humanBytes(toPruneBytes))
				}
				ok, err := housekeeping.ConfirmPrune(cmd.InOrStdin(), cmd.OutOrStdout(), msg)
				if err != nil {
					return err
				}
				if !ok {
					tui.Fprintln(cmd.ErrOrStderr(), "Prune cancelled.")
					return nil
				}
			}
		}

		prunedBytes := int64(0)
		prunedFiles := 0
		if prune {
			for _, a := range areas {
				if a.name != "marbles/" && a.name != "ai/" {
					continue
				}
				prunedBytes += pruneArea(a.path, cutoff, &prunedFiles)
			}
		}
		if pruneSnapshots {
			store, err := arch_timeline.NewSnapshotStore(filepath.Join(storageDir, "snapshots"))
			if err == nil {
				pf, pb := store.PruneKeepLast(keep)
				prunedFiles += pf
				prunedBytes += pb
			}
		}

		if asJSON {
			out, _ := json.MarshalIndent(housekeepingJSON{
				Areas:       areaJSONRows,
				TotalBytes:  totalBytes,
				TotalFiles:  totalFiles,
				PrunedBytes: prunedBytes,
				PrunedFiles: prunedFiles,
				Warning:     warning,
			}, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		}

		tui.Fprintln(cmd.OutOrStdout(), views.RenderHousekeepingReport(areaRows, totalBytes, totalFiles))
		if pruneSnapshots {
			tui.Fprintf(cmd.OutOrStdout(), "\nPruned %d snapshot file(s), %s reclaimed (keep: %d).\n", prunedFiles, humanBytes(prunedBytes), keep)
		} else {
			tui.Fprintf(cmd.OutOrStdout(), "\nPruned %d file(s), %s reclaimed (retention: %d days).\n", prunedFiles, humanBytes(prunedBytes), olderThan)
		}
		if warning != "" {
			tui.Fprintf(cmd.OutOrStdout(), "Remaining total: %s (post-prune estimate)\n", humanBytes(totalBytes-prunedBytes))
		}
		return nil
	},
}

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

func prunePreview(dir string, cutoff time.Time, staleFiles *int) int64 {
	var reclaimed int64
	filepath.Walk(dir, func(_ string, fi os.FileInfo, err error) error {
		if err != nil || fi == nil || !fi.Mode().IsRegular() {
			return nil
		}
		if fi.ModTime().Before(cutoff) {
			reclaimed += fi.Size()
			*staleFiles++
		}
		return nil
	})
	return reclaimed
}

func init() {
	housekeepingCmd.Flags().Bool("prune", false, "Delete marbles/sessions older than the retention window")
	housekeepingCmd.Flags().Int("older-than", 30, "Retention window in days for pruned working-set files")
	housekeepingCmd.Flags().Bool("prune-snapshots", false, "Prune snapshots to keep only last N (see --keep)")
	housekeepingCmd.Flags().Int("keep", 30, "Number of most recent snapshots to keep when pruning (default 30)")
	housekeepingCmd.Flags().Bool("json", false, "Emit machine-readable JSON storage report")
	rootCmd.AddCommand(housekeepingCmd)
}
