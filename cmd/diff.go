package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

type diffJSON struct {
	Initialized   bool   `json:"initialized"`
	StorageDir    string `json:"storage_dir,omitempty"`
	SchemaVersion int    `json:"schema_version,omitempty"`
	GraphVersion  uint64 `json:"graph_version,omitempty"`
	CommitHash    string `json:"commit_hash,omitempty"`
	Error         string `json:"error,omitempty"`
}

var diffCmd = &cobra.Command{
	Use:     "diff",
	GroupID: GroupGovern.ID,
	Short:   "Show the persisted AKG state and its committed transaction version",
	Long: `Reports the currently persisted architectural state: commit hash, schema
version, and the graph version (the sequence of committed transactions captured
in akg.json).`,
	Example: `  # Display current graph version and commit state
  gmb diff

  # Output state version as JSON
  gmb diff --json

  # Inspect diff metadata for a specific directory
  gmb diff --dir ./backend`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolveDir(cmd)
		asJSON, _ := cmd.Flags().GetBool("json")

		storageDir := filepath.Join(dir, ".glassmarble")
		statePath := filepath.Join(storageDir, "akg.json")
		if _, err := os.Stat(statePath); err != nil {
			if asJSON {
				out, _ := json.MarshalIndent(diffJSON{Initialized: false, Error: "no active AKG database"}, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			fmt.Println(views.RenderStatusUninitialized(statePath))
			return nil
		}

		commitHash, schemaVersion, version, err := akg.StateMetadata(storageDir)
		if err != nil {
			return fmt.Errorf("failed to read AKG metadata: %w — try 'gmb analyze'", err)
		}

		if asJSON {
			dj := diffJSON{
				Initialized:   true,
				StorageDir:    storageDir,
				SchemaVersion: schemaVersion,
				GraphVersion:  version,
				CommitHash:    commitHash,
			}
			out, _ := json.MarshalIndent(dj, "", "  ")
			fmt.Println(string(out))
			return nil
		}

		fmt.Println(views.RenderDiff(commitHash, schemaVersion, version, nil))
		return nil
	},
}

func init() {
	diffCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	rootCmd.AddCommand(diffCmd)
}
