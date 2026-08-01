package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run integrity diagnostics on the AKG state database",
	Long: `Parses the active .glassmarble/akg_state.ttl back through the canonical
parser and checks: parse-back integrity, ontology conformance of every gm:
term, dangling references, duplicate node IDs, WAL state, and file freshness.
Exits non-zero when the database fails any integrity check.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		if dir == "" {
			dir = "."
		}

		storageDir := filepath.Join(dir, ".glassmarble")
		rep, err := akg.RunDoctor(storageDir)
		if err != nil {
			return fmt.Errorf("doctor failed: %w", err)
		}

		if !rep.Initialized {
			fmt.Println("GlassMarble Doctor: Uninitialized")
			fmt.Printf("No active AKG database found at %s. Run 'glassmarble analyze' first.\n", rep.TTLPath)
			return nil
		}

		fmt.Println("=== GlassMarble AKG Doctor Report ===")
		fmt.Printf("  Storage:       %s\n", rep.StorageDir)
		fmt.Printf("  TTL size:      %s (%d bytes)\n", formatBytes(rep.TTLBytes), rep.TTLBytes)
		fmt.Printf("  TTL modified:  %s\n", rep.TTLModTime.Format(time.RFC3339))
		fmt.Printf("  Schema:        v%d\n", rep.SchemaVersion)
		fmt.Printf("  Graph version: %d\n", rep.GraphVersion)
		fmt.Printf("  Commit:        %s\n", shortHash(rep.CommitHash))
		if !rep.LoadOK {
			fmt.Printf("  Parse-back:    FAILED\n")
			fmt.Printf("    %s\n", rep.LoadError)
			fmt.Println("DOCTOR: FAILED — the state file cannot be trusted. Re-run 'glassmarble analyze --full'.")
			return fmt.Errorf("TTL parse-back failed: %s", rep.LoadError)
		}
		fmt.Printf("  Parse-back:    ok (%d nodes, %d edges)\n", rep.NodeCount, rep.EdgeCount)
		fmt.Printf("  Dangling:      %d\n", rep.Dangling)
		if len(rep.DuplicateIDs) > 0 {
			fmt.Printf("  Duplicate IDs: %d\n", len(rep.DuplicateIDs))
			for _, id := range rep.DuplicateIDs {
				fmt.Printf("    %s\n", id)
			}
		} else {
			fmt.Println("  Duplicate IDs: 0")
		}
		if len(rep.UnknownTerms) > 0 {
			fmt.Printf("  Unknown gm: terms: %d\n", len(rep.UnknownTerms))
			for _, term := range rep.UnknownTerms {
				fmt.Printf("    gm:%s\n", term)
			}
		} else {
			fmt.Println("  Unknown gm: terms: 0 (ontology conformant)")
		}
		fmt.Printf("  WAL:           %d transaction(s), %d committed, %d pending, %s\n",
			rep.WALTxCount, rep.WALCommitted, rep.WALPending, formatBytes(rep.WALBytes))
		if rep.StaleWAL {
			fmt.Println("  Freshness:     STALE WAL — WAL entries are newer than the TTL; a write may not be persisted")
		} else {
			fmt.Println("  Freshness:     ok")
		}

		failures := 0
		if !rep.LoadOK {
			failures++
		}
		if rep.Dangling > 0 {
			fmt.Printf("WARNING: %d dangling edge(s) persisted (Issue 5 finding 1).\n", rep.Dangling)
		}
		if rep.StaleWAL {
			failures++
		}
		if failures == 0 {
			fmt.Println("DOCTOR: OK")
			return nil
		}
		fmt.Println("DOCTOR: FAILED — integrity issues found (see above).")
		return fmt.Errorf("integrity check failed (%d issue(s))", failures)
	},
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	if h == "" {
		return "(none)"
	}
	return h
}

func formatBytes(b int64) string {
	const mb = 1024 * 1024
	if b >= mb {
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	}
	return fmt.Sprintf("%.1f KB", float64(b)/1024)
}

func init() {
	doctorCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ folder")
	rootCmd.AddCommand(doctorCmd)
}
