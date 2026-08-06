package errors

import (
	"errors"
	"testing"
)

func TestSentinelClassification(t *testing.T) {
	sentinels := []error{
		ErrValidation, ErrEmptySubgraph, ErrEntryMissing,
		ErrEntryNotFound, ErrScopeEmpty, ErrRenderLimit,
	}
	for _, kind := range sentinels {
		err := Tagged("user-visible message", kind)
		if got := err.Error(); got != "user-visible message" {
			t.Errorf("Tagged(%v) message = %q, want %q", kind, got, "user-visible message")
		}
		if !errors.Is(err, kind) {
			t.Errorf("errors.Is(err, %v) = false, want true", kind)
		}
		for _, other := range sentinels {
			if other != kind && errors.Is(err, other) {
				t.Errorf("errors.Is(err, %v) = true for a differently-classified error", other)
			}
		}
	}
}

func TestTaggedNilKind(t *testing.T) {
	err := Tagged("plain", nil)
	if err == nil || err.Error() != "plain" {
		t.Fatalf("Tagged with nil kind = %v, want plain message error", err)
	}
	for _, kind := range []error{ErrValidation, ErrEmptySubgraph} {
		if errors.Is(err, kind) {
			t.Errorf("plain error classifies as %v", kind)
		}
	}
}

func TestAnnotatePreservesChainAndMessage(t *testing.T) {
	inner := errors.New("inner failure")
	kind := ErrValidation
	err := Annotate(inner, kind)

	if err.Error() != "inner failure" {
		t.Errorf("Annotate changed message to %q", err.Error())
	}
	if !errors.Is(err, inner) {
		t.Errorf("errors.Is(err, inner) = false, want true (chain must survive)")
	}
	if !errors.Is(err, kind) {
		t.Errorf("errors.Is(err, %v) = false, want true", kind)
	}
	if errors.Is(err, ErrEmptySubgraph) {
		t.Errorf("Annotate leaked a wrong classification")
	}
}

func TestAnnotateEdgeCases(t *testing.T) {
	if err := Annotate(nil, ErrValidation); err != nil {
		t.Errorf("Annotate(nil, kind) = %v, want nil", err)
	}
	if err := Annotate(errors.New("x"), nil); err == nil || err.Error() != "x" {
		t.Errorf("Annotate(err, nil) = %v, want unchanged error", err)
	}
}

func TestSentinelValuesAreDistinct(t *testing.T) {
	seen := make(map[string]bool)
	for _, kind := range []error{
		ErrValidation, ErrEmptySubgraph, ErrEntryMissing,
		ErrEntryNotFound, ErrScopeEmpty, ErrRenderLimit,
	} {
		if seen[kind.Error()] {
			t.Errorf("duplicate sentinel message %q", kind.Error())
		}
		seen[kind.Error()] = true
	}
}
