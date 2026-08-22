package cmd

import (
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/product"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

// version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of GlassMarble",
	Long:  "This command prints the version of the Glassmarble ",

	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(views.RenderVersion(product.Version))
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
