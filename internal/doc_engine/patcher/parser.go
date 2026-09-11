// Package patcher implements Stage 5 (markdown anchor isolation) and
// Stage 8 (3-way merge) of the Documentation Intelligence Engine pipeline.
//
// parser.go: Parses a markdown file into three zone types:
//   - Human zones: content outside gmb:begin/end markers (never modified)
//   - Managed zones: content inside <!-- gmb:begin:id --> ... <!-- gmb:end:id --> (engine territory)
//   - Frozen zones: content bearing <!-- gmb:freeze --> or <!-- gmb:pin --> (never modified even if managed)
//
// Zone extraction (plan A1) is implemented as a goldmark AST walk, not a
// line scan. How zones map to the AST:
//
//   - The source is parsed with goldmark (CommonMark + GFM extension for
//     tables, strikethrough, and task lists). Only *ast.HTMLBlock nodes are
//     candidates for markers/directives: a standalone
//     `<!-- gmb:begin:id -->` / `<!-- gmb:end:id -->` / `<!-- gmb:*: ... -->`
//     comment line always parses as an HTML block.
//   - Anything inside a FencedCodeBlock, indented CodeBlock, or inline
//     CodeSpan never produces HTML nodes (code content is raw segments), so
//     `gmb:`-looking text in code is structurally invisible to the parser.
//     The walk additionally tracks code-node ancestry (codeDepth) and skips
//     HTML blocks nested inside code, belt and braces.
//   - Zone boundaries come from AST node source positions: each HTML block
//     contributes its text.Segment offsets (Lines() + ClosureLine), which
//     are converted to 1-based line numbers. Zones are then sliced as exact
//     byte ranges of the normalized source, so Parse→Reconstruct is
//     byte-identical (modulo CRLF→LF normalization).
//   - Per-line classification reuses the legacy helpers (parseBegin,
//     parseEnd, parseDirective), keeping marker syntax and the exported API
//     stable.
//
// The previous hand-rolled line scanner is kept as parseMarkdownLegacy and
// selected via the GMB_DOC_PARSER=legacy environment gate.
package patcher

import (
	"bufio"
	"os"
	"strings"

	"github.com/yuin/goldmark"
	gmast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	gmparser "github.com/yuin/goldmark/parser"
	gmtext "github.com/yuin/goldmark/text"
)

// ZoneKind classifies a parsed zone within a markdown document.
type ZoneKind int

const (
	ZoneHuman   ZoneKind = iota // human territory — never modified
	ZoneManaged                 // engine territory — may be replaced
	ZoneFrozen                  // freeze/pin applied — never modified even if managed
)

// Zone is a contiguous region of a markdown file with a known kind and section ID.
type Zone struct {
	SectionID  string            // non-empty only for managed/frozen zones
	Kind       ZoneKind          // human, managed, or frozen
	Content    string            // the exact source bytes of this zone, including anchor comment lines
	StartLine  int               // 1-based line number of first line
	EndLine    int               // 1-based line number of last line (inclusive)
	Directives map[string]string // gmb:* directives found inside this zone (key → value)
}

// ParsedDoc is the result of parsing a markdown file into zones.
type ParsedDoc struct {
	Zones []Zone
	// Raw is the normalized full text (CRLF/CR converted to LF); used for
	// determinism checks. Reconstruct(doc.Zones) == doc.Raw always holds.
	Raw string
}

// goldmarkMD is the shared goldmark instance: CommonMark plus the GFM
// extension (tables, strikethrough, task lists). A goldmark.Markdown value
// is safe for concurrent use; each parse creates its own parser instance.
var goldmarkMD = goldmark.New(goldmark.WithExtensions(extension.GFM))

