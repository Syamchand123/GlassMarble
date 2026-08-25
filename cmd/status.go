package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

// statusJSON is the machine-readable representation of `gmb status --json`.
type statusJSON struct {
	Initialized    bool      `json:"initialized"`
	StorageDir     string    `json:"storage_dir,omitempty"`
	SchemaVersion  int       `json:"schema_version,omitempty"`
	GraphVersion   uint64    `json:"graph_version,omitempty"`
	CommitHash     string    `json:"commit_hash,omitempty"`
	LastAnalysis   string    `json:"last_analysis,omitempty"`
	NodeCount      int       `json:"nodes,omitempty"`
	EdgeCount      int       `json:"edges,omitempty"`
	IndexedFiles   int       `json:"indexed_files,omitempty"`
	Entrypoints    int       `json:"entrypoints,omitempty"`
	VirtualCount   int       `json:"virtual_nodes,omitempty"`
	VirtualShare   float64   `json:"virtual_share_pct,omitempty"`
	Dangling       int       `json:"dangling_references,omitempty"`
	JSONBytes      int64     `json:"json_bytes,omitempty"`
	SnapshotCount  int       `json:"snapshot_count,omitempty"`
	SnapshotBytes  int64     `json:"snapshot_bytes,omitempty"`
	MemoryBytes    int64     `json:"memory_bytes,omitempty"`
	TotalBytes     int64     `json:"total_bytes,omitempty"`
	Verified       bool      `json:"verified,omitempty"`
	StorageWarning string    `json:"storage_warning,omitempty"`
	Error          string    `json:"error,omitempty"`
	GeneratedAt    time.Time `json:"generated_at"`
}

