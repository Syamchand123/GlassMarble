// Gate 6 (prose quality): deterministic Vale-class style rules, cspell-class
// spelling checks, and markdownlint-class structural checks over generated
// markdown.
//
// Pure stdlib; runs in-process on both tracks at zero token cost. Warns by
// default; fails only when the caller passes strict=true (renderer/engine.go
// wires FactSheet.Style.StrictProse through as that argument).
//
// Wired as its own pipeline step in renderer/engine.go's renderOneSection,
// immediately after RunGates (gates 1-5) rather than folded into RunGates
// itself: it runs CheckProseGateWithVocab on the rendered content and, on a
// strict-mode failure, drives its own repair-then-deterministic-fallback
// ladder (one LLM repair attempt re-checked through RunGates + Gate 6, else
// fall back to Track B) — see TestProcessDocument_GatesSixSevenWire for the
// wiring proof. RunGates' 5-gate signature stays a separate, narrower
// surface used by many existing callers/tests that don't need style/vocab
// inputs at all.
//
// WHAT IS SCANNED AS PROSE (and what is not):
//   - Fenced code blocks (``` / ~~~) are skipped entirely, delimiters included.
//   - Inline code spans (`...`, including multi-backtick spans) are removed.
//   - HTML comments (<!-- ... -->, including multi-line) are removed.
//   - URLs are removed: autolinks (<https://...>), inline link targets
//     ([text](url) keeps "text", drops "url"), and bare http(s):// / www. URLs.
//     Code-span stripping runs BEFORE comment stripping, so a backticked
//     "<!--" is never mistaken for a comment opener.
//
// RULES (rule IDs are stable strings used in ProseViolation.Rule):
//   - sentence-length: sentences split on '.', '!' and '?' (only when followed
//     by whitespace or end-of-text, so "v1.2.0" does not split) with > 25
//     words → warn. A trailing fragment without terminal punctuation still
//     counts as a sentence. Abbreviations ("e.g.") may split early —
//     accepted heuristic, documented here.
//   - passive-voice: HEURISTIC, not a parser. A be-verb (is/are/was/were/be/
//     been/being — note: no "am", matching the configured set) followed within
//     the next 3 tokens by a word ending in "ed"/"en" → warn. Known false
//     positives: predicate adjectives ("is red") and adverbs ("is often").
//     One violation per be-verb occurrence (first matching participle wins).
//   - jargon: every case-insensitive whole-word occurrence of a term in
//     style.JargonBlacklist → error in strict mode, warn otherwise. An empty
//     blacklist disables the rule entirely (no default list is enforced).
//   - typo: fixed whole-word list (~40 common misspellings, see typoWords)
//     → error; doubled adjacent words ("the the", case-insensitive,
//     letter-bearing tokens only) → error. Go's regexp engine (RE2) has no
//     backreferences, so doubled-word detection is a manual token scan, not
//     `\b(\w+) \1\b`.
//   - heading-skip (error): heading level deepens by more than one (H1→H3).
//     multiple-h1 (warn): second and later H1. trailing-whitespace (warn,
//     per line, all lines including fences — markdownlint MD009 parity).
//     fence-language (warn): opening ```/~~~ fence with no info string.
//     list-marker (warn): mixed "-"/"*"/"+" markers at the same indent
//     within one contiguous list (nested levels tracked separately).
//     tab-indentation (error): leading whitespace containing a tab, outside
//     fences (tabs inside fenced code are legitimate content).
//
// SCORE FORMULA (documented contract — readability is informational only and
// never emits violations, it only folds into Score):
//
//	score = max(0, 100 - 5*(#error) - 2*(#warn)
//	                  - max(0, trunc(avgSentenceWords - 20))
//	                  - max(0, trunc(longWordPct - 30)))
//
// where avgSentenceWords is mean words-per-sentence over scanned prose and
// longWordPct is the percentage of words with >= 7 letters.
package verifier

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// Severity values for ProseViolation.Severity.
const (
	proseWarn  = "warn"
	proseError = "error"
)

// Tunables for the sentence-length rule and the readability score fold-in.
const (
	maxSentenceWords     = 25
	avgSentenceThreshold = 20.0
	longWordPctThreshold = 30.0
	longWordMinLetters   = 7
	passiveLookahead     = 3
)

// ProseViolation is a single Gate 6 finding. Severity is "warn" or "error".
type ProseViolation struct {
	Rule     string
	Message  string
	Line     int
	Severity string
}

