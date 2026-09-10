// Plan A5 — Go native fuzzing for the devex snippet extractor (plus the
// grounding permalink regex as the second ReDoS surface).
//
// Scope: test-only. No production source changes.
//
// Invariants checked on every fuzz input (arbitrary bytes):
//  1. Never panics, never hangs: VerifyCodeSnippets, the snippetTagRe scan,
//     and the permalink heal path each complete within a 1s ReDoS budget.
//  2. Determinism: repeated verification returns identical results.
//  3. Round-trip: fixed valid fenced blocks always extract (regex finds the
//     expected block count and verification is stable).
package devex

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/grounding"
)

// snippetFuzzValidDocs pins the extraction round-trip: each doc must yield
// exactly wantBlocks snippetTagRe matches on every fuzz iteration.
var snippetFuzzValidDocs = []struct {
	name       string
	doc        string
	wantBlocks int
}{
	{
		name:       "bare_statement",
		doc:        "# Guide\n<!-- gmb:snippet:example -->\n```go\nGreet()\n```\n",
		wantBlocks: 1,
	},
	{
		name:       "package_main",
		doc:        "# Guide\n<!-- gmb:snippet:example -->\n```go\npackage main\nimport \"fmt\"\nfunc main(){fmt.Println(\"hi\")}\n```\n",
		wantBlocks: 1,
	},
}

// snippetFuzzSeeds covers valid blocks, CRLF, nested/malformed fences,
// unicode/zero-width content, untagged fences, multi-block docs, long code,
// backtick floods, and permalink-adjacent markdown. Minimum 15 seeds.
var snippetFuzzSeeds = []string{
	"# Guide\n<!-- gmb:snippet:example -->\n```go\npackage main\nimport \"fmt\"\nfunc main(){fmt.Println(\"hi\")}\n```\n",
	"# Guide\n<!-- gmb:snippet:example -->\n```go\nGreet()\n```\n",
	"# Guide\r\n<!-- gmb:snippet:example -->\r\n```go\r\nGreet()\r\n```\r\n",
	"# T\n<!-- gmb:snippet:example -->\n```go\nfmt.Println(\"inner\")\n```\nouter ``` trailing\n",
	"# T\n<!-- gmb:snippet:example -->\n```go\nfunc main() {\n",
	"# No tag here\n```go\nfmt.Println(\"untagged\")\n```\n",
	"# Two\n<!-- gmb:snippet:example -->\n```go\nOne()\n```\n<!-- gmb:snippet:example -->\n```go\nTwo()\n```\n",
	"# \u65e5\u672c\u8a9e \U0001F600\n<!-- gmb:snippet:example -->\n```go\n// \u30b3\u30e1\u30f3\u30c8 caf\u00e9\nGreet()\n```\n",
	"# T\n<!-- gmb:snippet:example -->\n```go\n⁠Greet()\n```\n",
	"# T\n<!-- gmb:snippet:example -->\n```go\n```\n",
	"# T\n<!-- gmb:snippet:example -->\n```\nfmt.Println(\"no-lang\")\n```\n",
	"# T\n<!--gmb:snippet:example-->\n```go\nTight()\n```\n",
	"# T\n<!-- gmb:snippet:example -->\n```go\n" + strings.Repeat("line := 1\n", 200) + "```\n",
	"# T\n<!-- gmb:snippet:example -->\n```go\ncode\n```\n" + strings.Repeat("`", 500) + "\ntext\n",
	"# T\n<!-- gmb:snippet:example -->\n```go\nfunc main( { broken syntax\n```\n",
	"See [ValidateToken](internal/auth/jwt.go#L42-L89).\n<!-- gmb:snippet:example -->\n```go\nGreet()\n```\n",
	"",
	"\x00\xff\xfe ``` \r\x00 <!-- gmb:snippet:example -->",
}

