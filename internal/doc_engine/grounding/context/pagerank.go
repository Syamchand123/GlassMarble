package context

import (
	"path"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
)

// PageRank and budget tunables (Aider repomap pattern). Fixed so runs are
// reproducible; see doc.go for the full parameter contract.
const (
	// pageRankDamping is the probability of following a graph edge instead
	// of restarting from the personalization vector.
	pageRankDamping = 0.85
	// pageRankEpsilon is the L1 convergence threshold between iterations.
	pageRankEpsilon = 1e-6
	// pageRankMaxIters bounds iteration so cyclic graphs always terminate.
	pageRankMaxIters = 100
	// maxRankedFiles caps the file graph for monorepo safety; truncation is
	// deterministic (sorted paths, keep first N).
	maxRankedFiles = 20000
	// defaultBudgetToks applies when budgetTokens <= 0.
	defaultBudgetToks = 1000
	// maxSymbolsPerFile bounds symbols listed per file.
	maxSymbolsPerFile = 8
	// baseEdgeWeight is the weight of a plain file-to-file reference.
	baseEdgeWeight = 1.0
	// seedBoost multiplies edges touching a user-named (seed) identifier.
	seedBoost = 10.0
	// neighborhoodBoost multiplies edges inside a seed's file directory.
	neighborhoodBoost = 50.0
)

// SymbolRef is a single grounded symbol with its relevance score.
type SymbolRef struct {
	FQN   string
	File  string
	Line  int
	Score float64
}

// RankedFile is a file ranked by personalized PageRank with its top symbols.
type RankedFile struct {
	Path    string
	Score   float64
	Symbols []SymbolRef
}

// RankedContext is the budget-bounded, relevance-ordered context package.
type RankedContext struct {
	Files        []RankedFile
	TotalTokens  int
	BudgetTokens int
}

// EstimateTokens returns a deterministic token heuristic for s: byte length
// divided by 4 (integer division), minimum 1 — so even the empty string
// costs 1 token and every file contributes a positive cost to the budget.
func EstimateTokens(s string) int {
	if n := len(s) / 4; n > 1 {
		return n
	}
	return 1
}

