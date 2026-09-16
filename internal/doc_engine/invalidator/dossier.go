// Package invalidator — dossier.go
// Assembles the GlobalCommitDossier once per commit from the AKG diff,
// commit metadata, and commit reasoning intent.
package invalidator

import (
	"context"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/commit_reasoning"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/git"
)

// BuildDossier constructs the GlobalCommitDossier for a given commit.
// If baseGraph and headGraph are available, it extracts exact added, modified,
// and removed symbols from the structural AKG diff.
func BuildDossier(repoDir string, commitHash string, baseGraph, headGraph *akg.CodePropertyGraph) (*config.GlobalCommitDossier, error) {
	dossier := &config.GlobalCommitDossier{
		CommitHash:   commitHash,
		CommitIntent: string(commit_reasoning.IntentUnknown),
	}

	// Keyword-derived events. These are the fallback when no CPG snapshots
	// are available (both graphs nil → structural detection yields nothing
	// and the merge below degrades to this list alone).
	var keywordEvents []string

	// 1. Read git commit metadata if available
	if repoDir != "" && commitHash != "" {
		meta, err := git.ReadCommit(repoDir, commitHash)
		if err == nil && meta != nil {
			dossier.CommitReason = meta.Subject
			dossier.PRDescription = meta.Body
			dossier.IssueRefs = meta.RelatedIssues

			// Classify commit intent deterministically via commit_reasoning
			extractor := commit_reasoning.NewIntentExtractor()
			res := extractor.Extract(context.Background(), meta, meta.Body)
			dossier.CommitIntent = string(res.Intent)

			// Derive architectural events from commit message keywords.
			// (internal/arch_timeline only exposes snapshot-diff APIs with no
			// clean per-commit event lookup, so keyword derivation is the
			// deterministic fallback here. Structural events computed below
			// from the CPG snapshots take precedence and are merged first.)
			keywordEvents = deriveArchEvents(meta.Subject + "\n" + meta.Body)
		}
	}

	// 2. Compute AKG GraphDiff if both graphs are provided
	if headGraph != nil {
		diff := akg.DiffGraphs(baseGraph, headGraph)
		if diff != nil {
			// Added symbols. A _test.go symbol is real data but never
			// part of the package's public interface (Go's own compiler
			// excludes test files from normal builds) — without this, a
			// "Recent Symbol Changes" block listed test helper functions
			// (TestCrashChildWriteTmpAndDie, ...) as if they were
			// meaningful API changes, found via live testing against a
			// real, large repository. Same exclusion as grounding/
			// collector.go's isTestFile, duplicated locally rather than
			// exported across packages for one one-line check.
			for _, node := range diff.NodesAdded {
				if isTestFile(node.File) {
					continue
				}
				fact := config.SymbolFact{
					FQN:  node.ID,
					Kind: node.Kind,
					File: node.File,
				}
				// Enrich with head node signature and doc comments if available
				if headGraph.Nodes != nil {
					if resolved, ok := headGraph.Nodes.Get(node.ID); ok && resolved != nil {
						populateSymbolFact(&fact, resolved)
					}
				}
				dossier.AddedSymbols = append(dossier.AddedSymbols, fact)

				// Detect config variables and sentinels. isConfigVar checks
				// the FQN for "config"/"getenv" substrings, which false-
				// positives on EVERY symbol added under a package literally
				// named "config" (an extremely common name) — the file
				// itself, an unrelated helper function, even its formal
				// parameters — since the substring comes from the package
				// path, not from what the symbol actually is. Gating on
				// Kind == "CALL" keeps the check meaningful: a real env-read
				// expression node, not any node that merely lives near the
				// word "config".
				if node.Kind == "CALL" && isConfigVar(fact.FQN) {
					dossier.AddedConfigVars = append(dossier.AddedConfigVars, config.ConfigVarFact{
						Name: fact.FQN,
						File: fact.File,
						Line: fact.Line,
					})
				}
				if isSentinelError(fact.FQN) {
					dossier.AddedSentinels = append(dossier.AddedSentinels, config.SentinelFact{
						FQN:  fact.FQN,
						Doc:  fact.Doc,
						File: fact.File,
						Line: fact.Line,
					})
				}
			}

			// Removed symbols
			for _, node := range diff.NodesRemoved {
				if isTestFile(node.File) {
					continue
				}
				dossier.RemovedSymbols = append(dossier.RemovedSymbols, node.ID)
				if node.Kind == "CALL" && isConfigVar(node.ID) {
					dossier.RemovedConfigVars = append(dossier.RemovedConfigVars, node.ID)
				}
			}

			// Modified symbols: compare nodes present in both graphs
			if baseGraph != nil && baseGraph.Nodes != nil && headGraph.Nodes != nil {
				headGraph.Nodes.Iterate(func(id string, headNode *link.ResolvedNode) {
					baseNode, ok := baseGraph.Nodes.Get(id)
					if !ok || baseNode == nil || headNode == nil {
						return
					}
					if isTestFile(headNode.FileSpec.Path) {
						return
					}

					baseSig := extractSignature(baseNode)
					headSig := extractSignature(headNode)
					baseDoc := extractDoc(baseNode)
					headDoc := extractDoc(headNode)

					if baseSig != headSig || baseDoc != headDoc {
						dossier.ModifiedSymbols = append(dossier.ModifiedSymbols, config.SymbolDelta{
							FQN:       id,
							Before:    baseSig,
							After:     headSig,
							DocBefore: baseDoc,
							DocAfter:  headDoc,
						})

						// Sentinel vars whose message changed get a dedicated delta
						// so Stage 3 can prioritize error-catalog sections.
						if isSentinelError(id) {
							before, after := baseSig, headSig
							if baseDoc != headDoc {
								before, after = baseDoc, headDoc
							}
							dossier.ModifiedSentinels = append(dossier.ModifiedSentinels, config.SentinelDelta{
								FQN:    id,
								Before: before,
								After:  after,
							})
						}
					}
				})
			}
		}
	}

	// 3. Structural arch events from the CPG snapshots (B3: message-
	// independent signals). Keyword events are appended ONLY when BOTH
	// graphs are nil (no structural signal exists at all); whenever either
	// snapshot is available the structural derivation is authoritative and
	// keyword proxies are suppressed entirely — even when the structural
	// pass yields zero events (a quiet graph means "no architectural
	// change", not "fall back to message guessing").
	dossier.ArchEvents = mergeArchEventsForGraphs(
		deriveStructuralEvents(baseGraph, headGraph), keywordEvents, baseGraph, headGraph)

	// NOTE: DirtySections is intentionally left empty here. It is populated
	// later by the invalidation engine (Invalidator.FindDirtySections +
	// DirtyQueue) once per-section hashes and priorities are computed.

	return dossier, nil
}

