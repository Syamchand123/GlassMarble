package stages_test

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestPipelineDeltaIncremental verifies that modifying a single file
// and running delta ingestion only re-parses that file. This is the
// incremental pipeline guarantee.
func TestPipelineDeltaIncremental(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.PolyglotProject()
	sb.GitInit()
	sb.GitCommit("initial")
	// Modify one file
	sb.WriteFile("internal/service/service.go", sb.ReadFile("internal/service/service.go")+"\n// tweak\n")
	commit := sb.GitCommit("tweak service")
	diff, err := ingest.CollectGitDiff(sb.Root, commit)
	if err != nil {
		t.Fatalf("CollectGitDiff: %v", err)
	}
	if len(diff) != 1 {
		t.Fatalf("delta after single-file tweak: got %d tasks, want 1: %v", len(diff), diff)
	}
	if diff[0].RelPath != "internal/service/service.go" {
		t.Errorf("delta RelPath = %q, want internal/service/service.go", diff[0].RelPath)
	}
	cfg := ingest.DefaultConfig(sb.Root)
	out, err := ingest.RunIngestionForDelta(cfg, diff)
	if err != nil {
		t.Fatalf("RunIngestionForDelta: %v", err)
	}
	if len(out.Updated) != 1 || out.Updated[0].RelPath != "internal/service/service.go" {
		t.Errorf("delta Updated = %v, want single service.go", out.Updated)
	}
}

// TestPipelineConfigExclusions verifies that config.yaml exclusions
// (hidden, vendor, generated) are honored even for polyglot projects.
func TestPipelineConfigExclusions(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.PolyglotProject()
	sb.WriteFile(".hidden/secret.go", "package hidden\nfunc Secret() {}")
	sb.WriteFile("vendor/foo/bar.go", "package foo\nfunc Bar() {}")
	cfg := ingest.DefaultConfig(sb.Root)
	out, err := ingest.RunIngestion(cfg)
	if err != nil {
		t.Fatalf("RunIngestion exclusions: %v", err)
	}
	got := updatedPaths(out)
	for _, bad := range []string{".hidden/secret.go", "vendor/foo/bar.go"} {
		if got[bad] {
			t.Errorf("excluded file %q should not be in Updated", bad)
		}
	}
	if !strings.Contains(strings.Join(out.Skipped, "\n"), "hidden") && !strings.Contains(strings.Join(out.Warnings, "\n"), "hidden") {
		t.Logf("no explicit hidden skip message (informational), skipped=%v warnings=%v", out.Skipped, out.Warnings)
	}
}

// TestPipelineLinkLevelVariants verifies that link levels (architecture,
// standard, full) all produce valid linked outputs without panic.
func TestPipelineLinkLevelVariants(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.SampleProject()
	sb.GitInit()
	for _, level := range []string{"architecture", "standard", "full"} {
		t.Run(level, func(t *testing.T) {
			out, err := harness.RunGmb(t, sb, "analyze", "--link-level", level, "--json")
			if err != nil {
				t.Fatalf("analyze --link-level %s: %v\n%s", level, err, out)
			}
			if !strings.Contains(out, "\"nodes\"") {
				t.Errorf("link-level %s json missing nodes: %s", level, out)
			}
		})
	}
}
