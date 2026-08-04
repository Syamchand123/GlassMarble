package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// completionCmd generates raw shell completion scripts. §A.2: this command
// must bypass Fang's styled help/usage wrapper so the generated scripts stay
// byte-clean when piped (`source <(gmb completion bash)`).
var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long:  `To load completions: Bash: source <(glassmarble completion bash), Zsh: glassmarble completion zsh > "${fpath[1]}/_glassmarble"`,
	Args:  cobra.ExactArgs(1),
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
			return cmd.Help()
		}
	},
}

// plainHelpFunc replaces Fang's styled help with a plain, ANSI-free help blurb
// so `gmb completion --help` (or a bad shell argument) never leaks styled
// output into a piped shell session (§A.2).
func plainHelpFunc(c *cobra.Command, _ []string) {
	fmt.Fprintf(c.OutOrStdout(), "%s\n\nUsage:\n  gmb completion [bash|zsh|fish|powershell]\n\nShells:\n  bash       Bash completion script\n  zsh        Zsh completion script\n  fish       Fish completion script\n  powershell PowerShell completion script\n", c.Long)
}

func init() {
	completionCmd.SetHelpFunc(plainHelpFunc)
	rootCmd.AddCommand(completionCmd)
}