// populateSymbolFact extracts location and documentation fields from a ResolvedNode.
func populateSymbolFact(fact *config.SymbolFact, n *link.ResolvedNode) {
	if n == nil {
		return
	}
	fact.Line = n.FileSpec.LineStart
	fact.Signature = extractSignature(n)
	fact.Doc = extractDoc(n)
	if fact.File == "" {
		fact.File = n.FileSpec.Path
	}
	if fact.Kind == "" {
		fact.Kind = string(n.Kind)
	}
}

func extractSignature(n *link.ResolvedNode) string {
	if n == nil {
		return ""
	}
	if sig, ok := n.Properties["signature"]; ok && sig != "" {
		return sig
	}
	return n.Name
}

func extractDoc(n *link.ResolvedNode) string {
	if n == nil {
		return ""
	}
	if doc, ok := n.Properties["doc_comment"]; ok {
		return doc
	}
	if doc, ok := n.Properties["doc"]; ok {
		return doc
	}
	return ""
}

func isConfigVar(fqn string) bool {
	lower := strings.ToLower(fqn)
	return strings.Contains(lower, "getenv") || strings.Contains(lower, "config")
}

// isSentinelError reports whether fqn's short name looks like a Go sentinel
// error (ErrXxx). Strips the same separators isExportedFQN/isExportedShortName
// do ("::", ".", "/") so an FQN using "." (e.g. "errors.ErrNotFound") is
// recognized here too, not just the "::"-delimited form — otherwise such a
// sentinel silently misses Stage-3 priority-1 classification (AddedSentinels/
// ModifiedSentinels) and falls back to a lower dirty-section priority.
// isTestFile reports whether path is a Go test file — see
// grounding/collector.go's isTestFile (same one-line check, duplicated
// here rather than exported across packages) for why a _test.go symbol
// must never surface as if it were part of the package's real interface
// or a meaningful "recent change."
func isTestFile(path string) bool {
	return strings.HasSuffix(path, "_test.go")
}

