package patcher

import (
	"strings"
	"testing"
)

// ────────────────────────────────────────────────────────────────────────────
// ParseMarkdown tests
// ────────────────────────────────────────────────────────────────────────────

func TestParseMarkdown_PureHuman(t *testing.T) {
	src := "# Title\n\nSome human text.\n"
	doc := ParseMarkdown(src)
	if len(doc.Zones) != 1 {
		t.Fatalf("expected 1 zone, got %d", len(doc.Zones))
	}
	if doc.Zones[0].Kind != ZoneHuman {
		t.Error("expected human zone")
	}
}

func TestParseMarkdown_SingleManagedZone(t *testing.T) {
	src := `# Title

<!-- gmb:begin:overview -->
This is generated.
<!-- gmb:end:overview -->

Human footer.
`
	doc := ParseMarkdown(src)
	// Expect: human, managed, human
	if len(doc.Zones) != 3 {
		t.Fatalf("expected 3 zones, got %d: %v", len(doc.Zones), zonesDebug(doc))
	}
	if doc.Zones[0].Kind != ZoneHuman {
		t.Error("zone 0 should be human")
	}
	if doc.Zones[1].Kind != ZoneManaged || doc.Zones[1].SectionID != "overview" {
		t.Errorf("zone 1 should be managed/overview, got %+v", doc.Zones[1])
	}
	if doc.Zones[2].Kind != ZoneHuman {
		t.Error("zone 2 should be human")
	}
}

func TestParseMarkdown_FrozenZone(t *testing.T) {
	src := `<!-- gmb:begin:locked -->
<!-- gmb:freeze -->
Never touch this.
<!-- gmb:end:locked -->
`
	doc := ParseMarkdown(src)
	found := false
	for _, z := range doc.Zones {
		if z.SectionID == "locked" {
			found = true
			if z.Kind != ZoneFrozen {
				t.Errorf("expected frozen zone, got kind %d", z.Kind)
			}
		}
	}
	if !found {
		t.Error("locked zone not found")
	}
}

func TestParseMarkdown_UnclosedAnchorRecovery(t *testing.T) {
	// Unclosed anchor should not panic; content falls into human zone.
	src := "<!-- gmb:begin:broken -->\nsome content\n"
	doc := ParseMarkdown(src) // must not panic
	if doc == nil {
		t.Fatal("nil ParsedDoc")
	}
}

