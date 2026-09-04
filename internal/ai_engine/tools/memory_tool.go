package tools

import (
	"context"
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// QueryMemoryTool enables the AI agent to query historical architecture facts and developer memory.
var QueryMemoryTool = Tool{
	Name:        "query_architecture_memory",
	Description: "Search the architecture memory for information about specific components, technologies, or decisions. Returns timeline entries, knowledge claims, and evidence.",
	Category:    CategoryAKG,
	Parameters: Schema(map[string]Prop{
		"query": {Type: "string", Description: "What to search for (technology name, component name, or architectural question)", Required: true},
	}),
	Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
		if env == nil || env.RootDir == "" {
			return nil, fmt.Errorf("workspace root is not configured")
		}

		query := strArg(args, "query", "")
		if query == "" {
			return nil, fmt.Errorf("missing 'query' parameter")
		}

		store := developer_memory.NewStoreForRepo(env.RootDir)
		result := developer_memory.QueryMemory(store, query)

		claims := result.Claims
		if claims == nil {
			claims = []developer_memory.KnowledgeClaim{}
		}
		components := result.Components
		if components == nil {
			components = []developer_memory.ComponentHistory{}
		}
		events := result.Events
		if events == nil {
			events = []archmodel.ArchEvent{}
		}
		timeline := result.Timeline
		if timeline == nil {
			timeline = []archmodel.TimelineEntry{}
		}

		return map[string]any{
			"ok":         true,
			"query":      query,
			"claims":     claims,
			"components": components,
			"events":     events,
			"timeline":   timeline,
		}, nil
	},
}