// SelectContext builds a file-to-file reference graph from graph, runs
// personalized PageRank biased toward seeds (dirty symbol FQNs, either
// "file::Name" or plain "Name" form), and fills budgetTokens top-down by
// file score. seeds may be nil/empty (then ranking is uniform). A nil or
// empty graph yields an empty (non-nil) RankedContext. Output is fully
// deterministic for identical inputs.
func SelectContext(graph *akg.CodePropertyGraph, seeds []string, budgetTokens int) RankedContext {
	budget := budgetTokens
	if budget <= 0 {
		budget = defaultBudgetToks
	}
	ctx := RankedContext{Files: []RankedFile{}, TotalTokens: 0, BudgetTokens: budget}
	if graph == nil || graph.Nodes == nil {
		return ctx
	}
	queries := normalizeSeeds(seeds)

	// 1. Collect nodes with file coordinates in deterministic (sorted) order.
	snap := graph.Nodes.Snapshot()
	ids := make([]string, 0, len(snap))
	for id := range snap {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	type fileSym struct {
		fqn  string
		line int
	}
	nodeFile := make(map[string]string, len(snap))
	nodeFQN := make(map[string]string, len(snap))
	fileNodes := make(map[string][]fileSym)
	for _, id := range ids {
		n := snap[id]
		if n == nil {
			continue
		}
		fqn := n.ID
		if fqn == "" {
			fqn = id
		}
		nodeFQN[id] = fqn
		file := n.FileSpec.Path
		if file == "" {
			continue
		}
		nodeFile[id] = file
		fileNodes[file] = append(fileNodes[file], fileSym{fqn: fqn, line: n.FileSpec.LineStart})
	}
	if len(fileNodes) == 0 {
		return ctx
	}
	files := make([]string, 0, len(fileNodes))
	for f := range fileNodes {
		files = append(files, f)
	}
	sort.Strings(files)
	if len(files) > maxRankedFiles {
		files = files[:maxRankedFiles]
	}
	kept := make(map[string]bool, len(files))
	for _, f := range files {
		kept[f] = true
	}

	// 2. Collect directed node edges from both outbound and inbound maps
	// (either map may be sparsely populated); dedupe identical pairs so an
	// edge stored in both maps counts once.
	type pair struct{ src, tgt string }
	pairSet := make(map[pair]bool)
	for _, id := range ids {
		for _, e := range graph.GetOutboundEdges(id) {
			if e.SourceID == "" || e.TargetID == "" {
				continue
			}
			pairSet[pair{src: e.SourceID, tgt: e.TargetID}] = true
		}
		for _, e := range graph.GetInboundEdges(id) {
			if e.SourceID == "" || e.TargetID == "" {
				continue
			}
			pairSet[pair{src: e.SourceID, tgt: e.TargetID}] = true
		}
	}
	pairs := make([]pair, 0, len(pairSet))
	for p := range pairSet {
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].src != pairs[j].src {
			return pairs[i].src < pairs[j].src
		}
		return pairs[i].tgt < pairs[j].tgt
	})

	// 3. Aggregate to weighted file-to-file edges. File self-loops carry no
	// inter-file signal and are skipped. Multipliers stack (x10 seed contact,
	// x50 in-scope neighborhood => x500 when both apply).
	fileIdx := make(map[string]int, len(files))
	for i, f := range files {
		fileIdx[f] = i
	}
	n := len(files)
	outWeight := make([]float64, n)
	type fileEdge struct {
		from int
		w    float64
	}
	inEdges := make([][]fileEdge, n)
	for _, p := range pairs {
		sf, ok1 := nodeFile[p.src]
		tf, ok2 := nodeFile[p.tgt]
		if !ok1 || !ok2 || !kept[sf] || !kept[tf] || sf == tf {
			continue
		}
		w := baseEdgeWeight
		if touchesSeed(queries, nodeFQN[p.src], nodeFQN[p.tgt]) {
			w *= seedBoost
		}
		if inNeighborhood(queries, sf, tf) {
			w *= neighborhoodBoost
		}
		si, ti := fileIdx[sf], fileIdx[tf]
		outWeight[si] += w
		// pairs arrive sorted by source, so each target list stays sorted.
		inEdges[ti] = append(inEdges[ti], fileEdge{from: si, w: w})
	}

	// 4. Personalization: uniform over seed files (files named by seeds plus
	// files holding seed-matched symbols); uniform over all files if none.
	personal := make([]float64, n)
	seedFiles := make(map[string]bool)
	for _, q := range queries {
		if q.file != "" {
			for _, f := range files {
				if slashPath(f) == q.file {
					seedFiles[f] = true
				}
			}
		}
		if q.raw != "" || q.name != "" {
			for _, id := range ids {
				f, ok := nodeFile[id]
				if !ok || !kept[f] {
					continue
				}
				if seedMatchesFQN(q, nodeFQN[id]) {
					seedFiles[f] = true
				}
			}
		}
	}
	if len(seedFiles) > 0 {
		for _, f := range files {
			if seedFiles[f] {
				personal[fileIdx[f]] = 1.0 / float64(len(seedFiles))
			}
		}
	} else {
		for i := range personal {
			personal[i] = 1.0 / float64(n)
		}
	}

	// 5. Weighted personalized PageRank. Dangling (sink) mass is
	// redistributed through the personalization vector, which keeps the
	// score vector summing to 1 and biases sinks back toward the seeds.
	rank := make([]float64, n)
	copy(rank, personal)
	next := make([]float64, n)
	for range pageRankMaxIters {
		dangling := 0.0
		for i := 0; i < n; i++ {
			if outWeight[i] == 0 {
				dangling += rank[i]
			}
		}
		delta := 0.0
		for j := 0; j < n; j++ {
			v := (1-pageRankDamping)*personal[j] + pageRankDamping*dangling*personal[j]
			for _, e := range inEdges[j] {
				if outWeight[e.from] > 0 {
					v += pageRankDamping * rank[e.from] * e.w / outWeight[e.from]
				}
			}
			next[j] = v
			if d := v - rank[j]; d < 0 {
				delta -= d
			} else {
				delta += d
			}
		}
		copy(rank, next)
		if delta < pageRankEpsilon {
			break
		}
	}
	scores := make(map[string]float64, n)
	for i, f := range files {
		scores[f] = rank[i]
	}

	// 6. Walk files score-desc (path-asc tiebreak) and include each file's
	// top symbols (by FQN, cap 8) while the budget allows; over-budget
	// files are skipped but cheaper later files may still fit.
	order := make([]string, len(files))
	copy(order, files)
	sort.Slice(order, func(i, j int) bool {
		si, sj := scores[order[i]], scores[order[j]]
		if si != sj {
			return si > sj
		}
		return order[i] < order[j]
	})
	for _, f := range order {
		syms := fileNodes[f]
		sort.Slice(syms, func(i, j int) bool { return syms[i].fqn < syms[j].fqn })
		if len(syms) > maxSymbolsPerFile {
			syms = syms[:maxSymbolsPerFile]
		}
		fqns := make([]string, len(syms))
		refs := make([]SymbolRef, len(syms))
		for i, s := range syms {
			fqns[i] = s.fqn
			refs[i] = SymbolRef{FQN: s.fqn, File: f, Line: s.line, Score: scores[f]}
		}
		if refs == nil {
			refs = []SymbolRef{}
		}
		ft := EstimateTokens(f + "\n" + strings.Join(fqns, "\n"))
		if ctx.TotalTokens+ft > budget {
			continue
		}
		ctx.Files = append(ctx.Files, RankedFile{Path: f, Score: scores[f], Symbols: refs})
		ctx.TotalTokens += ft
	}
	return ctx
}

