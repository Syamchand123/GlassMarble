package views

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine"
)

// RenderDoctorUninitialized and RenderStatusUninitialized must carry the
// "Uninitialized" marker plus the resolved state path so CLI users know
// exactly which database directory is missing (§5 Phase 1 non-interactive
// fallback).
func TestRenderDoctorUninitialized(t *testing.T) {
	out := RenderDoctorUninitialized(`/repo/.glassmarble/akg.json`)
	if !strings.Contains(out, "Uninitialized") {
		t.Errorf("missing Uninitialized marker:\n%s", out)
	}
	if !strings.Contains(out, "akg.json") {
		t.Errorf("missing state path:\n%s", out)
	}
}

func TestRenderStatusUninitialized(t *testing.T) {
	out := RenderStatusUninitialized(`/repo/.glassmarble/akg.json`)
	if !strings.Contains(out, "Status: Uninitialized") {
		t.Errorf("missing status header:\n%s", out)
	}
	if !strings.Contains(out, "/repo/.glassmarble/akg.json") {
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
			t.Errorf("status view missing %q:\n%s", out, want)
		}
	}
}

// TestWrapText verifies long diagnostic lines wrap instead of truncating, so
// `gmb ai doctor` never hides the failure cause behind an ellipsis.
func TestWrapText(t *testing.T) {
	in := "connectivity ping to \"nvidia/nemotron-3-super-120b-a12b\" failed: request to https://integrate.api.nvidia.com/v1/chat/completions failed: context deadline exceeded"
	lines := wrapText(in, 30)
	if len(lines) < 3 {
		t.Fatalf("expected multiple wrapped lines, got %d", len(lines))
	}
	joined := strings.Join(lines, " ")
	for _, tok := range []string{"nemotron-3-super", "context deadline exceeded"} {
		if !strings.Contains(joined, tok) {
			t.Errorf("wrapped text lost %q:\n%s", tok, joined)
		}
	}
	for _, l := range lines {
		if len(l) > 32 {
			t.Errorf("wrapped line too long (%d): %q", len(l), l)
		}
	}
}

// TestWrapTextSingleToken verifies over-long unbroken tokens are hard-split
// rather than dropped.
func TestWrapTextSingleToken(t *testing.T) {
	lines := wrapText("https://integrate.api.nvidia.com/v1/chat/completions", 12)
	if len(lines) < 2 {
		t.Fatalf("expected a hard-split token, got %v", lines)
	}
	joined := strings.Join(lines, "")
	if !strings.Contains(joined, "integrate.api.nvidia.com") {
		t.Errorf("token content lost after hard split: %s", joined)
	}
}

// TestRenderAIDoctorWrapsProblems verifies the doctor card shows the full
// error text even when it exceeds the card width.
func TestRenderAIDoctorWrapsProblems(t *testing.T) {
	rep := &ai_engine.DoctorReport{
		Provider:    "nvidia",
		DisplayName: "NVIDIA NIM",
		Model:       "nvidia/nemotron-3-super-120b-a12b",
		ConfigValid: true,
		Problems: []string{
			"connectivity ping to \"nvidia/nemotron-3-super-120b-a12b\" failed: request to https://integrate.api.nvidia.com/v1/chat/completions failed: context deadline exceeded",
		},
		PingStatus: "failed",
		AKGExists:  false,
	}
	out := RenderAIDoctor(rep, "nvap...meOq")
	if !strings.Contains(out, "context deadline exceeded") {
		t.Errorf("doctor card lost the root-cause text:\n%s", out)
	}
	if !strings.Contains(out, "Config valid") || !strings.Contains(out, "PASS") {
		t.Errorf("config must stay PASS when only connectivity failed:\n%s", out)
	}
	if !strings.Contains(out, "FAIL") || !strings.Contains(out, "Ping") {
		t.Errorf("ping failure must be shown:\n%s", out)
	}
}
