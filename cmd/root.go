package cmd

import (
	"context"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/app"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gmb",
	Short: "GlassMarble – Intelligent Architecture Knowledge Graph",
	Long:  `Build, query, and visualise a self‑evolving Architecture Knowledge Graph.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Build flag config
		debug, _ := cmd.Flags().GetBool("debug")
		rootDir, _ := cmd.Flags().GetString("root-dir")

		flagConfig := config.Config{
			Debug:   debug,
			RootDir: rootDir,
		}

		application, err := app.New(flagConfig)
		if err != nil {
			return err
		}

		// Inject into context
		ctx := context.WithValue(cmd.Context(), "app", application)
		cmd.SetContext(ctx)
		return nil
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func ExecuteContext(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}

func init() {
	rootCmd.PersistentFlags().String("root-dir", "", "Root directory for analysis")
	rootCmd.PersistentFlags().Bool("debug", false, "Enable debug logging")
	rootCmd.PersistentFlags().StringP("config", "c", "", "config file (default is $HOME/.glassmarble.yaml)")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().Int("max-ttl-mb", 0, "Refuse to load or commit an AKG state file larger than this many MiB (0 = unlimited) (AUDIT Phase 4A-4)")
}

// newAKGManager builds the AKG transaction manager for a storage directory,
// honoring the persistent --max-ttl-mb budget flag (AUDIT Issue 4 Phase 4A-4).
// A nil cmd (e.g. from the watch loop) means "no budget flag" — unlimited.
func newAKGManager(storageDir string, cmd *cobra.Command) (*akg.AKGTransactionManager, error) {
	var maxBytes int64
	if cmd != nil {
		if maxMB, _ := cmd.Flags().GetInt("max-ttl-mb"); maxMB > 0 {
			maxBytes = int64(maxMB) << 20
		}
	}
	return akg.NewAKGTransactionManagerWithOptions(storageDir, maxBytes)
}

func RootCmdForTesting() *cobra.Command {
	return rootCmd
}