// snippetFuzzPermalinkGraph is a minimal symbol table so the permalink heal
// path (and its regex) is genuinely exercised on every fuzz input.
func snippetFuzzPermalinkGraph() *akg.CodePropertyGraph {
	g := akg.NewCodePropertyGraph("snippet-fuzz")
	g.Nodes = g.Nodes.Set("internal/auth/jwt.go::ValidateToken", &link.ResolvedNode{
		ID:   "internal/auth/jwt.go::ValidateToken",
		Name: "ValidateToken",
		FileSpec: link.LocationMeta{
			Path:      "internal/auth/jwt.go",
			LineStart: 50,
			LineEnd:   65,
		},
	})
	return g
}

func FuzzSnippetExtract(f *testing.F) {
	for _, s := range snippetFuzzSeeds {
		f.Add(s)
	}
	graph := snippetFuzzPermalinkGraph()
	f.Fuzz(func(t *testing.T, input string) {
		// 1a. Verification must never panic or hang (ReDoS guard).
		start := time.Now()
		errs, err := VerifyCodeSnippets(input, nil)
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("ReDoS guard: VerifyCodeSnippets took %v on %d-byte input", elapsed, len(input))
		}
		if err != nil {
			t.Fatalf("VerifyCodeSnippets returned error: %v", err)
		}
		// 1b. Raw snippetTagRe scan has the same budget.
		normalized := normalizeSnippetNewlines(input)
		start = time.Now()
		_ = snippetTagRe.FindAllStringSubmatchIndex(normalized, -1)
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("ReDoS guard: snippetTagRe took %v on %d-byte input", elapsed, len(input))
		}
		// 1c. Permalink regex heal path has the same budget.
		start = time.Now()
		_ = grounding.UpdatePermalinksInMarkdown(input, graph)
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("ReDoS guard: permalink heal took %v on %d-byte input", elapsed, len(input))
		}
		// 2. Verification is deterministic.
		again, _ := VerifyCodeSnippets(input, nil)
		if !reflect.DeepEqual(errs, again) {
			t.Fatalf("non-deterministic verification:\nfirst:  %+v\nsecond: %+v", errs, again)
		}
		// 3. Valid fenced blocks round-trip: the fixed valid docs always
		// extract the expected block count with stable results.
		for _, vd := range snippetFuzzValidDocs {
			m := snippetTagRe.FindAllStringSubmatchIndex(normalizeSnippetNewlines(vd.doc), -1)
			if len(m) != vd.wantBlocks {
				t.Fatalf("%s: expected %d snippet block(s), got %d", vd.name, vd.wantBlocks, len(m))
			}
			first, _ := VerifyCodeSnippets(vd.doc, nil)
			second, _ := VerifyCodeSnippets(vd.doc, nil)
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("%s: unstable verification: %+v vs %+v", vd.name, first, second)
			}
		}
	})
}

// TestSnippetNestedFenceIgnored pins the existing non-greedy behavior: an
// inner ``` inside the fenced body closes the block early, the capture holds
// exactly the inner content, and the trailing outer fence is ignored —
// without hanging or erroring.
func TestSnippetNestedFenceIgnored(t *testing.T) {
	doc := "# T\n<!-- gmb:snippet:example -->\n```go\nfmt.Println(\"inner\")\n```\nouter ``` trailing\n"
	m := snippetTagRe.FindAllStringSubmatchIndex(doc, -1)
	if len(m) != 1 {
		t.Fatalf("expected exactly 1 snippet block, got %d", len(m))
	}
	if got := doc[m[0][2]:m[0][3]]; got != "fmt.Println(\"inner\")\n" {
		t.Fatalf("unexpected nested-fence capture: %q", got)
	}
	first, err := VerifyCodeSnippets(doc, nil)
	if err != nil {
		t.Fatalf("VerifyCodeSnippets error: %v", err)
	}
	second, _ := VerifyCodeSnippets(doc, nil)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("non-deterministic nested-fence result: %+v vs %+v", first, second)
	}
	if len(first) != 0 {
		t.Fatalf("expected no snippet errors for valid nested-fence content, got %+v", first)
	}
}

