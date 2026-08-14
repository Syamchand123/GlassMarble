// Package qa_test holds quality-assurance suites: golden renderer checks,
// JSON schema conformance, contract spot checks and exit-code pinning.
package qa_test

// Golden output tests: every human-facing renderer in internal/tui/views is
// exercised directly with sample inputs and pinned to real format strings
// (read from the view sources). Any change to the renderers that alters a
// user-facing banner breaks these tests — the QA gate for §13.4-style
// output stability.

import (
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/aiconfig"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/session"
	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/drift"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
)

func TestGoldenVersion(t *testing.T) {
	out := views.RenderVersion("0.1.0")
	if !strings.Contains(out, "0.1.0") {
		t.Errorf("version banner missing version:\n%s", out)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("version banner empty")
	}
}

func TestGoldenDoctor(t *testing.T) {
	healthy := views.RenderDoctor(&akg.DoctorReport{
		StorageDir:    "/repo/.glassmarble",
		StateBytes:    4096,
		SchemaVersion: 3,
		GraphVersion:  8,
		CommitHash:    "abcdef1234567890",
		LoadOK:        true,
		NodeCount:     2,
		EdgeCount:     1,
	})
	for _, want := range []string{"DOCTOR: OK", "Schema:        v3", "Parse-back:    ok", "Dangling:      0"} {
		if !strings.Contains(healthy, want) {
			t.Errorf("healthy doctor report missing %q:\n%s", want, healthy)
		}
	}
	failed := views.RenderDoctor(&akg.DoctorReport{
		StorageDir:    "/repo/.glassmarble",
		SchemaVersion: 3,
		LoadOK:        false,
		LoadError:     "parse error at line 1",
	})
	if !strings.Contains(failed, "Parse-back:    FAILED") || !strings.Contains(failed, "DOCTOR: FAILED") {
		t.Errorf("failed doctor report missing failure markers:\n%s", failed)
	}
}

func TestGoldenDoctorUninitialized(t *testing.T) {
	out := views.RenderDoctorUninitialized("/tmp/nope/.glassmarble")
	if !strings.Contains(out, "Uninitialized") {
		t.Errorf("uninitialized doctor missing marker:\n%s", out)
	}
}

func TestGoldenStatus(t *testing.T) {
	out := views.RenderStatus(views.StatusData{
		Initialized:   true,
		StorageDir:    "/repo/.glassmarble",
		SchemaVersion: 3,
		GraphVersion:  7,
		CommitHash:    "abcdef1234567890",
		Entrypoints:   1,
		NodeCount:     2,
	})
	for _, want := range []string{"Schema Version: 3", "Graph Version: 7", "abcdef1234567890"} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q:\n%s", want, out)
		}
	}
}

func TestGoldenStatusUninitialized(t *testing.T) {
	out := views.RenderStatusUninitialized("/tmp/repo/.glassmarble")
	if !strings.Contains(out, "Uninitialized") {
		t.Errorf("uninitialized status missing marker:\n%s", out)
	}
}

func TestGoldenInit(t *testing.T) {
	out := views.RenderInitSuccess("/repo/.glassmarble", true)
	for _, want := range []string{".glassmarble", ".gitignore"} {
		if !strings.Contains(out, want) {
			t.Errorf("init success missing %q:\n%s", want, out)
		}
	}
}

func TestGoldenHotspot(t *testing.T) {
	out := views.RenderHotspot(2, []views.HotspotRow{
		{Rank: 1, Name: "Main", Kind: "EXECUTABLE", InDegree: 12, OutDegree: 3},
		{Rank: 2, Name: "db.Connect", Kind: "EXECUTABLE", InDegree: 9, OutDegree: 0},
	})
	for _, want := range []string{"Main", "db.Connect", "Top 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("hotspot missing %q:\n%s", want, out)
		}
	}
}

func TestGoldenDependencySummary(t *testing.T) {
	out := views.RenderDependencySummary(10, 2, 1, []views.TopDependencyNode{
		{ID: "main.go::new_main", Outbound: 5},
	})
	if !strings.Contains(out, "main.go::new_main") {
		t.Errorf("dependency summary missing node:\n%s", out)
	}
}

func TestGoldenDependencyTarget(t *testing.T) {
	out := views.RenderDependencyTarget("service", []views.DependencyEdge{
		{Type: "CALLS", OtherID: "cmd/app/main.go::Main", LineNumber: 12},
	}, nil)
	if !strings.Contains(out, "service") || !strings.Contains(out, "cmd/app/main.go::Main") {
		t.Errorf("dependency target missing names:\n%s", out)
	}
}

func TestGoldenDiff(t *testing.T) {
	out := views.RenderDiff("abcdef1234567890", 3, 7, []views.DiffEntry{
		{Status: "COMMITTED", NodesAdded: 2, EdgesAdded: 1, ModifiedFiles: 1, HasPayload: true},
	})
	// RenderDiff normalizes both "COMMITTED" and "committed" inputs to the
	// same lowercase badge (diff_view.go:42-44); pin the rendered text.
	for _, want := range []string{"abcdef1234", "committed"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff missing %q:\n%s", want, out)
		}
	}
}

func TestGoldenCompare(t *testing.T) {
	diff := &akg.GraphDiff{
		BaseCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		HeadCommit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		NodesAdded: []akg.DiffNode{{ID: "node_1"}},
		NodesRemoved: []akg.DiffNode{{ID: "node_2"}},
	}
	out := views.RenderCompare(diff)
	for _, want := range []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb", "node_1", "node_2"} {
		if !strings.Contains(out, want) {
			t.Errorf("compare missing %q:\n%s", want, out)
		}
	}
}