// ProseReport is the Gate 6 verdict: all findings plus Score (0-100).
type ProseReport struct {
	Violations []ProseViolation
	Score      float64
}

// beVerbs is the closed be-verb set for the passive-voice heuristic.
var beVerbs = map[string]bool{
	"is": true, "are": true, "was": true, "were": true,
	"be": true, "been": true, "being": true,
}

// typoWords is the fixed cspell-class list of common misspellings
// (whole-word, case-insensitive). Each hit is error severity.
var typoWords = map[string]bool{
	"teh": true, "recieve": true, "seperate": true, "occurrance": true,
	"accomodate": true, "definately": true, "arguement": true,
	"calender": true, "cemetary": true, "changable": true,
	"collegue": true, "comming": true, "commited": true,
	"dependance": true, "existance": true, "experiance": true,
	"freind": true, "goverment": true, "grammer": true, "harrass": true,
	"imediate": true, "independant": true, "knowlege": true,
	"liason": true, "maintanance": true, "managment": true,
	"millenium": true, "neccessary": true, "noticable": true,
	"ocasionally": true, "persue": true, "posession": true,
	"publically": true, "rythm": true, "saftey": true, "seige": true,
	"sucess": true, "thier": true, "tommorow": true, "truely": true,
	"untill": true, "withold": true, "writting": true,
}

// CheckProseGate runs all Gate 6 rules over content. In strict mode any
// error-severity violation fails (pass=false). In non-strict mode it NEVER
// fails (pass=true always) but still returns one failure string per
// violation for reporting. Failure format: "line N [severity] rule: msg".
func CheckProseGate(content string, style config.StyleSpec, strict bool) (pass bool, failures []string) {
	report := CheckProse(content, style, strict)
	for _, v := range report.Violations {
		failures = append(failures, fmt.Sprintf("line %d [%s] %s: %s", v.Line, v.Severity, v.Rule, v.Message))
	}
	if strict {
		for _, v := range report.Violations {
			if v.Severity == proseError {
				return false, failures
			}
		}
	}
	return true, failures
}

// CheckProseGateWithVocab is CheckProseGate with a learned project
// vocabulary (gap C1): typo-rule tokens present in vocab (lowercased project
// terms from LearnVocabulary) are never flagged, so project identifiers
// never fail the gate. A nil/empty vocab behaves exactly like CheckProseGate.
func CheckProseGateWithVocab(content string, style config.StyleSpec, strict bool, vocab map[string]bool) (pass bool, failures []string) {
	report := CheckProseWithVocab(content, style, strict, vocab)
	for _, v := range report.Violations {
		failures = append(failures, fmt.Sprintf("line %d [%s] %s: %s", v.Line, v.Severity, v.Rule, v.Message))
	}
	if strict {
		for _, v := range report.Violations {
			if v.Severity == proseError {
				return false, failures
			}
		}
	}
	return true, failures
}

// CheckProseWithVocab is CheckProse with a learned project vocabulary:
// the typo rule skips tokens present in vocab. All other rules are
// identical. A nil/empty vocab behaves exactly like CheckProse.
func CheckProseWithVocab(content string, style config.StyleSpec, strict bool, vocab map[string]bool) ProseReport {
	return checkProseInner(content, style, strict, vocab)
}

