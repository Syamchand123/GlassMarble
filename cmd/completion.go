package cmd

import (
	"fmt"
	"os"

	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:     "completion [bash|zsh|fish|powershell]",
	GroupID: GroupUtility.ID,
	Short:   "Generate shell completion scripts",
	Long: `Generate shell completion script for gmb:
  Bash:       source <(gmb completion bash)
  Zsh:        gmb completion zsh > "${fpath[1]}/_gmb"
  Fish:       gmb completion fish | source
  PowerShell: gmb completion powershell | Out-String | Invoke-Expression`,
	Example: `  # Load bash completion in current session
  source <(gmb completion bash)

  # Load zsh completion in current session
  gmb completion zsh > "${fpath[1]}/_gmb"

  # Load fish completion
  gmb completion fish | source

  # Load PowerShell completion
  gmb completion powershell | Out-String | Invoke-Expression`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		default:
			return producterrs.Tagged(fmt.Sprintf("unknown completion shell %q (supported: bash, zsh, fish, powershell) — try 'gmb completion bash'", args[0]), producterrs.ErrValidation)
		}
	},
}

func plainHelpFunc(c *cobra.Command, _ []string) {
	fmt.Fprintf(c.OutOrStdout(), "%s\n\nUsage:\n  gmb completion [bash|zsh|fish|powershell]\n\nShells:\n  bash       Bash completion script\n  zsh        Zsh completion script\n  fish       Fish completion script\n  powershell PowerShell completion script\n", c.Long)
}

func init() {
	completionCmd.SetHelpFunc(plainHelpFunc)
	rootCmd.AddCommand(completionCmd)
}
