// Package verifier — references.go
//
// Gate 7 (reference integrity, plan C2): link, anchor, permalink,
// frontmatter, and TOC verification for generated markdown.
//
// Policy summary:
//   - Internal links [t](rel.md[#anchor]) and [t](#anchor): the target file
//     must exist (resolved as repoRoot + docDir + rel, cleaned; a leading "/"
//     is repo-root-relative). When the file exists, a #slug anchor must match
//     a heading slug in the target; a missing file reports "file not found"
//     and skips anchor verification. A bad anchor reports "anchor not found".
//   - Permalinks [t](file#L12[-L30]) and bare path#Lx occurrences: the file
//     must exist, 1 <= start <= linecount, and end >= start.
//   - Backticked symbols in link text ([`Sym`](...)) are verified with
//     symbolExists when non-nil; nil skips symbol verification entirely.
//   - External http(s) URLs are checked ONLY when checkExternal is true, via
//     HEAD with a 5s timeout, 1 retry, and a per-call result cache. Network
//     errors are skipped silently; HTTP 4xx/5xx report a Broken entry.
//   - Fenced code blocks and inline code spans are never treated as links.
//   - Results are in deterministic document order with 1-based line numbers.
//
// Stdlib only (net/http is used for external HEAD checks).
package verifier

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// BrokenRef is a single unresolvable reference found in a document.
type BrokenRef struct {
	Source string // docPath of the document containing the reference
	Line   int    // 1-based line number of the reference
	Target string // link target (or symbol / bare permalink text) that failed
	Reason string // "file not found" | "anchor not found" | permalink / symbol / external reason
}

// ReferenceReport is the verdict of CheckReferences for one document.
type ReferenceReport struct {
	Broken         []BrokenRef
	CheckedLinks   int // every link occurrence evaluated (internal, permalink, external when enabled)
	CheckedAnchors int // every #slug anchor validation performed
}

// externalHTTPClient is shared across CheckReferences calls; the per-call
// cache lives in the call itself. Timeout bounds each HEAD attempt.
var externalHTTPClient = &http.Client{Timeout: 5 * time.Second}

// barePermalinkRe matches bare permalink occurrences such as
// docs/foo.md#L12, src/main.go#L3-L30, or pkg/a.go#L3-30 in plain text.
// The ".ext" requirement keeps bare words and "#L12"-only fragments out.
var barePermalinkRe = regexp.MustCompile(`[A-Za-z0-9_][A-Za-z0-9_./\\-]*\.[A-Za-z0-9]+#L\d+(?:-L?\d+)?`)

// externalURLRe finds bare http(s) URLs so permalink scanning can avoid
// matching a "#L.." fragment tail inside them.
var externalURLRe = regexp.MustCompile(`https?://\S+`)

// permalinkAnchorRe classifies an anchor fragment as a permalink:
// L12, L12-L30, or L12-30.
var permalinkAnchorRe = regexp.MustCompile(`^L(\d+)(?:-L?(\d+))?$`)

// GitHubSlug converts a heading to its GitHub anchor slug: lowercase, drop
// anything that is not a letter, number, space, hyphen, or underscore
// (unicode-aware), then map spaces to hyphens.
func GitHubSlug(heading string) string {
	var b strings.Builder
	b.Grow(len(heading))
	for _, r := range strings.ToLower(heading) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
		default:
			// drop punctuation and all other symbols
		}
	}
	return b.String()
}

// ValidateFrontmatter minimally parses a leading "---"-delimited frontmatter
// block (key: value lines, no yaml dependency) and checks required keys for
// the target platform:
//
//	vitepress  -> [title]
//	docusaurus -> [title, sidebar_position]
//	mkdocs     -> [title]
//	github_flat / unknown -> nil (no requirements)
//
// No leading "---" means no frontmatter and returns nil. An unterminated
// opening "---" is a failure regardless of platform.
func ValidateFrontmatter(markdown, platform string) []string {
	lines := splitRefLines(markdown)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return []string{"frontmatter: unterminated opening '---'"}
	}
	keys := make(map[string]bool)
	for _, ln := range lines[1:end] {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		idx := strings.IndexByte(t, ':')
		if idx <= 0 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(t[:idx]))
		k = strings.Trim(k, `"'`)
		if k != "" {
			keys[k] = true
		}
	}
	var required []string
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "vitepress":
		required = []string{"title"}
	case "docusaurus":
		required = []string{"title", "sidebar_position"}
	case "mkdocs":
		required = []string{"title"}
	default:
		return nil
	}
	var errs []string
	for _, r := range required {
		if !keys[r] {
			errs = append(errs, fmt.Sprintf("frontmatter: missing required key %q for platform %q", r, platform))
		}
	}
	return errs
}

