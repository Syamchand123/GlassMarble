package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

var hooksCmd = &cobra.Command{
	Use:   "hooks [install|uninstall]",
	Short: "Install or uninstall Git post-commit hooks for automatic AKG updates",
	Long:  `Installs a post-commit hook into .git/hooks/ to automatically run 'gmb analyze' after each commit.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir, _ := cmd.Flags().GetString("dir")
		if targetDir == "" {
			targetDir = "."
		}
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}

		gitDir := filepath.Join(absDir, ".git", "hooks")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			return fmt.Errorf("not a git repository or .git/hooks missing at %s", gitDir)
		}

		hookPath := filepath.Join(gitDir, "post-commit")

		switch args[0] {
		case "install":
			// Use the real binary path and the absolute target directory so the
			// hook works regardless of CWD and even when gmb is not on PATH.
			// git hooks run via sh, so the command and dir are quoted.
			binary, err := os.Executable()
			if err != nil {
				return fmt.Errorf("failed to resolve binary path for hook: %w", err)
			}
			script := fmt.Sprintf("#!/bin/sh\n# GlassMarble auto-analysis post-commit hook\n%q analyze --dir %q\n", binary, absDir)
			if err := os.WriteFile(hookPath, []byte(script), 0755); err != nil {
				return fmt.Errorf("failed to install git hook: %w", err)
			}
			fmt.Println(views.RenderHooksInstalled(hookPath, binary, absDir))

		case "uninstall":
			if _, err := os.Stat(hookPath); err == nil {
				if err := os.Remove(hookPath); err != nil {
					return fmt.Errorf("failed to uninstall git hook: %w", err)
				}
				fmt.Println(views.RenderHooksUninstalled())
			} else {
				fmt.Println(views.RenderHooksNone())
			}

		default:
			return fmt.Errorf("unknown hooks subcommand %q (expected install or uninstall)", args[0])
		}

		return nil
	},
}

func init() {
	hooksCmd.Flags().String("dir", ".", "Target repository directory")
	rootCmd.AddCommand(hooksCmd)
}