var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"st"},
	GroupID: GroupInspect.ID,
	Short:   "Display AKG database status, node statistics, and graph health",
	Long: `Inspects the active .glassmarble database and prints graph metrics,
schema version, commit hash, analysis freshness, and overall graph health.`,
	Example: `  # View current graph status
  gmb status

  # Output status as JSON for scripting
  gmb status --json

  # Inspect a specific repository directory
  gmb status --dir ./backend`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolveDir(cmd)
		asJSON, _ := cmd.Flags().GetBool("json")

		storageDir := filepath.Join(dir, ".glassmarble")
		jsonPath := filepath.Join(storageDir, "akg.json")

		if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
			if asJSON {
				out, _ := json.MarshalIndent(statusJSON{Initialized: false, Error: "no active AKG database", GeneratedAt: time.Now()}, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			fmt.Println(views.RenderStatusUninitialized(jsonPath))
			return nil
		}

		commitHash, schemaVersion, version, err := akg.StateMetadata(storageDir)
		if err != nil {
			return fmt.Errorf("failed to read AKG metadata: %w — try 'gmb analyze'", err)
		}

		stateInfo, err := os.Stat(jsonPath)
		if err != nil {
			return fmt.Errorf("failed to stat AKG state: %w — try 'gmb doctor'", err)
		}

		// Lazy read: status streams akg.json once (bounded memory)
		stats, err := akg.StreamGraphStats(storageDir)
		if err != nil {
			return fmt.Errorf("failed to scan AKG: %w — try 'gmb doctor'", err)
		}

		virtualShare := 0.0
		if stats.NodeCount > 0 {
			virtualShare = 100 * float64(stats.VirtualCount) / float64(stats.NodeCount)
		}

		stateSize := akgStateSize(storageDir)
		snapCount, snapBytes := snapshotUsage(storageDir)
		memBytes := memoryUsage(storageDir)
		totalBytes := stateSize + snapBytes + memBytes
		// include intelligence + marbles quickly
		if st, err := os.Stat(filepath.Join(storageDir, "intelligence", "latest.json")); err == nil {
			totalBytes += st.Size()
		}
		if err := filepath.Walk(filepath.Join(storageDir, "marbles"), func(_ string, fi os.FileInfo, err error) error {
			if err == nil && fi != nil && fi.Mode().IsRegular() {
				totalBytes += fi.Size()
			}
			return nil
		}); err != nil {
		}
		warning := ""
		if totalBytes > 500<<20 {
			warning = fmt.Sprintf("total %s exceeds 500MB — run `gmb housekeeping --prune-snapshots --keep 10`", humanBytes(totalBytes))
		} else if snapBytes > 100<<20 {
			warning = fmt.Sprintf("snapshots %s exceeds 100MB — snapshots auto-capped at 30; run prune if needed", humanBytes(snapBytes))
		}

		sd := views.StatusData{
			Initialized:    true,
			StorageDir:     storageDir,
			SchemaVersion:  schemaVersion,
			GraphVersion:   version,
			CommitHash:     commitHash,
			LastAnalysis:   stateInfo.ModTime().Format(time.RFC3339),
			NodeCount:      stats.NodeCount,
			EdgeCount:      stats.Edges,
			IndexedFiles:   stats.IndexedFiles,
			Entrypoints:    stats.Entrypoints,
			VirtualCount:   stats.VirtualCount,
			VirtualShare:   virtualShare,
			Dangling:       stats.Dangling,
			JSONBytes:      stateSize,
			Verified:       stats.Dangling == 0,
			SnapshotCount:  snapCount,
			SnapshotBytes:  snapBytes,
			MemoryBytes:    memBytes,
			TotalBytes:     totalBytes,
			StorageWarning: warning,
		}

		if asJSON {
			sj := statusJSON{
				Initialized:    sd.Initialized,
				StorageDir:     sd.StorageDir,
				SchemaVersion:  sd.SchemaVersion,
				GraphVersion:   sd.GraphVersion,
				CommitHash:     sd.CommitHash,
				LastAnalysis:   sd.LastAnalysis,
				NodeCount:      sd.NodeCount,
				EdgeCount:      sd.EdgeCount,
				IndexedFiles:   sd.IndexedFiles,
				Entrypoints:    sd.Entrypoints,
				VirtualCount:   sd.VirtualCount,
				VirtualShare:   sd.VirtualShare,
				Dangling:       sd.Dangling,
				JSONBytes:      sd.JSONBytes,
				SnapshotCount:  sd.SnapshotCount,
				SnapshotBytes:  sd.SnapshotBytes,
				MemoryBytes:    sd.MemoryBytes,
				TotalBytes:     sd.TotalBytes,
				Verified:       sd.Verified,
				StorageWarning: warning,
				GeneratedAt:    time.Now(),
			}
			out, _ := json.MarshalIndent(sj, "", "  ")
			fmt.Println(string(out))
			return nil
		}

		fmt.Println(views.RenderStatus(sd))
		return nil
	},
}

func snapshotUsage(storageDir string) (count int, bytes int64) {
	snapDir := filepath.Join(storageDir, "snapshots")
	entries, err := os.ReadDir(snapDir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if st, err := e.Info(); err == nil {
			bytes += st.Size()
			if !e.IsDir() && filepath.Ext(e.Name()) == ".json" && e.Name() != "index.json" {
				count++
			}
		}
	}
	// Count snapshots via index if available for accuracy
	if idxData, err := os.ReadFile(filepath.Join(snapDir, "index.json")); err == nil {
		var idx []struct{ SnapshotFile string `json:"snapshot_file"` }
		if json.Unmarshal(idxData, &idx) == nil {
			count = len(idx)
		}
	}
	return
}

func memoryUsage(storageDir string) int64 {
	var total int64
	memDir := filepath.Join(storageDir, "memory")
	filepath.Walk(memDir, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && fi != nil && fi.Mode().IsRegular() {
			total += fi.Size()
		}
		return nil
	})
	return total
}

func init() {
	statusCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	rootCmd.AddCommand(statusCmd)
}