// LearnVocabulary collects a deterministic set of project terms (gap C1
// terminology-allowlist) from repoRoot: exported Go identifiers (funcs,
// methods as Recv.Name, types, vars/consts via go/parser) plus .go filename
// stems and directory base names as a parser-failure fallback. Terms are
// lowercased, minimum 4 letters, sorted, and capped at maxTerms (<=0 means
// a 1000-term default; 0 terms when repoRoot is empty/unreadable).
//
// Cost: one WalkDir capped at 2000 .go files, skipping vendor,
// node_modules, and .git. Callers should build once per ProcessDocument
// (not per section) and reuse the map for every CheckProseGateWithVocab
// call; the walk is read-only and safe to share across goroutines once
// built (maps are never mutated after return).
func LearnVocabulary(repoRoot string, maxTerms int) map[string]bool {
	out := make(map[string]bool)
	if maxTerms <= 0 {
		maxTerms = 1000
	}
	if strings.TrimSpace(repoRoot) == "" {
		return out
	}
	const maxFiles = 2000
	files := 0
	add := func(term string) {
		t := strings.ToLower(strings.TrimSpace(term))
		if len([]rune(t)) < 4 {
			return
		}
		out[t] = true
	}
	_ = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := strings.ToLower(d.Name())
			switch base {
			case "vendor", "node_modules", ".git":
				return filepath.SkipDir
			}
			// Directory names are project vocabulary ("auth", "ledger").
			if path != repoRoot && len([]rune(base)) >= 4 {
				add(base)
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if files >= maxFiles {
			return filepath.SkipDir
		}
		files++
		// Filename stem fallback (works even when parsing fails).
		stem := strings.ToLower(strings.TrimSuffix(filepath.Base(path), ".go"))
		if stem != "" && stem != "test" {
			stem = strings.TrimSuffix(stem, "_test")
			add(stem)
		}
		fset := token.NewFileSet()
		node, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil || node == nil {
			return nil
		}
		for _, decl := range node.Decls {
			switch t := decl.(type) {
			case *ast.FuncDecl:
				if ast.IsExported(t.Name.Name) {
					add(t.Name.Name)
				}
			case *ast.GenDecl:
				for _, spec := range t.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if ast.IsExported(s.Name.Name) {
							add(s.Name.Name)
						}
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if ast.IsExported(name.Name) {
								add(name.Name)
							}
						}
					}
				}
			}
		}
		return nil
	})
	if len(out) <= maxTerms {
		return out
	}
	// Deterministic cap: sort and keep the first maxTerms.
	sorted := make([]string, 0, len(out))
	for t := range out {
		sorted = append(sorted, t)
	}
	sort.Strings(sorted)
	capped := make(map[string]bool, maxTerms)
	for _, t := range sorted[:maxTerms] {
		capped[t] = true
	}
	return capped
}

// CheckProse runs every Gate 6 rule and scores the result. The strict flag
// only controls jargon severity (error when strict, warn otherwise); all
// other severities are fixed. Use CheckProseGate for the pass/fail contract.
func CheckProse(content string, style config.StyleSpec, strict bool) ProseReport {
	return checkProseInner(content, style, strict, nil)
}

// checkProseInner implements CheckProse / CheckProseWithVocab. vocab (when
// non-nil) exempts typo-rule tokens only; every other rule is unaffected.
func checkProseInner(content string, style config.StyleSpec, strict bool, vocab map[string]bool) ProseReport {
	normalized := strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	var violations []ProseViolation

	skipped, fenceLang := scanFences(lines)
	violations = append(violations, fenceLang...)

	checkHeadings(lines, skipped, &violations)
	checkTrailingWhitespace(lines, &violations)
	checkListMarkers(lines, skipped, &violations)
	checkTabIndentation(lines, skipped, &violations)

	// Build the prose view: drop fences, inline code spans, comments, URLs.
	spanStripped := make([]string, len(lines))
	for i, line := range lines {
		if skipped[i] {
			continue
		}
		spanStripped[i] = stripCodeSpans(line)
	}
	noComments := stripComments(spanStripped)
	var prose []proseLine
	for i, line := range noComments {
		if skipped[i] {
			continue
		}
		s := stripAutolinks(line)
		s = stripInlineLinks(s)
		s = stripBareURLs(s)
		if c := cleanProseLine(s); c != "" {
			prose = append(prose, proseLine{text: c, line: i + 1})
		}
	}

	numSentences, totalWords, longWords := collectSentences(prose, &violations)
	checkPassiveVoice(prose, &violations)
	checkJargon(prose, style.JargonBlacklist, strict, &violations)
	checkSpellingWithVocab(prose, vocab, &violations)

	score := 100.0
	for _, v := range violations {
		if v.Severity == proseError {
			score -= 5
		} else {
			score -= 2
		}
	}
	if numSentences > 0 && totalWords > 0 {
		if avg := float64(totalWords) / float64(numSentences); avg > avgSentenceThreshold {
			score -= float64(int(avg - avgSentenceThreshold))
		}
		if pct := 100 * float64(longWords) / float64(totalWords); pct > longWordPctThreshold {
			score -= float64(int(pct - longWordPctThreshold))
		}
	}
	if score < 0 {
		score = 0
	}
	return ProseReport{Violations: violations, Score: score}
}

// proseLine is one cleaned prose line plus its 1-based original line number.
type proseLine struct {
	text string
	line int
}

