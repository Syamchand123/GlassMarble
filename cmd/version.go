package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
)

// version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of GlassMarble",
	Long:  "This command prints the version of the Glassmarble ",

	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("GlassMarble v0.1.0")
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