// CheckTOC verifies same-document #anchor link entries, but ONLY when the
// document has a TOC marker: an "<!-- toc -->" comment (case-insensitive) or
// a "## Contents" / "## Table of Contents" heading. Documents without a TOC
// marker return nil. Each #anchor entry must match a heading slug.
func CheckTOC(markdown string) []string {
	if !hasTOCMarker(markdown) {
		return nil
	}
	slugs := collectHeadingSlugs(markdown)
	var errs []string
	for _, l := range scanRefLinks(markdown) {
		filePart, anchor, hasAnchor := splitRefTarget(l.target)
		if !hasAnchor || filePart != "" || anchor == "" {
			continue
		}
		if permalinkAnchorRe.MatchString(anchor) {
			continue // line permalinks are not heading anchors
		}
		if !slugs[anchor] {
			errs = append(errs, fmt.Sprintf("toc: anchor %q has no matching heading", "#"+anchor))
		}
	}
	return errs
}

// CheckReferences verifies every internal link, permalink, backticked link
// symbol, and (optionally) external URL in markdown.
//
//   - repoRoot is the repository root used to resolve relative targets.
//   - docPath is this document's path (repo-relative preferred; absolute is
//     also accepted) and becomes BrokenRef.Source. Its directory anchors
//     relative link resolution.
//   - symbolExists verifies backticked symbols in link text; nil skips.
//   - checkExternal=false ignores external URLs entirely (no network).
func CheckReferences(repoRoot, docPath, markdown string, symbolExists func(string) bool, checkExternal bool) ReferenceReport {
	var rep ReferenceReport
	ownSlugs := collectHeadingSlugs(markdown)
	ownLines := countRefLines(markdown)
	extCache := make(map[string]int) // per-call external status cache

	links := scanRefLinks(markdown)
	for _, l := range links {
		target := strings.TrimSpace(l.target)
		if target == "" || target == "#" {
			continue
		}
		// Link text symbol verification (backticked symbols only).
		if symbolExists != nil {
			for _, sym := range backtickedSymbols(l.text) {
				if !symbolExists(sym) {
					rep.Broken = append(rep.Broken, BrokenRef{
						Source: docPath,
						Line:   l.line,
						Target: sym,
						Reason: fmt.Sprintf("unknown symbol %q", sym),
					})
				}
			}
		}
		// External URLs.
		if isExternalRefURL(target) {
			if !checkExternal {
				continue // ignored entirely: no network, no counting
			}
			rep.CheckedLinks++
			status, ok := extCache[target]
			if !ok {
				status, ok = headRefURL(target)
				if ok {
					extCache[target] = status
				} else {
					continue // network error: skip silently
				}
			}
			if status >= 400 {
				rep.Broken = append(rep.Broken, BrokenRef{
					Source: docPath,
					Line:   l.line,
					Target: target,
					Reason: fmt.Sprintf("external link returned %d", status),
				})
			}
			continue
		}
		if isSkippedScheme(target) {
			continue // mailto:, tel:, data:, etc.: never checked
		}
		filePart, anchor, hasAnchor := splitRefTarget(target)
		// Same-document anchor or permalink.
		if filePart == "" {
			if !hasAnchor || anchor == "" {
				continue
			}
			if m := permalinkAnchorRe.FindStringSubmatch(anchor); m != nil {
				rep.CheckedLinks++
				if reason := checkLineRange(ownLines, m[1], m[2]); reason != "" {
					rep.Broken = append(rep.Broken, BrokenRef{Source: docPath, Line: l.line, Target: target, Reason: reason})
				}
				continue
			}
			rep.CheckedLinks++
			rep.CheckedAnchors++
			if !ownSlugs[anchor] {
				rep.Broken = append(rep.Broken, BrokenRef{Source: docPath, Line: l.line, Target: target, Reason: "anchor not found"})
			}
			continue
		}
		// Cross-file link, optionally with anchor or permalink.
		rep.CheckedLinks++
		full := resolveRefFile(repoRoot, docPath, filePart)
		data, err := os.ReadFile(full)
		if err != nil {
			rep.Broken = append(rep.Broken, BrokenRef{Source: docPath, Line: l.line, Target: target, Reason: "file not found"})
			continue // skip anchor verification when the file is missing
		}
		if !hasAnchor || anchor == "" {
			continue
		}
		if m := permalinkAnchorRe.FindStringSubmatch(anchor); m != nil {
			if reason := checkLineRange(countRefLines(string(data)), m[1], m[2]); reason != "" {
				rep.Broken = append(rep.Broken, BrokenRef{Source: docPath, Line: l.line, Target: target, Reason: reason})
			}
			continue
		}
		rep.CheckedAnchors++
		if !collectHeadingSlugs(string(data)) [anchor] {
			rep.Broken = append(rep.Broken, BrokenRef{Source: docPath, Line: l.line, Target: target, Reason: "anchor not found"})
		}
	}

	// Bare path#Lx occurrences outside links and code.
	for _, bp := range scanBarePermalinks(markdown) {
		rep.CheckedLinks++
		full := resolveRefFile(repoRoot, docPath, bp.filePart)
		data, err := os.ReadFile(full)
		if err != nil {
			rep.Broken = append(rep.Broken, BrokenRef{Source: docPath, Line: bp.line, Target: bp.text, Reason: "file not found"})
			continue
		}
		if m := permalinkAnchorRe.FindStringSubmatch(bp.anchor); m != nil {
			if reason := checkLineRange(countRefLines(string(data)), m[1], m[2]); reason != "" {
				rep.Broken = append(rep.Broken, BrokenRef{Source: docPath, Line: bp.line, Target: bp.text, Reason: reason})
			}
		}
	}
	return rep
}