// ────────────────────────────────────────────────────────────────────────────
// Fences
// ────────────────────────────────────────────────────────────────────────────

// fenceMarker reports whether s opens/closes a fenced code block:
// '`' for ``` runs, '~' for ~~~ runs, 0 otherwise.
func fenceMarker(s string) byte {
	if strings.HasPrefix(s, "```") {
		return '`'
	}
	if strings.HasPrefix(s, "~~~") {
		return '~'
	}
	return 0
}

// scanFences marks every line belonging to a fenced code block (delimiters
// included) and reports fences opened without a language tag. A fence only
// closes on the same marker char, so ``` inside a ~~~ block is content.
func scanFences(lines []string) (skipped []bool, fenceLang []ProseViolation) {
	skipped = make([]bool, len(lines))
	inFence := false
	var fenceChar byte
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		marker := fenceMarker(trimmed)
		if marker == 0 {
			if inFence {
				skipped[i] = true
			}
			continue
		}
		skipped[i] = true
		if !inFence {
			inFence = true
			fenceChar = marker
			if strings.TrimSpace(trimmed[3:]) == "" {
				fenceLang = append(fenceLang, ProseViolation{
					Rule:     "fence-language",
					Message:  "fenced code block has no language tag",
					Line:     i + 1,
					Severity: proseWarn,
				})
			}
			continue
		}
		if marker == fenceChar {
			inFence = false
		}
	}
	return skipped, fenceLang
}

// ────────────────────────────────────────────────────────────────────────────
// Prose-view construction: code spans, comments, links, URLs
// ────────────────────────────────────────────────────────────────────────────

// stripCodeSpans removes same-line `...` spans (any backtick-run length,
// opener closed by an equal-length run). A lone backtick is left alone.
func stripCodeSpans(line string) string {
	var b strings.Builder
	i := 0
	for i < len(line) {
		if line[i] != '`' {
			b.WriteByte(line[i])
			i++
			continue
		}
		j := i
		for j < len(line) && line[j] == '`' {
			j++
		}
		run := j - i
		found := -1
		for k := j; k < len(line); {
			if line[k] != '`' {
				k++
				continue
			}
			m := k
			for m < len(line) && line[m] == '`' {
				m++
			}
			if m-k == run {
				found = k
				break
			}
			k = m
		}
		if found == -1 {
			b.WriteString(line[i:j])
			i = j
			continue
		}
		i = found + run
	}
	return b.String()
}

// stripComments removes <!-- ... --> comments, including multi-line ones.
func stripComments(lines []string) []string {
	out := make([]string, len(lines))
	inComment := false
	for i, line := range lines {
		s := line
		for {
			if inComment {
				end := strings.Index(s, "-->")
				if end == -1 {
					s = ""
					break
				}
				s = s[end+3:]
				inComment = false
				continue
			}
			start := strings.Index(s, "<!--")
			if start == -1 {
				break
			}
			if end := strings.Index(s[start+4:], "-->"); end == -1 {
				s = s[:start]
				inComment = true
				break
			} else {
				s = s[:start] + s[start+4+end+3:]
			}
		}
		out[i] = s
	}
	return out
}

// stripAutolinks removes <https://...> / <mail@host> autolinks.
func stripAutolinks(s string) string {
	for {
		open := strings.Index(s, "<")
		if open == -1 {
			return s
		}
		rel := strings.Index(s[open+1:], ">")
		if rel == -1 {
			return s
		}
		inner := s[open+1 : open+1+rel]
		if strings.Contains(inner, "://") || (strings.Contains(inner, "@") && !strings.Contains(inner, " ")) {
			s = s[:open] + " " + s[open+1+rel+1:]
			continue
		}
		s = s[:open] + " " + s[open+1:]
	}
}

// stripInlineLinks replaces [text](url) with "text".
func stripInlineLinks(s string) string {
	for {
		mid := strings.Index(s, "](")
		if mid == -1 {
			return s
		}
		open := strings.LastIndex(s[:mid], "[")
		if open == -1 {
			return s
		}
		rel := strings.Index(s[mid+2:], ")")
		if rel == -1 {
			return s
		}
		s = s[:open] + s[open+1:mid] + s[mid+2+rel+1:]
	}
}

