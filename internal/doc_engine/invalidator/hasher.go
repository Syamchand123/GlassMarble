// Package invalidator implements git diff filtering, AST subgraph hashing,
// and GlobalCommitDossier assembly for the Documentation Intelligence Engine.
package invalidator

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// HashSection computes a deterministic SHA256 hash representing the complete
// factual input for a single managed section.
//
// If the recomputed hash matches SectionState.ASTSubgraphHash in docs_state.json,
// the section has not drifted and requires 0 tokens and 0 disk writes.
func HashSection(sec *config.SectionSpec, symbols []config.SymbolFact, diagrams []config.DiagramRef) string {
	h := sha256.New()

	// 1. Section metadata
	if sec != nil {
		h.Write([]byte("id:" + sec.ID + "\n"))
		h.Write([]byte("title:" + sec.Title + "\n"))
		h.Write([]byte("instruction:" + sec.Instruction + "\n"))
		for _, g := range sec.GroundWith {
			h.Write([]byte("ground:" + g + "\n"))
		}
	}

	// 2. Symbols sorted deterministically by FQN
	sortedSymbols := make([]config.SymbolFact, len(symbols))
	copy(sortedSymbols, symbols)
	sort.Slice(sortedSymbols, func(i, j int) bool {
		return sortedSymbols[i].FQN < sortedSymbols[j].FQN
	})

	for _, s := range sortedSymbols {
		h.Write([]byte("sym:" + s.FQN + "|" + s.Kind + "|" + s.Signature + "|" + s.Doc + "\n"))
	}

	// 3. Diagram references sorted deterministically
	sortedDiagrams := make([]config.DiagramRef, len(diagrams))
	copy(sortedDiagrams, diagrams)
	sort.Slice(sortedDiagrams, func(i, j int) bool {
		if sortedDiagrams[i].Type != sortedDiagrams[j].Type {
			return sortedDiagrams[i].Type < sortedDiagrams[j].Type
		}
		return sortedDiagrams[i].Entry < sortedDiagrams[j].Entry
	})

	for _, d := range sortedDiagrams {
		h.Write([]byte("diag:" + d.Type + "|" + d.Entry + "|" + d.Scope + "\n"))
	}

	return hex.EncodeToString(h.Sum(nil))
}

// HashStrings returns the deterministic SHA256 of sorted arbitrary strings.
func HashStrings(items []string) string {
	sorted := make([]string, len(items))
	copy(sorted, items)
	sort.Strings(sorted)

	h := sha256.New()
	for _, it := range sorted {
		h.Write([]byte(it))
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// NormalizeSignature cleans up signatures for stable comparison across formatting.
func NormalizeSignature(sig string) string {
	return strings.Join(strings.Fields(sig), " ")
}
