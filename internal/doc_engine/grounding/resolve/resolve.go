// Package resolve implements improvement plan B1: SCIP/LSP-backed symbol
// resolution with AST fallback for the doc engine grounding layer.
//
// Resolution order per symbol is SCIP → LSP → AST → unresolved, and the
// source that answered is recorded in Resolution.Provenance so downstream
// pipeline stages (and SymbolFact.Provenance in internal/doc_engine/config)
// can distinguish compiler-accurate answers from heuristic ones.
//
// Source notes:
//   - SCIP: consumed via a JSON sidecar at .glassmarble/scip/index.json
//     (see scip.go). Binary .scip indexes need the `scip` CLI to convert.
//   - LSP: minimal JSON-RPC 2.0 client over gopls stdio (see lsp.go).
//     Best-effort and bounded: 10s total, at most 50 symbols per call.
//   - AST: lookup against the passed AKG CodePropertyGraph (see ast.go).
//     Always available, zero setup.
package resolve

import (
	"context"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
)

// Provenance values recorded in Resolution.Provenance.
const (
	ProvenanceSCIP       = "scip"
	ProvenanceCrossRepo  = "scip:xrepo"
	ProvenanceLSP        = "lsp"
	ProvenanceAST        = "ast"
	ProvenanceUnresolved = "unresolved"
)

// Resolution is the located definition of one requested symbol.
type Resolution struct {
	FQN        string
	File       string
	Line       int
	EndLine    int
	Provenance string
}

// AvailableSources reports which of scip/lsp/ast are usable for repoRoot:
//   - "scip" when a SCIP index file exists at .glassmarble/scip/index.scip
//     OR the JSON sidecar exists at .glassmarble/scip/index.json
//     (resolution reads the JSON sidecar, so JSON-only repos count)
//   - "lsp" when a gopls binary is on PATH
//   - "ast" always (no setup required)
func AvailableSources(repoRoot string) []string {
	sources := make([]string, 0, 3)
	if scipIndexPresent(repoRoot) {
		sources = append(sources, ProvenanceSCIP)
	}
	if lspAvailable() {
		sources = append(sources, ProvenanceLSP)
	}
	sources = append(sources, ProvenanceAST)
	return sources
}

// ResolveBatch resolves each requested FQN to a file:line range.
// Order per symbol: SCIP → LSP → AST → unresolved; the answering source
// is recorded in Resolution.Provenance. The returned map holds an entry
// for every non-empty input FQN (misses are "unresolved", never omitted).
// It never returns an error: every stage degrades to the next on failure.
func ResolveBatch(fqns []string, graph *akg.CodePropertyGraph, repoRoot string) map[string]Resolution {
	out := make(map[string]Resolution, len(fqns))
	queue := dedupeFQNs(fqns)
	if len(queue) == 0 {
		return out
	}
	for fqn, r := range scipResolve(queue, repoRoot) {
		out[fqn] = r
	}
	if rest := stillUnresolved(queue, out); len(rest) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), lspTimeout)
		for fqn, r := range lspResolve(ctx, repoRoot, rest, graph) {
			out[fqn] = r
		}
		cancel()
	}
	if rest := stillUnresolved(queue, out); len(rest) > 0 {
		for fqn, r := range astResolve(rest, graph) {
			out[fqn] = r
		}
	}
	for _, fqn := range queue {
		if _, ok := out[fqn]; !ok {
			out[fqn] = Resolution{FQN: fqn, Provenance: ProvenanceUnresolved}
		}
	}
	return out
}

// shortName reduces an FQN to its trailing symbol: the text after the
// last "::" (the file::Symbol separator), then after the last "." so a
// method FQN ("pkg::Type.Method") matches on "Method".
func shortName(fqn string) string {
	s := fqn
	if i := strings.LastIndex(s, "::"); i >= 0 {
		s = s[i+2:]
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// dedupeFQNs drops empty strings and duplicates, preserving input order.
func dedupeFQNs(fqns []string) []string {
	seen := make(map[string]struct{}, len(fqns))
	out := make([]string, 0, len(fqns))
	for _, fqn := range fqns {
		if fqn == "" {
			continue
		}
		if _, dup := seen[fqn]; dup {
			continue
		}
		seen[fqn] = struct{}{}
		out = append(out, fqn)
	}
	return out
}

// stillUnresolved returns the queued FQNs with no resolution yet.
func stillUnresolved(queue []string, done map[string]Resolution) []string {
	var rest []string
	for _, fqn := range queue {
		if _, ok := done[fqn]; !ok {
			rest = append(rest, fqn)
		}
	}
	return rest
}
