package main

import (
	stderrors "errors"
	"fmt"
	"testing"

	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
)

// TestExitCodeFor pins the exit-code contract documented in docs/cli.md.
//
// Code 5 (policy violation) exists so CI can tell "the gate found problems"
// apart from "the tool crashed or was invoked wrongly": lint violations, drift
// over budget, doctor integrity failures, impact over threshold and an
// exceeded bench budget all returned 1 before, indistinguishable from a crash.
func TestExitCodeFor(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil-ish generic failure", stderrors.New("boom"), 1},
		{"entry point missing", producterrs.Tagged("no entry", producterrs.ErrEntryMissing), 2},
		{"entry point not found", producterrs.Tagged("bad entry", producterrs.ErrEntryNotFound), 2},
		{"empty subgraph", producterrs.Tagged("empty", producterrs.ErrEmptySubgraph), 3},
		{"render limit", producterrs.Tagged("too big", producterrs.ErrRenderLimit), 4},
		{"policy violation", producterrs.Tagged("12 violations", producterrs.ErrPolicyViolation), 5},
		{"validation stays generic", producterrs.Tagged("bad flag", producterrs.ErrValidation), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCodeFor(tc.err); got != tc.want {
				t.Errorf("exitCodeFor(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestPolicyViolationSurvivesWrapping ensures the classification is not lost
// when a command wraps the error on its way out.
func TestPolicyViolationSurvivesWrapping(t *testing.T) {
	base := producterrs.Tagged("3 violations", producterrs.ErrPolicyViolation)
	wrapped := fmt.Errorf("lint failed: %w", base)
	if got := exitCodeFor(wrapped); got != 5 {
		t.Errorf("wrapped policy violation = %d, want 5", got)
	}
}
