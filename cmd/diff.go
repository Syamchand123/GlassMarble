package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show architectural diff across committed transactions",
	Long: `Replays the write-ahead log and prints the structural mutations of every
recorded transaction: commit hash, node/edge deltas, and modified files.
Because the WAL is truncated after each successful atomic TTL write, a clean
database shows no pending transactions (the latest state is fully persisted
in akg_state.ttl).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		if dir == "" {
			dir = "."
		}

		storageDir := filepath.Join(dir, ".glassmarble")
		walDir := filepath.Join(storageDir, "wal")

		ttlPath := filepath.Join(storageDir, "akg_state.ttl")
		if _, err := os.Stat(ttlPath); err != nil {
			return fmt.Errorf("no AKG database at %s -- run 'glassmarble analyze' first", ttlPath)
		}

		commitHash, schemaVersion, version, err := akg.TTLMetadata(storageDir)
		if err != nil {
			return fmt.Errorf("failed to read TTL metadata: %w", err)
		}

		wal, err := akg.NewWriteAheadLog(walDir)
		if err != nil {
			return fmt.Errorf("failed to open WAL: %w", err)
		}
		entries, err := wal.ReadAllEntries()
		if err != nil {
			return fmt.Errorf("failed to read WAL: %w", err)
		}

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].TxID < entries[j].TxID
		})

		fmt.Println("=== Architectural Graph Mutation Diff ===")
		fmt.Printf("  Current: %s (schema v%d, graph version %d)\n", shortHash(commitHash), schemaVersion, version)

		if len(entries) == 0 {
			fmt.Println("  No pending transactions: the WAL was truncated after the last atomic write.")
			fmt.Println("  The current akg_state.ttl is the fully persisted latest state.")
			return nil
		}

		fmt.Printf("  %d recorded transaction(s):\n\n", len(entries))
		for _, e := range entries {
			fmt.Printf("  tx #%d  %s  %s\n", e.TxID, shortHash(e.CommitHash), e.Timestamp.Format(time.RFC3339))
			fmt.Printf("    status: %s\n", e.Status)
			if e.Payload != nil {
				fmt.Printf("    +%d nodes, %d edges\n", len(e.Payload.GraphNodes), countOutboundEdges(e.Payload.OutboundEdges))
			}
			if len(e.ModifiedFiles) > 0 {
				fmt.Printf("    files: %d changed\n", len(e.ModifiedFiles))
			}
		}
		return nil
	},
}

func countOutboundEdges(out map[string][]stage4.ResolvedEdge) int {
	n := 0
	for _, edges := range out {
		n += len(edges)
	}
	return n
}

func init() {
	diffCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ database folder")
	rootCmd.AddCommand(diffCmd)
}
