package verifier

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRefRepo creates a temp repo root populated with repo-relative files.
func writeRefRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func refBrokenTargets(rep ReferenceReport) []string {
	var out []string
	for _, b := range rep.Broken {
		out = append(out, b.Target)
	}
	return out
}

func TestGitHubSlug(t *testing.T) {
	cases := map[string]string{
		"Getting Started":        "getting-started",
		"Hello, World! (v2)":     "hello-world-v2",
		"Café au lait":           "café-au-lait",
		"foo_bar":                "foo_bar",
		"a  b":                   "a--b",
		"API_Reference (v2.0)":   "api_reference-v20",
		"Ünïcodé—dash":           "ünïcodédash",
		"Already-slugged string": "already-slugged-string",
		"dots...gone":            "dotsgone",
		"under_score kept":       "under_score-kept",
	}
	for in, want := range cases {
		if got := GitHubSlug(in); got != want {
			t.Errorf("GitHubSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckReferences_ValidDocClean(t *testing.T) {
	root := writeRefRepo(t, map[string]string{
		"docs/guide.md": "# Guide\n\n## Getting Started\n\nSee [other](other.md), [section](#getting-started),\n" +
			"[`ValidateToken`](other.md), [code](other.md#L2), and bare other.md#L3.\n",
		"docs/other.md": "# Other\n\nline2\nline3\n",
	})
	rep := CheckReferences(root, "docs/guide.md",
		"# Guide\n\n## Getting Started\n\nSee [other](other.md), [section](#getting-started),\n"+
			"[`ValidateToken`](other.md), [code](other.md#L2), and bare other.md#L3.\n",
		func(s string) bool { return s == "ValidateToken" }, false)
	if len(rep.Broken) != 0 {
		t.Fatalf("expected clean report, got %+v", rep.Broken)
	}
	if rep.CheckedLinks == 0 {
		t.Error("expected CheckedLinks > 0")
	}
	if rep.CheckedAnchors == 0 {
		t.Error("expected CheckedAnchors > 0")
	}
}

func TestCheckReferences_MissingFile(t *testing.T) {
	root := writeRefRepo(t, map[string]string{
		"docs/guide.md": "x",
	})
	md := "# Guide\n\nSee [gone](nope.md).\n\nMore [alsogone](../other/nope2.md#frag).\n"
	rep := CheckReferences(root, "docs/guide.md", md, nil, false)
	if len(rep.Broken) != 2 {
		t.Fatalf("expected 2 broken refs, got %+v", rep.Broken)
	}
	// Deterministic document order, 1-based lines.
	if rep.Broken[0].Line != 3 || rep.Broken[1].Line != 5 {
		t.Errorf("expected lines [3 5], got [%d %d]", rep.Broken[0].Line, rep.Broken[1].Line)
	}
	for _, b := range rep.Broken {
		if b.Source != "docs/guide.md" {
			t.Errorf("expected Source docs/guide.md, got %q", b.Source)
		}
		if b.Reason != "file not found" {
			t.Errorf("expected reason %q, got %q", "file not found", b.Reason)
		}
	}
}

func TestCheckReferences_BadAnchor(t *testing.T) {
	root := writeRefRepo(t, map[string]string{
		"docs/other.md": "# Other\n\n## Real Heading\n",
	})
	md := "# Guide\n\nSee [bad](other.md#nope).\n\nMissing file skips anchor: [m](gone.md#whatever).\n"
	rep := CheckReferences(root, "docs/guide.md", md, nil, false)
	if len(rep.Broken) != 2 {
		t.Fatalf("expected 2 broken refs, got %+v", rep.Broken)
	}
	if rep.Broken[0].Target != "other.md#nope" || rep.Broken[0].Reason != "anchor not found" {
		t.Errorf("unexpected first broken: %+v", rep.Broken[0])
	}
	// Missing file reports "file not found" exactly once (anchor skipped).
	if rep.Broken[1].Target != "gone.md#whatever" || rep.Broken[1].Reason != "file not found" {
		t.Errorf("unexpected second broken: %+v", rep.Broken[1])
	}
}

func TestCheckReferences_SlugEdgeCases(t *testing.T) {
	root := writeRefRepo(t, map[string]string{
		"docs/guide.md": "x",
	})
	// Heading "## Hello, World! (v2)" slugs to "hello-world-v2".
	md := "# Guide\n\n## Hello, World! (v2)\n\n" +
		"Bad single-hyphen guess: [x](#hello-world-goodbye-v2).\n" +
		"Wrong case anchor: [y](#Hello-World-V2).\n" +
		"Exact slug: [z](#hello-world-v2).\n"
	rep := CheckReferences(root, "docs/guide.md", md, nil, false)
	targets := refBrokenTargets(rep)
	if len(targets) != 2 {
		t.Fatalf("expected 2 broken anchors, got %+v", rep.Broken)
	}
	if targets[0] != "#hello-world-goodbye-v2" || targets[1] != "#Hello-World-V2" {
		t.Errorf("unexpected broken targets: %v", targets)
	}
	for _, b := range rep.Broken {
		if b.Reason != "anchor not found" {
			t.Errorf("expected anchor reason, got %+v", b)
		}
	}
}

func TestCheckReferences_PermalinkOutOfRange(t *testing.T) {
	root := writeRefRepo(t, map[string]string{
		"docs/other.md": "one\ntwo\nthree\n", // 3 lines
	})
	md := "# Guide\n\nLink [a](other.md#L99).\nLink [b](other.md#L0).\n" +
		"Link [c](other.md#L3-L1).\nLink [d](#L99).\n" +
		"Valid [e](other.md#L3) and [f](other.md#L1-L3) and [g](#L1).\n"
	rep := CheckReferences(root, "docs/guide.md", md, nil, false)
	if len(rep.Broken) != 4 {
		t.Fatalf("expected 4 broken permalinks, got %+v", rep.Broken)
	}
	wantReason := map[string]string{
		"other.md#L99":   "line out of range",
		"other.md#L0":    "line out of range",
		"other.md#L3-L1": "invalid line range",
		"#L99":           "line out of range",
	}
	for _, b := range rep.Broken {
		want, ok := wantReason[b.Target]
		if !ok {
			t.Errorf("unexpected broken permalink: %+v", b)
			continue
		}
		if b.Reason != want {
			t.Errorf("target %q: reason %q, want %q", b.Target, b.Reason, want)
		}
	}
}

func TestCheckReferences_BarePermalink(t *testing.T) {
	root := writeRefRepo(t, map[string]string{
		"docs/other.md": "one\ntwo\nthree\n",
	})
	md := "# Guide\n\nSee other.md#L2 (valid) and other.md#L99 (bad).\n"
	rep := CheckReferences(root, "docs/guide.md", md, nil, false)
	if len(rep.Broken) != 1 {
		t.Fatalf("expected 1 broken bare permalink, got %+v", rep.Broken)
	}
	b := rep.Broken[0]
	if b.Target != "other.md#L99" || b.Reason != "line out of range" || b.Line != 3 {
		t.Errorf("unexpected bare permalink broken: %+v", b)
	}
}

func TestValidateFrontmatter(t *testing.T) {
	withTitle := "---\ntitle: Guide\n---\n\n# Guide\n"
	full := "---\ntitle: Guide\nsidebar_position: 2\n---\n\n# Guide\n"
	noKeys := "---\nlayout: doc\n---\n\n# Guide\n"
	plain := "# Guide\n\nNo frontmatter.\n"
	unterminated := "---\ntitle: Guide\n\n# Guide\n"

	cases := []struct {
		name     string
		md       string
		platform string
		wantErr  bool
	}{
		{"vitepress ok", withTitle, "vitepress", false},
		{"vitepress missing", noKeys, "vitepress", true},
		{"docusaurus ok", full, "docusaurus", false},
		{"docusaurus missing sidebar", withTitle, "docusaurus", true},
		{"mkdocs ok", withTitle, "mkdocs", false},
		{"mkdocs missing", noKeys, "mkdocs", true},
		{"github_flat ignores keys", noKeys, "github_flat", false},
		{"unknown platform nil", noKeys, "confluence", false},
		{"no frontmatter nil", plain, "vitepress", false},
		{"unterminated vitepress", unterminated, "vitepress", true},
		{"unterminated github_flat still fails", unterminated, "github_flat", true},
	}
	for _, c := range cases {
		errs := ValidateFrontmatter(c.md, c.platform)
		if c.wantErr && len(errs) == 0 {
			t.Errorf("%s: expected errors, got nil", c.name)
		}
		if !c.wantErr && len(errs) != 0 {
			t.Errorf("%s: expected nil, got %v", c.name, errs)
		}
	}
}

func TestCheckTOC(t *testing.T) {
	matching := "<!-- toc -->\n\n- [A](#a)\n- [B](#b)\n\n# Doc\n\n## A\n\n## B\n"
	mismatch := "<!-- toc -->\n\n- [A](#a)\n- [Ghost](#ghost)\n\n# Doc\n\n## A\n"
	contentsMarker := "## Table of Contents\n\n- [A](#a)\n- [Ghost](#nope)\n\n# Doc\n\n## A\n"
	noMarker := "# Doc\n\n- [Ghost](#ghost)\n\n## A\n"

	if errs := CheckTOC(matching); len(errs) != 0 {
		t.Errorf("matching TOC: expected nil, got %v", errs)
	}
	if errs := CheckTOC(mismatch); len(errs) != 1 {
		t.Errorf("mismatched TOC: expected 1 error, got %v", errs)
	} else if !strings.Contains(errs[0], "#ghost") {
		t.Errorf("mismatched TOC error should name anchor, got %q", errs[0])
	}
	if errs := CheckTOC(contentsMarker); len(errs) != 1 {
		t.Errorf("contents-marker TOC: expected 1 error, got %v", errs)
	}
	if errs := CheckTOC(noMarker); errs != nil {
		t.Errorf("absent TOC: expected nil, got %v", errs)
	}
}

func TestCheckReferences_ExternalIgnoredWhenFalse(t *testing.T) {
	root := writeRefRepo(t, map[string]string{
		"docs/guide.md": "x",
	})
	md := "# Guide\n\nSee [web](https://example.com/) and [local](http://127.0.0.1:9/nope).\n"
	rep := CheckReferences(root, "docs/guide.md", md, nil, false)
	if len(rep.Broken) != 0 {
		t.Fatalf("externals must be ignored when false, got %+v", rep.Broken)
	}
	if rep.CheckedLinks != 0 {
		t.Errorf("ignored externals must not count as checked links, got %d", rep.CheckedLinks)
	}
}

func TestCheckReferences_UnroutableSkippedWhenTrue(t *testing.T) {
	root := writeRefRepo(t, map[string]string{
		"docs/guide.md": "x",
	})
	md := "# Guide\n\nSee [dead](http://127.0.0.1:9/unroutable-gmb).\n"
	rep := CheckReferences(root, "docs/guide.md", md, nil, true)
	if len(rep.Broken) != 0 {
		t.Fatalf("unroutable URL must be skipped silently, got %+v", rep.Broken)
	}
}

func TestCheckReferences_ExternalStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/boom":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	root := writeRefRepo(t, map[string]string{
		"docs/guide.md": "x",
	})
	md := "# Guide\n\nSee [ok](" + srv.URL + "/ok), [gone](" + srv.URL + "/missing), [bad](" + srv.URL + "/boom).\n"
	rep := CheckReferences(root, "docs/guide.md", md, nil, true)
	if len(rep.Broken) != 2 {
		t.Fatalf("expected 2 broken externals, got %+v", rep.Broken)
	}
	if !strings.Contains(rep.Broken[0].Reason, "404") {
		t.Errorf("expected 404 reason, got %+v", rep.Broken[0])
	}
	if !strings.Contains(rep.Broken[1].Reason, "500") {
		t.Errorf("expected 500 reason, got %+v", rep.Broken[1])
	}
}

