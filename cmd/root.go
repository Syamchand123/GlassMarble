package cmd

import (
	"context"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/app"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
)

// version is the GlassMarble CLI version surfaced by `gmb version`, Fang's
// --version handling, and the build.
const version = "0.1.0"

var rootCmd = &cobra.Command{
	Use:     "gmb",
	Short:   "GlassMarble – Intelligent Architecture Knowledge Graph",
	Long:    `Build, query, and visualise a self‑evolving Architecture Knowledge Graph.`,
	Version: version,
	// Fang owns error and usage presentation (see Execute/ExecuteContext), so
	// Cobra must not print them a second time.
	SilenceUsage:  true,
	SilenceErrors: true,
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

// Execute runs the CLI wrapped with Fang's styled help/error/version skin.
func Execute() error {
	return fang.Execute(context.Background(), rootCmd, fang.WithVersion(version))
}

// ExecuteContext runs the CLI with a caller-supplied context (used by main.go
// to propagate OS signal cancellation) wrapped with Fang's styled output.
func ExecuteContext(ctx context.Context) error {
	return fang.Execute(ctx, rootCmd, fang.WithVersion(version))
}

func init() {
	rootCmd.PersistentFlags().String("root-dir", "", "Root directory for analysis")
	rootCmd.PersistentFlags().Bool("debug", false, "Enable debug logging")
	rootCmd.PersistentFlags().StringP("config", "c", "", "config file (default is $HOME/.glassmarble.yaml)")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().Int("max-json-mb", 0, "Refuse to load or commit an AKG state file (akg.json) larger than this many MiB (0 = unlimited) (AUDIT Phase 4A-4)")
}

// newAKGManager builds the AKG transaction manager for a storage directory,
// honoring the persistent --max-json-mb budget flag (AUDIT Issue 4 Phase 4A-4).
// A nil cmd (e.g. from the watch loop) means "no budget flag" — unlimited.
func newAKGManager(storageDir string, cmd *cobra.Command) (*akg.AKGTransactionManager, error) {
	var maxBytes int64
	if cmd != nil {
		if maxMB, _ := cmd.Flags().GetInt("max-json-mb"); maxMB > 0 {
			maxBytes = int64(maxMB) << 20
		}
	}
	return akg.NewAKGTransactionManagerWithOptions(storageDir, maxBytes)
}

func RootCmdForTesting() *cobra.Command {
	return rootCmd
}
