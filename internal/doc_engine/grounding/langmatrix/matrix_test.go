package langmatrix

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
)

// goldenMinimums is the per-dimension grade floor per language, recorded
// from the grader's first honest run (B4 rubric: A = grounded, B =
// declared-shallow via N/A sentinel, C = ungrounded). The gate fails on
// REGRESSION (any grade dropping below its floor), never on improvement.
//
// TODO (drives B1/B2 work, lowest grades first):
//   - kotlin/swift/scala are all-C: no tree-sitter grammar module is wired
//     (declaration-only in the GAST registry). Wire grammars to lift them.
//   - css errors (C): CSS has no error construct; needs a deliberate
//     "not applicable" policy, not parser work.
//   - html call-edges/errors (C): the registry wires no call kinds and HTML
//     has no throw construct; inbound-link/deep-script parsing is the real
//     work if we ever want more than C here.
//   - json doc-comments/call-edges/errors/concurrency/tests (C): JSON has
//     no comments, calls, or error/test constructs by design; config (A)
//     and signatures-via-pairs (A) are the honest ceiling for most of them.
//   - css/html concurrency/config/tests (B): sentinel-declared N/A. Real
//     A-grade depth there means cross-file reasoning (e.g. linking HTML to
//     its scripts), not fixture edits.
var goldenMinimums = map[string]map[string]DimensionGrade{
	"go":         {"signatures": "A", "doc-comments": "A", "call-edges": "A", "errors": "A", "concurrency": "A", "config": "A", "tests": "A"},
	"java":       {"signatures": "A", "doc-comments": "A", "call-edges": "A", "errors": "A", "concurrency": "A", "config": "A", "tests": "A"},
	"python":     {"signatures": "A", "doc-comments": "A", "call-edges": "A", "errors": "A", "concurrency": "A", "config": "A", "tests": "A"},
	"javascript": {"signatures": "A", "doc-comments": "A", "call-edges": "A", "errors": "A", "concurrency": "A", "config": "A", "tests": "A"},
	"typescript": {"signatures": "A", "doc-comments": "A", "call-edges": "A", "errors": "A", "concurrency": "A", "config": "A", "tests": "A"},
	"cpp":        {"signatures": "A", "doc-comments": "A", "call-edges": "A", "errors": "A", "concurrency": "A", "config": "A", "tests": "A"},
	"c":          {"signatures": "A", "doc-comments": "A", "call-edges": "A", "errors": "A", "concurrency": "A", "config": "A", "tests": "A"},
	"csharp":     {"signatures": "A", "doc-comments": "A", "call-edges": "A", "errors": "A", "concurrency": "A", "config": "A", "tests": "A"},
	"rust":       {"signatures": "A", "doc-comments": "A", "call-edges": "A", "errors": "A", "concurrency": "A", "config": "A", "tests": "A"},
	"ruby":       {"signatures": "A", "doc-comments": "A", "call-edges": "A", "errors": "A", "concurrency": "A", "config": "A", "tests": "A"},
	"php":        {"signatures": "A", "doc-comments": "A", "call-edges": "A", "errors": "A", "concurrency": "A", "config": "A", "tests": "A"},
	"kotlin":     {"signatures": "C", "doc-comments": "C", "call-edges": "C", "errors": "C", "concurrency": "C", "config": "C", "tests": "C"},
	"swift":      {"signatures": "C", "doc-comments": "C", "call-edges": "C", "errors": "C", "concurrency": "C", "config": "C", "tests": "C"},
	"scala":      {"signatures": "C", "doc-comments": "C", "call-edges": "C", "errors": "C", "concurrency": "C", "config": "C", "tests": "C"},
	"css":        {"signatures": "A", "doc-comments": "A", "call-edges": "A", "errors": "C", "concurrency": "B", "config": "B", "tests": "B"},
	"html":       {"signatures": "A", "doc-comments": "A", "call-edges": "C", "errors": "C", "concurrency": "B", "config": "B", "tests": "B"},
	"json":       {"signatures": "A", "doc-comments": "C", "call-edges": "C", "errors": "C", "concurrency": "C", "config": "A", "tests": "C"},
}

// TestLanguageDepthMatrix is the B4 regression gate: every language must
// meet its per-dimension floor, and the language list must match the GAST
// registry (fails if the registry changes without a fixture update).
func TestLanguageDepthMatrix(t *testing.T) {
	// Registry conformance: langmatrix must track the canonical GAST list.
	regLangs := map[string]bool{}
	for _, spec := range ingest.Registry() {
		regLangs[string(spec.Lang)] = true
	}
	for _, lang := range Languages() {
		if !regLangs[lang] {
			t.Errorf("langmatrix language %q not in GAST ingest.Registry()", lang)
		}
	}
	if len(Languages()) != len(regLangs) {
		t.Errorf("language count mismatch: langmatrix has %d, GAST registry has %d %v",
			len(Languages()), len(regLangs), sortedKeys(regLangs))
	}

	reports := GradeAll()
	if len(reports) != len(Languages()) {
		t.Fatalf("GradeAll returned %d reports, want %d", len(reports), len(Languages()))
	}
	for _, lang := range Languages() {
		floor, ok := goldenMinimums[lang]
		if !ok {
			t.Errorf("no golden minimums recorded for language %q", lang)
			continue
		}
		rep := reports[lang]
		for _, dim := range Dimensions {
			got := rep.Grades[dim]
			if !AtLeast(got, floor[dim]) {
				t.Errorf("REGRESSION %s/%s: got %q, floor is %q", lang, dim, got, floor[dim])
			}
		}
		// Grammar-wired fixtures must parse cleanly; unparseable fixtures
		// grade C everywhere, which the floors above would also catch, but
		// this pinpoints the cause.
		if rep.HasGrammar && !rep.ParseOK {
			t.Errorf("language %q has a grammar but its fixture does not parse cleanly", lang)
		}
	}
	t.Logf("\n%s", MarkdownReport(reports))
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// TestMarkdownReport checks the matrix renders every language and every
// dimension (for future docs/CI use).
func TestMarkdownReport(t *testing.T) {
	md := MarkdownReport(GradeAll())
	for _, lang := range Languages() {
		if !strings.Contains(md, "| "+lang+" |") {
			t.Errorf("MarkdownReport missing row for language %q", lang)
		}
	}
	for _, dim := range Dimensions {
		if !strings.Contains(md, dim) {
			t.Errorf("MarkdownReport missing dimension column %q", dim)
		}
	}
	// One header row (at string start) + one row per language.
	rows := strings.Count(md, "\n| ")
	if rows != len(Languages()) {
		t.Errorf("MarkdownReport has %d language rows, want %d", rows, len(Languages()))
	}
}