func isSentinelError(fqn string) bool {
	name := fqn
	if idx := strings.LastIndex(name, "::"); idx >= 0 {
		name = name[idx+2:]
	}
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return strings.HasPrefix(name, "Err")
}

// archEventKeywords maps commit-message keywords to architectural event kinds.
// Checked in fixed order for deterministic output.
var archEventKeywords = []struct {
	keyword string
	event   string
}{
	{"split", "COMPONENT_SPLIT"},
	{"cycle", "CYCLE_INTRODUCED"},
	{"database", "NEW_DATABASE_LAYER"},
	{"layer", "LAYER_VIOLATION"},
	{"service", "SERVICE_ADDED"},
	{"interface", "INTERFACE_CHANGED"},
	{"auth", "SECURITY_BOUNDARY_CHANGED"},
}

// deriveArchEvents scans commit text for architectural keywords and returns
// the corresponding event kinds in deterministic order.
func deriveArchEvents(commitText string) []string {
	lower := strings.ToLower(commitText)
	var events []string
	for _, kw := range archEventKeywords {
		if strings.Contains(lower, kw.keyword) {
			events = append(events, kw.event)
		}
	}
	return events
}

// ────────────────────────────────────────────────────────────────────────────
// B3 structural arch events (retire the keyword proxies)
// ────────────────────────────────────────────────────────────────────────────
//
// deriveStructuralEvents computes message-independent architectural events
// from the base/head CPG snapshots in fixed order:
//
//  1. COMPONENT_ADDED / COMPONENT_REMOVED — package/directory set diff over
//     node file paths.
//  2. CYCLE_INTRODUCED / CYCLE_RESOLVED — Tarjan SCC over the module graph
//     (CPG CALLS + DEPENDS_ON edges), comparing base vs head.
//  3. LAYER_VIOLATION — a NEW call/depends edge crossing layer boundaries in
//     the forbidden (upward) direction.
//  4. PUBLIC_SURFACE_CHANGED — added/removed symbols, or modified symbols
//     whose short name is exported.
//
// A nil head graph yields no events. A nil base graph is treated as empty
// (every head package/edge/symbol is new).
func deriveStructuralEvents(baseGraph, headGraph *akg.CodePropertyGraph) []string {
	if headGraph == nil {
		return nil
	}
	var events []string

	// 1. Component (package directory) set diff.
	basePkgs := packageDirSet(baseGraph)
	headPkgs := packageDirSet(headGraph)
	for p := range headPkgs {
		if !basePkgs[p] {
			events = append(events, "COMPONENT_ADDED")
			break
		}
	}
	for p := range basePkgs {
		if !headPkgs[p] {
			events = append(events, "COMPONENT_REMOVED")
			break
		}
	}

	// 2. Dependency cycle delta via Tarjan SCC on the module graph.
	baseCycle := graphHasCycle(baseGraph)
	headCycle := graphHasCycle(headGraph)
	switch {
	case !baseCycle && headCycle:
		events = append(events, "CYCLE_INTRODUCED")
	case baseCycle && !headCycle:
		events = append(events, "CYCLE_RESOLVED")
	}

	// 3. New cross-layer edge in the forbidden direction.
	if hasNewLayerViolation(baseGraph, headGraph) {
		events = append(events, "LAYER_VIOLATION")
	}

	// 4. Public-surface delta.
	if hasPublicSurfaceDelta(baseGraph, headGraph) {
		events = append(events, "PUBLIC_SURFACE_CHANGED")
	}

	return events
}

