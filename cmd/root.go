package cmd

import (
	"context"
	"os"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/app"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/product"
	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
)

// Command Groups for structured help presentation (UX-06)
var (
	GroupAnalyze   = &cobra.Group{ID: "analyze", Title: "Analyze & Index Commands:"}
	GroupInspect   = &cobra.Group{ID: "inspect", Title: "Inspect & Query Commands:"}
	GroupGovern    = &cobra.Group{ID: "govern", Title: "Architecture Governance Commands:"}
	GroupVisualize = &cobra.Group{ID: "visualize", Title: "Visualization Commands:"}
	GroupAI        = &cobra.Group{ID: "ai", Title: "AI & Memory Commands:"}
	GroupUtility   = &cobra.Group{ID: "utility", Title: "Utility & Configuration Commands:"}
)

var rootCmd = &cobra.Command{
	Use:   "gmb",
	Short: "GlassMarble – Intelligent Architecture Knowledge Graph Platform",
	Long: `GlassMarble (gmb) builds, queries, governs, and visualizes a self-evolving
Architecture Knowledge Graph (AKG) from your source code repositories.

Analyze codebases with sub-second incremental parsing, track architectural drift,
render 31+ diagram types (Mermaid, PlantUML, Graphviz), and reason over system design
with local or cloud AI models.

GitHub: https://github.com/Syamchand123/GlassMarble
Documentation: https://github.com/Syamchand123/GlassMarble#readme`,
	Version: product.Version,
	// Fang owns error and usage presentation (see Execute/ExecuteContext), so
	// Cobra must not print them a second time.
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Output mode handling (UX-02)
		if colorFlag, err := cmd.Flags().GetString("color"); err == nil {
			switch colorFlag {
			case "never":
				os.Setenv("NO_COLOR", "1")
			case "always":
				os.Unsetenv("NO_COLOR")
			}
		}

		// Unified dir handling (UX-01)
		dir, _ := cmd.Flags().GetString("dir")
		if dir == "" {
			dir, _ = cmd.Flags().GetString("root-dir")
		}

		flagConfig := config.Config{
			RootDir: dir,
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

// RootCmd returns the root cobra command for man page and completions generators.
func RootCmd() *cobra.Command {
	return rootCmd
}

func init() {
	// Add command groups (UX-06)
	rootCmd.AddGroup(
		GroupAnalyze,
		GroupInspect,
		GroupGovern,
		GroupVisualize,
		GroupAI,
		GroupUtility,
	)

	// Global / Persistent Flags (UX-01, UX-02)
	rootCmd.PersistentFlags().String("dir", "", "Target repository directory for analysis and queries")
	rootCmd.PersistentFlags().String("root-dir", "", "Target repository directory (deprecated alias for --dir)")
	_ = rootCmd.PersistentFlags().MarkHidden("root-dir")

	rootCmd.PersistentFlags().String("color", "auto", "Color output mode: auto|always|never")
	rootCmd.PersistentFlags().BoolP("quiet", "q", false, "Suppress non-error output")

	rootCmd.PersistentFlags().Bool("debug", false, "Enable debug logging")
	rootCmd.PersistentFlags().StringP("config", "c", "", "Config file path (default is $HOME/.glassmarble.yaml)")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Verbose output logging")
	rootCmd.PersistentFlags().Int("max-json-mb", 0, "Refuse to load or commit an AKG state file (akg.json) larger than this many MiB (0 = unlimited)")
}

// resolveDir reads the unified --dir / --root-dir flag or returns "."
func resolveDir(cmd *cobra.Command) string {
	if cmd == nil {
		return "."
	}
	if cmd.Flags().Changed("dir") {
		if dir, err := cmd.Flags().GetString("dir"); err == nil && dir != "" {
			return dir
		}
	}
	if cmd.Flags().Changed("root-dir") {
		if rootDir, err := cmd.Flags().GetString("root-dir"); err == nil && rootDir != "" {
			return rootDir
		}
	}
	if dir, err := cmd.Flags().GetString("dir"); err == nil && dir != "" {
		return dir
	}
	if rootDir, err := cmd.Flags().GetString("root-dir"); err == nil && rootDir != "" {
		return rootDir
	}
	return "."
}

// newAKGManager builds the AKG transaction manager for a storage directory,
// honoring the persistent --max-json-mb budget flag.
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
	if f := rootCmd.PersistentFlags().Lookup("dir"); f != nil {
		_ = f.Value.Set("")
		f.Changed = false
	}
	if f := rootCmd.PersistentFlags().Lookup("root-dir"); f != nil {
		_ = f.Value.Set("")
		f.Changed = false
	}
	return rootCmd
}