// ParseMarkdown parses markdown text into zones based on gmb comment markers.
//
// The default implementation parses with goldmark and walks the AST (see the
// package doc for the zone↔AST mapping). Input line endings are normalized
// (CRLF/CR → LF) before parsing.
//
// If the environment variable GMB_DOC_PARSER is set to "legacy", parsing
// delegates to the previous hand-rolled line scanner (parseMarkdownLegacy).
func ParseMarkdown(src string) *ParsedDoc {
	if os.Getenv("GMB_DOC_PARSER") == "legacy" {
		return parseMarkdownLegacy(src)
	}
	return parseMarkdownGoldmark(src)
}

// parseMarkdownGoldmark parses markdown text into zones via a goldmark AST
// walk. Zone boundaries derive from AST node source positions (segment
// offsets); zones tile the normalized source with exact byte slices, so
// Reconstruct is byte-identical to the normalized input.
func parseMarkdownGoldmark(src string) *ParsedDoc {
	normalized := normalizeLineEndings(src)
	doc := &ParsedDoc{Raw: normalized}

	lines := splitLines(normalized)
	n := len(lines)
	if n == 0 {
		return doc
	}
	starts := lineStartOffsets(normalized)

	source := []byte(normalized)
	root := goldmarkMD.Parser().Parse(gmtext.NewReader(source), gmparser.WithContext(gmparser.NewContext()))

	// commentLines marks the 1-based numbers of source lines covered by a
	// real (non-code) HTML comment block in the AST.
	commentLines := collectCommentLines(root, n, starts)

	lineText := func(line int) string {
		s := starts[line-1]
		e := len(normalized)
		if line < len(starts) {
			e = starts[line] - 1 // exclude the terminating '\n'
		}
		return normalized[s:e]
	}
	sliceLines := func(a, b int) string {
		if a > b {
			return ""
		}
		s := starts[a-1]
		e := len(normalized)
		if b < len(starts) {
			e = starts[b] // include line b's terminating '\n'
		}
		return normalized[s:e]
	}

	type anchorEvent struct {
		line    int
		beginID string
		isBegin bool
	}
	var events []anchorEvent
	for line := 1; line <= n; line++ {
		if !commentLines[line] {
			continue
		}
		if id, ok := parseBegin(lineText(line)); ok {
			events = append(events, anchorEvent{line: line, beginID: id, isBegin: true})
		} else if parseEnd(lineText(line)) {
			events = append(events, anchorEvent{line: line})
		}
	}

	emitHuman := func(a, b int) {
		if a > b {
			return
		}
		doc.Zones = append(doc.Zones, Zone{
			Kind:      ZoneHuman,
			Content:   sliceLines(a, b),
			StartLine: a,
			EndLine:   b,
		})
	}

	hStart := 1
	inManaged := false
	var (
		sectionID  string
		mStart     int
		directives map[string]string
	)
	for _, ev := range events {
		switch {
		case !inManaged && ev.isBegin:
			emitHuman(hStart, ev.line-1)
			inManaged = true
			sectionID = ev.beginID
			mStart = ev.line
			directives = make(map[string]string)
			hStart = ev.line
		case inManaged && !ev.isBegin:
			// Collect gmb:* directives from real comment lines strictly
			// inside the zone (begin line excluded, end line included —
			// mirroring the legacy scanner, which also records the end
			// marker's "end" key).
			for line := mStart + 1; line <= ev.line; line++ {
				if !commentLines[line] {
					continue
				}
				if k, v, ok := parseDirective(lineText(line)); ok {
					directives[k] = v
				}
			}
			kind := ZoneManaged
			if _, frozen := directives["freeze"]; frozen {
				kind = ZoneFrozen
			}
			if _, pinned := directives["pin"]; pinned {
				kind = ZoneFrozen
			}
			doc.Zones = append(doc.Zones, Zone{
				SectionID:  sectionID,
				Kind:       kind,
				Content:    sliceLines(mStart, ev.line),
				StartLine:  mStart,
				EndLine:    ev.line,
				Directives: directives,
			})
			inManaged = false
			sectionID = ""
			directives = nil
			hStart = ev.line + 1
		default:
			// Stray end marker outside a zone, or a nested begin marker
			// inside a zone: ordinary content, same as the legacy scanner.
		}
	}

	if inManaged {
		// Unclosed anchor: treat remaining content as human (recovery mode).
		emitHuman(hStart, n)
	} else {
		emitHuman(hStart, n)
	}

	return doc
}

