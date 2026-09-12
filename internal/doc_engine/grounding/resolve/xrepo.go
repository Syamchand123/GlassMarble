package resolve

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
)

// Cross-repo resolution (gap B1e).
//
// Extra roots come from the GMB_EXTRA_REPOS environment variable: an OS
// path-list (`filepath.ListSeparator` separated — ':' on unix, ';' on
// windows) of additional repository checkouts to search when the primary
// repo cannot resolve a symbol.

// ExtraRepoRoots parses GMB_EXTRA_REPOS into a list of non-empty,
// whitespace-trimmed roots. Unset or empty → nil (callers treat it as a
// no-op). Split uses filepath.ListSeparator so the same variable works on
// unix (colon) and Windows (semicolon).
func ExtraRepoRoots() []string {
	raw := strings.TrimSpace(os.Getenv("GMB_EXTRA_REPOS"))
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, string(filepath.ListSeparator)) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// maxCrossRepoRoots caps the extra roots consulted per ResolveCrossRepo
// call so a long GMB_EXTRA_REPOS list cannot fan out unboundedly. The
// per-factsheet lookup budget (maxCrossRepoLookups in grounding) sits one
// layer up in applyPrecisionLayer; this is the per-call backstop.
const maxCrossRepoRoots = 20

// ResolveCrossRepo looks up fqn in each extra repoRoot's JSON sidecar at
// .glassmarble/scip/index.json (via the shared loadSCIPSidecar reader — no
// duplicated parsing) and returns the first hit with Provenance
// "scip:xrepo". Matching mirrors scipResolve: exact FQN first, then
// short-name suffix match (deterministic: first list entry by FQN order).
// At most maxCrossRepoRoots extra roots are consulted per call (budget
// guard; first hit still wins). No hit (or no roots) → Provenance
// "unresolved", never an error.
//
// The graph parameter is accepted for signature symmetry with the other
// stages; cross-repo answers come from sidecars only and graph is unused.
func ResolveCrossRepo(fqn string, repoRoots []string, graph *akg.CodePropertyGraph) Resolution {
	_ = graph
	if strings.TrimSpace(fqn) == "" || len(repoRoots) == 0 {
		return Resolution{FQN: fqn, Provenance: ProvenanceUnresolved}
	}
	consulted := 0
	for _, root := range repoRoots {
		if consulted >= maxCrossRepoRoots {
			break
		}
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		consulted++
		exact, byShort, ok := loadSCIPSidecar(root)
		if !ok {
			continue
		}
		if e, hit := exact[fqn]; hit {
			return Resolution{FQN: fqn, File: e.File, Line: e.Line, EndLine: e.EndLine, Provenance: ProvenanceCrossRepo}
		}
		if list := byShort[shortName(fqn)]; len(list) > 0 {
			e := list[0]
			return Resolution{FQN: fqn, File: e.File, Line: e.Line, EndLine: e.EndLine, Provenance: ProvenanceCrossRepo}
		}
	}
	return Resolution{FQN: fqn, Provenance: ProvenanceUnresolved}
}
