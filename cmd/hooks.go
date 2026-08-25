package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

var hooksCmd = &cobra.Command{
	Use:       "hooks [install|uninstall]",
	GroupID:   GroupUtility.ID,
	Short:     "Install or uninstall Git post-commit hooks for automatic AKG updates",
	Long:      `Installs a post-commit hook into .git/hooks/ to automatically run 'gmb analyze' after each commit.`,
	Example:   `  # Install post-commit hook in active repository
  gmb hooks install

  # Uninstall post-commit hook
  gmb hooks uninstall`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"install", "uninstall"},
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}

		gitDir := filepath.Join(absDir, ".git", "hooks")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			return producterrs.Tagged(fmt.Sprintf("not a git repository or .git/hooks missing at %s — try 'git init' first", gitDir), producterrs.ErrValidation)
		}

		hookPath := filepath.Join(gitDir, "post-commit")

		switch args[0] {
		case "install":
			binary, err := os.Executable()
			if err != nil {
				return fmt.Errorf("failed to resolve binary path for hook: %w", err)
			}
			script := fmt.Sprintf("#!/bin/sh\n# GlassMarble auto-analysis post-commit hook\n%q analyze --dir %q\n", binary, absDir)
			if existing, err := os.ReadFile(hookPath); err == nil {
				if strings.Contains(string(existing), "# GlassMarble") {
					// Already managed by GlassMarble — overwrite in place.
				} else {
					bakPath := hookPath + ".gmb.bak"
					if _, statErr := os.Stat(bakPath); os.IsNotExist(statErr) {
						if wErr := os.WriteFile(bakPath, existing, 0755); wErr != nil {
							return fmt.Errorf("failed to back up existing hook to %s: %w", bakPath, wErr)
						}
						fmt.Printf("Existing post-commit hook backed up to %s\n", bakPath)
					} else {
						chained := string(existing) + "\n" + script
						if wErr := os.WriteFile(hookPath, []byte(chained), 0755); wErr != nil {
							return fmt.Errorf("failed to install chained git hook: %w", wErr)
						}
						fmt.Println(views.RenderHooksInstalled(hookPath, binary, absDir))
						break
					}
				}
			}
			if err := os.WriteFile(hookPath, []byte(script), 0755); err != nil {
				return fmt.Errorf("failed to install git hook: %w", err)
			}
			fmt.Println(views.RenderHooksInstalled(hookPath, binary, absDir))

		case "uninstall":
			data, readErr := os.ReadFile(hookPath)
			if readErr != nil {
				if os.IsNotExist(readErr) {
					fmt.Println(views.RenderHooksNone())
					break
				}
				return fmt.Errorf("failed to read git hook: %w", readErr)
			}
			if !strings.Contains(string(data), "# GlassMarble") {
				fmt.Printf("Refusing to remove %s: does not contain GlassMarble marker (# GlassMarble); leaving user hook intact\n", hookPath)
				break
			}
			if err := os.Remove(hookPath); err != nil {
				return fmt.Errorf("failed to uninstall git hook: %w", err)
			}
			bakPath := hookPath + ".gmb.bak"
			if bakData, err := os.ReadFile(bakPath); err == nil {
				if wErr := os.WriteFile(hookPath, bakData, 0755); wErr == nil {
					_ = os.Remove(bakPath)
					fmt.Printf("Restored previous hook from %s\n", bakPath)
				}
			}
			fmt.Println(views.RenderHooksUninstalled())

		default:
			return producterrs.Tagged(fmt.Sprintf("unknown hooks subcommand %q: expected install or uninstall — try 'gmb hooks install' or 'gmb hooks uninstall'", args[0]), producterrs.ErrValidation)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(hooksCmd)
}
