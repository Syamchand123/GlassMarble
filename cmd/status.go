package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Display AKG database status, node statistics, and graph health",
	Long:  `Inspects the active .glassmarble state file and prints graph counts, last commit, and health summary.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		if dir == "" {
			dir = "."
		}

		storageDir := filepath.Join(dir, ".glassmarble")
		ttlPath := filepath.Join(storageDir, "akg_state.ttl")

		if _, err := os.Stat(ttlPath); os.IsNotExist(err) {
			fmt.Printf("GlassMarble Status: Uninitialized\nNo active AKG database found at %s. Run 'glassmarble analyze' first.\n", ttlPath)
			return nil
		}

		tm, err := akg.NewAKGTransactionManager(storageDir)
		if err != nil {
			return fmt.Errorf("failed to open AKG database: %w", err)
		}

		snapshot := tm.GetActiveSnapshot()
		if snapshot == nil {
			fmt.Println("AKG State: Empty")
			return nil
		}

		fmt.Println("=== GlassMarble Architecture Knowledge Graph Status ===")
		fmt.Printf("  Storage Dir:  %s\n", storageDir)
		fmt.Printf("  Commit Hash:  %s\n", snapshot.CommitHash)
		fmt.Printf("  Graph Version: %d\n", snapshot.Version)
		fmt.Printf("  Nodes Count:   %d\n", snapshot.Nodes.Len())
		fmt.Printf("  Outbound Edges:%d\n", snapshot.OutboundEdges.Len())
		fmt.Printf("  Inbound Edges: %d\n", snapshot.InboundEdges.Len())
		fmt.Printf("  Indexed Files: %d\n", snapshot.FileNodeIndex.Len())
		fmt.Printf("  Macro Rules:   %d\n", snapshot.MacroRules.Len())
		fmt.Printf("  Health Errors: %d dangling reference(s)\n", len(snapshot.Errors))

		return nil
	},
}

func init() {
	statusCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ folder")
	rootCmd.AddCommand(statusCmd)
}
