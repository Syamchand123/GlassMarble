package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/product"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/spf13/cobra"
)

type versionJSON struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	BuiltBy string `json:"built_by"`
}

var versionCmd = &cobra.Command{
	Use:     "version",
	GroupID: GroupUtility.ID,
	Short:   "Print the version number of GlassMarble",
	Long:    "Display the GlassMarble CLI version, commit SHA, build timestamp, and build toolchain.",
	Example: `  # Print branded version one-liner
  gmb version

  # Output version metadata as JSON
  gmb version --json`,
	Run: func(cmd *cobra.Command, args []string) {
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			out, _ := json.MarshalIndent(versionJSON{
				Version: product.Version,
				Commit:  product.Commit,
				Date:    product.Date,
				BuiltBy: product.BuiltBy,
			}, "", "  ")
			fmt.Println(string(out))
			return
		}
		fmt.Println(views.RenderVersion(product.Version, product.Commit, product.Date, product.BuiltBy))
	},
}

func init() {
	versionCmd.Flags().Bool("json", false, "Emit machine-readable JSON version metadata")
	rootCmd.AddCommand(versionCmd)
}
