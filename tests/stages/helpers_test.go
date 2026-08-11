// Package stages_test exercises the GlassMarble analysis pipeline stages 1-4
// and their storage backends directly (no CLI). Every test builds its own
// sandbox via harness.NewSandbox and calls the stage APIs in-process.
package stages_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// slashed renders a string with forward slashes so golden matches hold on
// every platform (stage outputs use native separators on Windows).
func slashed(s string) string {
	return strings.ReplaceAll(s, "\\", "/")
}

// lookupPath finds a map entry by its slash-normalized key. Stage outputs
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

// runStages123 executes the stage 1-3 pipeline over the sandbox contents and
// returns every intermediate output plus the list of ingested file paths
// (suitable as stage 4 modifiedFiles).
func runStages123(t *testing.T, sb *harness.Sandbox, commitHash string) (*stage1.StageOutput, *stage2.Stage2Payload, *stage3.Stage3Output, []string) {
	t.Helper()
	out, err := stage1.RunIngestion(stage1.DefaultConfig(sb.Root))
	if err != nil {
		t.Fatalf("stage1.RunIngestion: %v", err)
	}
	payload, err := stage2.Normalize(out, commitHash)
	if err != nil {
		t.Fatalf("stage2.Normalize: %v", err)
	}
	agg, err := stage3.Aggregate(payload, nil, sb.Root)
	if err != nil {
		t.Fatalf("stage3.Aggregate: %v", err)
	}
	modified := make([]string, 0, len(out.Updated))
	for _, res := range out.Updated {
		modified = append(modified, res.RelPath)
	}
	return out, payload, agg, modified
}

// runPipeline executes the full stage 1-4 pipeline over a fresh sample sandbox
// at the requested level of detail and returns the linked CPG delta.
func runPipeline(t *testing.T, sb *harness.Sandbox, commitHash, level string) *stage4.Stage4Output {
	t.Helper()
	_, _, agg, modified := runStages123(t, sb, commitHash)
	linked, err := stage4.Link(agg, modified, akg.NewCodePropertyGraph(commitHash), stage4.LinkerConfig{LevelOfDetail: level})
	if err != nil {
		t.Fatalf("stage4.Link(%q): %v", level, err)
	}
	return linked
}