// stripBareURLs removes http(s):// and www. URLs (up to whitespace or a
// closing delimiter).
func stripBareURLs(s string) string {
	for _, prefix := range []string{"https://", "http://", "www."} {
		for {
			idx := strings.Index(s, prefix)
			if idx == -1 {
				break
			}
			end := idx + len(prefix)
			for end < len(s) && !strings.ContainsRune(" \t\n\r)\"'<>", rune(s[end])) {
				end++
			}
			s = s[:idx] + " " + s[end:]
		}
	}
	return s
}

// stripATXPrefix drops leading # markers, returning the heading text.
func stripATXPrefix(s string) (string, bool) {
	i := 0
	for i < len(s) && s[i] == '#' {
		i++
	}
	if i == 0 || i > 6 {
		return s, false
	}
	if i < len(s) && s[i] != ' ' && s[i] != '\t' {
		return s, false
	}
	return strings.TrimSpace(s[i:]), true
}

// stripListMarker drops one leading list marker ("-", "*", "+", "1."/"1)").
func stripListMarker(s string) string {
	rest := strings.TrimLeft(s, " \t")
	if rest == "" {
		return s
	}
	if (rest[0] == '-' || rest[0] == '*' || rest[0] == '+') && len(rest) > 1 && (rest[1] == ' ' || rest[1] == '\t') {
		return strings.TrimSpace(rest[2:])
	}
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(rest) && (rest[i] == '.' || rest[i] == ')') && (rest[i+1] == ' ' || rest[i+1] == '\t') {
		return strings.TrimSpace(rest[i+2:])
	}
	return s
}

// isHorizontalRule reports --- / *** / ___ style rules (uniform char only).
func isHorizontalRule(s string) bool {
	t := strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "\t", "")
	if len(t) < 3 {
		return false
	}
	for i := 0; i < len(t); i++ {
		if t[i] != t[0] || (t[i] != '-' && t[i] != '*' && t[i] != '_') {
			return false
		}
	}
	return true
}

// cleanProseLine reduces a raw line to its prose content: heading markers,
// blockquote markers, list markers, table pipes and hr lines are normalized
// away; "" means "no prose on this line".
func cleanProseLine(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return ""
	}
	if h, ok := stripATXPrefix(t); ok {
		t = strings.TrimSpace(h)
	}
	for strings.HasPrefix(t, ">") {
		t = strings.TrimSpace(strings.TrimPrefix(t, ">"))
	}
	t = stripListMarker(t)
	if isHorizontalRule(t) {
		return ""
	}
	t = strings.ReplaceAll(t, "|", " ")
	return strings.TrimSpace(t)
}

// ────────────────────────────────────────────────────────────────────────────
// Word helpers
// ────────────────────────────────────────────────────────────────────────────

