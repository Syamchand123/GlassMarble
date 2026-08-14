// Package stages_test exercises the GlassMarble analysis pipeline phases 1-4
// and their storage backends directly (no CLI). Every test builds its own
// sandbox via harness.NewSandbox and calls the phase APIs in-process.
package stages_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// slashed renders a string with forward slashes so golden matches hold on
// every platform (phase outputs use native separators on Windows).
func slashed(s string) string {
	return strings.ReplaceAll(s, "\\", "/")
}

// lookupPath finds a map entry by its slash-normalized key. Phase outputs
// key maps by RelPath, which is native-separated on Windows ("cmd\api\main.go")
// while tests reason in forward-slash form.
func lookupPath[V any](m map[string]V, want string) (V, bool) {
	if v, ok := m[want]; ok {
		return v, true
	}
	for k, v := range m {
		if filepath.ToSlash(k) == want {
			return v, true
		}
	}
	var zero V
	return zero, false
}

// newSampleSandbox builds a sandbox pre-populated with the canonical sample
// project fixture (Go API + service + repo + cache, Python, JS, vendored and
// generated files, hidden dir, oversized doc).
func newSampleSandbox(t *testing.T) *harness.Sandbox {
	t.Helper()
	sb := harness.NewSandbox(t)
	sb.SampleProject()
	return sb
}

// runAnalysisPipeline executes the ingest-to-aggregate pipeline over the sandbox contents and
// returns every intermediate output plus the list of ingested file paths
// (suitable as phase 4 modifiedFiles).
func runAnalysisPipeline(t *testing.T, sb *harness.Sandbox, commitHash string) (*ingest.IngestOutput, *normalize.NormalizeOutput, *aggregate.AggregateOutput, []string) {
	t.Helper()
	out, err := ingest.RunIngestion(ingest.DefaultConfig(sb.Root))
	if err != nil {
		t.Fatalf("ingest.RunIngestion: %v", err)
	}
	payload, err := normalize.Normalize(out, commitHash)
	if err != nil {
		t.Fatalf("normalize.Normalize: %v", err)
	}
	agg, err := aggregate.Aggregate(payload, nil, sb.Root)
	if err != nil {
		t.Fatalf("aggregate.Aggregate: %v", err)
	}
	modified := make([]string, 0, len(out.Updated))
	for _, res := range out.Updated {
		modified = append(modified, res.RelPath)
	}
	return out, payload, agg, modified
}

// runPipeline executes the full ingest-to-link pipeline over a fresh sample sandbox
// at the requested level of detail and returns the linked CPG delta.
func runPipeline(t *testing.T, sb *harness.Sandbox, commitHash, level string) *link.LinkOutput {
	t.Helper()
	_, _, agg, modified := runAnalysisPipeline(t, sb, commitHash)
	linked, err := link.Link(agg, modified, akg.NewCodePropertyGraph(commitHash), link.LinkerConfig{LevelOfDetail: level})
	if err != nil {
		t.Fatalf("link.Link(%q): %v", level, err)
	}
	return linked
}
