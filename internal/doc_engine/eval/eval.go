// Package eval implements the faithfulness evaluation harness for the
// documentation engine (improvement plan D1: "Faithfulness evaluation
// harness (RAGAS-style, deterministic-first)").
//
// Reference-free scoring: the FactSheet assembled deterministically from the
// AKG *is* the ground truth, so no gold docs are needed and every managed
// section can be scored. Score = supported claims / total claims (RAGAS
// faithfulness formula).
//
// Claim decomposition (v1): prose is split into sentences on . ! ? boundaries
// (plus line breaks, so Markdown headings count as claims). Each sentence is
// one claim. This is sentence-granularity, NOT atomic-subject decomposition
// (one subject-predicate proposition per claim); finer decomposition is
// future work. Fenced code blocks, HTML comments, and URLs are stripped
// before splitting (they are not prose claims); inline code spans are kept
// because backticked identifiers are the strict entity check.
//
// Deterministic support rule per claim (exact):
//
//	(a) if the claim contains backticked identifiers, it is supported iff
//	    EVERY backticked identifier appears in the FactSheet ground truth
//	    (symbols, sentinels, config keys, signatures, doc comments, purpose,
//	    audience, and instruction text); otherwise
//	(b) if the claim contains no backticks, it is supported iff it shares
//	    >=1 non-stopword token (length >= 4, case-insensitive) with the
//	    ground-truth text blob (signatures + docs + instruction + purpose,
//	    plus config/sentinel/call-flow text).
//
// Tradeoff (deliberate): the metric is STRICT on invented entities (any
// unknown `Symbol` fails its claim even if the surrounding prose is fluent)
// and LENIENT on phrasing (a paraphrase with one shared content word
// passes). That matches the failure users feel most (invented facts) while
// keeping the check deterministic, offline, and zero-token. Claims with no
// content-bearing tokens at all cannot pass deterministically and are left
// for the Judge.
//
// Judge design: Judge is a two-method-free seam (one method:
// Supported(claim, context)). LLM judges (or a small open NLI classifier in
// the Vectara HHEM spirit) implement it later. ScoreSectionWithJudge runs the
// deterministic pass first and consults the judge ONLY for claims the
// deterministic pass rejects: deterministic pass => supported; deterministic
// fail + nil judge => unsupported; deterministic fail + judge => the judge
// decides. The context argument carries the deterministic ground-truth text
// blob so judges can do entailment without refetching facts. Question
// generation (RAGAS "reverse questions") is intentionally SKIPPED: it needs
// an LLM; InstructionSatisfied is the deterministic relevance proxy
// (answer-relevancy direction: does the prose satisfy its instruction?).
package eval

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// FaithfulnessReport is the result of scoring one rendered section.
type FaithfulnessReport struct {
	// Score is SupportedClaims/TotalClaims, or 1.0 when TotalClaims == 0.
	Score float64
	// TotalClaims is the number of sentence-granularity claims found.
	TotalClaims int
	// SupportedClaims is the number of claims with ground-truth support.
	SupportedClaims int
	// Unsupported lists the verbatim claim texts that failed support,
	// in document order.
	Unsupported []string
}

// Judge resolves claims the deterministic pass rejects. LLM judges (or an
// offline NLI classifier) implement this later; nil means
// deterministic-only. The context argument is the deterministic
// ground-truth text blob (signatures + docs + instruction + purpose).
type Judge interface {
	Supported(claim, context string) bool
}

// ScoreSection scores prose deterministically (no network, no LLM).
// Equivalent to ScoreSectionWithJudge(prose, fs, nil).
func ScoreSection(prose string, fs *config.FactSheet) FaithfulnessReport {
	return ScoreSectionWithJudge(prose, fs, nil)
}