func TestCheckReferences_FencedAndInlineCodeIgnored(t *testing.T) {
	root := writeRefRepo(t, map[string]string{
		"docs/guide.md": "x",
		"docs/other.md": strings.Repeat("line\n", 150),
	})
	md := "# Guide\n\n```md\n[a](nope.md)\n[b](other.md#nope)\n```\n\n" +
		"~~~md\n[c](nope2.md)\n~~~\n\n" +
		"Inline `[d](nope3.md)` is code, not a link.\n\n" +
		"Bare other.md#L99 inside `code other.md#L99 span` is ignored.\n"
	rep := CheckReferences(root, "docs/guide.md", md, nil, false)
	if len(rep.Broken) != 0 {
		t.Fatalf("fenced/inline links must be ignored, got %+v", rep.Broken)
	}
}

func TestCheckReferences_SymbolVerification(t *testing.T) {
	root := writeRefRepo(t, map[string]string{
		"docs/other.md": "# Other\n",
	})
	md := "# Guide\n\nSee [`KnownSym`](other.md) and [`GhostSym`](other.md).\n"
	exists := func(s string) bool { return s == "KnownSym" }

	rep := CheckReferences(root, "docs/guide.md", md, exists, false)
	if len(rep.Broken) != 1 {
		t.Fatalf("expected 1 unknown symbol, got %+v", rep.Broken)
	}
	b := rep.Broken[0]
	if b.Target != "GhostSym" || !strings.Contains(b.Reason, "unknown symbol") || b.Line != 3 {
		t.Errorf("unexpected symbol broken: %+v", b)
	}

	// nil symbolExists skips symbol verification entirely.
	repNil := CheckReferences(root, "docs/guide.md", md, nil, false)
	if len(repNil.Broken) != 0 {
		t.Fatalf("nil symbolExists must skip symbols, got %+v", repNil.Broken)
	}
}

