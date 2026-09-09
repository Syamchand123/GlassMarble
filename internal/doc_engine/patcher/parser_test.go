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
