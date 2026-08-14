package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a repository for GlassMarble analysis",
	Long:  `Creates the .glassmarble workspace directory and configuration files.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir, _ := cmd.Flags().GetString("dir")
		if targetDir == "" {
			targetDir = "."
		}

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

		// V2 pipeline directories (master plan §4.2): intelligence (Architecture Intelligence
		// latest.json), snapshots (intelligence arch snapshots) and memory
		// (developer memory). Also created lazily by `gmb analyze`.
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

		// Create empty GraphJSON AKG state (v3 store). The JSON file is the
		// canonical state from Phase C onward; an empty-but-valid document
		// parses cleanly on first load.
		jsonPath := filepath.Join(gmDir, "akg.json")
		if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
			if err := akg.WriteEmptyJSONState(jsonPath); err != nil {
				return fmt.Errorf("failed to create akg.json: %w", err)
			}
		}

		// Update .gitignore to ignore .glassmarble folder
		gitignorePath := filepath.Join(abs, ".gitignore")
		entry := ".glassmarble\n"
		gitignoreUpdated := false
		if data, err := os.ReadFile(gitignorePath); err == nil {
			content := string(data)
			if !strings.Contains(content, ".glassmarble") {
				if !strings.HasSuffix(content, "\n") && len(content) > 0 {
					entry = "\n.glassmarble\n"
				}
				if err := os.WriteFile(gitignorePath, []byte(content+entry), 0644); err != nil {
					return fmt.Errorf("failed to update .gitignore: %w", err)
				}
				gitignoreUpdated = true
			}
		} else if os.IsNotExist(err) {
			if err := os.WriteFile(gitignorePath, []byte(entry), 0644); err != nil {
				return fmt.Errorf("failed to create .gitignore: %w", err)
			}
			gitignoreUpdated = true
		}

		fmt.Println(views.RenderInitSuccess(gmDir, gitignoreUpdated))
		return nil
	},
}

func init() {
	initCmd.Flags().String("dir", ".", "Target repository directory")
	rootCmd.AddCommand(initCmd)
}