// ── Gap C2: TOC regeneration ──

func TestRegenerateTOC_CommentMarkerRewrite(t *testing.T) {
	md := "# Guide\n\n<!-- toc -->\n\n- [Stale](#gone)\n- [Old](#old)\n\n## Getting Started\n\nBody.\n\n### Install\n\nMore.\n"
	got := RegenerateTOC(md)
	want := "# Guide\n\n<!-- toc -->\n\n- [Guide](#guide)\n  - [Getting Started](#getting-started)\n    - [Install](#install)\n\n## Getting Started\n\nBody.\n\n### Install\n\nMore.\n"
	if got != want {
		t.Errorf("TOC rewrite mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	// Idempotence: a second regeneration is a no-op.
	if again := RegenerateTOC(got); again != got {
		t.Errorf("RegenerateTOC not idempotent:\n--- first ---\n%s\n--- second ---\n%s", got, again)
	}
}

func TestRegenerateTOC_ContentsHeadingRewrite(t *testing.T) {
	md := "# Guide\n\n## Contents\n\n- [Stale](#gone)\n\n## Alpha\n\nText.\n\n## Beta\n\nText.\n"
	got := RegenerateTOC(md)
	if strings.Contains(got, "#gone") {
		t.Errorf("stale TOC entry survived regeneration:\n%s", got)
	}
	if !strings.Contains(got, "- [Alpha](#alpha)") || !strings.Contains(got, "- [Beta](#beta)") {
		t.Errorf("regenerated TOC missing headings:\n%s", got)
	}
	// The Contents heading itself must never be listed.
	if strings.Contains(got, "#contents") {
		t.Errorf("TOC must not list itself:\n%s", got)
	}
	if again := RegenerateTOC(got); again != got {
		t.Errorf("RegenerateTOC not idempotent:\n--- first ---\n%s\n--- second ---\n%s", got, again)
	}
}

func TestRegenerateTOC_TableOfContentsHeading(t *testing.T) {
	md := "# Guide\n\n## Table of Contents\n\n## Alpha\n"
	got := RegenerateTOC(md)
	if !strings.Contains(got, "- [Alpha](#alpha)") {
		t.Errorf("## Table of Contents not treated as TOC marker:\n%s", got)
	}
	if again := RegenerateTOC(got); again != got {
		t.Errorf("RegenerateTOC not idempotent:\n--- first ---\n%s\n--- second ---\n%s", got, again)
	}
}

func TestRegenerateTOC_NoMarkerPassthrough(t *testing.T) {
	md := "# Guide\n\n## Alpha\n\n- [Alpha](#alpha)\n"
	if got := RegenerateTOC(md); got != md {
		t.Errorf("document without TOC marker must return unchanged:\n--- got ---\n%s\n--- want ---\n%s", got, md)
	}
}

func TestRegenerateTOC_SlugDedupAndFences(t *testing.T) {
	md := "# Guide\n\n<!-- toc -->\n\n## Repeat\n\n## Repeat\n\n```md\n## Not A Heading\n```\n"
	got := RegenerateTOC(md)
	if !strings.Contains(got, "- [Repeat](#repeat)") || !strings.Contains(got, "- [Repeat](#repeat-1)") {
		t.Errorf("repeated headings need GitHub -1 dedup suffixes:\n%s", got)
	}
	if strings.Contains(got, "not-a-heading") {
		t.Errorf("fenced code must never contribute TOC entries:\n%s", got)
	}
	if again := RegenerateTOC(got); again != got {
		t.Errorf("RegenerateTOC not idempotent:\n--- first ---\n%s\n--- second ---\n%s", got, again)
	}
}
