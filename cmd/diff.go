package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show the persisted AKG state and its committed transaction version",
	Long: `Reports the currently persisted architectural state: commit hash, schema
version, and the graph version (the count of committed transactions captured
in akg.json). The state file is the single source of truth — every committed
transaction is fully persisted in akg.json, so there is no transaction log
left to replay.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		if dir == "" {
			dir = "."
		}

		storageDir := filepath.Join(dir, ".glassmarble")
		statePath := filepath.Join(storageDir, "akg.json")
		if _, err := os.Stat(statePath); err != nil {
			return producterrs.Tagged(fmt.Sprintf("no AKG database at %s -- run 'glassmarble analyze' first", statePath), producterrs.ErrValidation)
		}

		commitHash, schemaVersion, version, err := akg.StateMetadata(storageDir)
		if err != nil {
			return fmt.Errorf("failed to read AKG metadata: %w", err)
		}

		fmt.Println(views.RenderDiff(commitHash, schemaVersion, version, nil))
		return nil
	},
}

func init() {
	diffCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ database folder")
	rootCmd.AddCommand(diffCmd)
}
