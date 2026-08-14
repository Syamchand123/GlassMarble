package learning

import (
	"sort"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// Convention is one learned project convention with its supporting
// evidence: how many occurrences backed the detection and how confident the
// learner is in it. Confidence is always deterministic — it is the
// occurrence count divided by the candidate population.
type Convention struct {
	// Value is the learned convention ("*Service", "docs/adr",
	// "internal/domain", ...).
	Value string `json:"value"`
	// Confidence is 0..1, computed deterministically from counts.
	Confidence float64 `json:"confidence"`
	// Evidence is the number of occurrences that backed the detection.
	Evidence int `json:"evidence"`
}

// ProjectConventions holds the repository-specific conventions learned
// from history (master plan §8.4). Every convention carries its evidence
// and confidence so downstream consumers (naming hints, evidence retrieval retrieval)
// can weight weak conventions accordingly.
//
// ProjectConventions is persisted to .glassmarble/memory/conventions.json
// as a DERIVED aggregate: it can always be recomputed by
// LearnConventions(graph, memory) and is never a source of truth.
type ProjectConventions struct {
	// ServiceNamingPattern is the dominant class/struct name suffix
	// ("*Service", "*Handler", ...).
	ServiceNamingPattern Convention `json:"service_naming_pattern"`
	// LayerDirectories lists the directory names that act as architecture
	// layers (internal/domain, internal/api, ...), sorted, each with its
	// occurrence evidence.
	LayerDirectories []Convention `json:"layer_directories"`
	// TestFilePattern is the dominant test-file name pattern
	// ("*_test.go", "*.spec.ts", ...).
	TestFilePattern Convention `json:"test_file_pattern"`
	// ADRDirectory is where the project keeps architecture decision
	// records ("docs/adr", "docs/decisions", ...).
	ADRDirectory Convention `json:"adr_directory"`
	// PreferredPatterns are architecture patterns the developer explicitly
	// confirmed via ACCEPT corrections (empty when none).
	PreferredPatterns []string `json:"preferred_patterns,omitempty"`
	// RejectedPatterns are architecture patterns the developer explicitly
	// rejected via REJECT corrections (empty when none).
	RejectedPatterns []string `json:"rejected_patterns,omitempty"`
	// LearnedPatterns are patterns detected repeatedly in the project's
	// architectural history (PATTERN_DETECTED events in memory), with
	// occurrence evidence — the deterministic counterpart to user
	// preferences.
	LearnedPatterns []Convention `json:"learned_patterns,omitempty"`
	// LearnedAt is when the conventions were last extracted.
	LearnedAt time.Time `json:"learned_at"`
}

// LearnOption configures LearnConventions.
type LearnOption func(*learnOptions)

type learnOptions struct {
	minEvidence int
	preferred   []string
	rejected    []string
	learnedAt   time.Time
}

// WithMinEvidence sets the minimum occurrence count a convention needs
// before it is reported (default 2 — a single file must not become a
// convention).
func WithMinEvidence(n int) LearnOption {
	return func(o *learnOptions) { o.minEvidence = n }
}

// WithPatternFeedback feeds the user's accepted/rejected patterns (from the
// correction log) into the conventions.
func WithPatternFeedback(preferred, rejected []string) LearnOption {
	return func(o *learnOptions) {
		o.preferred = preferred
		o.rejected = rejected
	}
}

// WithLearnedAt fixes the extraction timestamp (tests use this for
// deterministic output).
func WithLearnedAt(t time.Time) LearnOption {
	return func(o *learnOptions) { o.learnedAt = t }
}

// LearnConventions extracts repository conventions deterministically from
// the graph structure AND the developer memory (master plan §8.4 — "analyze
// the directory structure, analyze class/struct naming patterns, analyze
// file naming patterns"). No LLM: every convention is a frequency
// measurement with traceable evidence.
//
// Sources:
//   - graph.FileNodeIndex → test-file pattern, layer directories, ADR
//     directory,
//   - graph.Nodes        → dominant class/struct name suffix,
//   - memory             → ADR/layer cross-check via claim evidence
//     references, and repeatedly detected architecture patterns.
//
// Output is fully deterministic: directory and pattern lists are sorted,
// and ties break on the convention value.
func LearnConventions(graph *akg.CodePropertyGraph, mem *developer_memory.DeveloperMemory, opts ...LearnOption) *ProjectConventions {
	o := &learnOptions{minEvidence: 2}
	for _, f := range opts {
		f(o)
	}
	if o.minEvidence < 1 {
		o.minEvidence = 1
	}
	if o.learnedAt.IsZero() {
		o.learnedAt = time.Now()
	}

	conv := &ProjectConventions{
		PreferredPatterns: dedupeSorted(o.preferred),
		RejectedPatterns:  dedupeSorted(o.rejected),
		LearnedAt:         o.learnedAt,
	}
	if graph == nil {
		return conv
	}

	// --- file-derived conventions ---
	var files []string
	if graph.FileNodeIndex != nil {
		graph.FileNodeIndex.Iterate(func(file string, _ map[string]bool) {
			files = append(files, normalizePath(file))
		})
	}
	sort.Strings(files)

	totalFiles := len(files)
	if totalFiles == 0 {
		return conv
	}

	// Test-file pattern: dominant suffix among known test conventions.
	testSuffixCounts := make(map[string]int)
	for _, file := range files {
		for _, suffix := range testSuffixes {
			if strings.HasSuffix(file, suffix) {
				testSuffixCounts[suffix]++
			}
		}
	}
	if best, count := dominant(testSuffixCounts); count >= o.minEvidence {
		conv.TestFilePattern = Convention{
			// Display form: "*_test.go", "*.spec.ts", ...
			Value:      "*" + best,
			Confidence: float64(count) / float64(totalFiles),
			Evidence:   count,
		}
	}

	// Layer directories: known architecture layer names appearing as path
	// segments, gated by minimum evidence.
	layerCounts := make(map[string]int)
	for _, file := range files {
		seen := make(map[string]bool)
		for _, seg := range strings.Split(file, "/") {
			if isLayerSegment(seg) && !seen[seg] {
				seen[seg] = true
				layerCounts[seg]++
			}
		}
	}
	for _, seg := range sortedKeys(layerCounts) {
		count := layerCounts[seg]
		if count < o.minEvidence {
			continue
		}
		conv.LayerDirectories = append(conv.LayerDirectories, Convention{
			Value:      seg,
			Confidence: float64(count) / float64(totalFiles),
			Evidence:   count,
		})
	}

	// ADR directory: the deepest common "docs/adr"-style prefix, with
	// cross-checking against memory claim evidence references.
	adrCandidates := make(map[string]int)
	for _, file := range files {
		if dir, ok := adrDirOf(file); ok {
			adrCandidates[dir]++
		}
	}
	// Memory cross-check: fused docs claims carry file references under the
	// ADR directory; confirm candidates from that side too.
	adrRefs := adrDirsFromMemory(mem)
	for dir := range adrRefs {
		adrCandidates[dir]++
	}
	if dir, count := dominant(adrCandidates); count >= o.minEvidence {
		conv.ADRDirectory = Convention{
			Value:      dir,
			Confidence: 1.0,
			Evidence:   count,
		}
	}

	// --- node-derived conventions ---
	// Dominant class/struct name suffix ("XService", "XHandler", ...).
	suffixCounts := make(map[string]int)
	totalCandidates := 0
	if graph.Nodes != nil {
		graph.Nodes.Iterate(func(_ string, node *link.ResolvedNode) {
			if node.Kind != "STRUCT" && node.Kind != "MODULE" {
				return
			}
			totalCandidates++
			for _, suffix := range namingSuffixes {
				if strings.HasSuffix(node.Name, suffix) {
					suffixCounts[suffix]++
					break
				}
			}
		})
	}
	if best, count := dominant(suffixCounts); count >= o.minEvidence && totalCandidates > 0 {
		conv.ServiceNamingPattern = Convention{
			Value:      "*" + best,
			Confidence: float64(count) / float64(totalCandidates),
			Evidence:   count,
		}
	}

	// --- memory-derived conventions ---
	// Architecture patterns detected repeatedly in the project's history.
	patternCounts := make(map[string]int)
	if mem != nil {
		for _, ev := range mem.Events {
			if ev.Kind == archmodel.EventPatternDetected && len(ev.Components) > 0 {
				patternCounts[ev.Components[0]]++
			}
		}
	}
	for _, p := range sortedKeys(patternCounts) {
		count := patternCounts[p]
		if count < o.minEvidence {
			continue
		}
		conv.LearnedPatterns = append(conv.LearnedPatterns, Convention{
			Value:      p,
			Confidence: float64(count) / float64(totalEventsPatterns(mem)),
			Evidence:   count,
		})
	}
	return conv
}

// totalEventsPatterns returns the total number of pattern events (the
// denominator for LearnedPatterns confidence).
func totalEventsPatterns(mem *developer_memory.DeveloperMemory) float64 {
	if mem == nil {
		return 1
	}
	n := 0
	for _, ev := range mem.Events {
		if ev.Kind == archmodel.EventPatternDetected {
			n++
		}
	}
	if n == 0 {
		return 1
	}
	return float64(n)
}

// testSuffixes are the recognized test-file name conventions. Values are
// matched as path suffixes (forward-slash normalized).
var testSuffixes = []string{
	"_test.go", ".spec.ts", ".test.ts", ".test.js", ".spec.js", "_test.py",
}

// namingSuffixes are the recognized class/struct name conventions, checked
// in order against node names.
var namingSuffixes = []string{
	"Service", "Handler", "Controller", "Repository", "Manager",
	"Factory", "Provider", "Client",
}

// layerSegments are the directory names that commonly mark architecture
// layers. Detection is purely nominal + frequency-gated; a directory only
// counts when it appears across multiple files.
var layerSegments = map[string]bool{
	"domain": true, "core": true, "infrastructure": true, "infra": true,
	"api": true, "handlers": true, "controllers": true, "services": true,
	"repository": true, "repositories": true, "application": true,
	"presentation": true, "delivery": true, "usecases": true, "ports": true,
	"adapters": true, "external": true,
}

// isLayerSegment reports whether seg is a recognized architecture-layer
// directory name.
func isLayerSegment(seg string) bool {
	return layerSegments[strings.ToLower(seg)]
}

// normalizePath converts a stored file path to the canonical forward-slash
// form so the analysis is platform-independent (the graph stores paths
// using the host separator).
func normalizePath(file string) string {
	return strings.ReplaceAll(file, "\\", "/")
}

// adrDirOf returns the ADR directory a file lives under, when it is one of
// the standard locations ("docs/adr", "docs/decisions", "docs/adrs",
// "docs/architecture/decisions", ...).
func adrDirOf(file string) (string, bool) {
	file = normalizePath(file)
	for _, dir := range []string{
		"docs/adr", "docs/decisions", "docs/adrs",
		"docs/architecture/decisions", "docs/architecture/adr",
		"adr", "decisions",
	} {
		if file == dir || strings.HasPrefix(file, dir+"/") {
			return dir, true
		}
	}
	return "", false
}

// adrDirsFromMemory collects ADR directory references from the evidence of
// documentation claims in memory (knowledge fusion fused claims carry file
// references).
func adrDirsFromMemory(mem *developer_memory.DeveloperMemory) map[string]bool {
	dirs := make(map[string]bool)
	if mem == nil {
		return dirs
	}
	for _, claim := range mem.GlobalMemory {
		for _, item := range claim.Evidence.Items {
			if dir, ok := adrDirOf(item.Reference); ok {
				dirs[dir] = true
			}
		}
	}
	return dirs
}

// dominant returns the highest-count entry of a frequency map, breaking
// ties deterministically on the (sorted) key.
func dominant(counts map[string]int) (string, int) {
	bestKey, bestCount := "", 0
	for _, k := range sortedKeys(counts) {
		if counts[k] > bestCount {
			bestKey, bestCount = k, counts[k]
		}
	}
	return bestKey, bestCount
}

// sortedKeys returns the map keys in sorted order.
func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// dedupeSorted returns the input strings deduplicated and sorted — the
// deterministic order conventions are always presented in.
func dedupeSorted(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
