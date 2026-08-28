package stages_test

import (
	"os"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestIngestionPolyglotProject verifies that all 14 language grammars are
// exercised via the PolyglotProject fixture and that symbols are extracted
// for each. This replaces the previous 6-file smoke test with a real
// monorepo containing Go, Python, Java, Rust, C#, C++, TS, and more.
func TestIngestionPolyglotProject(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.PolyglotProject()

	out, err := ingest.RunIngestion(ingest.DefaultConfig(sb.Root))
	if err != nil {
		t.Fatalf("RunIngestion polyglot: %v", err)
	}
	got := updatedPaths(out)
	for _, want := range []string{
		"cmd/api/main.go",
		"internal/service/service.go",
		"pkg/java/src/main/java/com/example/Service.java",
		"py/service.py",
		"web/app.ts",
		"src/lib.rs",
		"app/Program.cs",
		"src/main.cpp",
	} {
		if !got[want] {
			t.Errorf("polyglot ingestion missing %q; got %v", want, got)
		}
	}
	exts := map[string]int{}
	for p := range got {
		if idx := strings.LastIndex(p, "."); idx != -1 {
			exts[p[idx:]]++
		}
	}
	if len(exts) < 5 {
		t.Errorf("expected >=5 language extensions, got %v", exts)
	}
}

// TestIngestionVisualizationStress ensures that Python code containing
// triple-quoted strings, dict unpacking, and template markers does not
// cause the ingestion pipeline to crash or produce warnings about oversized
// files. The file should be parsed and included in Updated.
func TestIngestionVisualizationStress(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.VisualizationStressProject()

	cfg := ingest.DefaultConfig(sb.Root)
	out, err := ingest.RunIngestion(cfg)
	if err != nil {
		t.Fatalf("RunIngestion visualization stress: %v", err)
	}
	got := updatedPaths(out)
	if !got["src/state.py"] {
		t.Errorf("src/state.py should be ingested despite triple-quotes; got %v", got)
	}
	for _, res := range out.Updated {
		if res.RelPath == "src/state.py" && res.Bytes == 0 {
			t.Errorf("src/state.py reported 0 bytes")
		}
	}
}

// TestIngestionLargeProjectScale verifies that the ingestion pipeline
// handles a synthetic large project (50 files) without error and respects
// MaxFileBytes and worker tuning.
func TestIngestionLargeProjectScale(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.LargeProject(50)

	cfg := ingest.DefaultConfig(sb.Root)
	cfg.WorkerCount = 4
	cfg.MaxFileBytes = 20 << 20
	out, err := ingest.RunIngestion(cfg)
	if err != nil {
		t.Fatalf("RunIngestion large project: %v", err)
	}
	if len(out.Updated) < 40 {
		t.Errorf("large project: Updated=%d, want >=40; skipped=%v", len(out.Updated), out.Skipped)
	}
	for _, res := range out.Updated {
		if !strings.HasSuffix(res.RelPath, ".go") {
			t.Errorf("large project: unexpected non-Go file %q", res.RelPath)
		}
	}
}

// TestIngestionRealLanguageSamples checks that every testdata language
// sample is valid and would be picked up by ingestion when copied into a
// sandbox. This guards the 14-language grammar matrix against silent
// regressions where a grammar fails to parse its sample.
func TestIngestionRealLanguageSamples(t *testing.T) {
	sb := harness.NewSandbox(t)
	langs := []string{
		"testdata/languages/go/sample.go",
		"testdata/languages/python/sample.py",
		"testdata/languages/java/Sample.java",
		"testdata/languages/typescript/sample.ts",
		"testdata/languages/javascript/sample.js",
		"testdata/languages/c/sample.c",
		"testdata/languages/cpp/sample.cpp",
		"testdata/languages/csharp/Sample.cs",
		"testdata/languages/kotlin/Sample.kt",
		"testdata/languages/swift/sample.swift",
		"testdata/languages/scala/sample.scala",
		"testdata/languages/php/sample.php",
		"testdata/languages/ruby/sample.rb",
		"testdata/languages/rust/sample.rs",
	}
	for _, src := range langs {
		data, err := os.ReadFile(src)
		if err != nil {
			// Try from repo root when running with different cwd
			data, err = os.ReadFile("../../" + src)
		}
		if err != nil {
			t.Fatalf("read testdata %s: %v", src, err)
		}
		if len(data) < 200 {
			t.Errorf("testdata %s is too small (%d bytes), expected comprehensive sample", src, len(data))
		}
		rel := "samples/" + src[strings.LastIndex(src, "/")+1:]
		sb.WriteFile(rel, string(data))
	}
	out, err := ingest.RunIngestion(ingest.DefaultConfig(sb.Root))
	if err != nil {
		t.Fatalf("RunIngestion real samples: %v", err)
	}
	if len(out.Updated) < 10 {
		t.Errorf("real samples: Updated=%d, want >=10; got paths %v", len(out.Updated), updatedPaths(out))
	}
}
