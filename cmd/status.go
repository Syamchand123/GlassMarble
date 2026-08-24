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
	Initialized   bool      `json:"initialized"`
	StorageDir    string    `json:"storage_dir,omitempty"`
	SchemaVersion int       `json:"schema_version,omitempty"`
	GraphVersion  uint64    `json:"graph_version,omitempty"`
	CommitHash    string    `json:"commit_hash,omitempty"`
	LastAnalysis  string    `json:"last_analysis,omitempty"`
	NodeCount     int       `json:"nodes,omitempty"`
	EdgeCount     int       `json:"edges,omitempty"`
	IndexedFiles  int       `json:"indexed_files,omitempty"`
	Entrypoints   int       `json:"entrypoints,omitempty"`
	VirtualCount  int       `json:"virtual_nodes,omitempty"`
	VirtualShare  float64   `json:"virtual_share_pct,omitempty"`
	Dangling      int       `json:"dangling_references,omitempty"`
	JSONBytes     int64     `json:"json_bytes,omitempty"`
	Verified      bool      `json:"verified,omitempty"`
	Error         string    `json:"error,omitempty"`
	GeneratedAt   time.Time `json:"generated_at"`
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

		sd := views.StatusData{
			Initialized:   true,
			StorageDir:    storageDir,
			SchemaVersion: schemaVersion,
			GraphVersion:  version,
			CommitHash:    commitHash,
			LastAnalysis:  stateInfo.ModTime().Format(time.RFC3339),
			NodeCount:     stats.NodeCount,
			EdgeCount:     stats.Edges,
			IndexedFiles:  stats.IndexedFiles,
			Entrypoints:   stats.Entrypoints,
			VirtualCount:  stats.VirtualCount,
			VirtualShare:  virtualShare,
			Dangling:      stats.Dangling,
			JSONBytes:     stateSize,
			Verified:      stats.Dangling == 0,
		}

		if asJSON {
			sj := statusJSON{
				Initialized:   sd.Initialized,
				StorageDir:    sd.StorageDir,
				SchemaVersion: sd.SchemaVersion,
				GraphVersion:  sd.GraphVersion,
				CommitHash:    sd.CommitHash,
				LastAnalysis:  sd.LastAnalysis,
				NodeCount:     sd.NodeCount,
				EdgeCount:     sd.EdgeCount,
				IndexedFiles:  sd.IndexedFiles,
				Entrypoints:   sd.Entrypoints,
				VirtualCount:  sd.VirtualCount,
				VirtualShare:  sd.VirtualShare,
				Dangling:      sd.Dangling,
				JSONBytes:     sd.JSONBytes,
				Verified:      sd.Verified,
				GeneratedAt:   time.Now(),
			}
			out, _ := json.MarshalIndent(sj, "", "  ")
			fmt.Println(string(out))
			return nil
		}

		fmt.Println(views.RenderStatus(sd))
		return nil
	},
}

func init() {
	statusCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	rootCmd.AddCommand(statusCmd)
}