// ────────────────────────────────────────────────────────────────────────────
// Line / heading helpers
// ────────────────────────────────────────────────────────────────────────────

// splitRefLines normalizes CRLF/CR and splits markdown into lines.
func splitRefLines(markdown string) []string {
	s := strings.ReplaceAll(markdown, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

// countRefLines returns the editor-style line count of content
// (trailing newline does not add a phantom line; empty file has 0 lines).
func countRefLines(content string) int {
	if content == "" {
		return 0
	}
	s := strings.ReplaceAll(content, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// isFenceLine reports whether a line opens/closes a fenced code block.
func isFenceLine(line string) bool {
	t := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// collectHeadingSlugs parses ATX headings outside fenced blocks and returns
// the set of GitHub slugs, with GitHub-style dedup suffixes (-1, -2, ...)
// for repeated headings.
func collectHeadingSlugs(markdown string) map[string]bool {
	slugs := make(map[string]bool)
	counts := make(map[string]int)
	inFence := false
	for _, line := range splitRefLines(markdown) {
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		t := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(t, "#") {
			continue
		}
		level := 0
		for level < len(t) && t[level] == '#' {
			level++
		}
		if level == 0 || level > 6 {
			continue
		}
		rest := t[level:]
		if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
			continue // "#tag" is not a heading
		}
		text := strings.TrimSpace(rest)
		// Strip optional closing hash run ("## Title ##").
		text = strings.TrimRight(text, "#")
		text = strings.TrimSpace(text)
		slug := GitHubSlug(text)
		if n := counts[slug]; n > 0 {
			slug = fmt.Sprintf("%s-%d", slug, n)
		}
		counts[GitHubSlug(text)]++
		slugs[slug] = true
	}
	return slugs
}

// hasTOCMarker reports whether markdown declares a table of contents.
func hasTOCMarker(markdown string) bool {
	if strings.Contains(strings.ToLower(markdown), "<!-- toc -->") {
		return true
	}
	inFence := false
	for _, line := range splitRefLines(markdown) {
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		t := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(t, "##") || strings.HasPrefix(t, "###") {
			continue
		}
		text := strings.ToLower(strings.TrimSpace(strings.Trim(strings.TrimLeft(t, "#"), " \t#")))
		if text == "contents" || text == "table of contents" {
			return true
		}
	}
	return false
}

// ────────────────────────────────────────────────────────────────────────────
// Link scanning (fence- and code-span-aware, document order)
// ────────────────────────────────────────────────────────────────────────────

// refLink is one inline [text](target) occurrence outside code.
type refLink struct {
	text   string
	target string
	line   int // 1-based
	start  int // byte offset of '[' within the line
	end    int // byte offset just past ')'
}

// scanRefLinks extracts inline links in document order, skipping fenced
// blocks, image links (![...]), and links inside inline code spans.
func scanRefLinks(markdown string) []refLink {
	var out []refLink
	inFence := false
	for i, line := range splitRefLines(markdown) {
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		out = append(out, scanRefLineLinks(line, i+1)...)
	}
	return out
}

// scanRefLineLinks scans a single non-fence line for links. Backtick runs
// toggle inline-code state, except inside a consumed link span (so backticks
// in link text do not corrupt target parsing).
func scanRefLineLinks(line string, lineNum int) []refLink {
	var out []refLink
	inCode := false
	tickLen := 0
	i := 0
	for i < len(line) {
		c := line[i]
		if c == '`' {
			n := 1
			for i+n < len(line) && line[i+n] == '`' {
				n++
			}
			if !inCode {
				inCode = true
				tickLen = n
			} else if n == tickLen {
				inCode = false
				tickLen = 0
			}
			i += n
			continue
		}
		if c == '[' && !inCode && !(i > 0 && line[i-1] == '!') {
			if text, target, end, ok := parseRefLink(line, i); ok {
				out = append(out, refLink{text: text, target: target, line: lineNum, start: i, end: end})
				i = end
				continue
			}
		}
		i++
	}
	return out
}

// parseRefLink parses "[text](target)" starting at line[pos] == '['.
// It supports nested brackets in text and balanced parens in target, and
// strips an optional quoted title after the target.
func parseRefLink(line string, pos int) (text, target string, end int, ok bool) {
	depth := 0
	closeBracket := -1
	i := pos
	for ; i < len(line); i++ {
		switch line[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				closeBracket = i
			}
		case '`':
			// Link text is consumed literally: backticks inside it must
			// not disturb bracket matching, so just keep scanning.
		}
		if closeBracket != -1 {
			break
		}
	}
	if closeBracket == -1 || closeBracket+1 >= len(line) || line[closeBracket+1] != '(' {
		return "", "", 0, false
	}
	depth = 1
	j := closeBracket + 2
	for ; j < len(line); j++ {
		switch line[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				raw := strings.TrimSpace(line[closeBracket+2 : j])
				raw = firstRefTargetToken(raw)
				raw = strings.TrimSpace(strings.Trim(raw, "<>"))
				if raw == "" {
					return "", "", 0, false
				}
				return line[pos+1 : closeBracket], raw, j + 1, true
			}
		}
	}
	return "", "", 0, false
}

// firstRefTargetToken strips an optional quoted title after the URL:
// `file.md "Title"` -> `file.md`.
func firstRefTargetToken(raw string) string {
	inSingle, inDouble := false, false
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case ' ', '\t':
			if !inSingle && !inDouble {
				return raw[:i]
			}
		}
	}
	return raw
}

