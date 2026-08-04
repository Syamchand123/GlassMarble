package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
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
			fmt.Println(views.RenderDoctorUninitialized(rep.TTLPath))
			return nil
		}

		fmt.Println(views.RenderDoctor(rep))
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
			return nil
		}
		return fmt.Errorf("integrity check failed (%d issue(s))", failures)
	},
}

func init() {
	doctorCmd.Flags().String("dir", ".", "Directory path containing the .glassmarble/ folder")
	rootCmd.AddCommand(doctorCmd)
}
