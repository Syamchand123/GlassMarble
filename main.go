package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Syamchand123/GlassMarble/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cmd.ExecuteContext(ctx); err != nil {
		// Fang already rendered the styled error to stderr.
		os.Exit(1)
	}
}