// trimWord strips non-letter/digit runes from both ends ("build." → "build").
func trimWord(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// isWordToken reports whether s carries word content (vs pure punctuation).
func isWordToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// letterCount counts unicode letters in s.
func letterCount(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n
}

// isASCIIWordByte reports [A-Za-z0-9_] for jargon boundary checks.
func isASCIIWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// ────────────────────────────────────────────────────────────────────────────
// Rule 1 + readability: sentence splitting
// ────────────────────────────────────────────────────────────────────────────

// collectSentences splits prose on '.', '!' and '?' (only when followed by
// whitespace or end-of-text), counts words, flags sentences over
// maxSentenceWords, and returns sentence/word/long-word totals for the
// readability score fold-in.
func collectSentences(prose []proseLine, violations *[]ProseViolation) (numSentences, totalWords, longWords int) {
	var chars []rune
	var lines []int
	for _, pl := range prose {
		for _, r := range pl.text {
			chars = append(chars, r)
			lines = append(lines, pl.line)
		}
		chars = append(chars, '\n')
		lines = append(lines, pl.line)
	}
	var word []rune
	curWords := 0
	flushWord := func() {
		w := trimWord(string(word))
		word = word[:0]
		if !isWordToken(w) {
			return
		}
		curWords++
		totalWords++
		if letterCount(w) >= longWordMinLetters {
			longWords++
		}
	}
	endSentence := func(line int) {
		if curWords == 0 {
			return
		}
		numSentences++
		if curWords > maxSentenceWords {
			*violations = append(*violations, ProseViolation{
				Rule:     "sentence-length",
				Message:  fmt.Sprintf("sentence has %d words (limit %d)", curWords, maxSentenceWords),
				Line:     line,
				Severity: proseWarn,
			})
		}
		curWords = 0
	}
	for i, r := range chars {
		switch {
		case r == '.' || r == '!' || r == '?':
			flushWord()
			if i+1 >= len(chars) || unicode.IsSpace(chars[i+1]) {
				endSentence(lines[i])
			}
		case unicode.IsSpace(r):
			flushWord()
		default:
			word = append(word, r)
		}
	}
	flushWord()
	if len(prose) > 0 {
		endSentence(prose[len(prose)-1].line)
	}
	return numSentences, totalWords, longWords
}

// ────────────────────────────────────────────────────────────────────────────
// Rule 2: passive voice (heuristic — see package doc)
// ────────────────────────────────────────────────────────────────────────────

// checkPassiveVoice flags be-verb + participle-looking word (ending in
// "ed"/"en") within passiveLookahead tokens. One violation per be-verb.
func checkPassiveVoice(prose []proseLine, violations *[]ProseViolation) {
	type tok struct {
		word string
		line int
	}
	var toks []tok
	for _, pl := range prose {
		for _, f := range strings.Fields(pl.text) {
			w := strings.ToLower(trimWord(f))
			if w == "" {
				continue
			}
			toks = append(toks, tok{word: w, line: pl.line})
		}
	}
	for i, t := range toks {
		if !beVerbs[t.word] {
			continue
		}
		for j := i + 1; j < len(toks) && j <= i+passiveLookahead; j++ {
			if strings.HasSuffix(toks[j].word, "ed") || strings.HasSuffix(toks[j].word, "en") {
				*violations = append(*violations, ProseViolation{
					Rule:     "passive-voice",
					Message:  fmt.Sprintf("possible passive voice: %q followed by %q (heuristic)", t.word, toks[j].word),
					Line:     t.line,
					Severity: proseWarn,
				})
				break
			}
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Rule 3: jargon blacklist (only configured terms; empty list disables)
// ────────────────────────────────────────────────────────────────────────────

// checkJargon flags every whole-word occurrence of each blacklisted term.
// Severity is error in strict mode, warn otherwise.
func checkJargon(prose []proseLine, blacklist []string, strict bool, violations *[]ProseViolation) {
	sev := proseWarn
	if strict {
		sev = proseError
	}
	for _, pl := range prose {
		lower := strings.ToLower(pl.text)
		for _, term := range blacklist {
			t := strings.ToLower(strings.TrimSpace(term))
			if t == "" || len(t) > len(lower) {
				continue
			}
			for from := 0; from <= len(lower)-len(t); {
				rel := strings.Index(lower[from:], t)
				if rel == -1 {
					break
				}
				start := from + rel
				end := start + len(t)
				from = end
				if start > 0 && isASCIIWordByte(lower[start-1]) {
					continue
				}
				if end < len(lower) && isASCIIWordByte(lower[end]) {
					continue
				}
				*violations = append(*violations, ProseViolation{
					Rule:     "jargon",
					Message:  fmt.Sprintf("jargon term %q is blacklisted by style config", term),
					Line:     pl.line,
					Severity: sev,
				})
			}
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Rule 4: typos + doubled words (error severity)
// ────────────────────────────────────────────────────────────────────────────

// checkSpelling flags fixed-list typos and doubled adjacent words.
func checkSpelling(prose []proseLine, violations *[]ProseViolation) {
	checkSpellingWithVocab(prose, nil, violations)
}

// checkSpellingWithVocab is checkSpelling with a learned-vocabulary
// exemption (gap C1): tokens present in vocab (lowercased) skip the
// fixed-list typo rule. Doubled-word detection is unaffected — a repeated
// project term is still a doubled word.
func checkSpellingWithVocab(prose []proseLine, vocab map[string]bool, violations *[]ProseViolation) {
	prev := ""
	for _, pl := range prose {
		for _, f := range strings.Fields(pl.text) {
			w := strings.ToLower(trimWord(f))
			if w == "" {
				continue
			}
			if typoWords[w] && !vocab[w] {
				*violations = append(*violations, ProseViolation{
					Rule:     "typo",
					Message:  fmt.Sprintf("possible typo %q", w),
					Line:     pl.line,
					Severity: proseError,
				})
			}
			if w == prev && letterCount(w) > 0 {
				*violations = append(*violations, ProseViolation{
					Rule:     "doubled-word",
					Message:  fmt.Sprintf("doubled word %q", w),
					Line:     pl.line,
					Severity: proseError,
				})
			}
			prev = w
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Rule 5: markdownlint-class structural rules
// ────────────────────────────────────────────────────────────────────────────

// checkHeadings flags heading-level skips (error) and multiple H1s (warn).
func checkHeadings(lines []string, skipped []bool, violations *[]ProseViolation) {
	lastLevel := 0
	h1Count := 0
	for i, line := range lines {
		if skipped[i] {
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		level := 0
		for level < len(trimmed) && trimmed[level] == '#' {
			level++
		}
		if level == 0 || level > 6 {
			continue
		}
		if level < len(trimmed) && trimmed[level] != ' ' && trimmed[level] != '\t' {
			continue
		}
		if lastLevel > 0 && level > lastLevel+1 {
			*violations = append(*violations, ProseViolation{
				Rule:     "heading-skip",
				Message:  fmt.Sprintf("heading jumps from H%d to H%d", lastLevel, level),
				Line:     i + 1,
				Severity: proseError,
			})
		}
		lastLevel = level
		if level == 1 {
			h1Count++
			if h1Count > 1 {
				*violations = append(*violations, ProseViolation{
					Rule:     "multiple-h1",
					Message:  "document has more than one H1 heading",
					Line:     i + 1,
					Severity: proseWarn,
				})
			}
		}
	}
}

// checkTrailingWhitespace flags every line with trailing spaces/tabs.
func checkTrailingWhitespace(lines []string, violations *[]ProseViolation) {
	for i, line := range lines {
		if strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t") {
			*violations = append(*violations, ProseViolation{
				Rule:     "trailing-whitespace",
				Message:  "line has trailing whitespace",
				Line:     i + 1,
				Severity: proseWarn,
			})
		}
	}
}

// unorderedItem parses an unordered list item into (indent, marker).
func unorderedItem(line string) (string, byte, bool) {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i < len(line) && (line[i] == '-' || line[i] == '*' || line[i] == '+') &&
		i+1 < len(line) && (line[i+1] == ' ' || line[i+1] == '\t') {
		return line[:i], line[i], true
	}
	return "", 0, false
}

// checkListMarkers flags mixed "-"/"*"/"+" markers at the same indent
// within one contiguous list (blank lines, ordered items, fences and any
// other content end the current list run).
func checkListMarkers(lines []string, skipped []bool, violations *[]ProseViolation) {
	var (
		inList      bool
		markers     map[string]map[byte]int
		secondSeen  map[string][]byte
		indentOrder []string
	)
	flushRun := func() {
		if !inList {
			return
		}
		for _, indent := range indentOrder {
			if len(markers[indent]) > 1 {
				offender := secondSeen[indent][1]
				*violations = append(*violations, ProseViolation{
					Rule:     "list-marker",
					Message:  fmt.Sprintf("inconsistent list markers at indent %d", len(indent)),
					Line:     markers[indent][offender],
					Severity: proseWarn,
				})
			}
		}
		inList = false
		markers = nil
		secondSeen = nil
		indentOrder = nil
	}
	for i, line := range lines {
		if skipped[i] || strings.TrimSpace(line) == "" {
			flushRun()
			continue
		}
		indent, marker, ok := unorderedItem(line)
		if !ok {
			flushRun()
			continue
		}
		if !inList {
			inList = true
			markers = map[string]map[byte]int{}
			secondSeen = map[string][]byte{}
		}
		if _, exists := markers[indent]; !exists {
			markers[indent] = map[byte]int{}
			indentOrder = append(indentOrder, indent)
		}
		if _, seen := markers[indent][marker]; !seen {
			markers[indent][marker] = i + 1
			secondSeen[indent] = append(secondSeen[indent], marker)
		}
	}
	flushRun()
}

// checkTabIndentation flags tab-indented lines outside fenced blocks.
func checkTabIndentation(lines []string, skipped []bool, violations *[]ProseViolation) {
	for i, line := range lines {
		if skipped[i] {
			continue
		}
		j := 0
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}
		if j >= len(line) {
			continue
		}
		if strings.Contains(line[:j], "\t") {
			*violations = append(*violations, ProseViolation{
				Rule:     "tab-indentation",
				Message:  "line is indented with a tab",
				Line:     i + 1,
				Severity: proseError,
			})
		}
	}
}
