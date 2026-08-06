package stage4

import (
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// ApplySyntheticHygiene is the W1-17 virtual/synthetic node hygiene pass
// (master_overhaul_plan.md §5.4.4, A-14). For every node without a
// FileSpec.Path:
//
//   - when a file path is derivable from the node ID (the first "::"
//     segment of "path::Receiver::Name" style IDs, e.g. CFG_SUMMARY,
//     DFG_SUMMARY, VIRTUAL_CONTEXT), it is stamped as the real FileSpec —
//     the node is scopeable and keeps gm:belongsToFile;
//   - otherwise the node gets FileSpec.Path="" (the serializer then never
//     emits a gm:belongsToFile triple for it) plus
//     Properties["gm:synthetic"]="true";
//   - orphan CFG-only branch nodes (IF_BRANCH/LOOP_BRANCH/SWITCH_BRANCH/
//     EXCEPTIONAL_BRANCH/CFG_FLOW) that cannot be scoped to any file are
//     dropped entirely — they are unscopable noise.
func ApplySyntheticHygiene(cpg *Stage4Output) {
	if cpg == nil {
		return
	}

	var drop []string
	for id, n := range cpg.GraphNodes {
		if n == nil {
			continue
		}
		if n.FileSpec.Path != "" {
			continue
		}
		if p := deriveFilePath(id); p != "" {
			n.FileSpec.Path = stage3.NormalizeRelativePath(p)
			continue
		}
		if n.Properties == nil {
			n.Properties = make(map[string]string)
		}
		n.Properties[ont.PredSynthetic] = "true"
		if isOrphanCFGKind(n.Kind) {
			drop = append(drop, id)
		}
	}

	for _, id := range drop {
		cpg.RemoveNode(id)
	}
}

// deriveFilePath extracts a real relative file path from a node ID when
// possible: the first "::" segment of "path::..." IDs. Namespaced virtual
// IDs (ext:/file:/module:/virt:) and unqualified IDs yield "".
func deriveFilePath(id string) string {
	if id == "" {
		return ""
	}
	lower := strings.ToLower(id)
	for _, prefix := range []string{ont.PrefixExt, ont.PrefixFile, ont.PrefixModule, ont.PrefixVirt} {
		if strings.HasPrefix(lower, prefix) {
			return ""
		}
	}
	idx := strings.Index(id, "::")
	if idx == -1 {
		return ""
	}
	seg := id[:idx]
	if seg == "" {
		return ""
	}
	// A real path carries a directory separator or a file extension.
	if strings.ContainsAny(seg, "/\\") {
		return seg
	}
	if filepath.Ext(seg) != "" {
		return seg
	}
	return ""
}

// isOrphanCFGKind reports whether a node kind is CFG-only (per-branch CFG
// artifacts) that must not survive without a file scope.
func isOrphanCFGKind(kind string) bool {
	switch kind {
	case "IF_BRANCH", "LOOP_BRANCH", "SWITCH_BRANCH", "EXCEPTIONAL_BRANCH", "CFG_FLOW":
		return true
	default:
		return false
	}
}
