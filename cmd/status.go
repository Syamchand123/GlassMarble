package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Display AKG database status, node statistics, and graph health",
	Long:  `Inspects the active .glassmarble state file and prints graph counts, schema version, commit, freshness, and health summary (AUDIT Issue 5 Phase 5B-5).`,
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

		commitHash, schemaVersion, version, err := akg.TTLMetadata(storageDir)
		if err != nil {
			return fmt.Errorf("failed to read AKG metadata: %w", err)
		}

		ttlInfo, err := os.Stat(ttlPath)
		if err != nil {
			return fmt.Errorf("failed to stat TTL: %w", err)
		}

		// Lazy read: status streams the TTL once (bounded memory) instead of
		// restoring the graph and rebuilding every index (AUDIT Issue 4
		// Phase 4A-2). Restore-only figures (macro rules, which are derived
		// data recomputed on load) are intentionally not shown here.
		stats, err := akg.StreamGraphStats(storageDir)
		if err != nil {
			return fmt.Errorf("failed to scan AKG: %w", err)
		}

		virtualShare := 0.0
		if stats.NodeCount > 0 {
			virtualShare = 100 * float64(stats.VirtualCount) / float64(stats.NodeCount)
		}

		fmt.Println("=== GlassMarble Architecture Knowledge Graph Status ===")
		fmt.Printf("  Storage Dir:   %s\n", storageDir)
		fmt.Printf("  Schema Version: %d\n", schemaVersion)
		fmt.Printf("  Graph Version: %d\n", version)
		fmt.Printf("  Commit Hash:   %s\n", commitHash)
		fmt.Printf("  Last Analysis: %s\n", ttlInfo.ModTime().Format(time.RFC3339))
		fmt.Printf("  Nodes Count:   %d\n", stats.NodeCount)
		// Cumulative graph totals: each persisted (s,p,t) triple is one
		// outbound and one inbound edge of the restored graph (AUDIT Issue 5
		// Phase 5B-5).
		fmt.Printf("  Outbound Edges:%d\n", stats.Edges)
		fmt.Printf("  Inbound Edges: %d\n", stats.Edges)
		fmt.Printf("  Indexed Files: %d\n", stats.IndexedFiles)
		fmt.Printf("  Entrypoints:   %d\n", stats.Entrypoints)
		fmt.Printf("  Virtual Nodes: %d (%.1f%%)\n", stats.VirtualCount, virtualShare)
		fmt.Printf("  Health Errors: %d dangling reference(s)\n", stats.Dangling)
		ttlSize, walSize := akgStorageSizes(storageDir)
		fmt.Printf("  Storage:       TTL %s | WAL %s\n", humanBytes(ttlSize), humanBytes(walSize))
		if stats.Dangling == 0 {
			fmt.Println("  Verification:  verified (no dangling edges)")
		} else {
			fmt.Printf("  Verification:  UNVERIFIED — %d dangling reference(s)\n", stats.Dangling)
		}
		if stale, txCount, err := akg.WALFreshness(storageDir); err == nil {
			if stale {
				fmt.Printf("  Freshness:     WARNING — WAL is newer than the TTL (%d unpersisted transaction(s))\n", txCount)
			} else {
				fmt.Println("  Freshness:     ok")
			}
		}

		return nil
	},
}

func init() {
	statusCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ folder")
	rootCmd.AddCommand(statusCmd)
}
