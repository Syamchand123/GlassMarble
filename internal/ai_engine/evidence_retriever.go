package ai_engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/akgbridge"
	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/knowledge_aging"
	"github.com/Syamchand123/GlassMarble/internal/learning"
)

// Evidence retrieval (master plan §10.2): the deterministic layer that
// selects what the LLM may see. This file implements the retrieval itself;
// context_builder.go owns the prompt rendering and budgets.
//
// Retrieval is strictly read-only: it never mutates memory, the graph or the
// corrections log. It combines five deterministic sources:
//
//  1. AKG nodes (bridge snapshot) matched by query terms,
//  2. developer_memory claims + timeline, ranked by the developer memory query layer
//     over an aging-freshened projection (knowledge aging) and corrected by the
//     convention learning learning overlay,
//  3. Architecture Intelligence patterns/smells/components from intelligence/latest.json
//     (falling back to the latest architecture snapshot),
//  4. the Architecture Intelligence metrics summary,
//  5. convention learning learned pattern rejections: patterns the developer explicitly
//     rejected are excluded from evidence.
const (
	// DefaultEvidenceTopK caps every evidence section.
	DefaultEvidenceTopK = 25
	// DefaultEvidenceTokens caps the rendered grounded prompt an LLM call
	// may receive from this retriever.
	DefaultEvidenceTokens = 1200
)

// Retriever assembles the deterministic evidence context for a question. It
// is cheap to construct (no I/O) and safe for concurrent use as long as the
// underlying stores are not being mutated concurrently.
type Retriever struct {
	rootDir  string
	bridge   *akgbridge.Bridge
	memStore *developer_memory.MemoryStore
	snapDir  string
	learner  *learning.Learner
	now      func() time.Time
}

// NewRetriever builds a retriever for the repository rooted at rootDir.
func NewRetriever(rootDir string) *Retriever {
	return &Retriever{
		rootDir:  rootDir,
		bridge:   akgbridge.New(rootDir),
		memStore: developer_memory.NewStoreForRepo(rootDir),
		snapDir:  filepath.Join(rootDir, ".glassmarble", "snapshots"),
		learner:  learning.NewLearnerForRepo(rootDir),
		now:      time.Now,
	}
}

// RetrieveOptions controls one retrieval pass.
type RetrieveOptions struct {
	// TopK caps every evidence section (0 = DefaultEvidenceTopK).
	TopK int
	// MaxTokens caps the rendered grounded prompt (0 =
	// DefaultEvidenceTokens).
	MaxTokens int
	// MinConfidence drops patterns/smells/components below the threshold
	// (0.0 keeps everything above relevance filtering).
	MinConfidence float64
}

// RetrieveForQuestion runs the full deterministic retrieval pipeline and
// returns a token-budgeted evidence context. It never fails on missing data:
// a repository without graph/memory/intelligence yields an Empty context.
func (r *Retriever) RetrieveForQuestion(question string, opts RetrieveOptions) *EvidenceContext {
	ctx := &EvidenceContext{Question: question}
	if strings.TrimSpace(question) == "" {
		return ctx
	}

	topK := opts.TopK
	if topK <= 0 {
		topK = DefaultEvidenceTopK
	}

	terms := developer_memory.QueryTerms(question)
	if len(terms) == 0 {
		return ctx
	}

	// 1. Memory: the developer memory ranked query over an aging-freshened
	// projection, overlaid with convention learning corrections. This reuses the
	// existing ranking layer instead of re-implementing a weaker scan.
	if res := r.memoryQuery(question, topK); res != nil {
		ctx.Claims = res.Claims
		ctx.Timeline = res.Timeline
		ctx.Corrections = res.CorrectionsApplied
	}

	// 2. AKG nodes matched by query terms, deduplicated and ranked.
	ctx.Nodes = r.matchNodes(terms, topK)

	// 3. Architecture Intelligence intelligence: patterns, smells, components, metrics.
	r.intelligence(terms, opts.MinConfidence, topK, ctx)

	// 4. Token budget; the estimate always reflects the final prompt.
	tokens := opts.MaxTokens
	if tokens <= 0 {
		tokens = DefaultEvidenceTokens
	}
	ctx.TrimToBudget(tokens)
	return ctx
}

