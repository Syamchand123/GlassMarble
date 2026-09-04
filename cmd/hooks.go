package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

// hooksJSON is the machine-readable receipt of a hooks install/uninstall run.
type hooksJSON struct {
	Action     string `json:"action"`
	Repo       string `json:"repo"`
	HookPath   string `json:"hook_path"`
	Installed  bool   `json:"installed"`
	Changed    bool   `json:"changed"`
	Chained    bool   `json:"chained,omitempty"`
	BackupPath string `json:"backup_path,omitempty"`
	Binary     string `json:"binary,omitempty"`
	Message    string `json:"message"`
}

var hooksCmd = &cobra.Command{
	Use:     "hooks [install|uninstall]",
	GroupID: GroupUtility.ID,
	Short:   "Install or uninstall Git post-commit hooks for automatic AKG updates",
	Long:    `Installs a post-commit hook into .git/hooks/ to automatically run 'gmb analyze' after each commit.`,
	Example: `  # Install post-commit hook in active repository
  gmb hooks install

  # Uninstall post-commit hook
  gmb hooks uninstall

  # Emit an install receipt as JSON for provisioning scripts
  gmb hooks install --json`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"install", "uninstall"},
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := resolveDir(cmd)
		asJSON, _ := cmd.Flags().GetBool("json")

		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}

		gitDir := filepath.Join(absDir, ".git", "hooks")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			return producterrs.Tagged(fmt.Sprintf("not a git repository or .git/hooks missing at %s — try 'git init' first", gitDir), producterrs.ErrValidation)
		}

		hookPath := filepath.Join(gitDir, "post-commit")
		receipt := hooksJSON{Action: args[0], Repo: absDir, HookPath: hookPath}

		switch args[0] {
		case "install":
			binary, err := os.Executable()
			if err != nil {
				return fmt.Errorf("failed to resolve binary path for hook: %w", err)
			}
			receipt.Binary = binary
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
						receipt.BackupPath = bakPath
						if !asJSON {
							tui.Fprintf(cmd.ErrOrStderr(), "Existing post-commit hook backed up to %s\n", bakPath)
						}
					} else {
						chained := string(existing) + "\n" + script
						if wErr := os.WriteFile(hookPath, []byte(chained), 0755); wErr != nil {
							return fmt.Errorf("failed to install chained git hook: %w", wErr)
						}
						receipt.Installed, receipt.Changed, receipt.Chained = true, true, true
						receipt.Message = "GlassMarble post-commit hook chained onto the existing hook"
						if !asJSON {
							tui.Fprintln(cmd.OutOrStdout(), views.RenderHooksInstalled(hookPath, binary, absDir))
						}
						break
					}
				}
			}
			if err := os.WriteFile(hookPath, []byte(script), 0755); err != nil {
				return fmt.Errorf("failed to install git hook: %w", err)
			}
			receipt.Installed, receipt.Changed = true, true
			receipt.Message = "GlassMarble post-commit hook installed"
			if !asJSON {
				tui.Fprintln(cmd.OutOrStdout(), views.RenderHooksInstalled(hookPath, binary, absDir))
			}

		case "uninstall":
			data, readErr := os.ReadFile(hookPath)
			if readErr != nil {
				if os.IsNotExist(readErr) {
					receipt.Message = "no post-commit hook present"
					if !asJSON {
						tui.Fprintln(cmd.OutOrStdout(), views.RenderHooksNone())
					}
					break
				}
				return fmt.Errorf("failed to read git hook: %w", readErr)
			}
			if !strings.Contains(string(data), "# GlassMarble") {
				receipt.Installed = true
				receipt.Message = fmt.Sprintf("refused to remove %s: does not contain the GlassMarble marker; user hook left intact", hookPath)
				if !asJSON {
					tui.Fprintf(cmd.ErrOrStderr(), "Refusing to remove %s: does not contain GlassMarble marker (# GlassMarble); leaving user hook intact\n", hookPath)
				}
				break
			}
			if err := os.Remove(hookPath); err != nil {
				return fmt.Errorf("failed to uninstall git hook: %w", err)
			}
			receipt.Changed = true
			receipt.Message = "GlassMarble post-commit hook removed"
			bakPath := hookPath + ".gmb.bak"
			if bakData, err := os.ReadFile(bakPath); err == nil {
				if wErr := os.WriteFile(hookPath, bakData, 0755); wErr == nil {
					_ = os.Remove(bakPath)
					receipt.BackupPath = bakPath
					receipt.Message = "GlassMarble post-commit hook removed; previous hook restored"
					if !asJSON {
						tui.Fprintf(cmd.ErrOrStderr(), "Restored previous hook from %s\n", bakPath)
					}
				}
			}
			if !asJSON {
				tui.Fprintln(cmd.OutOrStdout(), views.RenderHooksUninstalled())
			}

		default:
			return producterrs.Tagged(fmt.Sprintf("unknown hooks subcommand %q: expected install or uninstall — try 'gmb hooks install' or 'gmb hooks uninstall'", args[0]), producterrs.ErrValidation)
		}

		if asJSON {
			out, _ := json.MarshalIndent(receipt, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
		}

		return nil
	},
}

func init() {
	hooksCmd.Flags().Bool("json", false, "Emit machine-readable JSON receipt")
	rootCmd.AddCommand(hooksCmd)
}
