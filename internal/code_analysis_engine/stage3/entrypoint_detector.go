package stage3

import (
	"strings"
)

// IndexEntrypoints scans all nodes to identify RootExecutionNodes (Entrypoints).
// These are functions like main(), init(), or endpoints exposed via HTTP, RPC, etc.
func IndexEntrypoints(output *Stage3Output) {
	if output.EntrypointRegistry == nil {
		output.EntrypointRegistry = make([]string, 0)
	}

	for fqn, nodes := range output.GlobalDefinitionIndex {
		isRoot := false

		for _, node := range nodes {
			if node == nil {
				continue
			}

			// 1. Standard name checks
			nameLower := strings.ToLower(node.Name)
			if nameLower == "main" || nameLower == "init" {
				isRoot = true
			}

			// 2. Behavioral Primitive Checks (from Step 2.4)
			if node.Properties != nil {
				primitive := node.Properties["primitive"]
				if primitive == "NETWORK_IO" || primitive == "EXPOSES_ENDPOINT" || primitive == "ROUTER" || primitive == "RPC_HANDLER" {
					isRoot = true
				}
			}

			if isRoot {
				if node.Properties == nil {
					node.Properties = make(map[string]string)
				}
				node.Properties["is_entrypoint"] = "true"
				output.EntrypointRegistry = append(output.EntrypointRegistry, fqn)
				break // Found a reason, mark the FQN and move on
			}
		}
	}
}
