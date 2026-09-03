package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

type doctorCheckJSON struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "ok" | "warn" | "fail"
	Detail string `json:"detail,omitempty"`
}

type doctorJSON struct {
	Initialized   bool              `json:"initialized"`
	StorageDir    string            `json:"storage_dir,omitempty"`
	StateBytes    int64             `json:"state_bytes,omitempty"`
	SchemaVersion int               `json:"schema_version,omitempty"`
	GraphVersion  uint64            `json:"graph_version,omitempty"`
	CommitHash    string            `json:"commit_hash,omitempty"`
	Checks        []doctorCheckJSON `json:"checks"`
	FailureCount  int               `json:"failure_count"`
	Error         string            `json:"error,omitempty"`
}

var doctorCmd = &cobra.Command{
	Use:     "doctor",
	GroupID: GroupInspect.ID,
	Short:   "Run integrity diagnostics on the AKG state database",
	Long: `Parses the active .glassmarble/akg.json database and verifies schema conformance,
parse-back integrity, duplicate node identifiers, and dangling edge references.`,
	Example: `  # Run health and integrity checks
  gmb doctor

  # Output diagnostic results as JSON
  gmb doctor --json

  # Run diagnostics on a specific repository
  gmb doctor --dir ./backend`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolveDir(cmd)
		asJSON, _ := cmd.Flags().GetBool("json")

		storageDir := filepath.Join(dir, ".glassmarble")
		rep, err := akg.RunDoctor(storageDir)
		if err != nil {
			return fmt.Errorf("doctor diagnostics failed: %w — try 'gmb analyze'", err)
		}

		if !rep.Initialized {
			if asJSON {
				out, _ := json.MarshalIndent(doctorJSON{
					Initialized:  false,
					StorageDir:   storageDir,
					Checks:       []doctorCheckJSON{},
					FailureCount: 0,
					Error:        "database uninitialized",
				}, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			fmt.Println(views.RenderDoctorUninitialized(rep.StatePath))
			return nil
		}

		checks := []doctorCheckJSON{
			{Name: "schema_version", Status: "ok", Detail: fmt.Sprintf("v%d", rep.SchemaVersion)},
		}

		failures := 0
		if rep.LoadOK {
			checks = append(checks, doctorCheckJSON{Name: "parse_back", Status: "ok", Detail: "state verified"})
		} else {
			failures++
			checks = append(checks, doctorCheckJSON{Name: "parse_back", Status: "fail", Detail: rep.LoadError})
		}

		if rep.Dangling == 0 {
			checks = append(checks, doctorCheckJSON{Name: "dangling_edges", Status: "ok", Detail: "0 dangling references"})
		} else {
			failures++
			checks = append(checks, doctorCheckJSON{Name: "dangling_edges", Status: "fail", Detail: fmt.Sprintf("%d dangling references", rep.Dangling)})
		}

		if asJSON {
			dj := doctorJSON{
				Initialized:   true,
				StorageDir:    rep.StorageDir,
				StateBytes:    rep.StateBytes,
				SchemaVersion: rep.SchemaVersion,
				GraphVersion:  rep.GraphVersion,
				CommitHash:    rep.CommitHash,
				Checks:        checks,
				FailureCount:  failures,
			}
			out, _ := json.MarshalIndent(dj, "", "  ")
			fmt.Println(string(out))
			if failures > 0 {
				return producterrs.Tagged(fmt.Sprintf("integrity check failed (%d issue(s)) — try 'gmb analyze --full'", failures), producterrs.ErrPolicyViolation)
			}
			return nil
		}

		fmt.Println(views.RenderDoctor(rep))
		if failures == 0 {
			return nil
		}
		return producterrs.Tagged(fmt.Sprintf("integrity check failed (%d issue(s)) — try 'gmb analyze --full'", failures), producterrs.ErrPolicyViolation)
	},
}

func init() {
	doctorCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	rootCmd.AddCommand(doctorCmd)
}