// memoryQuery runs the developer memory ranked query over the freshened projection
// with the convention learning learning overlay applied. Returns nil when memory is
// missing or unreadable.
func (r *Retriever) memoryQuery(question string, topK int) *MemoryProjection {
	mem, err := r.memStore.LoadMemory()
	if err != nil || mem == nil {
		return nil
	}

	freshed := knowledge_aging.FreshenMemoryWithSnapshot(mem, r.latestSnapshot(), r.now(), nil)
	res := developer_memory.QueryMemoryFromMemory(freshed, question, topK)

	corrected, cerr := r.learner.OverlayQuery(res)
	if cerr != nil || corrected == nil || corrected.MemoryQueryResult == nil {
		return &MemoryProjection{MemoryQueryResult: res}
	}
	return &MemoryProjection{
		MemoryQueryResult:  corrected.MemoryQueryResult,
		CorrectionsApplied: len(corrected.CorrectionsApplied),
	}
}

// MemoryProjection is the fresh query result plus how many convention learning
// corrections took effect on it.
type MemoryProjection struct {
	*developer_memory.MemoryQueryResult
	CorrectionsApplied int
}

// latestSnapshot loads the most recent architecture snapshot, returning nil
// when the snapshot store is unavailable or empty. Reading is side-effect
// free: the store directory must already exist.
func (r *Retriever) latestSnapshot() *archmodel.ArchSnapshot {
	if fi, err := os.Stat(r.snapDir); err != nil || !fi.IsDir() {
		return nil
	}
	store, err := arch_timeline.NewSnapshotStore(r.snapDir)
	if err != nil {
		return nil
	}
	snap, err := store.Latest()
	if err != nil {
		return nil
	}
	return snap
}

// --- AKG node matching ---

// matchNodes finds AKG nodes matching the query terms. Matching is
// case-insensitive prefix/substring scoring over the node name and file
// path. A node matched by several terms keeps its best score (dedup by ID);
// the result is ranked deterministically by score, then ID.
func (r *Retriever) matchNodes(terms []string, topK int) []NodeEvidence {
	snap, err := r.bridge.Snapshot()
	if err != nil || snap == nil || snap.Nodes == nil {
		return nil
	}

	best := make(map[string]float64)
	byID := make(map[string]*link.ResolvedNode)
	for _, term := range terms {
		needle := strings.ToLower(term)
		snap.Nodes.Iterate(func(id string, node *link.ResolvedNode) {
			score := nodeMatchScore(node, needle)
			if score == 0 {
				return
			}
			if prev, ok := best[id]; !ok || score > prev {
				best[id] = score
				byID[id] = node
			}
		})
	}

	type scoredNode struct {
		ev    NodeEvidence
		score float64
	}
	all := make([]scoredNode, 0, len(best))
	for id, score := range best {
		node := byID[id]
		if node == nil {
			continue
		}
		all = append(all, scoredNode{ev: toNodeEvidence(node, score), score: score})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].ev.ID < all[j].ev.ID
	})
	if len(all) > topK {
		all = all[:topK]
	}
	out := make([]NodeEvidence, 0, len(all))
	for _, s := range all {
		out = append(out, s.ev)
	}
	return out
}

// nodeMatchScore scores a node against one query term: exact name match 1.0,
// name prefix 0.9, name substring 0.8, file-path substring 0.6, else 0.
func nodeMatchScore(node *link.ResolvedNode, needle string) float64 {
	if node == nil {
		return 0
	}
	name := strings.ToLower(node.Name)
	switch {
	case name == needle:
		return 1.0
	case strings.HasPrefix(name, needle):
		return 0.9
	case strings.Contains(name, needle):
		return 0.8
	}
	if node.FileSpec.Path != "" && strings.Contains(strings.ToLower(node.FileSpec.Path), needle) {
		return 0.6
	}
	return 0
}

// toNodeEvidence projects a resolved node into the compact, prompt-safe
// NodeEvidence summary with a curated property subset.
func toNodeEvidence(node *link.ResolvedNode, score float64) NodeEvidence {
	ev := NodeEvidence{
		ID:    node.ID,
		Name:  node.Name,
		Kind:  node.Kind,
		Match: score,
	}
	if node.FileSpec.Path != "" {
		ev.File = node.FileSpec.Path
		if node.FileSpec.LineEnd > 0 {
			ev.Lines = fmt.Sprintf("%d-%d", node.FileSpec.LineStart, node.FileSpec.LineEnd)
		}
	}
	for _, key := range []string{"role", "macro_rules", "pagerank", "blast_radius", "instability", "doc", "signature", "package", "purpose"} {
		if v, ok := node.Properties[key]; ok && v != "" {
			if ev.Properties == nil {
				ev.Properties = make(map[string]string, 4)
			}
			ev.Properties[key] = v
		}
	}
	return ev
}

// --- Architecture Intelligence intelligence ---

