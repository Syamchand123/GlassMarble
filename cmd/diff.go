package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
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

		var viewEntries []views.DiffEntry
		for _, e := range entries {
			ve := views.DiffEntry{
				TxID:       int(e.TxID),
				CommitHash: e.CommitHash,
				Timestamp:  e.Timestamp.Format(time.RFC3339),
				Status:     string(e.Status),
			}
			if e.Payload != nil {
				ve.HasPayload = true
				ve.NodesAdded = len(e.Payload.GraphNodes)
				ve.EdgesAdded = countOutboundEdges(e.Payload.OutboundEdges)
			}
			ve.ModifiedFiles = len(e.ModifiedFiles)
			viewEntries = append(viewEntries, ve)
		}

		fmt.Println(views.RenderDiff(commitHash, schemaVersion, version, viewEntries))
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
