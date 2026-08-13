package main

import (
	"context"
	stderrors "errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/Syamchand123/GlassMarble/cmd"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cmd.ExecuteContext(ctx); err != nil {
		// Fang already rendered the styled error to stderr.
		os.Exit(exitCodeFor(err))
	}
}

// exitCodeFor maps the error taxonomy to the exit codes documented in
// cmd/visualize.go: 1 validation (and any other failure), 2 entry point
// missing/not found, 3 empty subgraph, 4 render limit.
func exitCodeFor(err error) int {
	switch {
	case stderrors.Is(err, producterrs.ErrEntryMissing), stderrors.Is(err, producterrs.ErrEntryNotFound):
		return 2
	case stderrors.Is(err, producterrs.ErrEmptySubgraph):
		return 3
	case stderrors.Is(err, producterrs.ErrRenderLimit):
		return 4
	default:
		return 1
	}
}
