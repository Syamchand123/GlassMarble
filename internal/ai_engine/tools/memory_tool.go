package tools

import (
	"context"
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// QueryMemoryTool enables the AI to query historical architecture facts.
var QueryMemoryTool = Tool{
	Name:        "query_architecture_memory",
	Description: "Search the architecture memory for information about specific components, technologies, or decisions. Returns timeline entries, knowledge claims, and evidence.",
	Category:    CategoryAKG,
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "what to search for (technology name, component name, or question)",
			},
		},
		"required": []string{"query"},
	},
	Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
		query, ok := args["query"].(string)
		if !ok || query == "" {
			return nil, fmt.Errorf("missing 'query' parameter")
		}

		store := developer_memory.NewMemoryStore(env.RootDir)
		result := developer_memory.QueryMemory(store, query)

		return map[string]any{
			"ok":     true,
			"claims": result.Claims,
			"events": result.Components,
		}, nil
	},
}
