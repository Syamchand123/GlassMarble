package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var hooksCmd = &cobra.Command{
	Use:   "hooks [install|uninstall]",
	Short: "Install or uninstall Git post-commit hooks for automatic AKG updates",
	Long:  `Installs a post-commit hook into .git/hooks/ to automatically run 'glassmarble analyze' after each commit.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		gitDir := filepath.Join(".", ".git", "hooks")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			return fmt.Errorf("not a git repository or .git/hooks missing at %s", gitDir)
		}

		hookPath := filepath.Join(gitDir, "post-commit")

		switch args[0] {
		case "install":
			script := "#!/bin/sh\n# GlassMarble auto-analysis post-commit hook\nglassmarble analyze --dir .\n"
			if err := os.WriteFile(hookPath, []byte(script), 0755); err != nil {
				return fmt.Errorf("failed to install git hook: %w", err)
			}
			fmt.Printf("GlassMarble post-commit hook installed successfully at %s\n", hookPath)

		case "uninstall":
			if _, err := os.Stat(hookPath); err == nil {
				if err := os.Remove(hookPath); err != nil {
					return fmt.Errorf("failed to uninstall git hook: %w", err)
				}
				fmt.Println("GlassMarble post-commit hook uninstalled successfully.")
			} else {
				fmt.Println("No active GlassMarble post-commit hook found.")
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(hooksCmd)
}