// ScoreSectionWithJudge scores prose deterministically first; claims
// unresolved deterministically go to j. A nil judge means fall back to the
// deterministic verdict only (deterministic fail + nil judge => unsupported).
func ScoreSectionWithJudge(prose string, fs *config.FactSheet, j Judge) FaithfulnessReport {
	claims := splitClaims(prose)
	rep := FaithfulnessReport{TotalClaims: len(claims)}
	if len(claims) == 0 {
		rep.Score = 1.0
		return rep
	}
	blob := groundTruthText(fs)
	blobLower := strings.ToLower(blob)
	blobTokens := tokenSet(blobLower)
	for _, c := range claims {
		if claimSupported(c, blobLower, blobTokens) {
			rep.SupportedClaims++
			continue
		}
		if j != nil && j.Supported(c, blob) {
			rep.SupportedClaims++
			continue
		}
		rep.Unsupported = append(rep.Unsupported, c)
	}
	rep.Score = float64(rep.SupportedClaims) / float64(rep.TotalClaims)
	return rep
}

// InstructionSatisfied is the deterministic relevance proxy (RAGAS
// answer-relevancy direction, without question generation): it reports
// whether prose plausibly satisfies instruction. True iff the two share >=2
// distinct non-stopword tokens (length >= 4, case-insensitive), or the
// instruction is empty (nothing to satisfy).
func InstructionSatisfied(prose, instruction string) bool {
	if strings.TrimSpace(instruction) == "" {
		return true
	}
	proseToks := contentTokenSet(prose)
	shared := 0
	for _, tok := range contentTokens(instruction) {
		if _, ok := proseToks[tok]; ok {
			shared++
		}
	}
	return shared >= 2
}

// ---------------------------------------------------------------------------
// Claim decomposition
// ---------------------------------------------------------------------------

// deterministicRendererHeadings are the fixed literal section headings
// deterministic.go emits verbatim in every Track B render (see the
// sb.WriteString("### ...") call sites there). Unlike an LLM-authored
// heading, these can never be hallucinated — they are Go string literals
// chosen by the renderer, not model output — but they also essentially
// never share a content word with a FactSheet's ground_truth JSON (whose
// keys are snake_case field names like "config_vars", not this English
// phrasing), so scoring them as claims failed every 100%-faithful
// deterministic section for having structure at all. Kept as an explicit,
// narrow allowlist (rather than skipping every heading) so an arbitrary,
// possibly-hallucinated LLM-authored heading is still scored normally.
var deterministicRendererHeadings = map[string]bool{
	"Endpoints":                true,
	"Call Flow":                true,
	"Direct Callers":           true,
	"Functions and Methods":    true,
	"Types and Interfaces":     true,
	"Error Catalog":            true,
	"Configuration Variables":  true,
	"Architectural Milestones": true,
	"Recent Symbol Changes":    true,
}

// splitClaims normalizes prose (CRLF -> LF), strips fenced code blocks, HTML
// comments, and URLs, drops known deterministic-renderer headings and
// table-header/separator lines (structural labels, not factual assertions
// — see the notes above and below), then splits into sentences on . ! ?
// boundaries (plus line breaks). Each non-empty fragment containing
// alphanumeric content is one claim.
//
// Table header rows ("| Name | Signature | Description | File |") and
// their separator rows ("| --- | --- | --- | --- |") are always dropped:
// there is no LLM-authored equivalent to worry about hallucinating — every
// Track A/B table in this engine uses the same fixed column layout per
// table type, emitted by the renderer, never invented per-row.
func splitClaims(prose string) []string {
	text := strings.ReplaceAll(prose, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = stripFencedCode(text)
	text = stripHTMLComments(text)
	text = stripLinksAndURLs(text)
	rawLines := strings.Split(text, "\n")
	drop := make([]bool, len(rawLines))
	for i, ln := range rawLines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "#") && deterministicRendererHeadings[strings.TrimSpace(strings.TrimLeft(t, "#"))] {
			drop[i] = true
		}
		if isTableSeparatorRow(ln) {
			drop[i] = true
			if i > 0 {
				drop[i-1] = true // the header row this separator belongs to
			}
		}
	}
	lines := make([]string, len(rawLines))
	for i, ln := range rawLines {
		if drop[i] {
			continue
		}
		lines[i] = stripLineMarkup(strings.TrimSpace(ln))
	}
	var claims []string
	for _, sent := range splitSentences(strings.Join(lines, "\n")) {
		for _, part := range strings.Split(sent, "\n") {
			part = strings.Join(strings.Fields(part), " ")
			if part == "" || !hasAlnum(part) {
				continue
			}
			claims = append(claims, part)
		}
	}
	return claims
}

