package aggregate

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// EntryPointKind classifies the root execution node.
type EntryPointKind string

const (
	// EntryPointMain is a language entry (func main, if __name__ == "__main__", etc.).
	EntryPointMain EntryPointKind = "main"
	// EntryPointHandler is a network-facing root (HTTP handlers, routers, RPC).
	EntryPointHandler EntryPointKind = "handler"
)

// EntryPoint is a v2 (W1-08) structured root execution node.
type EntryPoint struct {
	FQN      string           `json:"fqn"`
	Name     string           `json:"name"`
	Kind     EntryPointKind   `json:"kind"`
	FilePath string           `json:"file_path,omitempty"`
	Node     *normalize.GASTNode `json:"-"`
}

// FindEntryPoints scans the global definition index for root execution
// nodes: language entry functions (main/init) and network-facing handlers
// (HTTP/RPC/Router endpoints). Returns a stable, sorted slice.
func FindEntryPoints(output *AggregateOutput) []EntryPoint {
	var entries []EntryPoint
	if output == nil || output.GlobalDefinitionIndex == nil {
		return entries
	}

	seen := make(map[*normalize.GASTNode]bool)
	for fqn, nodes := range output.GlobalDefinitionIndex {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			if seen[node] {
				continue
			}
			seen[node] = true
			kind := entryPointKindOf(node)
			if kind == "" {
				continue
			}
			entries = append(entries, EntryPoint{
				FQN:      fqn,
				Name:     node.Name,
				Kind:     kind,
				FilePath: node.Properties["file_path"],
				Node:     node,
			})
		}
	}

	sortEntryPoints(entries)
	return entries
}

func entryPointKindOf(node *normalize.GASTNode) EntryPointKind {
	nameLower := strings.ToLower(node.Name)
	if nameLower == "main" || nameLower == "init" {
		if strings.EqualFold(node.Kind, "method") {
			return ""
		}
		if node.Type != "" && node.Type != normalize.GASTFunction {
			return ""
		}
		return EntryPointMain
	}

	if node.Properties != nil {
		switch node.Properties["primitive"] {
		case "NETWORK_IO", "EXPOSES_ENDPOINT", "ROUTER", "RPC_HANDLER":
			return EntryPointHandler
		}
	}
	return ""
}

func sortEntryPoints(entries []EntryPoint) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j-1].FQN > entries[j].FQN; j-- {
			entries[j-1], entries[j] = entries[j], entries[j-1]
		}
	}
}

// IndexEntrypoints scans all nodes to identify RootExecutionNodes (Entrypoints).
// These are functions like main(), init(), or endpoints exposed via HTTP, RPC, etc.
// v2 (W1-08/W1-09): delegates to FindEntryPoints and stamps gm:isMain on
// language entry nodes.
func IndexEntrypoints(output *AggregateOutput) {
	if output == nil {
		return
	}
	output.EntrypointRegistry = make([]string, 0)
	// Purge stale entrypoint markers from previous incremental run (C2-10).
	for _, nodes := range output.GlobalDefinitionIndex {
		for _, n := range nodes {
			if n != nil && n.Properties != nil {
				delete(n.Properties, "is_entrypoint")
				delete(n.Properties, ont.PredIsMain)
			}
		}
	}

	for _, ep := range FindEntryPoints(output) {
		if ep.Node == nil {
			continue
		}
		if ep.Node.Properties == nil {
			ep.Node.Properties = make(map[string]string)
		}
		ep.Node.Properties["is_entrypoint"] = "true"
		if ep.Kind == EntryPointMain {
			ep.Node.Properties[ont.PredIsMain] = "true"
		}
		if !containsString(output.EntrypointRegistry, ep.FQN) {
			output.EntrypointRegistry = append(output.EntrypointRegistry, ep.FQN)
		}
	}
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