func TestParseMarkdown_MultipleManagedZones(t *testing.T) {
	src := `Intro.
<!-- gmb:begin:a -->
Section A.
<!-- gmb:end:a -->
Middle.
<!-- gmb:begin:b -->
Section B.
<!-- gmb:end:b -->
End.
`
	doc := ParseMarkdown(src)
	var managed []string
	for _, z := range doc.Zones {
		if z.Kind == ZoneManaged {
			managed = append(managed, z.SectionID)
		}
	}
	if len(managed) != 2 {
		t.Fatalf("expected 2 managed zones, got %d", len(managed))
	}
	if managed[0] != "a" || managed[1] != "b" {
		t.Errorf("wrong section IDs: %v", managed)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Reconstruct tests
// ────────────────────────────────────────────────────────────────────────────

func TestReconstruct_RoundTrip(t *testing.T) {
	src := "# Title\n\n<!-- gmb:begin:s1 -->\nGenerated.\n<!-- gmb:end:s1 -->\n\nFoo.\n"
	doc := ParseMarkdown(src)
	got := Reconstruct(doc.Zones)
	if got != src {
		t.Errorf("round-trip failed.\nwant: %q\ngot:  %q", src, got)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// ManagedZone lookup
// ────────────────────────────────────────────────────────────────────────────

func TestManagedZone_Found(t *testing.T) {
	src := "<!-- gmb:begin:sec -->\nBody.\n<!-- gmb:end:sec -->\n"
	doc := ParseMarkdown(src)
	z := ManagedZone(doc, "sec")
	if z == nil {
		t.Fatal("zone not found")
	}
	if z.SectionID != "sec" {
		t.Errorf("unexpected section ID: %q", z.SectionID)
	}
}

func TestManagedZone_NotFound(t *testing.T) {
	doc := ParseMarkdown("# Just human.\n")
	z := ManagedZone(doc, "nonexistent")
	if z != nil {
		t.Error("expected nil for unknown section")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// ExtractBody
// ────────────────────────────────────────────────────────────────────────────

func TestExtractBody(t *testing.T) {
	src := "<!-- gmb:begin:s -->\nLine1.\nLine2.\n<!-- gmb:end:s -->\n"
	doc := ParseMarkdown(src)
	z := ManagedZone(doc, "s")
	body := ExtractBody(z)
	if !strings.Contains(body, "Line1.") || !strings.Contains(body, "Line2.") {
		t.Errorf("body missing expected lines: %q", body)
	}
	if strings.Contains(body, "gmb:begin") || strings.Contains(body, "gmb:end") {
		t.Error("anchor markers should be stripped from body")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// parseBegin / parseEnd
// ────────────────────────────────────────────────────────────────────────────

func TestParseBegin(t *testing.T) {
	cases := []struct {
		line string
		id   string
		ok   bool
	}{
		{"<!-- gmb:begin:overview -->", "overview", true},
		{"  <!-- gmb:begin:foo-bar -->  ", "foo-bar", true},
		{"<!-- gmb:end:overview -->", "", false},
		{"# Heading", "", false},
		{"<!-- not-gmb -->", "", false},
	}
	for _, c := range cases {
		id, ok := parseBegin(c.line)
		if ok != c.ok || id != c.id {
			t.Errorf("parseBegin(%q) = (%q, %v), want (%q, %v)", c.line, id, ok, c.id, c.ok)
		}
	}
}

func TestParseEnd(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"<!-- gmb:end:overview -->", true},
		{"<!-- gmb:end -->", true},
		{"<!-- gmb:begin:x -->", false},
		{"# Heading", false},
	}
	for _, c := range cases {
		got := parseEnd(c.line)
		if got != c.want {
			t.Errorf("parseEnd(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Directive parsing
// ────────────────────────────────────────────────────────────────────────────

func TestParseDirective(t *testing.T) {
	cases := []struct {
		line  string
		key   string
		value string
		ok    bool
	}{
		{"<!-- gmb:freeze -->", "freeze", "", true},
		{"<!-- gmb:instruction: Do this. -->", "instruction", "Do this.", true},
		{"<!-- gmb:todo: Fill in errors. -->", "todo", "Fill in errors.", true},
		{"# Not a directive", "", "", false},
	}
	for _, c := range cases {
		key, val, ok := parseDirective(c.line)
		if ok != c.ok || key != c.key || val != c.value {
			t.Errorf("parseDirective(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.line, key, val, ok, c.key, c.value, c.ok)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Human zone invariant
// ────────────────────────────────────────────────────────────────────────────

// TestHumanZonesPreserved: reconstructed doc must contain all human text unchanged.
func TestHumanZonesPreserved(t *testing.T) {
	humanText := "This is tribal knowledge.\n<!-- some-other-comment -->\nNever touch me.\n"
	src := humanText + "<!-- gmb:begin:machine -->\nOld content.\n<!-- gmb:end:machine -->\n"
	doc := ParseMarkdown(src)

	// Simulate replacing the managed zone.
	result := ApplyToDoc(doc, "machine", "New generated content.")

	if !strings.Contains(result, "tribal knowledge") {
		t.Error("human text 'tribal knowledge' was lost")
	}
	if !strings.Contains(result, "Never touch me") {
		t.Error("human text 'Never touch me' was lost")
	}
	if !strings.Contains(result, "New generated content") {
		t.Error("new machine content was not applied")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

func zonesDebug(doc *ParsedDoc) []string {
	var out []string
	for _, z := range doc.Zones {
		out = append(out, formatZone(z))
	}
	return out
}

func formatZone(z Zone) string {
	kind := "human"
	switch z.Kind {
	case ZoneManaged:
		kind = "managed"
	case ZoneFrozen:
		kind = "frozen"
	}
	return kind + ":" + z.SectionID
}

// ────────────────────────────────────────────────────────────────────────────
// A1 goldmark AST parser tests
// ────────────────────────────────────────────────────────────────────────────

// TestParseMarkdown_FencedCodeMarkersIgnored: gmb markers inside fenced code
// blocks must never be treated as directives/anchors — every zone is human.
func TestParseMarkdown_FencedCodeMarkersIgnored(t *testing.T) {
	src := "# Doc\n\n```markdown\n<!-- gmb:begin:fake -->\n<!-- gmb:freeze -->\nFake body.\n<!-- gmb:end:fake -->\n```\n\nReal text.\n"
	doc := ParseMarkdown(src)
	for _, z := range doc.Zones {
		if z.Kind != ZoneHuman {
			t.Errorf("expected only human zones, got %+v", z)
		}
		if z.SectionID != "" {
			t.Errorf("human zone must not carry a section ID, got %q", z.SectionID)
		}
	}
	if z := ManagedZone(doc, "fake"); z != nil {
		t.Errorf("marker inside fenced code must not create a zone, got %+v", z)
	}
	if got := Reconstruct(doc.Zones); got != src {
		t.Errorf("round-trip failed.\nwant: %q\ngot:  %q", src, got)
	}
}

// TestParseMarkdown_InlineCodeSpanIgnored: gmb-looking text in an inline
// code span is not a marker.
func TestParseMarkdown_InlineCodeSpanIgnored(t *testing.T) {
	src := "# T\n\nSome `<!-- gmb:begin:x -->` code.\n\nText.\n"
	doc := ParseMarkdown(src)
	if len(doc.Zones) != 1 || doc.Zones[0].Kind != ZoneHuman {
		t.Fatalf("expected a single human zone, got %v", zonesDebug(doc))
	}
	if z := ManagedZone(doc, "x"); z != nil {
		t.Errorf("code-span text must not create a zone, got %+v", z)
	}
}

// TestParseMarkdown_IndentedCodeIgnored: a 4-space indented marker is an
// indented code block, not an anchor.
func TestParseMarkdown_IndentedCodeIgnored(t *testing.T) {
	src := "# T\n\n    <!-- gmb:begin:indented -->\n\nText.\n"
	doc := ParseMarkdown(src)
	if z := ManagedZone(doc, "indented"); z != nil {
		t.Errorf("indented-code text must not create a zone, got %+v", z)
	}
	if got := Reconstruct(doc.Zones); got != src {
		t.Errorf("round-trip failed.\nwant: %q\ngot:  %q", src, got)
	}
}

// TestParseMarkdown_NestedFencesDoNotCorrupt: nested fences plus a real zone
// — only the real zone is extracted and the document round-trips exactly.
func TestParseMarkdown_NestedFencesDoNotCorrupt(t *testing.T) {
	src := "````\n```\n<!-- gmb:begin:x -->\n```\n````\n\n<!-- gmb:begin:real -->\nBody.\n<!-- gmb:end:real -->\n"
	doc := ParseMarkdown(src)
	if z := ManagedZone(doc, "x"); z != nil {
		t.Errorf("marker inside nested fences must not create a zone, got %+v", z)
	}
	z := ManagedZone(doc, "real")
	if z == nil {
		t.Fatalf("real zone not found; zones: %v", zonesDebug(doc))
	}
	if body := ExtractBody(z); !strings.Contains(body, "Body.") {
		t.Errorf("unexpected body: %q", body)
	}
	if got := Reconstruct(doc.Zones); got != src {
		t.Errorf("round-trip failed.\nwant: %q\ngot:  %q", src, got)
	}
}

// TestParseMarkdown_UnclosedFenceRecovery: an unclosed fence swallows the
// rest of the file per CommonMark, so markers inside it are code — recovery
// is human-only zones with an exact round-trip (no corruption, no panic).
func TestParseMarkdown_UnclosedFenceRecovery(t *testing.T) {
	src := "# T\n\n```go\nfunc main() {}\n\n<!-- gmb:begin:s -->\nBody.\n<!-- gmb:end:s -->\n"
	doc := ParseMarkdown(src) // must not panic
	for _, z := range doc.Zones {
		if z.Kind != ZoneHuman {
			t.Errorf("expected only human zones under unclosed fence, got %+v", z)
		}
	}
	if got := Reconstruct(doc.Zones); got != src {
		t.Errorf("round-trip failed.\nwant: %q\ngot:  %q", src, got)
	}
}

// TestParseMarkdown_CRLFNormalized: CRLF input parses into correct zones and
// reconstructs to the LF-normalized form.
func TestParseMarkdown_CRLFNormalized(t *testing.T) {
	src := "# Title\r\n\r\n<!-- gmb:begin:s -->\r\nBody.\r\n<!-- gmb:end:s -->\r\nTail.\r\n"
	doc := ParseMarkdown(src)
	z := ManagedZone(doc, "s")
	if z == nil {
		t.Fatalf("managed zone not found; zones: %v", zonesDebug(doc))
	}
	if body := ExtractBody(z); !strings.Contains(body, "Body.") {
		t.Errorf("unexpected body: %q", body)
	}
	want := strings.ReplaceAll(src, "\r\n", "\n")
	if got := Reconstruct(doc.Zones); got != want {
		t.Errorf("CRLF round-trip failed.\nwant: %q\ngot:  %q", want, got)
	}
	if strings.Contains(Reconstruct(doc.Zones), "\r") {
		t.Error("reconstructed output must not contain CR characters")
	}
}

// TestParseMarkdown_GFMTableAndTaskList: tables and task lists (GFM) parse
// alongside a managed zone; zones and round-trip are exact.
func TestParseMarkdown_GFMTableAndTaskList(t *testing.T) {
	src := `# Status

| Name | Value |
| ---- | ----- |
| a    | 1     |

- [ ] open task
- [x] done task

<!-- gmb:begin:tbl -->
| H1 | H2 |
| -- | -- |
| x  | y  |
<!-- gmb:end:tbl -->

Tail with ~~strikethrough~~.
`
	doc := ParseMarkdown(src)
	z := ManagedZone(doc, "tbl")
	if z == nil {
		t.Fatalf("managed zone not found; zones: %v", zonesDebug(doc))
	}
	if z.Kind != ZoneManaged {
		t.Errorf("expected managed zone, got kind %d", z.Kind)
	}
	if got := Reconstruct(doc.Zones); got != src {
		t.Errorf("round-trip failed.\nwant: %q\ngot:  %q", src, got)
	}
}

// TestParseMarkdown_UnclosedAnchorTreatedAsHuman: an unclosed begin anchor
// recovers as human zones only, preserving every byte.
func TestParseMarkdown_UnclosedAnchorTreatedAsHuman(t *testing.T) {
	src := "<!-- gmb:begin:broken -->\nsome content\n"
	doc := ParseMarkdown(src) // must not panic
	if len(doc.Zones) == 0 {
		t.Fatal("expected at least one zone")
	}
	for _, z := range doc.Zones {
		if z.Kind != ZoneHuman {
			t.Errorf("unclosed anchor must recover as human, got %+v", z)
		}
	}
	if z := ManagedZone(doc, "broken"); z != nil {
		t.Errorf("unclosed anchor must not create a managed zone, got %+v", z)
	}
	if got := Reconstruct(doc.Zones); got != src {
		t.Errorf("round-trip failed.\nwant: %q\ngot:  %q", src, got)
	}
}

// TestParseMarkdown_RoundTripIdentity: Parse→Reconstruct is byte-identical
// on a battery of documents, with and without zones.
func TestParseMarkdown_RoundTripIdentity(t *testing.T) {
	cases := []string{
		"# Title\n\nSome human text.\n",
		"# Title\n\nSome human text.", // no trailing newline
		"",
		"<!-- gmb:begin:s1 -->\nGenerated.\n<!-- gmb:end:s1 -->\n",
		"# T\n\n<!-- gmb:begin:a -->\nA.\n<!-- gmb:end:a -->\n\nMid.\n\n<!-- gmb:begin:b -->\n<!-- gmb:freeze -->\nB.\n<!-- gmb:end:b -->\n\nEnd.\n",
		"<!-- gmb:begin:locked -->\n<!-- gmb:pin -->\nNever touch.\n<!-- gmb:end:locked -->\n",
		"# T\n\n<!-- gmb:begin:s -->\n<!-- gmb:instruction: Do this. -->\n<!-- gmb:todo: Fill in. -->\n<!-- gmb:mode:deterministic -->\nBody.\n<!-- gmb:end:s -->\n",
		"# Setext\n===\n\n> <!-- gmb:begin:q -->\n> quoted\n> <!-- gmb:end:q -->\n",
	}
	for i, src := range cases {
		doc := ParseMarkdown(src)
		if got := Reconstruct(doc.Zones); got != src {
			t.Errorf("case %d round-trip failed.\nwant: %q\ngot:  %q", i, src, got)
		}
	}
}

// TestParseMarkdown_DirectivesCaptured: all documented directive keys are
// captured from a managed zone parsed via the AST.
func TestParseMarkdown_DirectivesCaptured(t *testing.T) {
	src := `<!-- gmb:begin:s -->
<!-- gmb:instruction: Do this. -->
<!-- gmb:assert: no-todo -->
<!-- gmb:todo: Fill in errors. -->
<!-- gmb:diagram:callgraph:pkg -->
<!-- gmb:mode:deterministic -->
Body.
<!-- gmb:end:s -->
`
	doc := ParseMarkdown(src)
	z := ManagedZone(doc, "s")
	if z == nil {
		t.Fatalf("managed zone not found; zones: %v", zonesDebug(doc))
	}
	for _, key := range []string{"instruction", "assert", "todo", "diagram", "mode"} {
		if _, ok := z.Directives[key]; !ok {
			t.Errorf("directive %q not captured; got %v", key, z.Directives)
		}
	}
	pd := ProcessDirectives(z.Directives)
	if pd.Instruction != "Do this." || pd.Todo != "Fill in errors." || pd.Mode != "deterministic" {
		t.Errorf("unexpected processed directives: %+v", pd)
	}
}

// TestParseMarkdown_LegacyFallbackEnv: GMB_DOC_PARSER=legacy delegates to
// the old line scanner, which (unlike the AST parser) treats markers inside
// fenced code as anchors.
func TestParseMarkdown_LegacyFallbackEnv(t *testing.T) {
	src := "# Doc\n\n```markdown\n<!-- gmb:begin:fake -->\nFake body.\n<!-- gmb:end:fake -->\n```\n"
	t.Setenv("GMB_DOC_PARSER", "legacy")
	doc := ParseMarkdown(src)
	z := ManagedZone(doc, "fake")
	if z == nil {
		t.Fatalf("legacy parser should treat fenced markers as anchors; zones: %v", zonesDebug(doc))
	}
	if z.Kind != ZoneManaged {
		t.Errorf("expected managed zone under legacy parser, got kind %d", z.Kind)
	}
	// Default (goldmark) path must NOT see that zone.
	t.Setenv("GMB_DOC_PARSER", "")
	if z := ManagedZone(ParseMarkdown(src), "fake"); z != nil {
		t.Errorf("goldmark parser must ignore fenced markers, got %+v", z)
	}
}
