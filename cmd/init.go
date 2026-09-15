package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

type initReceiptJSON struct {
	Status           string `json:"status"`
	WorkspaceDir     string `json:"workspace_dir"`
	GitignoreUpdated bool   `json:"gitignore_updated"`
}

// ensureGlassmarbleGitignored adds a ".glassmarble" line to abs/.gitignore
// (creating the file if absent) unless one is already present. Returns
// whether it made a change.
//
// Originally only `gmb init` did this. `gmb analyze` creates the very same
// .glassmarble directory on its own — internal engine state (AKG
// snapshots, telemetry, the review queue, learned conventions) plus every
// generated doc's *.gmb.bak backup — but a user who runs `gmb analyze`
// directly (arguably the more obvious "first command" name for "analyze my
// code," with `gmb init` reading as an optional, skippable scaffold step)
// got no gitignore protection at all and would commit all of that straight
// into their repo. Called from both commands now so either one protects it.
func ensureGlassmarbleGitignored(abs string) (bool, error) {
	gitignorePath := filepath.Join(abs, ".gitignore")
	entry := ".glassmarble\n"
	if data, err := os.ReadFile(gitignorePath); err == nil {
		content := string(data)
		if strings.Contains(content, ".glassmarble") {
			return false, nil
		}
		if !strings.HasSuffix(content, "\n") && len(content) > 0 {
			entry = "\n.glassmarble\n"
		}
		if err := os.WriteFile(gitignorePath, []byte(content+entry), 0644); err != nil {
			return false, fmt.Errorf("failed to update .gitignore: %w", err)
		}
		return true, nil
	} else if os.IsNotExist(err) {
		if err := os.WriteFile(gitignorePath, []byte(entry), 0644); err != nil {
			return false, fmt.Errorf("failed to create .gitignore: %w", err)
		}
		return true, nil
	}
	return false, nil
}

var initCmd = &cobra.Command{
	Use:     "init",
	GroupID: GroupUtility.ID,
	Short:   "Initialize a repository for GlassMarble analysis",
	Long:    `Creates the .glassmarble workspace directory, state databases, and configuration files.`,
	Example: `  # Initialize GlassMarble in current directory
  gmb init

  # Initialize a specific directory
  gmb init --dir ./backend

  # Initialize and emit JSON receipt
  gmb init --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		asJSON, _ := cmd.Flags().GetBool("json")

		abs, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
		}

		gmDir := filepath.Join(abs, ".glassmarble")
		if err := os.MkdirAll(gmDir, 0755); err != nil {
			return fmt.Errorf("failed to create .glassmarble directory: %w", err)
		}

		marblesDir := filepath.Join(gmDir, "marbles")
		if err := os.MkdirAll(marblesDir, 0755); err != nil {
			return fmt.Errorf("failed to create marbles directory: %w", err)
		}

		for _, sub := range []string{"intelligence", "snapshots", "memory"} {
			if err := os.MkdirAll(filepath.Join(gmDir, sub), 0755); err != nil {
				return fmt.Errorf("failed to create %s directory: %w", sub, err)
			}
		}

		configPath := filepath.Join(gmDir, "config.yaml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			defaultConfig := []byte("root_dir: .\ndebug: false\noutput_format: mermaid\nmax_file_bytes: 2097152\n")
			if err := os.WriteFile(configPath, defaultConfig, 0644); err != nil {
				return fmt.Errorf("failed to create config.yaml: %w", err)
			}
		}

		jsonPath := filepath.Join(gmDir, "akg.json")
		if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
			if err := akg.WriteEmptyJSONState(jsonPath); err != nil {
				return fmt.Errorf("failed to create akg.json: %w", err)
			}
		}

		gitignoreUpdated, err := ensureGlassmarbleGitignored(abs)
		if err != nil {
			return err
		}

		if asJSON {
			receipt := initReceiptJSON{
				Status:           "initialized",
				WorkspaceDir:     gmDir,
				GitignoreUpdated: gitignoreUpdated,
			}
			out, _ := json.MarshalIndent(receipt, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		}

		tui.Fprintln(cmd.OutOrStdout(), views.RenderInitSuccess(gmDir, gitignoreUpdated))
		return nil
	},
}

func init() {
	initCmd.Flags().Bool("json", false, "Emit machine-readable JSON receipt")
	rootCmd.AddCommand(initCmd)
}