// mergeArchEvents concatenates structural events first, then keyword events,
// deduped by exact name for deterministic output.
//
// B3b suppression policy: keyword proxies are only meaningful when NO
// structural signal exists (both CPG snapshots nil). Use
// mergeArchEventsForGraphs at the BuildDossier call site so keywords are
// dropped whenever either graph is available — even if the structural pass
// is quiet. This bare two-arg form is kept for unit-test compatibility and
// retains the historical structural-first merge.
func mergeArchEvents(structural, keywords []string) []string {
	seen := make(map[string]bool, len(structural)+len(keywords))
	var out []string
	for _, e := range structural {
		if e != "" && !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	for _, e := range keywords {
		if e != "" && !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}

// mergeArchEventsForGraphs applies the B3b suppression policy: structural
// events always; keyword events appended ONLY when BOTH graphs are nil.
// When either snapshot is available, keywords are suppressed (nil) and the
// result is the deduped structural list alone.
func mergeArchEventsForGraphs(structural, keywords []string, baseGraph, headGraph *akg.CodePropertyGraph) []string {
	if baseGraph != nil || headGraph != nil {
		keywords = nil
	}
	return mergeArchEvents(structural, keywords)
}

// packageDirSet collects the set of package directories holding CPG nodes.
// The directory of a node file path (e.g. "internal/auth" for
// "internal/auth/jwt.go") is the component identity; repo-root files carry
// no directory and are skipped.
func packageDirSet(g *akg.CodePropertyGraph) map[string]bool {
	out := make(map[string]bool)
	if g == nil || g.Nodes == nil {
		return out
	}
	g.Nodes.Iterate(func(_ string, n *link.ResolvedNode) {
		if n == nil {
			return
		}
		if d := nodeDir(n.FileSpec.Path); d != "" {
			out[d] = true
		}
	})
	return out
}

// nodeDir returns the slash-normalized directory of a repo-relative path.
func nodeDir(p string) string {
	norm := strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	norm = strings.TrimPrefix(norm, "./")
	if norm == "" {
		return ""
	}
	if idx := strings.LastIndex(norm, "/"); idx > 0 {
		return norm[:idx]
	}
	return ""
}

// maxCycleNodes caps the Tarjan SCC pass for safety on huge snapshots;
// oversized graphs skip cycle detection (report no cycle either way).
const maxCycleNodes = 2000

// moduleAdjacency builds the module graph: vertices are CPG node IDs (plus
// any edge endpoints), directed edges are CALLS and DEPENDS_ON relations.
// Neighbor lists are sorted and deduped for deterministic traversal.
func moduleAdjacency(g *akg.CodePropertyGraph) (map[string][]string, int) {
	adj := make(map[string][]string)
	if g == nil {
		return adj, 0
	}
	ensure := func(id string) {
		if id == "" {
			return
		}
		if _, ok := adj[id]; !ok {
			adj[id] = nil
		}
	}
	if g.Nodes != nil {
		g.Nodes.Iterate(func(id string, _ *link.ResolvedNode) { ensure(id) })
	}
	if g.OutboundEdges != nil {
		g.OutboundEdges.Iterate(func(src string, edges []link.ResolvedEdge) {
			for _, e := range edges {
				if e.Type != link.EdgeCalls && e.Type != link.EdgeDependsOn {
					continue
				}
				ensure(src)
				ensure(e.TargetID)
				if src != "" && e.TargetID != "" {
					adj[src] = append(adj[src], e.TargetID)
				}
			}
		})
	}
	for v := range adj {
		adj[v] = sortedUnique(adj[v])
	}
	return adj, len(adj)
}

// graphHasCycle reports whether the graph's module graph contains a directed
// cycle (iterative Tarjan SCC; any SCC of size > 1, or a self-loop, counts).
func graphHasCycle(g *akg.CodePropertyGraph) bool {
	adj, n := moduleAdjacency(g)
	if n == 0 || n > maxCycleNodes {
		return false
	}
	return hasCycleTarjan(adj)
}

// hasCycleTarjan is an iterative Tarjan strongly-connected-components pass
// returning true on the first cyclic SCC found. The explicit frame stack
// avoids recursion-depth limits on deep call chains.
func hasCycleTarjan(adj map[string][]string) bool {
	index := make(map[string]int, len(adj))
	low := make(map[string]int, len(adj))
	onStack := make(map[string]bool, len(adj))
	var stack []string
	counter := 0

	// Deterministic outer visit order.
	verts := make([]string, 0, len(adj))
	for v := range adj {
		verts = append(verts, v)
	}
	sort.Strings(verts)

	type frame struct {
		v  string
		pi int // next successor index to visit
	}
	assign := func(v string) {
		index[v] = counter
		low[v] = counter
		counter++
		stack = append(stack, v)
		onStack[v] = true
	}

	for _, root := range verts {
		if _, seen := index[root]; seen {
			continue
		}
		assign(root)
		call := []frame{{v: root}}
		for len(call) > 0 {
			top := len(call) - 1
			v := call[top].v
			succ := adj[v]
			if call[top].pi < len(succ) {
				w := succ[call[top].pi]
				call[top].pi++
				if _, seen := index[w]; !seen {
					assign(w)
					call = append(call, frame{v: w})
				} else if onStack[w] {
					if index[w] < low[v] {
						low[v] = index[w]
					}
				}
			} else {
				call = call[:top]
				if len(call) > 0 {
					parent := call[len(call)-1].v
					if low[v] < low[parent] {
						low[parent] = low[v]
					}
				}
				if low[v] == index[v] {
					size := 0
					for {
						topNode := stack[len(stack)-1]
						stack = stack[:len(stack)-1]
						onStack[topNode] = false
						size++
						if topNode == v {
							break
						}
					}
					if size > 1 {
						return true
					}
					for _, w := range adj[v] {
						if w == v {
							return true // self-loop
						}
					}
				}
			}
		}
	}
	return false
}

// layerRank maps a top-level directory to its architectural rank.
//
// Convention (documented for the violation check below): rank increases
// toward the entry-point edge — cmd(3) > internal(2) > pkg(1) > rest(0).
// Upper (higher-rank) layers may depend DOWNWARD on lower-rank layers; a NEW
// edge from a LOWER-rank package to a HIGHER-rank package is an upward
// dependency and is flagged as LAYER_VIOLATION.
func layerRank(top string) int {
	switch top {
	case "cmd":
		return 3
	case "internal":
		return 2
	case "pkg":
		return 1
	default:
		return 0
	}
}

// topLevelDir returns the first path segment of a repo-relative file path
// ("" for repo-root files).
func topLevelDir(p string) string {
	norm := strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	norm = strings.TrimPrefix(norm, "./")
	norm = strings.TrimPrefix(norm, "/")
	if norm == "" {
		return ""
	}
	if idx := strings.Index(norm, "/"); idx >= 0 {
		return norm[:idx]
	}
	return ""
}

// edgeEndpointFile resolves the repo-relative file path for an edge
// endpoint: head node first, then base node, then the ID's file part
// ("path::Name" → "path").
func edgeEndpointFile(head, base *akg.CodePropertyGraph, id string) string {
	if head != nil && head.Nodes != nil {
		if n, ok := head.Nodes.Get(id); ok && n != nil && n.FileSpec.Path != "" {
			return n.FileSpec.Path
		}
	}
	if base != nil && base.Nodes != nil {
		if n, ok := base.Nodes.Get(id); ok && n != nil && n.FileSpec.Path != "" {
			return n.FileSpec.Path
		}
	}
	if idx := strings.Index(id, "::"); idx > 0 {
		return id[:idx]
	}
	return id
}

// moduleEdgeKey identifies a module-graph edge for base/head set comparison.
func moduleEdgeKey(e link.ResolvedEdge) string {
	return string(e.Type) + "\x00" + e.SourceID + "\x00" + e.TargetID
}

// hasNewLayerViolation reports whether head introduces a call/depends edge
// absent from base that runs from a lower-rank layer to a higher-rank layer
// (e.g. pkg/* → cmd/*). Only NEW edges are considered, so pre-existing
// layering debt does not re-fire the event every commit.
func hasNewLayerViolation(base, head *akg.CodePropertyGraph) bool {
	if head == nil || head.OutboundEdges == nil {
		return false
	}
	baseKeys := make(map[string]bool)
	if base != nil && base.OutboundEdges != nil {
		base.OutboundEdges.Iterate(func(_ string, edges []link.ResolvedEdge) {
			for _, e := range edges {
				if e.Type != link.EdgeCalls && e.Type != link.EdgeDependsOn {
					continue
				}
				baseKeys[moduleEdgeKey(e)] = true
			}
		})
	}
	violation := false
	head.OutboundEdges.Iterate(func(_ string, edges []link.ResolvedEdge) {
		if violation {
			return
		}
		for _, e := range edges {
			if e.Type != link.EdgeCalls && e.Type != link.EdgeDependsOn {
				continue
			}
			if baseKeys[moduleEdgeKey(e)] {
				continue
			}
			srcRank := layerRank(topLevelDir(edgeEndpointFile(head, base, e.SourceID)))
			dstRank := layerRank(topLevelDir(edgeEndpointFile(head, base, e.TargetID)))
			if srcRank < dstRank {
				violation = true
				return
			}
		}
	})
	return violation
}

// hasPublicSurfaceDelta reports whether the node sets differ in a
// public-surface-relevant way: any added symbol, any removed symbol, or any
// modified symbol whose short name is exported (uppercase). Signature and
// doc-comment text both count as modifications.
func hasPublicSurfaceDelta(base, head *akg.CodePropertyGraph) bool {
	baseSigs := make(map[string]string)
	if base != nil && base.Nodes != nil {
		base.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
			if n == nil {
				return
			}
			baseSigs[id] = extractSignature(n) + "\x00" + extractDoc(n)
		})
	}
	changed := false
	if head != nil && head.Nodes != nil {
		head.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
			if changed || n == nil {
				return
			}
			before, ok := baseSigs[id]
			if !ok {
				changed = true // added symbol
				return
			}
			if isExportedShortName(id) && before != extractSignature(n)+"\x00"+extractDoc(n) {
				changed = true // exported symbol modified
			}
		})
	}
	if changed {
		return true
	}
	if base != nil && base.Nodes != nil && head != nil {
		base.Nodes.Iterate(func(id string, _ *link.ResolvedNode) {
			if changed {
				return
			}
			if head.Nodes == nil {
				changed = true
				return
			}
			if _, ok := head.Nodes.Get(id); !ok {
				changed = true // removed symbol
			}
		})
	}
	return changed
}

// sortedUnique sorts a slice copy and drops duplicates.
func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return in
	}
	cp := append([]string(nil), in...)
	sort.Strings(cp)
	out := cp[:1]
	for _, s := range cp[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}
