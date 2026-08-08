package knowledge_fusion

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// DocKind distinguishes the document flavours the parser understands.
type DocKind string

const (
	// DocKindADR marks an Architecture Decision Record (marked-down
	// decision history — see the ADR template in the package tests).
	DocKindADR DocKind = "adr"

	// DocKindReadme marks a README-style file parsed for technology
	// mentions.
	DocKindReadme DocKind = "readme"
)

// DocSource identifies one discovered source document. Rel is the
// repo-relative, slash-separated path used as the evidence reference and as
// part of the claim identity.
type DocSource struct {
	Path    string
	Rel     string
	Kind    DocKind
	ModTime time.Time
}

// skipDirs are directory names never walked for documentation. They mirror
// the pipeline's own walker exclusions so vendored/derived content cannot
// leak claims into memory.
var skipDirs = map[string]bool{
	".git": true, ".glassmarble": true, "node_modules": true,
	"vendor": true, "dist": true, "build": true, "target": true,
	"__pycache__": true, ".venv": true, "venv": true, "coverage": true,
}

// FindDocs discovers ADR and README sources under repoDir according to the
// config globs. Results are sorted by relative path for determinism, and
// oversized files are skipped. A repoDir that is not a directory is an
// error; an empty result is not.
func FindDocs(repoDir string, cfg *config.FusionConfig) ([]DocSource, error) {
	if cfg == nil {
		cfg = config.DefaultFusionConfig()
	}
	abs, err := filepath.Abs(repoDir)
	if err != nil {
		return nil, fmt.Errorf("knowledge_fusion: resolve repo dir: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("knowledge_fusion: stat repo dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("knowledge_fusion: %q is not a directory", repoDir)
	}

	adrPatterns := cfg.ADRGlobs
	readmeNames := make(map[string]bool, len(cfg.ReadmeFiles))
	for _, name := range cfg.ReadmeFiles {
		readmeNames[strings.ToLower(filepath.ToSlash(name))] = true
	}

	var docs []DocSource
	err = filepath.WalkDir(abs, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if p != abs && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(abs, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		kind, ok := matchDoc(rel, adrPatterns, readmeNames)
		if !ok {
			return nil
		}

		fi, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if cfg.DocMaxSizeBytes > 0 && fi.Size() > cfg.DocMaxSizeBytes {
			return nil
		}
		docs = append(docs, DocSource{
			Path:    p,
			Rel:     rel,
			Kind:    kind,
			ModTime: fi.ModTime().UTC(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge_fusion: walk docs: %w", err)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Rel < docs[j].Rel })
	return docs, nil
}

// matchDoc decides whether a repo-relative path is an ADR (glob match) or a
// configured README (exact name match). ADR globs are matched
// case-insensitively; README names are compared case-insensitively.
func matchDoc(rel string, adrPatterns []string, readmeNames map[string]bool) (DocKind, bool) {
	if readmeNames[strings.ToLower(rel)] {
		return DocKindReadme, true
	}
	for _, pattern := range adrPatterns {
		if globMatch(pattern, rel) {
			return DocKindADR, true
		}
	}
	return "", false
}

// globMatch reports whether path matches pattern with "**" spanning zero or
// more path segments and "*"/"?" per segment. Matching is
// case-insensitive so "docs/ADR" and "docs/adr" both work.
func globMatch(pattern, path string) bool {
	pp := strings.Split(strings.ToLower(filepath.ToSlash(pattern)), "/")
	np := strings.Split(strings.ToLower(filepath.ToSlash(path)), "/")
	return matchSegs(pp, np)
}

func matchSegs(p, n []string) bool {
	if len(p) == 0 {
		return len(n) == 0
	}
	if p[0] == "**" {
		if matchSegs(p[1:], n) {
			return true
		}
		if len(n) > 0 && matchSegs(p, n[1:]) {
			return true
		}
		return false
	}
	if len(n) == 0 {
		return false
	}
	ok, err := filepath.Match(p[0], n[0])
	if err != nil || !ok {
		return false
	}
	return matchSegs(p[1:], n[1:])
}

// --- ADR parsing ---

// adrTitlePrefix strips ADR numbering conventions from a title so the claim
// subject is the decision itself ("0001-record-arch.md → Record Arch",
// "ADR-0002 Use Redis → Use Redis", "12. Add JWT → Add JWT").
var adrTitlePrefix = regexp.MustCompile(`(?i)^(?:adr[- ]?\d+\s*[-:.]?\s*|\d{1,5}\s*[-.:]?\s*|\[\w+\]\s*)`)

// ParseADR parses one Architecture Decision Record into knowledge claims.
//
// Supported shape (see the template in doc_parser_test.go):
//
//	---
//	front matter (optional)
//	---
//	# [NNNN-]Title
//	## Status       Accepted | Deprecated | Superseded | ...
//	## Context      prose
//	## Decision     prose (may also be "## Decision: inline text")
//	## Consequences prose
//
// Output claims:
//
//  1. one decision claim — subject: title, predicate "decided_to",
//     object: the decision text, ClaimKind EXPLICIT_REASON (explicit
//     human decision documentation);
//  2. one "decided_to_use" claim per technology mentioned in the decision
//     text, so ADRs become queryable by technology ("why is Redis used?"
//     surfaces the ADR that chose it).
//
// The status maps onto the claim state (accepted → CURRENT,
// deprecated/superseded → DEPRECATED, proposed/experimental →
// EXPERIMENTAL). Evidence is SourceDocs with the file path as reference and
// the Context + Decision excerpts, timestamped with the file's mtime — the
// closest proxy for when the decision was written. lexicon is the effective
// technology lexicon (config-driven) used for the "decided_to_use" claims.
func ParseADR(doc DocSource, lexicon []string) ([]developer_memory.KnowledgeClaim, error) {
	data, err := os.ReadFile(doc.Path)
	if err != nil {
		return nil, fmt.Errorf("knowledge_fusion: read ADR %s: %w", doc.Rel, err)
	}
	title, status, context, decision, err := parseADRSections(string(data))
	if err != nil {
		return nil, fmt.Errorf("knowledge_fusion: parse ADR %s: %w", doc.Rel, err)
	}

	state := developer_memory.StateActive
	switch {
	case containsFold(status, "deprecated"), containsFold(status, "superseded"), containsFold(status, "obsolete"):
		state = developer_memory.StateDeprecated
	case containsFold(status, "proposed"), containsFold(status, "experimental"), containsFold(status, "draft"):
		state = developer_memory.StateExperimental
	}

	excerpt := strings.TrimSpace("Context: " + context + " | Decision: " + decision)
	bundle := evidence.NewBundle(evidence.EvidenceItem{
		Source:     evidence.SourceDocs,
		Reference:  doc.Rel,
		Excerpt:    excerpt,
		Confidence: 0.95,
		Timestamp:  doc.ModTime,
	})

	claims := []developer_memory.KnowledgeClaim{
		newFusedClaim(
			"adr", doc.Rel, title, "decided_to", decision,
			developer_memory.ClaimExplicitReason, state,
			doc.ModTime, bundle, "", "",
		),
	}

	for _, tech := range techMentions(decision, lexicon) {
		b := evidence.NewBundle(evidence.EvidenceItem{
			Source:     evidence.SourceDocs,
			Reference:  doc.Rel,
			Excerpt:    decision,
			Confidence: 0.95,
			Timestamp:  doc.ModTime,
		})
		claims = append(claims, newFusedClaim(
			"adr", doc.Rel, title, "decided_to_use", tech,
			developer_memory.ClaimExplicitReason, state,
			doc.ModTime, b, "", "",
		))
	}
	return claims, nil
}

// parseADRSections extracts the title and the Status/Context/Decision/
// Consequences sections from an ADR body. Heading matching is
// case-insensitive, headings may carry inline content ("## Decision: Use
// Redis"), and a leading YAML front matter block is skipped. A UTF-8 BOM
// (common on Windows-authored files) is stripped before parsing. An ADR
// without a title or without a decision is malformed.
func parseADRSections(content string) (title, status, context, decision string, err error) {
	content = strings.TrimPrefix(content, "\uFEFF")
	lines := strings.Split(content, "\n")
	var section string
	inFence := false
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		// Skip YAML front matter (--- delimited) at the top of the file.
		if i == 0 && line == "---" {
			for j := i + 1; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == "---" {
					i = j
					break
				}
			}
			continue
		}
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}

		switch {
		case strings.HasPrefix(line, "# "):
			title = cleanADRTitle(strings.TrimPrefix(line, "# "))
			section = ""
		case isHeading(line, "status"):
			section, line = "status", headingInline(line)
			status += " " + line
		case isHeading(line, "context"):
			section, line = "context", headingInline(line)
			context += " " + line
		case isHeading(line, "decision"):
			section, line = "decision", headingInline(line)
			decision += " " + line
		case isHeading(line, "consequences"):
			section = ""
		case strings.HasPrefix(line, "#"):
			section = ""
		case line != "":
			switch section {
			case "status":
				status += " " + line
			case "context":
				context += " " + line
			case "decision":
				decision += " " + line
			}
		}
	}

	title = strings.TrimSpace(title)
	decision = strings.TrimSpace(decision)
	if title == "" || decision == "" {
		return "", "", "", "", fmt.Errorf("missing title or decision section")
	}
	return title, strings.TrimSpace(status), strings.TrimSpace(context), decision, nil
}

// isHeading reports whether a trimmed line is a markdown heading whose text
// is the given section name, possibly followed by inline content ("## Status",
// "## Decision: Use Redis"). Matching is case-insensitive.
func isHeading(line, name string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "#") {
		return false
	}
	text := strings.TrimSpace(strings.TrimLeft(line, "#"))
	text = strings.TrimSpace(strings.TrimSuffix(text, ":"))
	if strings.EqualFold(text, name) {
		return true
	}
	// "Decision: Use Redis" — the inline text after the colon belongs to the
	// section (headingInline extracts it).
	head, _, _ := strings.Cut(text, ":")
	return strings.EqualFold(strings.TrimSpace(head), name)
}

// headingInline returns the inline text after the colon on a heading line
// ("## Decision: Use Redis" → "Use Redis"), or "" when the heading carries
// no inline content.
func headingInline(line string) string {
	if _, after, ok := strings.Cut(line, ":"); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

// cleanADRTitle strips ADR numbering and bracket tags from a title.
func cleanADRTitle(t string) string {
	return strings.TrimSpace(adrTitlePrefix.ReplaceAllString(t, ""))
}

// --- README parsing ---

// ParseReadme extracts technology-mention claims from a README-style file.
// Matching is word-boundary based and case-insensitive against the
// effective lexicon, so "uses redis" and "REDIS" both match "Redis" but
// "rediscount" does not. Code fences, HTML comments and table rows are
// skipped (tables most often list alternatives rather than state what the
// project uses); a claim is emitted once per (file, technology) with the
// surrounding context as evidence. A UTF-8 BOM is stripped before parsing.
//
// Claims use the global subject "architecture" with predicate
// "uses_technology" — a README states what the PROJECT uses, not what one
// node uses — and ClaimKind EXPLICIT_REASON (human-authored documentation;
// the 0.7 confidence carries the informality compared to an ADR's 0.95).
func ParseReadme(doc DocSource, lexicon []string) []developer_memory.KnowledgeClaim {
	data, err := os.ReadFile(doc.Path)
	if err != nil {
		return nil
	}
	content := strings.TrimPrefix(string(data), "\uFEFF")
	lines := strings.Split(content, "\n")

	seen := make(map[string]bool)
	var claims []developer_memory.KnowledgeClaim
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "```") {
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
			}
			continue
		}
		if strings.HasPrefix(line, "<!--") {
			continue
		}
		if strings.HasPrefix(line, "|") {
			// Table rows (and their separator lines) most often enumerate
			// alternatives or comparison matrices; skipping them avoids
			// false "project uses X" claims.
			continue
		}
		for _, tech := range techMentions(line, lexicon) {
			if seen[tech] {
				continue
			}
			seen[tech] = true
			excerpt := contextWindow(lines, i, 1)
			bundle := evidence.NewBundle(evidence.EvidenceItem{
				Source:     evidence.SourceDocs,
				Reference:  doc.Rel,
				Excerpt:    excerpt,
				Confidence: 0.70,
				Timestamp:  doc.ModTime,
			})
			claims = append(claims, newFusedClaim(
				"readme", doc.Rel, "architecture", "uses_technology", tech,
				developer_memory.ClaimExplicitReason, developer_memory.StateActive,
				doc.ModTime, bundle, "", "",
			))
		}
	}
	sort.Slice(claims, func(i, j int) bool { return claims[i].Object < claims[j].Object })
	return claims
}