// intelligence loads the intelligence result — from intelligence/latest.json when
// available, else from the latest architecture snapshot — and filters it by
// term relevance, confidence and learned pattern rejection.
func (r *Retriever) intelligence(terms []string, minConf float64, topK int, ctx *EvidenceContext) {
	intel, err := arch_intelligence.LoadLatestResult(filepath.Join(r.rootDir, ".glassmarble"))
	if err != nil {
		snap := r.latestSnapshot()
		if snap == nil {
			return
		}
		intel = &arch_intelligence.IntelligenceResult{
			Metrics:    snap.Metrics,
			Components: snap.Components,
			Patterns:   snap.Patterns,
			Smells:     snap.Smells,
			GraphHash:  snap.TopologyHash,
		}
	}
	if intel == nil {
		return
	}

	rejected := r.rejectedPatterns()

	minConfOK := func(p archmodel.DetectedPattern) bool {
		return p.Confidence >= minConf && string(p.Kind) != ""
	}
	ctx.Patterns = selectRelevant(intel.Patterns, topK, minConfOK,
		func(p archmodel.DetectedPattern) float64 { return p.Confidence },
		func(p archmodel.DetectedPattern) []string { return append(p.Components, p.Name, string(p.Kind)) },
		terms, rejected)

	ctx.Smells = selectRelevant(intel.Smells, topK, func(s archmodel.ArchSmell) bool {
		return s.Severity != "" || s.Kind != ""
	},
		func(s archmodel.ArchSmell) float64 { return severityWeight(s.Severity) },
		func(s archmodel.ArchSmell) []string { return append([]string{s.Title, string(s.Kind)}, s.AffectedIDs...) },
		terms, nil)

	ctx.Components = selectRelevant(intel.Components, topK,
		func(c archmodel.DetectedComponent) bool { return c.Confidence >= minConf },
		func(c archmodel.DetectedComponent) float64 { return c.Confidence },
		func(c archmodel.DetectedComponent) []string {
			return append(append([]string{c.Name}, c.Directories...), c.NodeIDs...)
		},
		terms, nil)

	ctx.MetricSummary = arch_intelligence.MetricSummary(intel.Metrics)
}

// selectRelevant filters items by the minimum-confidence gate, ranks them by
// score, and applies the term-relevance filter only when at least one item
// matches — general questions ("explain the architecture") still receive the
// top-ranked context instead of nothing. Rejected pattern kinds (convention learning)
// are excluded via the reject set.
func selectRelevant[T any](items []T, topK int, gate func(T) bool, score func(T) float64, names func(T) []string, terms []string, reject map[string]bool) []T {
	type scored struct {
		item  T
		score float64
	}
	var matched, all []scored
	for _, it := range items {
		if !gate(it) {
			continue
		}
		s := score(it)
		all = append(all, scored{item: it, score: s})
		if reject != nil {
			if pat, ok := any(it).(archmodel.DetectedPattern); ok && reject[string(pat.Kind)] {
				continue
			}
		}
		if namesMatch(names(it), terms) {
			matched = append(matched, scored{item: it, score: s})
		}
	}
	pool := all
	if len(matched) > 0 {
		pool = matched
	}
	sort.SliceStable(pool, func(i, j int) bool {
		if pool[i].score != pool[j].score {
			return pool[i].score > pool[j].score
		}
		return fmt.Sprintf("%v", pool[i].item) < fmt.Sprintf("%v", pool[j].item)
	})
	if len(pool) > topK {
		pool = pool[:topK]
	}
	out := make([]T, 0, len(pool))
	for _, s := range pool {
		out = append(out, s.item)
	}
	return out
}

// rejectedPatterns returns the set of pattern kinds the developer explicitly
// rejected through convention learning corrections; rejected patterns are excluded from
// evidence so learned preferences shape explanations.
func (r *Retriever) rejectedPatterns() map[string]bool {
	mem, err := r.memStore.LoadMemory()
	if err != nil || mem == nil {
		return nil
	}
	_, rejected, err := r.learner.PatternFeedback(mem)
	if err != nil || len(rejected) == 0 {
		return nil
	}
	out := make(map[string]bool, len(rejected))
	for _, p := range rejected {
		out[p] = true
	}
	return out
}

// namesMatch reports whether any term appears in any name (substring,
// case-insensitive).
func namesMatch(names []string, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	for _, t := range terms {
		for _, n := range names {
			if strings.Contains(strings.ToLower(n), t) {
				return true
			}
		}
	}
	return false
}

// severityWeight orders smells CRITICAL > HIGH > MEDIUM > LOW so the
// fallback ranking surfaces the worst smells first.
func severityWeight(s archmodel.Severity) float64 {
	switch s {
	case archmodel.SeverityCritical:
		return 4
	case archmodel.SeverityHigh:
		return 3
	case archmodel.SeverityMedium:
		return 2
	case archmodel.SeverityLow:
		return 1
	default:
		return 0
	}
}