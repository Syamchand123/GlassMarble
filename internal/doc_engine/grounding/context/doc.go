// Package context implements improvement plan B2: a PageRank-ranked,
// budget-aware grounding context package (Aider repomap pattern).
//
// A file-to-file directed graph is built from the CPG once per call and a
// personalized PageRank biased toward the dirty (seed) symbols ranks files
// by relevance. The caller then fills its token budget top-down so prose
// about an interface also sees the one-hop neighborhood (callers, types)
// that explains *why*, not just the signature that states *what*.
//
// Algorithm parameters (fixed):
//
//	damping 0.85, convergence delta (L1) < 1e-6, at most 100 iterations,
//	file graph capped at 20000 files (deterministic truncation: sort file
//	paths, keep the first N). Edge weight 1.0, x10 for edges touching a
//	seed identifier (seed substring match on either endpoint FQN), x50 for
//	edges where either endpoint file is inside a seed's file directory
//	(multipliers stack). Restart vector uniform over seed files, uniform
//	over all files when no seed resolves to a file.
//
// Budget behavior: files are walked score-desc and a file is included only
// while TotalTokens+fileTokens <= budget, where fileTokens is EstimateTokens
// over "path + joined symbol FQNs" of the file's (at most 8) symbols.
// budgetTokens <= 0 selects the 1000-token default.
//
// Determinism: every map iteration is funneled through sorted key order,
// edge pairs are sorted and deduplicated, ties break by path ascending, and
// symbols sort by FQN — identical inputs yield identical output.
package context
