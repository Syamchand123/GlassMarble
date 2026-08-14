package link

// Level-of-detail policy (master_overhaul_plan.md §5.4.3 / W1-15, A-13).
//
// The linker pass registry is partitioned into families:
//
//	STRUCTURAL  type, member, interface, callgraph, filedeps (+ builder spine)
//	DYNAMIC     cfg, dfg, concurrency, semantics, rpc, constraints, ffi,
//	            eventsourcing, di, escape, alias
//	SECURITY    security (LinkSecurityVulnerabilities, post-merge)
//
// applyLevelPolicy maps LevelOfDetail to the set of disabled passes:
//
//	"architecture" — STRUCTURAL only: spine + calls + hierarchy, zero
//	    dynamic/security artifacts (default for visualization consumers).
//	"standard"     — STRUCTURAL + aggregate CFG_SUMMARY/DFG_SUMMARY (cfg/dfg
//	    passes run in summary mode, see cfg_linker/dfg_linker). The named
//	    full-only heuristics — constraints, alias, escape — and security are
//	    gated off. The unlisted lightweight dynamic passes (concurrency,
//	    semantics, rpc, ffi, eventsourcing, di) stay enabled: the §5.4.3
//	    table names only the CFG/DFG summaries for standard, and enterprise
//	    consumers depend on their labeled edges.
//	"full" / ""    — every pass (per-branch CFG/DFG, heuristics, security).
//
// The old behavior (architecture disabled only cfg+dfg, leaking
// ControlStructure/Constraint/CFGFlow soup — A-13) is replaced entirely.

// dynamicPasses produce dynamic-view nodes/edges; none may run at the
// architecture level (acceptance gate: zero dynamic-view artifacts).
var dynamicPasses = []string{
	"cfg", "dfg", "concurrency", "semantics", "rpc", "constraints",
	"ffi", "eventsourcing", "di", "escape", "alias",
}

// securityPasses produce security-view artifacts.
var securityPasses = []string{"security"}

// fullOnlyPasses are the named full-level heuristics in §5.4.3 (the
// "CFG soup" producers) — off under standard, on under full.
var fullOnlyPasses = []string{"constraints", "alias", "escape"}

// applyLevelPolicy mutates the disabled-pass set in place. Explicit
// DisabledPasses from LinkerConfig are merged afterwards by the caller.
func applyLevelPolicy(level string, disabled map[string]bool) {
	switch level {
	case LevelArchitecture:
		for _, p := range dynamicPasses {
			disabled[p] = true
		}
		for _, p := range securityPasses {
			disabled[p] = true
		}
	case LevelStandard:
		for _, p := range fullOnlyPasses {
			disabled[p] = true
		}
		for _, p := range securityPasses {
			disabled[p] = true
		}
	}
}