// isTableSeparatorRow reports whether ln is a Markdown table separator row:
// once every "|", "-", ":", and whitespace character is removed, nothing is
// left (and at least one "-" was present, so a blank line doesn't count).
// Handles any column count, e.g. "| --- | :--- | ---: |".
func isTableSeparatorRow(ln string) bool {
	if !strings.Contains(ln, "-") {
		return false
	}
	stripped := strings.Map(func(r rune) rune {
		switch r {
		case '|', '-', ':', ' ', '\t':
			return -1
		}
		return r
	}, ln)
	return stripped == ""
}

// splitSentences splits on runs of . ! ? that terminate a sentence: the run
// (plus any closing quotes/parens/brackets) must be followed by whitespace
// or end of input. Splits inside single-backtick code spans are suppressed
// so `ident.` stays one claim, and common abbreviations (e.g. "e.g.") do
// not split.
func splitSentences(s string) []string {
	var out []string
	start := 0
	inCode := false
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '`' {
			inCode = !inCode
			i++
			continue
		}
		if inCode || (c != '.' && c != '!' && c != '?') {
			i++
			continue
		}
		j := i
		for j < len(s) && (s[j] == '.' || s[j] == '!' || s[j] == '?') {
			j++
		}
		k := j
		for k < len(s) && (s[k] == '"' || s[k] == '\'' || s[k] == ')' || s[k] == ']' || s[k] == '`') {
			k++
		}
		boundary := k >= len(s) || s[k] == ' ' || s[k] == '\t' || s[k] == '\n'
		if boundary && c == '.' {
			if _, ok := abbrevBeforePeriod[strings.ToLower(wordBefore(s, start, i))]; ok {
				boundary = false
			}
		}
		if !boundary {
			i = j
			continue
		}
		out = append(out, s[start:k])
		for k < len(s) && (s[k] == ' ' || s[k] == '\t' || s[k] == '\n') {
			k++
		}
		start = k
		i = k
	}
	if rest := strings.TrimSpace(s[start:]); rest != "" {
		out = append(out, s[start:])
	}
	return out
}

// wordBefore returns the alphabetic word immediately before position i.
func wordBefore(s string, start, i int) string {
	end := i
	for i > start && isLetterByte(s[i-1]) {
		i--
	}
	return s[i:end]
}

func isLetterByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// abbrevBeforePeriod guards sentence splitting after common abbreviations.
var abbrevBeforePeriod = map[string]struct{}{
	"e.g": {}, "i.e": {}, "etc": {}, "mr": {}, "mrs": {}, "ms": {},
	"dr": {}, "vs": {}, "fig": {}, "no": {}, "st": {}, "jr": {},
	"sr": {}, "prof": {},
}

// stripFencedCode removes ``` and ~~~ fenced blocks (fence lines and
// contents). An unclosed fence drops the remainder.
func stripFencedCode(s string) string {
	lines := strings.Split(s, "\n")
	var kept []string
	inFence := false
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if !inFence {
			kept = append(kept, ln)
		}
	}
	return strings.Join(kept, "\n")
}