// collectCommentLines walks the goldmark AST and returns the set of 1-based
// source line numbers covered by HTML comment blocks that are NOT inside
// fenced code blocks, indented code blocks, or inline code spans. HTML
// comments inside code never surface as HTMLBlock nodes (code is raw), and
// the codeDepth ancestry guard skips anything nested inside code regardless.
func collectCommentLines(root gmast.Node, n int, starts []int) map[int]bool {
	commentLines := make(map[int]bool)
	lineOf := func(off int) int {
		// Largest line index with starts[i] <= off, returned 1-based.
		lo, hi := 0, len(starts)-1
		for lo < hi {
			mid := (lo + hi + 1) / 2
			if starts[mid] <= off {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		return lo + 1
	}
	markRange := func(start, stop int) {
		if stop <= start {
			return
		}
		if start < 0 {
			start = 0
		}
		a, b := lineOf(start), lineOf(stop-1)
		if a < 1 {
			a = 1
		}
		if b > n {
			b = n
		}
		for line := a; line <= b; line++ {
			commentLines[line] = true
		}
	}

	codeDepth := 0
	_ = gmast.Walk(root, func(node gmast.Node, entering bool) (gmast.WalkStatus, error) {
		switch node.Kind() {
		case gmast.KindFencedCodeBlock, gmast.KindCodeBlock, gmast.KindCodeSpan:
			if entering {
				codeDepth++
			} else {
				codeDepth--
			}
		case gmast.KindHTMLBlock:
			if entering && codeDepth == 0 {
				if hb, ok := node.(*gmast.HTMLBlock); ok {
					segs := hb.Lines()
					if segs != nil {
						for i := 0; i < segs.Len(); i++ {
							seg := segs.At(i)
							markRange(seg.Start, seg.Stop)
						}
					}
					if hb.HasClosure() {
						markRange(hb.ClosureLine.Start, hb.ClosureLine.Stop)
					}
				}
			}
		}
		return gmast.WalkContinue, nil
	})
	return commentLines
}

// normalizeLineEndings converts CRLF and lone CR to LF.
func normalizeLineEndings(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// lineStartOffsets returns the byte offset at which each 1-based line
// starts. The count matches splitLines: a trailing newline does not create
// an extra line.
func lineStartOffsets(src string) []int {
	if src == "" {
		return nil
	}
	starts := []int{0}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' && i+1 < len(src) {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// parseMarkdownLegacy is the pre-A1 hand-rolled markdown line scanner,
// retained as a fallback behind the GMB_DOC_PARSER=legacy environment gate.
// It is purely string-based (no AST) — robust for the anchor-comment
// protocol in simple files, but blind to fenced/indented code blocks,
// inline code spans, and CRLF edge cases.
//
// ponytail: regex-free line scanner; O(n) single pass. No performance ceiling visible.
func parseMarkdownLegacy(src string) *ParsedDoc {
	lines := splitLines(src)
	doc := &ParsedDoc{Raw: src}

	var (
		human      strings.Builder
		humanStart = 1
		inManaged  = false
		sectionID  string
		managed    strings.Builder
		mStart     int
		directives map[string]string
	)

	flush := func(i int) {
		if human.Len() > 0 {
			doc.Zones = append(doc.Zones, Zone{
				Kind:      ZoneHuman,
				Content:   human.String(),
				StartLine: humanStart,
				EndLine:   i,
			})
			human.Reset()
		}
	}

	for i, line := range lines {
		lineNum := i + 1

		if !inManaged {
			// Look for <!-- gmb:begin:id -->
			if id, ok := parseBegin(line); ok {
				flush(lineNum - 1)
				inManaged = true
				sectionID = id
				mStart = lineNum
				managed.Reset()
				managed.WriteString(line + "\n")
				directives = make(map[string]string)
				humanStart = lineNum
				continue
			}
			human.WriteString(line + "\n")
		} else {
			managed.WriteString(line + "\n")
			// Collect gmb:* directives inside the zone
			if d, v, ok := parseDirective(line); ok {
				directives[d] = v
			}
			// Look for <!-- gmb:end:id --> (matches any id for robustness)
			if parseEnd(line) {
				kind := ZoneManaged
				if _, frozen := directives["freeze"]; frozen {
					kind = ZoneFrozen
				}
				if _, pinned := directives["pin"]; pinned {
					kind = ZoneFrozen
				}
				doc.Zones = append(doc.Zones, Zone{
					SectionID:  sectionID,
					Kind:       kind,
					Content:    managed.String(),
					StartLine:  mStart,
					EndLine:    lineNum,
					Directives: directives,
				})
				inManaged = false
				sectionID = ""
				managed.Reset()
				humanStart = lineNum + 1
			}
		}
	}

	// Flush any remaining human content (or an unclosed managed zone recovered as human).
	if inManaged {
		// Unclosed anchor: treat remaining content as human (recovery mode)
		human.WriteString(managed.String())
	}
	if human.Len() > 0 {
		doc.Zones = append(doc.Zones, Zone{
			Kind:      ZoneHuman,
			Content:   human.String(),
			StartLine: humanStart,
			EndLine:   len(lines),
		})
	}

	return doc
}

// Reconstruct reassembles the full document text from zones.
// Used after replacing managed zone content.
func Reconstruct(zones []Zone) string {
	var sb strings.Builder
	for _, z := range zones {
		sb.WriteString(z.Content)
	}
	return sb.String()
}

// ManagedZone returns the Zone for a given sectionID, or nil if not found.
func ManagedZone(doc *ParsedDoc, sectionID string) *Zone {
	for i := range doc.Zones {
		if doc.Zones[i].SectionID == sectionID {
			return &doc.Zones[i]
		}
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Low-level line parsers
// ────────────────────────────────────────────────────────────────────────────

func parseBegin(line string) (id string, ok bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "<!--") || !strings.HasSuffix(s, "-->") {
		return "", false
	}
	if len(s) < 7 { // overlapping "<!--" / "-->" (e.g. "<!-->"): no inner text
		return "", false
	}
	inner := strings.TrimSpace(s[4 : len(s)-3])
	if !strings.HasPrefix(inner, "gmb:begin:") {
		return "", false
	}
	id = strings.TrimPrefix(inner, "gmb:begin:")
	id = strings.TrimSpace(id)
	return id, id != ""
}

func parseEnd(line string) bool {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "<!--") || !strings.HasSuffix(s, "-->") {
		return false
	}
	if len(s) < 7 { // overlapping "<!--" / "-->" (e.g. "<!-->"): no inner text
		return false
	}
	inner := strings.TrimSpace(s[4 : len(s)-3])
	return strings.HasPrefix(inner, "gmb:end")
}

// parseDirective extracts a <!-- gmb:key --> or <!-- gmb:key: value --> directive.
func parseDirective(line string) (key, value string, ok bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "<!--") || !strings.HasSuffix(s, "-->") {
		return "", "", false
	}
	if len(s) < 7 { // overlapping "<!--" / "-->" (e.g. "<!-->"): no inner text
		return "", "", false
	}
	inner := strings.TrimSpace(s[4 : len(s)-3])
	if !strings.HasPrefix(inner, "gmb:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(inner, "gmb:")
	if idx := strings.Index(rest, ":"); idx >= 0 {
		return rest[:idx], strings.TrimSpace(rest[idx+1:]), true
	}
	return rest, "", true
}

func splitLines(s string) []string {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}