// backtickedSymbols returns `symbol` spans inside link text.
func backtickedSymbols(text string) []string {
	var out []string
	for {
		a := strings.IndexByte(text, '`')
		if a == -1 {
			return out
		}
		b := strings.IndexByte(text[a+1:], '`')
		if b == -1 {
			return out
		}
		if sym := strings.TrimSpace(text[a+1 : a+1+b]); sym != "" {
			out = append(out, sym)
		}
		text = text[a+1+b+1:]
	}
}

// barePermalink is a path#Lx occurrence in plain text (not inside a link).
type barePermalink struct {
	text     string
	filePart string
	anchor   string
	line     int
	start    int
	end      int
}

// scanBarePermalinks finds bare path#Lx occurrences outside links, code
// spans, fenced blocks, and bare http(s) URLs.
func scanBarePermalinks(markdown string) []barePermalink {
	var out []barePermalink
	inFence := false
	for i, line := range splitRefLines(markdown) {
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		skip := lineSkipRanges(line)
		for _, m := range barePermalinkRe.FindAllStringSubmatchIndex(line, -1) {
			s, e := m[0], m[1]
			if overlapsRange(skip, s, e) {
				continue
			}
			text := line[s:e]
			hash := strings.LastIndexByte(text, '#')
			if hash == -1 {
				continue
			}
			out = append(out, barePermalink{
				text:     text,
				filePart: text[:hash],
				anchor:   text[hash+1:],
				line:     i + 1,
				start:    s,
				end:      e,
			})
		}
	}
	return out
}

