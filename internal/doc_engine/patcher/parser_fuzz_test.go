package patcher

import (
	"strings"
	"testing"
)

// FuzzParseMarkdown fuzzes the goldmark markdown zone parser (gap A5a).
// Invariants, on every input (never gated on input shape):
//
//  1. Never panics; ParseMarkdown always returns non-nil.
//  2. Round-trip: Reconstruct(Parse(x).Zones) is byte-identical to the
//     LF-normalized input (the parser documents CRLF/CR → LF
//     normalization, so equality is asserted against the normalized form
//     unconditionally — zones tile 1..n exactly, modulo that documented
//     normalization).
//  3. Zone-count stability: parsing the reconstruction yields the same
//     number of zones with identical kinds/ids/contents.
//
// Seed corpus (12+): empty, fences, nested fences, CRLF, directives,
// tables, unicode, HTML blocks, unclosed anchors/fences, inline code,
// indented code, freeze markers, task lists.
func FuzzParseMarkdown(f *testing.F) {
	// Seed corpus.
	f.Add("")
	f.Add("\n")
	f.Add("# Title\n\nSome prose.\n")
	f.Add("# T\n\n```go\nfmt.Println(\"hi\")\n```\n")
	f.Add("# T\n\n```md\n```go\nnested?\n```\n```\n")
	f.Add("# T\n\n~~~md\n~~~inner~~~\n~~~\n")
	f.Add("A\r\nB\r\n")
	f.Add("A\rmiddle\rB\n")
	f.Add("<!-- gmb:begin:sec -->\nBody.\n<!-- gmb:end:sec -->\n")
	f.Add("<!-- gmb:begin:sec -->\n<!-- gmb:instruction: be brief -->\nBody.\n<!-- gmb:end:sec -->\n")
	f.Add("| a | b |\n|---|---|\n| 1 | 2 |\n")
	f.Add("Héllo 🌍 — ünïcodé — dash\n")
	f.Add("<div>\nHTML block.\n</div>\n")
	f.Add("<!-- gmb:begin:open -->\nUnclosed anchor.\n")
	f.Add("# T\n\n```go\nunclosed fence\n")
	f.Add("Inline `code <!-- gmb:begin:x -->` span.\n")
	f.Add("    indented <!-- gmb:begin:x --> code\n")
	f.Add("<!-- gmb:begin:sec -->\n<!-- gmb:freeze -->\nPin.\n<!-- gmb:end:sec -->\n")
	f.Add("- [ ] task\n- [x] done\n")
	f.Add("# A\r\n\r\n<!-- gmb:begin:s -->\r\nX\r\n<!-- gmb:end:s -->\r\n")
	f.Add("<!-- gmb:end:stray -->\nText.\n<!-- gmb:begin:a -->\nA\n<!-- gmb:end:a -->\n<!-- gmb:begin:a -->\nB\n<!-- gmb:end:a -->\n")

	f.Fuzz(func(t *testing.T, src string) {
		// Invariant 1: never panics; non-nil result (a panic fails the run).
		var doc *ParsedDoc
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ParseMarkdown panicked on %q: %v", src, r)
				}
			}()
			doc = ParseMarkdown(src)
		}()
		if doc == nil {
			t.Fatalf("ParseMarkdown returned nil on %q", src)
		}

		// Invariant 2: round-trip against the LF-normalized input.
		want := parserFuzzNormalize(src)
		if got := Reconstruct(doc.Zones); got != want {
			t.Errorf("round-trip mismatch on %q:\n--- got ---\n%q\n--- want ---\n%q",
				src, got, want)
		}

		// Invariant 3: zone-count stability across re-parse, with
		// identical kinds, section IDs, and contents.
		again := ParseMarkdown(Reconstruct(doc.Zones))
		if again == nil {
			t.Fatalf("re-parse returned nil on %q", src)
		}
		if len(again.Zones) != len(doc.Zones) {
			t.Errorf("zone-count instability on %q: first=%d second=%d",
				src, len(doc.Zones), len(again.Zones))
		} else {
			for i := range doc.Zones {
				a, b := doc.Zones[i], again.Zones[i]
				if a.Kind != b.Kind || a.SectionID != b.SectionID || a.Content != b.Content {
					t.Errorf("zone %d unstable on %q:\nfirst=%+v\nsecond=%+v",
						i, src, a, b)
					break
				}
			}
		}
	})
}

// parserFuzzNormalize mirrors the parser's documented line-ending
// normalization (CRLF/CR → LF) so the round-trip invariant can be asserted
// unconditionally against the normalized form.
func parserFuzzNormalize(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}
