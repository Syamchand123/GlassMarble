package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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
	uiJSONFlag   bool
)

// uiJSON is the startup document emitted by `gmb ui --json`. The server is a
// long-running process with no natural result, so the machine-readable answer
// is "where did it bind and what is it serving": one complete document written
// once the listener is up, after which the process keeps serving as usual.
type uiJSON struct {
	Status     string    `json:"status"`
	URL        string    `json:"url"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	PID        int       `json:"pid"`
	RootDir    string    `json:"root_dir"`
	StorageDir string    `json:"storage_dir"`
	Nodes      int       `json:"nodes"`
	Edges      int       `json:"edges"`
	AutoOpen   bool      `json:"auto_open"`
	StartedAt  time.Time `json:"started_at"`
}

var uiCmd = &cobra.Command{
	Use:     "ui",
	Aliases: []string{"serve"},
	GroupID: GroupVisualize.ID,
	Short:   "Launch local interactive architecture visualizer web server",
	Long: `Starts a lightweight, zero-dependency local web server serving an interactive
2D/3D force-directed architecture graph with real-time node inspection, search,
and blast-radius simulation.

With --json the bound address and graph size are written to stdout as a single
JSON document as soon as the listener is up; the server then keeps running, so
a supervising script can read the document, learn the URL, and leave the
process serving.`,
	Example: `  # Launch interactive visualizer and open default browser
  gmb ui

  # Serve on custom port without automatically opening browser
  gmb ui --port 3000 --no-open

  # Equivalent alias
  gmb serve -p 8080

  # Discover the bound URL from a script (auto-assigned port)
  gmb ui --port 0 --no-open --json`,
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

		// A browser popping open is human-facing behaviour; a JSON consumer
		// gets the URL from the document instead.
		autoOpen := uiOpenFlag && !uiNoOpenFlag && !uiJSONFlag

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

		if uiJSONFlag {
			out, _ := json.MarshalIndent(uiJSON{
				Status:     "listening",
				URL:        fmt.Sprintf("http://%s:%d", uiHostFlag, port),
				Host:       uiHostFlag,
				Port:       port,
				PID:        os.Getpid(),
				RootDir:    rootDir,
				StorageDir: storageDir,
				Nodes:      nodeCount,
				Edges:      edgeCount,
				AutoOpen:   autoOpen,
				StartedAt:  time.Now(),
			}, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
		} else {
			fmt.Println(views.RenderUIServerStart(uiHostFlag, port, nodeCount, edgeCount))
		}

		<-ctx.Done()
		if uiJSONFlag {
			// stdout is spoken for by the startup document.
			fmt.Fprintln(cmd.ErrOrStderr(), "Shutting down visualizer server...")
		} else {
			fmt.Println("\nShutting down visualizer server...")
		}
		return server.Stop()
	},
}

func init() {
	uiCmd.Flags().IntVarP(&uiPortFlag, "port", "p", 8080, "Port to listen on")
	uiCmd.Flags().StringVar(&uiHostFlag, "host", "127.0.0.1", "Host address to bind to")
	uiCmd.Flags().BoolVarP(&uiOpenFlag, "open", "o", true, "Automatically open default web browser")
	uiCmd.Flags().BoolVar(&uiNoOpenFlag, "no-open", false, "Do not open web browser automatically")
	uiCmd.Flags().BoolVar(&uiJSONFlag, "json", false, "Emit a machine-readable JSON startup document, then keep serving")

	rootCmd.AddCommand(uiCmd)
}
