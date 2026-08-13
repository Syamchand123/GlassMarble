package ai_engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// NodeEvidence is a compact, traceable summary of one AKG node included in an
// evidence context. The ID always maps back to the graph, so every answer can
// be traced to specific graph evidence (master plan §9d, principle 11).
type NodeEvidence struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Kind       string            `json:"kind"`
	File       string            `json:"file,omitempty"`
	Lines      string            `json:"lines,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
	// Match is the deterministic match quality (exact 1.0 / prefix 0.9 /
	// substring 0.8 / file-path 0.6). Displayed to the LLM so it can weigh
	// how strong the link between the question and the node is.
	Match float64 `json:"match"`
}

// EvidenceContext is the structured, grounded context handed to the LLM. It
// is assembled deterministically by the Retriever BEFORE any model call — the
// LLM never sees the raw repository or an unfiltered graph (master plan §12).
//
// Every section keeps provenance: claims carry their own evidence bundles,
// patterns/smells carry rule evidence, timeline entries carry commit hashes,
// and nodes carry graph IDs.
type EvidenceContext struct {
	Question string `json:"question"`

	// Nodes are the AKG nodes matching the question, ranked by match quality.
	Nodes []NodeEvidence `json:"nodes"`

	// Claims are the ranked developer-memory knowledge claims relevant to
	// the question (FACT / EXPLICIT_REASON / INFERENCE / SPECULATION with
	// states, freshness and evidence — the honesty mechanism of Stage 6).
	Claims []developer_memory.KnowledgeClaim `json:"claims"`

	// Timeline holds the relevant architecture evolution entries, most
	// recent first.
	Timeline []archmodel.TimelineEntry `json:"timeline"`

	// Patterns and Smells come from the Stage 5 intelligence run, filtered
	// by relevance and confidence.
	Patterns   []archmodel.DetectedPattern `json:"patterns"`
	Smells     []archmodel.ArchSmell       `json:"smells"`

	// Components are the detected architectural units (Stage 5D) relevant
	// to the question.
	Components []archmodel.DetectedComponent `json:"components"`

	// MetricSummary is a one-line quantitative snapshot (Stage 5A).
	MetricSummary string `json:"metric_summary,omitempty"`

	// Corrections is the number of Stage 10 learning corrections that took
	// effect on the returned memory items (0 when none — the projection is
	// applied regardless, corrections are reflected in the item values).
	Corrections int `json:"corrections_applied,omitempty"`

	// TokenCount is an estimate of the rendered grounded prompt tokens,
	// computed after TrimToBudget. The LLM is never given a context section
	// that overflows its budget.
	TokenCount int `json:"token_count"`
}

// Empty reports whether the context carries no evidence at all. Callers use
// this to skip an LLM call entirely rather than asking the model to speculate
// on nothing (groundedness discipline, master plan §10.5).
func (c *EvidenceContext) Empty() bool {
	return c == nil || (len(c.Nodes) == 0 && len(c.Claims) == 0 && len(c.Timeline) == 0 &&
		len(c.Patterns) == 0 && len(c.Smells) == 0 && len(c.Components) == 0 &&
		c.MetricSummary == "")
}

// --- per-item renderers (shared by trimming and prompt building) ---

func nodeLine(n NodeEvidence) string {
	loc := ""
	if n.File != "" {
		loc = " — " + n.File
		if n.Lines != "" {
			loc += ":" + n.Lines
		}
	}
	return fmt.Sprintf("- %s (%s)%s [match %.2f]", n.Name, n.Kind, loc, n.Match)
}

func claimLine(c developer_memory.KnowledgeClaim) string {
	refs := evidenceRefs(c.Evidence.Items, 3)
	vs := fmt.Sprintf("conf=%.2f fresh=%.2f", claimConfidence(c), c.FreshnessScore)
	if c.ValidUntil != nil {
		vs += " until=" + c.ValidUntil.Format("2006-01-02")
	}
	line := fmt.Sprintf("- %s %s %s | %s | state=%s | %s",
		c.Subject, c.Predicate, c.Object, c.ClaimKind, c.State, vs)
	if len(refs) > 0 {
		line += " | refs: " + strings.Join(refs, ", ")
	}
	return line
}

func timelineLine(e archmodel.TimelineEntry) string {
	when := e.Timestamp.Format("2006-01-02")
	line := fmt.Sprintf("- [%s] %s: %s", when, e.EventKind, e.Title)
	if e.CommitHash != "" {
		line += fmt.Sprintf(" (commit %s)", shortHash(e.CommitHash))
	}
	if e.Intent != "" {
		line += fmt.Sprintf(" — intent: %s", e.Intent)
	}
	return line
}

func patternLine(p archmodel.DetectedPattern) string {
	line := fmt.Sprintf("- Pattern %s: %s (confidence %.2f)", p.Kind, p.Name, p.Confidence)
	if len(p.Components) > 0 {
		line += " — components: " + strings.Join(p.Components, ", ")
	}
	if refs := evidenceRefs(p.Evidence.Items, 3); len(refs) > 0 {
		line += " | refs: " + strings.Join(refs, ", ")
	}
	return line
}

func smellLine(s archmodel.ArchSmell) string {
	return fmt.Sprintf("- Smell [%s] %s: %s", s.Severity, s.Kind, s.Title)
}

func componentLine(c archmodel.DetectedComponent) string {
	line := fmt.Sprintf("- Component %s (%s, confidence %.2f)", c.Name, c.Kind, c.Confidence)
	if len(c.Directories) > 0 {
		line += " — dirs: " + strings.Join(c.Directories, ", ")
	}
	return line
}

// claimConfidence returns the claim's aggregate evidence confidence with a
// neutral fallback for claims persisted without a bundle.
func claimConfidence(c developer_memory.KnowledgeClaim) float64 {
	if c.Evidence.AggConfidence > 0 {
		return c.Evidence.AggConfidence
	}
	return 0.5
}

// evidenceRefs extracts the human-citable references of a bundle (commit
// hashes, PR/issue refs, file paths, rule IDs), deduplicated and capped.
func evidenceRefs(items []evidence.EvidenceItem, cap int) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(items))
	var refs []string
	for _, it := range items {
		ref := strings.TrimSpace(it.Reference)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
		if len(refs) >= cap {
			break
		}
	}
	return refs
}

// shortHash truncates a commit hash for display ("abcdef1234567890" →
// "abcdef12"). Short hashes pass through unchanged.
func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// --- trimming ---

// sectionBudget names the per-section share of the total evidence budget.
// Deterministic proportional allocation: the graph facts get the largest
// share, metrics the smallest (metrics are one line and always included).
func sectionBudget(name string, maxTokens int) int {
	share := map[string]int{
		"nodes":     40,
		"claims":    25,
		"timeline":  15,
		"patterns":  7,
		"smells":    7,
		"components": 4,
		"metric":    2,
	}[name]
	if maxTokens <= 0 {
		maxTokens = DefaultEvidenceTokens
	}
	b := maxTokens * share / 100
	if b < 64 {
		// floor: never render a section with fewer tokens than one item,
		// capped at the caller's budget so tiny budgets are honored instead
		// of being silently overridden.
		b = min(64, maxTokens)
	}
	return b
}

// estimateTokens approximates tokens at ~4 characters per token, matching the
// agent loop's pre-flight estimation so budgets behave consistently.
func estimateTokens(s string) int {
	return len(s) / 4
}

// trimSection keeps the longest prefix of items whose cumulative rendered
// size fits the budget. Items arrive pre-sorted by the retriever (rank),
// so dropping the tail preserves the highest-ranked evidence.
func trimSection[T any](items []T, budget int, render func(T) string) []T {
	if len(items) <= 1 {
		return items
	}
	chars := budget * 4
	total := 0
	for i, it := range items {
		total += len(render(it)) + 1
		if total > chars {
			if i == 0 {
				// floor: a budget too small to fit one item still keeps the
				// section's top item so non-empty sections never render empty.
				return items[:1]
			}
			return items[:i]
		}
	}
	return items
}

// TrimToBudget reduces every section so the whole grounded prompt fits within
// maxTokens (0 → DefaultEvidenceTokens) and recomputes TokenCount. Sections
// are trimmed in rank order (rank is assigned deterministically by the
// retriever), so the highest-scoring evidence always survives.
func (c *EvidenceContext) TrimToBudget(maxTokens int) {
	if c == nil {
		return
	}
	if maxTokens <= 0 {
		maxTokens = DefaultEvidenceTokens
	}
	c.Nodes = trimSection(c.Nodes, sectionBudget("nodes", maxTokens), nodeLine)
	c.Claims = trimSection(c.Claims, sectionBudget("claims", maxTokens), claimLine)
	c.Timeline = trimSection(c.Timeline, sectionBudget("timeline", maxTokens), timelineLine)
	c.Patterns = trimSection(c.Patterns, sectionBudget("patterns", maxTokens), patternLine)
	c.Smells = trimSection(c.Smells, sectionBudget("smells", maxTokens), smellLine)
	c.Components = trimSection(c.Components, sectionBudget("components", maxTokens), componentLine)
	c.MetricSummary = trimMetric(c.MetricSummary, sectionBudget("metric", maxTokens))
	c.TokenCount = estimateTokens(c.BuildPrompt())
}

// trimMetric clips an overlong metrics line deterministically at the budget.
func trimMetric(summary string, budget int) string {
	if summary == "" || len(summary) <= budget*4 {
		return summary
	}
	out := summary
	for len(out) > 0 && estimateTokens(out) > budget {
		cut := strings.LastIndexByte(out, ',')
		if cut < 0 {
			out = out[:budget*4]
			break
		}
		out = strings.TrimSpace(out[:cut])
	}
	return out
}

// buildSections renders every non-empty section, optionally with the question
// and grounding instructions (full prompt) or without (context injection).
func (c *EvidenceContext) buildSections(withQuestion bool) string {
	if c == nil {
		return ""
	}
	var b strings.Builder

	writeSection := func(header string, lines []string) {
		if len(lines) == 0 {
			return
		}
		b.WriteString(header + "\n")
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
		b.WriteString("\n")
	}

	var nodes []string
	for _, n := range c.Nodes {
		nodes = append(nodes, nodeLine(n))
	}
	writeSection(EvidenceSectionAKG, nodes)

	var claims []string
	for _, cl := range c.Claims {
		claims = append(claims, claimLine(cl))
	}
	writeSection(EvidenceSectionHistory, claims)

	var timeline []string
	for _, e := range c.Timeline {
		timeline = append(timeline, timelineLine(e))
	}
	writeSection(EvidenceSectionTimeline, timeline)

	var comps []string
	for _, co := range c.Components {
		comps = append(comps, componentLine(co))
	}
	writeSection(EvidenceSectionComponents, comps)

	var pat []string
	for _, p := range c.Patterns {
		pat = append(pat, patternLine(p))
	}
	var sm []string
	for _, s := range c.Smells {
		sm = append(sm, smellLine(s))
	}
	switch {
	case len(pat) > 0 && len(sm) > 0:
		writeSection(EvidenceSectionPatterns, append(pat, sm...))
	case len(pat) > 0:
		writeSection(EvidenceSectionPatterns, pat)
	case len(sm) > 0:
		writeSection(EvidenceSectionPatterns, sm)
	}

	if c.MetricSummary != "" {
		writeSection(EvidenceSectionMetrics, []string{"- " + c.MetricSummary})
	}

	if withQuestion {
		b.WriteString(EvidenceSectionQuestion + "\n")
		b.WriteString(c.Question + "\n\n")
		b.WriteString(GroundingInstructions)
	}
	return b.String()
}

// ContextBlock renders the evidence sections without the question or
// instructions. Used by AskAgent to prepend the deterministic context to a
// tool-capable session without repeating the query (master plan §13.3).
func (c *EvidenceContext) ContextBlock() string {
	return c.buildSections(false)
}

// BuildPrompt constructs the final grounded LLM prompt: the evidence sections,
// the user question, and the grounding instructions (master plan §10.3). The
// LLM is told to answer only from this material and to cite it.
func (c *EvidenceContext) BuildPrompt() string {
	return c.buildSections(true)
}

// Citations returns the deduplicated, sorted human-citable evidence references
// backing this context (commit hashes, PR/issue references, rule IDs, file
// paths). `gmb why` prints these after the answer so every claim is traceable.
func (c *EvidenceContext) Citations() []string {
	seen := make(map[string]bool)
	var out []string
	add := func(ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] {
			return
		}
		seen[ref] = true
		out = append(out, ref)
	}
	for _, cl := range c.Claims {
		for _, it := range cl.Evidence.Items {
			add(it.Reference)
		}
	}
	for _, e := range c.Timeline {
		add(e.CommitHash)
	}
	for _, p := range c.Patterns {
		for _, it := range p.Evidence.Items {
			add(it.Reference)
		}
	}
	for _, s := range c.Smells {
		for _, it := range s.Evidence.Items {
			add(it.Reference)
		}
	}
	sort.Strings(out)
	return out
}