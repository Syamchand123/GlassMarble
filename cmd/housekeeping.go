package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
}

var housekeepingCmd = &cobra.Command{
	Use:     "housekeeping",
	GroupID: GroupUtility.ID,
	Short:   "Report and prune .glassmarble working-set storage (marbles, sessions)",
	Long: `Scans .glassmarble/ and reports the bytes held by each working-set area
(marbles/, ai/), then optionally prunes saved diagrams and chat
sessions older than --older-than days. The AKG state file (akg.json)
is never pruned by this command.`,
	Example: `  # Report storage sizes of .glassmarble directory
  gmb housekeeping

  # Prune marbles and AI sessions older than 30 days
  gmb housekeeping --prune

  # Prune with custom retention threshold
  gmb housekeeping --prune --older-than 7

  # Output storage usage as JSON
  gmb housekeeping --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolveDir(cmd)
		prune, _ := cmd.Flags().GetBool("prune")
		olderThan, _ := cmd.Flags().GetInt("older-than")
		asJSON, _ := cmd.Flags().GetBool("json")

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
			{name: "state (akg.json)", path: filepath.Join(storageDir, "akg.json")},
			{name: "marbles/", path: filepath.Join(storageDir, "marbles")},
			{name: "ai/", path: filepath.Join(storageDir, "ai")},
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

		if !prune {
			if asJSON {
				out, _ := json.MarshalIndent(housekeepingJSON{
					Areas:      areaJSONRows,
					TotalBytes: totalBytes,
					TotalFiles: totalFiles,
				}, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			fmt.Println(views.RenderHousekeepingReport(areaRows, totalBytes, totalFiles))
			fmt.Println("\nRun `gmb housekeeping --prune` to delete marbles/sessions older than the retention window.")
			return nil
		}

		if !asJSON && tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout()) {
			var toPruneBytes int64
			var toPruneFiles int
			for _, a := range areas {
				if a.name != "marbles/" && a.name != "ai/" {
					continue
				}
				toPruneBytes += prunePreview(a.path, cutoff, &toPruneFiles)
			}
			if toPruneFiles > 0 {
				ok, err := housekeeping.ConfirmPrune(cmd.InOrStdin(), cmd.OutOrStdout(),
					fmt.Sprintf("Delete marbles/sessions older than %d days? This removes %d file(s) and reclaims %s.", olderThan, toPruneFiles, humanBytes(toPruneBytes)))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("Prune cancelled.")
					return nil
				}
			}
		}

		prunedBytes := int64(0)
		prunedFiles := 0
		for _, a := range areas {
			if a.name != "marbles/" && a.name != "ai/" {
				continue
			}
			prunedBytes += pruneArea(a.path, cutoff, &prunedFiles)
		}

		if asJSON {
			out, _ := json.MarshalIndent(housekeepingJSON{
				Areas:       areaJSONRows,
				TotalBytes:  totalBytes,
				TotalFiles:  totalFiles,
				PrunedBytes: prunedBytes,
				PrunedFiles: prunedFiles,
			}, "", "  ")
			fmt.Println(string(out))
			return nil
		}

		fmt.Println(views.RenderHousekeepingReport(areaRows, totalBytes, totalFiles))
		fmt.Printf("\nPruned %d file(s), %s reclaimed (retention: %d days).\n", prunedFiles, humanBytes(prunedBytes), olderThan)
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
	housekeepingCmd.Flags().Bool("json", false, "Emit machine-readable JSON storage report")
	rootCmd.AddCommand(housekeepingCmd)
}