// seedQuery is a normalized dirty-symbol seed.
type seedQuery struct {
	raw  string // trimmed raw seed
	name string // identifier part (after last "::", else whole seed)
	file string // slash-normalized file part, "" if the seed names no file
	dir  string // containing dir of file, "" for root-level or file-less seeds
}

// normalizeSeeds trims, splits "file::Name" seeds, drops empties, and sorts
// + dedupes so seed order never affects output.
func normalizeSeeds(seeds []string) []seedQuery {
	out := make([]seedQuery, 0, len(seeds))
	for _, s := range seeds {
		t := strings.TrimSpace(s)
		if t == "" {
			continue
		}
		q := seedQuery{raw: t, name: t}
		if i := strings.LastIndex(t, "::"); i >= 0 {
			q.file = slashPath(strings.TrimSpace(t[:i]))
			q.name = strings.TrimSpace(t[i+2:])
		} else if looksLikeFile(t) {
			q.file = slashPath(t)
		}
		if q.file != "" {
			if d := path.Dir(q.file); d != "." && d != "" && d != "/" {
				q.dir = d
			}
		}
		if q.raw == "" && q.name == "" {
			continue
		}
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].raw != out[j].raw {
			return out[i].raw < out[j].raw
		}
		if out[i].name != out[j].name {
			return out[i].name < out[j].name
		}
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}
		return out[i].dir < out[j].dir
	})
	uniq := out[:0]
	for i, q := range out {
		if i == 0 || q != out[i-1] {
			uniq = append(uniq, q)
		}
	}
	return uniq
}

// seedMatchesFQN reports whether q names (a substring of) fqn. Empty keys
// never match: strings.Contains(x, "") is true and would boost everything.
func seedMatchesFQN(q seedQuery, fqn string) bool {
	if fqn == "" {
		return false
	}
	if q.raw != "" && strings.Contains(fqn, q.raw) {
		return true
	}
	return q.name != "" && strings.Contains(fqn, q.name)
}

// touchesSeed reports whether any seed names an identifier appearing in the
// source or target endpoint FQN.
func touchesSeed(queries []seedQuery, srcFQN, tgtFQN string) bool {
	for _, q := range queries {
		if seedMatchesFQN(q, srcFQN) || seedMatchesFQN(q, tgtFQN) {
			return true
		}
	}
	return false
}

// inNeighborhood reports whether either endpoint file lives under any seed's
// file directory. Seeds without a usable directory never match.
func inNeighborhood(queries []seedQuery, srcFile, tgtFile string) bool {
	sf, tf := slashPath(srcFile), slashPath(tgtFile)
	for _, q := range queries {
		if q.dir == "" {
			continue
		}
		if strings.Contains(sf, q.dir) || strings.Contains(tf, q.dir) {
			return true
		}
	}
	return false
}

// slashPath normalizes separators for cross-platform dir comparisons.
func slashPath(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

// looksLikeFile guesses whether a "::"-free seed names a file rather than a
// bare identifier (path separators or a dotted extension).
func looksLikeFile(s string) bool {
	if strings.ContainsAny(s, "/\\") {
		return true
	}
	return strings.Contains(s, ".")
}