// stripHTMLComments removes <!-- ... --> spans. An unclosed opener drops
// the remainder.
func stripHTMLComments(s string) string {
	var b strings.Builder
	for {
		open := strings.Index(s, "<!--")
		if open < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:open])
		rest := s[open+len("<!--"):]
		close := strings.Index(rest, "-->")
		if close < 0 {
			break
		}
		s = rest[close+len("-->"):]
	}
	return b.String()
}

var (
	bareURLRe  = regexp.MustCompile(`https?://\S+|www\.\S+`)
	linkRe     = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	autoLinkRe = regexp.MustCompile(`<https?://[^>]*>`)
)

// stripLinksAndURLs removes bare URLs and autolinks and reduces Markdown
// links [text](target) to their visible text.
func stripLinksAndURLs(s string) string {
	s = autoLinkRe.ReplaceAllString(s, "")
	s = linkRe.ReplaceAllString(s, "$1")
	s = bareURLRe.ReplaceAllString(s, "")
	return s
}

// stripLineMarkup removes per-line Markdown markers (headings, blockquotes,
// list bullets) while keeping the text as a claim candidate.
func stripLineMarkup(ln string) string {
	for strings.HasPrefix(ln, "#") {
		ln = strings.TrimSpace(strings.TrimLeft(ln, "#"))
	}
	for strings.HasPrefix(ln, ">") {
		ln = strings.TrimSpace(strings.TrimLeft(ln, ">"))
	}
	if m := listMarkerRe.FindString(ln); m != "" {
		ln = strings.TrimSpace(strings.TrimPrefix(ln, m))
	}
	return ln
}

var listMarkerRe = regexp.MustCompile(`^([-*+]\s+|\d+[.)]\s+)`)