// lineSkipRanges returns byte ranges of a line that bare-permalink scanning
// must ignore: link spans, inline code spans, and bare external URLs.
func lineSkipRanges(line string) [][2]int {
	var skip [][2]int
	for _, l := range scanRefLineLinks(line, 0) {
		skip = append(skip, [2]int{l.start, l.end})
	}
	for _, m := range externalURLRe.FindAllStringIndex(line, -1) {
		skip = append(skip, [2]int{m[0], m[1]})
	}
	// Inline code spans, computed while jumping over link spans so that
	// backticks inside link text never open a code range.
	inCode := false
	tickLen, codeStart := 0, 0
	i := 0
	for i < len(line) {
		if insideRanges(skip, i) {
			if inCode {
				skip = append(skip, [2]int{codeStart, i})
				inCode = false
				tickLen = 0
			}
			i++
			continue
		}
		if line[i] == '`' {
			n := 1
			for i+n < len(line) && line[i+n] == '`' && !insideRanges(skip, i+n) {
				n++
			}
			if !inCode {
				inCode = true
				tickLen = n
				codeStart = i
			} else if n == tickLen {
				skip = append(skip, [2]int{codeStart, i + n})
				inCode = false
				tickLen = 0
			}
			i += n
			continue
		}
		i++
	}
	if inCode {
		skip = append(skip, [2]int{codeStart, len(line)})
	}
	return skip
}

func insideRanges(ranges [][2]int, pos int) bool {
	for _, r := range ranges {
		if pos >= r[0] && pos < r[1] {
			return true
		}
	}
	return false
}

func overlapsRange(ranges [][2]int, s, e int) bool {
	for _, r := range ranges {
		if s < r[1] && e > r[0] {
			return true
		}
	}
	return false
}

// ────────────────────────────────────────────────────────────────────────────
// Target classification / resolution / validation
// ────────────────────────────────────────────────────────────────────────────

// splitRefTarget splits a link target on the first '#'.
func splitRefTarget(target string) (filePart, anchor string, hasAnchor bool) {
	if i := strings.IndexByte(target, '#'); i != -1 {
		return target[:i], target[i+1:], true
	}
	return target, "", false
}

// isExternalRefURL reports http(s) URLs (case-insensitive).
func isExternalRefURL(target string) bool {
	l := strings.ToLower(target)
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

// isSkippedScheme reports non-HTTP schemes that are never checked
// (mailto:, tel:, ftp:, data:, ...). A "scheme:" prefix with no "/" path
// before the colon counts; plain relative paths never count.
func isSkippedScheme(target string) bool {
	i := strings.IndexByte(target, ':')
	if i <= 0 {
		return false
	}
	scheme := target[:i]
	if strings.ContainsAny(scheme, "/\\.#?") {
		return false
	}
	return true
}

// resolveRefFile resolves a link's file part against the containing document:
// repoRoot + docDir + rel (cleaned). A leading "/" is repo-root-relative.
// An absolute docPath anchors resolution to its own directory.
func resolveRefFile(repoRoot, docPath, rel string) string {
	rel = filepath.FromSlash(rel)
	if strings.HasPrefix(rel, string(filepath.Separator)) {
		return filepath.Join(repoRoot, strings.TrimLeft(rel, string(filepath.Separator)))
	}
	var base string
	if filepath.IsAbs(docPath) {
		base = filepath.Dir(docPath)
	} else {
		base = filepath.Join(repoRoot, filepath.Dir(docPath))
	}
	return filepath.Join(base, rel)
}

// checkLineRange validates a permalink's start/end line numbers against a
// file's line count. Returns "" when valid.
func checkLineRange(lineCount int, startStr, endStr string) string {
	start, err := strconv.Atoi(startStr)
	if err != nil || start < 1 || start > lineCount {
		return "line out of range"
	}
	if endStr != "" {
		end, err := strconv.Atoi(endStr)
		if err != nil || end < start {
			return "invalid line range"
		}
	}
	return ""
}

// headRefURL performs a HEAD request with 1 retry. It returns the status code
// and true on any completed request, or (0, false) when the network failed.
func headRefURL(url string) (int, bool) {
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequest(http.MethodHead, url, nil)
		if err != nil {
			return 0, false // malformed URL: skip silently
		}
		resp, err := externalHTTPClient.Do(req)
		if err != nil {
			continue // network error: retry once, then skip silently
		}
		status := resp.StatusCode
		_ = resp.Body.Close()
		return status, true
	}
	return 0, false
}
