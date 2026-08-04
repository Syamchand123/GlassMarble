package views

import (
	"strings"
	"testing"
)

// RenderDoctorUninitialized and RenderStatusUninitialized must carry the
// "Uninitialized" marker plus the resolved TTL path so CLI users know exactly
// which database directory is missing (§5 Phase 1 non-interactive fallback).
func TestRenderDoctorUninitialized(t *testing.T) {
	out := RenderDoctorUninitialized(`/repo/.glassmarble/akg_state.ttl`)
	if !strings.Contains(out, "Uninitialized") {
		t.Errorf("missing Uninitialized marker:\n%s", out)
	}
	if !strings.Contains(out, "akg_state.ttl") {
		t.Errorf("missing ttl path:\n%s", out)
	}
}

func TestRenderStatusUninitialized(t *testing.T) {
	out := RenderStatusUninitialized(`/repo/.glassmarble/akg_state.ttl`)
	if !strings.Contains(out, "Status: Uninitialized") {
		t.Errorf("missing status header:\n%s", out)
	}
	if !strings.Contains(out, "/repo/.glassmarble/akg_state.ttl") {
		t.Errorf("missing analyzed path:\n%s", out)
	}
}

// RenderStatus must preserve the exact line prefixes the CLI tests assert on.
func TestRenderStatusPreservesPrefixes(t *testing.T) {
	s := StatusData{
		Initialized:   true,
		StorageDir:    "/repo/.glassmarble",
		SchemaVersion: 1,
		GraphVersion:  3,
		CommitHash:    "abc123",
		LastAnalysis:  "now",
		NodeCount:     42,
		EdgeCount:     17,
		IndexedFiles:  6,
		Entrypoints:   2,
		VirtualCount:  0,
		FreshnessOK:   true,
	}
	out := RenderStatus(s)
	for _, want := range []string{
		"Nodes Count:   42",
		"Schema Version: 1",
		"Graph Version: 3",
		"Entrypoints:   2",
		"Virtual Nodes: 0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status view missing %q:\n%s", want, out)
		}
	}
}
