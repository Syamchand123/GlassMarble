// Package patcher implements Stage 5 (markdown anchor isolation) and
// Stage 8 (3-way merge) of the Documentation Intelligence Engine pipeline.
//
// parser.go: Parses a markdown file into three zone types:
//   - Human zones: content outside gmb:begin/end markers (never modified)
//   - Managed zones: content inside <!-- gmb:begin:id --> ... <!-- gmb:end:id --> (engine territory)
//   - Frozen zones: content bearing <!-- gmb:freeze --> or <!-- gmb:pin --> (never modified even if managed)
package patcher

import (
	"bufio"
	"strings"
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
	SectionID  string   // non-empty only for managed/frozen zones
	Kind       ZoneKind
	Content    string   // the exact content of this zone, including anchor comment lines
	StartLine  int      // 1-based line number of first line
	EndLine    int      // 1-based line number of last line (inclusive)
	Directives map[string]string // gmb:* directives found inside this zone (key → value)
}

// ParsedDoc is the result of parsing a markdown file into zones.
type ParsedDoc struct {
	Zones []Zone
	// Raw is the original full text; used for determinism checks.
	Raw string
}

// ParseMarkdown parses markdown text into zones based on gmb comment markers.
// It is purely string-based (no tree-sitter dependency) — robust enough for
// the anchor-comment protocol and order-of-magnitude faster than a full AST parse.
//
// ponytail: regex-free line scanner; O(n) single pass. No performance ceiling visible.
func ParseMarkdown(src string) *ParsedDoc {
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
	inner := strings.TrimSpace(s[4 : len(s)-3])
	return strings.HasPrefix(inner, "gmb:end")
}

// parseDirective extracts a <!-- gmb:key --> or <!-- gmb:key: value --> directive.
func parseDirective(line string) (key, value string, ok bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "<!--") || !strings.HasSuffix(s, "-->") {
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