// techMentions returns the lexicon entries mentioned in text, lowercased
// and deduplicated in lexicon order (deterministic). Matching uses word
// boundaries on both sides so abbreviations like "K8s" and "S3" match
// exactly without leaking into surrounding words.
func techMentions(text string, lexicon []string) []string {
	var out []string
	for _, tech := range lexicon {
		if wordBoundaryContains(text, tech) {
			out = append(out, tech)
		}
	}
	return out
}

// wordBoundaryContains reports whether needle (lowercase) occurs in text as
// a whole word on either side — i.e. neither the character before nor after
// the match is a letter or digit.
func wordBoundaryContains(text, needle string) bool {
	t := strings.ToLower(text)
	start := 0
	for {
		i := strings.Index(t[start:], needle)
		if i < 0 {
			return false
		}
		i += start
		before := i == 0 || !isWordChar(t[i-1])
		after := i+len(needle) >= len(t) || !isWordChar(t[i+len(needle)])
		if before && after {
			return true
		}
		start = i + len(needle)
	}
}

func isWordChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// contextWindow joins lines[i-window..i+window] trimmed, for evidence
// excerpts around a mention.
func contextWindow(lines []string, i, window int) string {
	from := i - window
	if from < 0 {
		from = 0
	}
	to := i + window
	if to >= len(lines) {
		to = len(lines) - 1
	}
	var parts []string
	for j := from; j <= to; j++ {
		if s := strings.TrimSpace(lines[j]); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " | ")
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
