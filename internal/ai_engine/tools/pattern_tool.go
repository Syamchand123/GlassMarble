package tools

import (
	"context"
)

var PatternTool = Tool{
	Name:        "get_architecture_patterns",
	Description: "Get detected architectural patterns and anti-patterns (smells) in the codebase.",
	Category:    CategoryAKG,
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"include_smells": map[string]any{
				"type": "boolean",
			},
			"min_confidence": map[string]any{
				"type": "number",
			},
		},
	},
	Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
		// Mocked for now - we would fetch the current ArchSnapshot from arch_timeline.SnapshotStore
		return map[string]any{
			"ok": true,
			"patterns": []any{},
			"smells": []any{},
		}, nil
	},
}