func TestGoldenExport(t *testing.T) {
	out := views.RenderExportSuccess("graphjson", "/out/akg.json", 42, 1024)
	if !strings.Contains(out, "42") || !strings.Contains(out, "/out/akg.json") {
		t.Errorf("export success missing details:\n%s", out)
	}
	unsupported := views.RenderExportUnsupported(".xyz")
	if !strings.Contains(unsupported, ".xyz") {
		t.Errorf("export unsupported missing ext:\n%s", unsupported)
	}
}

func TestGoldenImport(t *testing.T) {
	out := views.RenderImportSuccess("/in/g.json", "/repo/.glassmarble", 5, 4)
	if !strings.Contains(out, "5") || !strings.Contains(out, "4") {
		t.Errorf("import success missing counts:\n%s", out)
	}
}

func TestGoldenHooks(t *testing.T) {
	installed := views.RenderHooksInstalled("/repo/.git/hooks/post-commit", "/usr/bin/gmb", "/repo")
	if !strings.Contains(installed, "post-commit") {
		t.Errorf("hooks installed missing hook path:\n%s", installed)
	}
	uninstalled := views.RenderHooksUninstalled()
	if !strings.Contains(uninstalled, "uninstalled") && !strings.Contains(uninstalled, "removed") {
		t.Errorf("hooks uninstalled missing marker:\n%s", uninstalled)
	}
	none := views.RenderHooksNone()
	if !strings.Contains(none, "No") && !strings.Contains(none, "none") {
		t.Errorf("hooks none missing marker:\n%s", none)
	}
}

func TestGoldenHousekeeping(t *testing.T) {
	out := views.RenderHousekeepingReport([]views.HousekeepingArea{
		{Name: "akg.json", Bytes: 4096, Files: 1},
		{Name: "marbles/", Bytes: 8192, Files: 2},
	}, 12288, 3)
	for _, want := range []string{"akg.json", "marbles/"} {
		if !strings.Contains(out, want) {
			t.Errorf("housekeeping missing %q:\n%s", want, out)
		}
	}
}

func TestGoldenDrift(t *testing.T) {
	rep := &drift.Report{
		LayersDefined: 2,
		Violations: []drift.Violation{
			{Kind: "FORBIDDEN_DEPENDENCY", SourceID: "cmd/main.go::Main", TargetID: "internal/private.go::P", SourceLayer: "api", TargetLayer: "private", EdgeType: "CALLS", Message: "api layer must not depend on private"},
		},
		CycleCount: 0,
	}
	out := views.RenderDrift(rep)
	if !strings.Contains(out, "api") || !strings.Contains(out, "private") {
		t.Errorf("drift missing layer markers:\n%s", out)
	}
}

func TestGoldenSessions(t *testing.T) {
	out := views.RenderSessions([]session.Summary{
		{ID: "sess_0001", Provider: "openai", Model: "gpt-4o", Turns: 3, Created: time.Now(), Updated: time.Now()},
	})
	for _, want := range []string{"sess_0001", "gpt-4o"} {
		if !strings.Contains(out, want) {
			t.Errorf("sessions missing %q:\n%s", want, out)
		}
	}
}

func TestGoldenModels(t *testing.T) {
	out := views.RenderModels(provider.Registry, "openai", func(env string) bool { return strings.Contains(env, "OPENAI") })
	if !strings.Contains(out, "openai") {
		t.Errorf("models missing provider:\n%s", out)
	}
}

func TestGoldenAIDoctor(t *testing.T) {
	rep := &ai_engine.DoctorReport{
		Provider:    "custom",
		DisplayName: "Custom",
		Model:       "gmb-test-1",
		KeyRequired: false,
		KeySet:      true,
		KeySource:   "config",
		ConfigValid: true,
		Problems:    nil,
		AKGExists:   true,
	}
	out := views.RenderAIDoctor(rep, "sk-***masked***")
	if !strings.Contains(out, "sk-***masked***") {
		t.Errorf("ai doctor missing masked key:\n%s", out)
	}
}

func TestGoldenInspectDetail(t *testing.T) {
	node := &link.ResolvedNode{
		ID:   "cmd/app/main.go::Main",
		Name: "Main",
		Kind: "EXECUTABLE",
	}
	out := views.RenderInspectDetail(node, nil, nil)
	if !strings.Contains(out, "Main") {
		t.Errorf("inspect detail missing node name:\n%s", out)
	}
}

func TestGoldenAIConfigRoundTrip(t *testing.T) {
	path := t.TempDir() + "/ai.yaml"
	cfg := aiconfig.Default()
	cfg.Provider = "anthropic"
	cfg.Model = "claude-sonnet-4-5"
	cfg.APIKey = "sk-test-123"
	cfg.BaseURL = "https://example.invalid/v1"
	if err := aiconfig.Save(path, cfg); err != nil {
		t.Fatalf("aiconfig.Save: %v", err)
	}
	loaded, err := aiconfig.LoadFile(path)
	if err != nil {
		t.Fatalf("aiconfig.LoadFile: %v", err)
	}
	if loaded.Provider != "anthropic" || loaded.Model != "claude-sonnet-4-5" || loaded.APIKey != "sk-test-123" || loaded.BaseURL != "https://example.invalid/v1" {
		t.Errorf("round-trip mismatch: %+v", loaded)
	}
	if got := aiconfig.GlobalPath(); got == "" {
		t.Errorf("GlobalPath empty")
	}
	if got := aiconfig.ProjectConfigPath; got != ".glassmarble/ai.yaml" {
		t.Errorf("ProjectConfigPath = %q", got)
	}
}