// TestSnippetCRLFFound pins CRLF parity: a CRLF working-tree checkout must
// extract the identical block and report identical errors as LF.
func TestSnippetCRLFFound(t *testing.T) {
	lf := "# Guide\n<!-- gmb:snippet:example -->\n```go\npkg.UnknownThing()\n```\n"
	crlf := strings.ReplaceAll(lf, "\n", "\r\n")
	mLF := snippetTagRe.FindAllStringSubmatchIndex(normalizeSnippetNewlines(lf), -1)
	mCRLF := snippetTagRe.FindAllStringSubmatchIndex(normalizeSnippetNewlines(crlf), -1)
	if len(mLF) != 1 || len(mCRLF) != 1 {
		t.Fatalf("expected 1 block in both LF (%d) and CRLF (%d)", len(mLF), len(mCRLF))
	}
	errsLF, err := VerifyCodeSnippets(lf, map[string]bool{"Known": true})
	if err != nil {
		t.Fatalf("LF verify error: %v", err)
	}
	errsCRLF, err := VerifyCodeSnippets(crlf, map[string]bool{"Known": true})
	if err != nil {
		t.Fatalf("CRLF verify error: %v", err)
	}
	if len(errsCRLF) == 0 {
		t.Fatalf("CRLF extraction silently found zero blocks (LF found %d errors)", len(errsLF))
	}
	if !reflect.DeepEqual(errsLF, errsCRLF) {
		t.Fatalf("CRLF/LF parity broken:\nLF:   %+v\nCRLF: %+v", errsLF, errsCRLF)
	}
}

// TestRegexTimeBox1MB asserts the snippet regex and the permalink regex both
// complete on ~1MB adversarial inputs well within the test timeout, catching
// catastrophic backtracking. Go's RE2 engine matches in linear time, so both
// budgets (10s) should pass with wide margin.
func TestRegexTimeBox1MB(t *testing.T) {
	const budget = 10 * time.Second

	snipAdv := "<!-- gmb:snippet:example -->\n```go\n" +
		strings.Repeat("a", 1<<20) +
		strings.Repeat("[", 1<<16) +
		"\n```\n" + strings.Repeat("```", 1000)
	start := time.Now()
	_ = snippetTagRe.FindAllStringSubmatchIndex(snipAdv, -1)
	if elapsed := time.Since(start); elapsed > budget {
		t.Fatalf("snippet regex took %v on 1MB adversarial input (budget %v)", elapsed, budget)
	}
	start = time.Now()
	if _, err := VerifyCodeSnippets(snipAdv, nil); err != nil {
		t.Fatalf("VerifyCodeSnippets error on 1MB input: %v", err)
	}
	if elapsed := time.Since(start); elapsed > budget {
		t.Fatalf("VerifyCodeSnippets took %v on 1MB adversarial input (budget %v)", elapsed, budget)
	}

	graph := snippetFuzzPermalinkGraph()
	healed := grounding.UpdatePermalinksInMarkdown(
		"See [ValidateToken](internal/auth/jwt.go#L1-L2) for details.", graph)
	if healed != "See [ValidateToken](internal/auth/jwt.go#L50-L65) for details." {
		t.Fatalf("permalink did not self-heal: %q", healed)
	}
	linkAdv := strings.Repeat("[ValidateToken](internal/auth/jwt.go#L1-L2) ", 15000) +
		strings.Repeat("[", 1<<20) + strings.Repeat("a", 1<<17)
	start = time.Now()
	_ = grounding.UpdatePermalinksInMarkdown(linkAdv, graph)
	if elapsed := time.Since(start); elapsed > budget {
		t.Fatalf("permalink regex took %v on 1MB adversarial input (budget %v)", elapsed, budget)
	}
}
