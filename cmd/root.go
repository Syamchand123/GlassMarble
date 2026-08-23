package cmd

import (
	"context"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/app"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/product"
	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:     "gmb",
	Short:   "GlassMarble – Intelligent Architecture Knowledge Graph",
	Long:    `Build, query, and visualise a self‑evolving Architecture Knowledge Graph.`,
	Version: product.Version,
	// Fang owns error and usage presentation (see Execute/ExecuteContext), so
	// Cobra must not print them a second time.
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Build flag config — debug is tri-state via *bool (C7-2): only set
		// when the flag was explicitly supplied, so YAML debug:false can be
		// overridden to false from CLI.
		rootDir, _ := cmd.Flags().GetString("root-dir")
		flagConfig := config.Config{
			RootDir: rootDir,
		}
		if cmd.Flags().Changed("debug") {
			b, _ := cmd.Flags().GetBool("debug")
			flagConfig.Debug = config.BoolPtr(b)
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
	return fang.Execute(context.Background(), rootCmd, fang.WithVersion(product.Version))
}

// ExecuteContext runs the CLI with a caller-supplied context (used by main.go
// to propagate OS signal cancellation) wrapped with Fang's styled output.
func ExecuteContext(ctx context.Context) error {
	return fang.Execute(ctx, rootCmd, fang.WithVersion(product.Version))
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
