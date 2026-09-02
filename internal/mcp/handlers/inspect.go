package handlers

import (
	"fmt"
	"strings"
)

// Inspect tool names (Master Plan §6 inspect category).
const (
	ToolInspectSearch      = "gmb_inspect_search"
	ToolInspectNode        = "gmb_inspect_node"
	ToolDependencyAnalysis = "gmb_dependency_analysis"
)

// InspectToolNames returns the inspect tool set.
func InspectToolNames() []string {
	return []string{ToolInspectSearch, ToolInspectNode, ToolDependencyAnalysis}
}

// InspectSearchArgs are validated args for gmb_inspect_search.
type InspectSearchArgs struct {
	Query string
	Kind  string
	Limit int
}

// ValidateInspectSearchArgs validates gmb_inspect_search arguments.
func ValidateInspectSearchArgs(args map[string]any) (InspectSearchArgs, error) {
	q, _ := args["query"].(string)
	q = strings.TrimSpace(q)
	if q == "" {
		return InspectSearchArgs{}, fmt.Errorf("missing required parameter \"query\"")
	}
	if len(q) > 1000 {
		return InspectSearchArgs{}, fmt.Errorf("query too long (%d chars, max 1000)", len(q))
	}
	kind, _ := args["kind"].(string)
	kind = strings.TrimSpace(kind)
	limit := 50
	if v, ok := args["limit"]; ok {
		switch n := v.(type) {
		case float64:
			limit = int(n)
		case int:
			limit = n
		}
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	return InspectSearchArgs{Query: q, Kind: kind, Limit: limit}, nil
}

// ValidateInspectNodeArgs validates gmb_inspect_node required id.
func ValidateInspectNodeArgs(args map[string]any) (string, error) {
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("missing required parameter \"id\"")
	}
	if len(id) > 500 {
		return "", fmt.Errorf("id too long (%d chars, max 500)", len(id))
	}
	return id, nil
}

// ValidateDependencyArgs validates gmb_dependency_analysis target.
func ValidateDependencyArgs(args map[string]any) (string, error) {
	target, _ := args["target"].(string)
	target = strings.TrimSpace(target)
	// Empty target is allowed => summary mode per tools_inspect.go
	if len(target) > 1000 {
		return "", fmt.Errorf("target too long (%d chars, max 1000)", len(target))
	}
	return target, nil
}
