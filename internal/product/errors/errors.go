// Package errors defines the GlassMarble error taxonomy
// (master_overhaul_plan.md §4.4, workbook W0-06). Every error a CLI/TUI
// surface can show belongs to one of the stable sentinels below so callers
// can classify failures with errors.Is without string matching.
//
// Two constructors preserve the exact user-facing message while attaching a
// sentinel for classification:
//
//	err := errors.Tagged("unsupported diagram type 'x'", ErrValidation)
//	err := errors.Annotate(innerErr, ErrValidation)
//
// Both report true for errors.Is(err, ErrValidation) and print the original
// message unchanged.
package errors

import stderrors "errors"

// Error taxonomy sentinels (W0-06). The Error() text of each sentinel is a
// stable classification label; user-facing messages are attached via Tagged
// or Annotate so they never change.
var (
	// ErrValidation marks invalid CLI arguments, flags, formats, or config.
	ErrValidation = stderrors.New("validation error")

	// ErrEmptySubgraph marks a graph extraction/analysis that produced no
	// nodes or edges where output was expected.
	ErrEmptySubgraph = stderrors.New("empty subgraph")

	// ErrEntryMissing marks a required entry point/start node that was not
	// supplied or could not be derived.
	ErrEntryMissing = stderrors.New("entry point missing")

	// ErrEntryNotFound marks an explicitly requested entry point/ID that does
	// not exist in the graph.
	ErrEntryNotFound = stderrors.New("entry point not found")

	// ErrScopeEmpty marks a scope (file/folder) that matched no nodes.
	ErrScopeEmpty = stderrors.New("scope empty")

	// ErrRenderLimit marks a renderer node/edge budget exceeded (MaxNodes,
	// MaxDepth, or renderer-specific limits).
	ErrRenderLimit = stderrors.New("render limit exceeded")
)

// classified carries a user-facing message plus its classification sentinel
// and an optional underlying error. Error() returns the message verbatim so
// migration to the taxonomy never changes what users see.
type classified struct {
	msg   string
	kind  error
	inner error
}

func (c classified) Error() string { return c.msg }

// Unwrap exposes the underlying error (or the sentinel itself when there is
// no underlying error), so errors.Is reaches both the chain and the kind.
func (c classified) Unwrap() error {
	if c.inner != nil {
		return c.inner
	}
	return c.kind
}

func (c classified) Is(target error) bool { return c.kind == target }

// Tagged builds a classified error whose message is exactly msg and which
// classifies as kind via errors.Is.
func Tagged(msg string, kind error) error {
	if kind == nil {
		return stderrors.New(msg)
	}
	return classified{msg: msg, kind: kind}
}

// Annotate classifies an existing error without changing its message or
// chain: errors.Is(err, kind) and errors.Is(err, inner) both hold.
func Annotate(err error, kind error) error {
	if err == nil {
		return nil
	}
	if kind == nil {
		return err
	}
	return classified{msg: err.Error(), kind: kind, inner: err}
}
