package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/Syamchand123/GlassMarble/internal/visualizer"
	"github.com/spf13/cobra"
)

var (
	uiPortFlag   int
	uiHostFlag   string
	uiOpenFlag   bool
	uiNoOpenFlag bool
)

var uiCmd = &cobra.Command{
	Use:     "ui",
	Aliases: []string{"serve"},
	GroupID: GroupVisualize.ID,
	Short:   "Launch local interactive architecture visualizer web server",
	Long: `Starts a lightweight, zero-dependency local web server serving an interactive
2D/3D force-directed architecture graph with real-time node inspection, search,
and blast-radius simulation.`,
	Example: `  # Launch interactive visualizer and open default browser
  gmb ui

  # Serve on custom port without automatically opening browser
  gmb ui --port 3000 --no-open

  # Equivalent alias
  gmb serve -p 8080`,
	RunE: func(cmd *cobra.Command, args []string) error {
		rootDir := resolveDir(cmd)

		storageDir := filepath.Join(rootDir, ".glassmarble")
		tm, err := newAKGManager(storageDir, cmd)
		if err != nil {
			return fmt.Errorf("failed to open AKG database: %w — try 'gmb analyze'", err)
		}
		graph := tm.GetActiveSnapshot()
		if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
			return producterrs.Tagged("no analyzed Architecture Knowledge Graph found — try 'gmb analyze' first", producterrs.ErrEmptySubgraph)
		}

		autoOpen := uiOpenFlag && !uiNoOpenFlag

		server := visualizer.NewServer(graph, visualizer.ServerOptions{
			Host:     uiHostFlag,
			Port:     uiPortFlag,
			AutoOpen: autoOpen,
			RootDir:  rootDir,
		})

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		port, err := server.Start(ctx)
		if err != nil {
			return fmt.Errorf("failed to start visualizer server: %w", err)
		}

		nodeCount := graph.Nodes.Len()
		edgeCount := 0
		if graph.OutboundEdges != nil {
			edgeCount = graph.OutboundEdges.Len()
		}

		fmt.Println(views.RenderUIServerStart(uiHostFlag, port, nodeCount, edgeCount))

		<-ctx.Done()
		fmt.Println("\nShutting down visualizer server...")
		return server.Stop()
	},
}

func init() {
	uiCmd.Flags().IntVarP(&uiPortFlag, "port", "p", 8080, "Port to listen on")
	uiCmd.Flags().StringVar(&uiHostFlag, "host", "127.0.0.1", "Host address to bind to")
	uiCmd.Flags().BoolVarP(&uiOpenFlag, "open", "o", true, "Automatically open default web browser")
	uiCmd.Flags().BoolVar(&uiNoOpenFlag, "no-open", false, "Do not open web browser automatically")

	rootCmd.AddCommand(uiCmd)
}
