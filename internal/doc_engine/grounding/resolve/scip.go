package resolve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// SCIP resolution (plan B1).
//
// Binding status (verified via `go list -m`): the module path suggested in
// the plan, github.com/sourcegraph/scip/bindings/go/scip, does not exist —
// upstream sourcegraph/scip@v0.10.0 ships no Go bindings (only
// dotnet/haskell/java/kotlin/rust/typescript). The SCIP schema now lives at
// module github.com/scip-code/scip (Go bindings: .../bindings/go/scip, a
// nested module). This resolver deliberately takes no dependency on it:
// SCIP symbol strings are language-specific descriptors (e.g.
// "scip-go go . . pkg#Type.Method()."), not repo FQNs
// ("path/to/file.go::Method"), so a protobuf consumer would still need a
// per-language descriptor→FQN map to answer ResolveBatch.
//
// Instead, resolution reads a JSON sidecar at
// .glassmarble/scip/index.json:
//
//	[{"fqn":"internal/auth/jwt.go::ValidateToken","file":"internal/auth/jwt.go","line":25,"end_line":50}, ...]
//
// ("endLine" is accepted as an alias of "end_line"; a missing end resolves
// to the start line.) Lookup is exact-FQN first, then short-name suffix
// match (see shortName). To serve a binary index produced by scip-go,
// scip-typescript, scip-python, etc., convert it with the `scip` CLI: dump
// the binary index to JSON and reshape entries to the
// {fqn,file,line,end_line} schema above.
const (
	scipIndexBinaryRel = ".glassmarble/scip/index.scip"
	scipIndexJSONRel   = ".glassmarble/scip/index.json"
)

// scipEntry is one JSON-sidecar definition record.
type scipEntry struct {
	FQN     string `json:"fqn"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	EndLine int    `json:"end_line"`
}

// UnmarshalJSON accepts "end_line" (canonical) and "endLine" (alias); a
// missing end resolves to the start line.
func (e *scipEntry) UnmarshalJSON(data []byte) error {
	type plain scipEntry
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	var camel struct {
		EndLine *int `json:"endLine"`
	}
	if err := json.Unmarshal(data, &camel); err != nil {
		return err
	}
	*e = scipEntry(p)
	if e.EndLine == 0 && camel.EndLine != nil {
		e.EndLine = *camel.EndLine
	}
	if e.EndLine == 0 {
		e.EndLine = e.Line
	}
	return nil
}

// scipIndexPresent reports SCIP usability per contract: a binary index
// file exists at .glassmarble/scip/index.scip under repoRoot.
func scipIndexPresent(repoRoot string) bool {
	if repoRoot == "" {
		return false
	}
	st, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(scipIndexBinaryRel)))
	return err == nil && !st.IsDir()
}

// loadSCIPSidecar reads the JSON sidecar once, returning an exact-FQN map
// and a short-name map (each list sorted by FQN for determinism).
func loadSCIPSidecar(repoRoot string) (map[string]scipEntry, map[string][]scipEntry, bool) {
	if repoRoot == "" {
		return nil, nil, false
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(scipIndexJSONRel)))
	if err != nil {
		return nil, nil, false
	}
	var entries []scipEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, nil, false
	}
	exact := make(map[string]scipEntry, len(entries))
	byShort := make(map[string][]scipEntry)
	for _, e := range entries {
		if e.FQN == "" {
			continue
		}
		if _, dup := exact[e.FQN]; !dup {
			exact[e.FQN] = e
		}
		s := shortName(e.FQN)
		byShort[s] = append(byShort[s], e)
	}
	for s, list := range byShort {
		sort.Slice(list, func(i, j int) bool { return list[i].FQN < list[j].FQN })
		byShort[s] = list
	}
	return exact, byShort, true
}

// scipResolve answers from the JSON sidecar: exact FQN first, then
// short-name suffix match. Symbols with no hit are left out for the next
// stage (LSP → AST).
func scipResolve(fqns []string, repoRoot string) map[string]Resolution {
	out := make(map[string]Resolution)
	exact, byShort, ok := loadSCIPSidecar(repoRoot)
	if !ok {
		return out
	}
	for _, fqn := range fqns {
		if e, hit := exact[fqn]; hit {
			out[fqn] = Resolution{FQN: fqn, File: e.File, Line: e.Line, EndLine: e.EndLine, Provenance: ProvenanceSCIP}
			continue
		}
		if list := byShort[shortName(fqn)]; len(list) > 0 {
			e := list[0]
			out[fqn] = Resolution{FQN: fqn, File: e.File, Line: e.Line, EndLine: e.EndLine, Provenance: ProvenanceSCIP}
		}
	}
	return out
}
