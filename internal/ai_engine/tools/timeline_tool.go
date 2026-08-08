package tools

import (
	"context"

	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

var TimelineTool = Tool{
	Name:        "get_architecture_timeline",
	Description: "Get the architecture evolution timeline, optionally filtered by component or time range.",
	Category:    CategoryAKG,
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"component": map[string]any{
				"type":        "string",
				"description": "optional component name",
			},
			"from_date": map[string]any{
				"type":        "string",
				"description": "optional ISO 8601 date",
			},
			"to_date": map[string]any{
				"type":        "string",
				"description": "optional ISO 8601 date",
			},
		},
	},
	Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
		store := developer_memory.NewMemoryStore(env.RootDir)
		
		component, _ := args["component"].(string)
		if component != "" {
			timeline := developer_memory.GetComponentTimeline(store, component)
			return map[string]any{
				"ok":       true,
				"timeline": timeline,
			}, nil
		}

		// Simplified for full timeline (usually bounded by recent)
		mem, err := store.LoadMemory()
		if err != nil || mem == nil {
			return map[string]any{"ok": true, "timeline": []any{}}, nil
		}

		return map[string]any{
			"ok":       true,
			"timeline": mem.Timeline,
		}, nil
	},
}