// hasAlnum reports whether s contains any letter or digit (markup-only
// fragments such as table separators are not claims).
func hasAlnum(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Deterministic support
// ---------------------------------------------------------------------------

// claimSupported applies the exact rule: (a) claims with backticked
// identifiers need every identifier grounded; (b) claims without backticks
// need >=1 shared content token.
func claimSupported(claim, blobLower string, blobTokens map[string]struct{}) bool {
	spans := extractBacktickSpans(claim)
	hasIdent := false
	for _, sp := range spans {
		pieces := identPieces(sp)
		if len(pieces) == 0 {
			continue
		}
		hasIdent = true
		if strings.Contains(blobLower, strings.ToLower(strings.TrimSpace(sp))) {
			continue
		}
		for _, p := range pieces {
			if _, ok := blobTokens[strings.ToLower(p)]; !ok {
				return false
			}
		}
	}
	if hasIdent {
		return true
	}
	for _, tok := range contentTokens(claim) {
		if _, ok := blobTokens[tok]; ok {
			return true
		}
	}
	return false
}

// extractBacktickSpans returns the contents of `...` spans (single or
// repeated-backtick runs, CommonMark same-length close). Unclosed runs are
// ignored.
func extractBacktickSpans(s string) []string {
	var spans []string
	i := 0
	for i < len(s) {
		if s[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] == '`' {
			j++
		}
		n := j - i
		found := -1
		k := j
		for k < len(s) {
			if s[k] != '`' {
				k++
				continue
			}
			m := k
			for m < len(s) && s[m] == '`' {
				m++
			}
			if m-k == n {
				found = k
				break
			}
			k = m
		}
		if found < 0 {
			break
		}
		spans = append(spans, s[j:found])
		i = found + n
	}
	return spans
}

// identPieces splits a backticked span into identifier-like pieces
// ([A-Za-z0-9_]+ runs, pure numbers dropped). A span with no pieces (empty
// or punctuation-only) does not affect support.
func identPieces(span string) []string {
	fields := strings.FieldsFunc(span, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
	})
	var out []string
	for _, f := range fields {
		if f == "" || isNumeric(f) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func isNumeric(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return len(s) > 0
}

// groundTruthText concatenates every FactSheet field that counts as ground
// truth: purpose, audience, instruction, commit context, symbols
// (FQN/signature/doc/file), deltas, removals, call flow, callers, diagrams,
// doc comments, config vars, sentinels, and arch events.
func groundTruthText(fs *config.FactSheet) string {
	if fs == nil {
		return ""
	}
	var b strings.Builder
	write := func(s string) {
		if s != "" {
			b.WriteString(s)
			b.WriteString("\n")
		}
	}
	write(fs.DocPurpose)
	write(fs.DocAudience)
	write(fs.SectionInstruction)
	write(fs.CommitReason)
	write(fs.CommitIntent)
	gt := fs.GroundTruth
	writeSymbols := func(list []config.SymbolFact) {
		for _, s := range list {
			write(s.FQN)
			write(s.Signature)
			write(s.Doc)
			write(s.File)
		}
	}
	writeSymbols(gt.AddedSymbols)
	writeSymbols(gt.AllSymbols)
	writeSymbols(gt.Symbols)
	for _, d := range gt.ModifiedSymbols {
		write(d.FQN)
		write(d.Before)
		write(d.After)
		write(d.DocBefore)
		write(d.DocAfter)
	}
	for _, s := range gt.RemovedSymbols {
		write(s)
	}
	for _, s := range gt.CallFlow {
		write(s)
	}
	for _, s := range gt.Callers {
		write(s)
	}
	write(gt.DiagramMermaid)
	for _, v := range gt.DocComments {
		write(v)
	}
	writeConfigVars := func(list []config.ConfigVarFact) {
		for _, c := range list {
			write(c.Name)
			write(c.Doc)
			write(c.Source)
			write(c.File)
		}
	}
	writeConfigVars(gt.ConfigVars)
	writeConfigVars(gt.AddedConfigVars)
	for _, s := range gt.RemovedConfigVars {
		write(s)
	}
	writeSentinels := func(list []config.SentinelFact) {
		for _, e := range list {
			write(e.FQN)
			write(e.Value)
			write(e.Doc)
			write(e.File)
		}
	}
	writeSentinels(gt.Sentinels)
	writeSentinels(gt.AddedSentinels)
	for _, d := range gt.ModifiedSentinels {
		write(d.FQN)
		write(d.Before)
		write(d.After)
	}
	for _, s := range gt.ArchEvents {
		write(s)
	}
	for _, d := range gt.Diagrams {
		write(d.Content)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Tokens and stopwords
// ---------------------------------------------------------------------------

// stopwords is the embedded ~40-word common-word list. Tokens on this list
// never count as grounding overlap.
var stopwords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "and": {}, "or": {}, "but": {},
	"of": {}, "to": {}, "in": {}, "on": {}, "for": {}, "with": {},
	"as": {}, "at": {}, "by": {}, "from": {}, "is": {}, "are": {},
	"was": {}, "were": {}, "be": {}, "been": {}, "has": {}, "have": {},
	"had": {}, "do": {}, "does": {}, "did": {}, "will": {}, "would": {},
	"can": {}, "could": {}, "should": {}, "may": {}, "might": {}, "must": {},
	"it": {}, "its": {}, "this": {}, "that": {}, "these": {}, "those": {},
	"they": {}, "them": {}, "their": {},
}

// tokenize splits s into lowercase [letter/digit/underscore] tokens.
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
	})
}

// tokenSet returns all tokens (any length) for identifier membership.
func tokenSet(s string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, t := range tokenize(s) {
		set[t] = struct{}{}
	}
	return set
}

// contentTokens returns non-stopword tokens with length >= 4 (duplicates
// preserved, document order).
func contentTokens(s string) []string {
	var out []string
	for _, t := range tokenize(s) {
		if len([]rune(t)) < 4 {
			continue
		}
		if _, stop := stopwords[t]; stop {
			continue
		}
		out = append(out, t)
	}
	return out
}

// contentTokenSet returns the distinct content tokens of s.
func contentTokenSet(s string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, t := range contentTokens(s) {
		set[t] = struct{}{}
	}
	return set
}
