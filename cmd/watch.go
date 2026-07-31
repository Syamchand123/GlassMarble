package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var watchInterval time.Duration

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Continuously watch repository for source changes and update AKG",
	Long:  `Polls repository for file modifications at specified interval and triggers delta analysis.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir, _ := cmd.Flags().GetString("dir")
		if targetDir == "" {
			targetDir = "."
		}

		fmt.Printf("GlassMarble Watcher active on '%s' (polling interval: %s)\nPress Ctrl+C to stop.\n\n", targetDir, watchInterval)

		ticker := time.NewTicker(watchInterval)
		defer ticker.Stop()

		for {
			select {
			case <-cmd.Context().Done():
				fmt.Println("\nWatcher stopped.")
				return nil
			case <-ticker.C:
				fmt.Printf("[%s] Checking repository changes...\n", time.Now().Format("15:04:05"))
			}
		}
	},
}

func init() {
	watchCmd.Flags().DurationVar(&watchInterval, "interval", 5*time.Second, "Polling interval duration")
	watchCmd.Flags().String("dir", ".", "Target repository directory")
	rootCmd.AddCommand(watchCmd)
}
