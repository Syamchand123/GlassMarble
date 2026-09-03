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

// exitCodeFor maps the error taxonomy to stable exit codes:
//
//	1  any other failure (crash, bad usage, I/O)
//	2  entry point missing / not found
//	3  empty subgraph
//	4  render limit exceeded
//	5  policy violation - the command ran fine and found problems
//
// Code 5 lets CI distinguish "the gate found issues" from "the tool broke";
// lint, drift, doctor, impact --threshold and analyze --bench all returned 1
// for both cases before.
func exitCodeFor(err error) int {
	switch {
	case stderrors.Is(err, producterrs.ErrEntryMissing), stderrors.Is(err, producterrs.ErrEntryNotFound):
		return 2
	case stderrors.Is(err, producterrs.ErrEmptySubgraph):
		return 3
	case stderrors.Is(err, producterrs.ErrRenderLimit):
		return 4
	case stderrors.Is(err, producterrs.ErrPolicyViolation):
		return 5
	default:
		return 1
	}
}
