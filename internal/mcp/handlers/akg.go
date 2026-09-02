package handlers

import (
	"fmt"
	"strings"
)

// AKG tool names (delegated to internal/ai_engine/tools, Master Plan §6 AKG 18 tools).
var AKGToolNames = []string{
	"akg_status",
	"akg_summary",
	"akg_search",
	"akg_get_node",
	"akg_edges",
	"akg_traverse",
	"akg_path",
	"akg_cycles",
	"akg_orphans",
	"akg_god_objects",
	"akg_hotspots",
	"akg_page_rank",
	"akg_impact_radius",
	"akg_communities",
	"akg_articulation_points",
	"akg_topological_order",
	"akg_entrypoints",
	"akg_similarity",
}

// IsAKGTool reports whether name is an AKG graph tool.
func IsAKGTool(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, t := range AKGToolNames {
		if n == t {
			return true
		}
	}
	return false
}

// AKGSearchArgs holds validated args for akg_search.
type AKGSearchArgs struct {
	Kind       string
	NameContains string
	Primitive  string
	Limit      int
	Offset     int
}

// ValidateAKGSearchArgs validates and clamps akg_search arguments.
func ValidateAKGSearchArgs(args map[string]any) (AKGSearchArgs, error) {
	out := AKGSearchArgs{Limit: 20}
	if v, ok := args["kind"].(string); ok {
		out.Kind = strings.TrimSpace(v)
	}
	if v, ok := args["name_contains"].(string); ok {
		out.NameContains = strings.TrimSpace(v)
	}
	if v, ok := args["primitive"].(string); ok {
		out.Primitive = strings.TrimSpace(v)
	}
	if v, ok := args["limit"]; ok {
		switch n := v.(type) {
		case float64:
			out.Limit = int(n)
		case int:
			out.Limit = n
		}
	}
	if out.Limit < 1 {
		out.Limit = 1
	}
	if out.Limit > 200 {
		out.Limit = 200
	}
	if v, ok := args["offset"]; ok {
		switch n := v.(type) {
		case float64:
			out.Offset = int(n)
		case int:
			out.Offset = n
		}
		if out.Offset < 0 {
			out.Offset = 0
		}
	}
	if strings.TrimSpace(out.Kind) == "" && strings.TrimSpace(out.NameContains) == "" && strings.TrimSpace(out.Primitive) == "" {
		// Empty search is allowed but warn: will return top N.
	}
	return out, nil
}

// ValidateAKGGetNodeArgs validates akg_get_node required id.
func ValidateAKGGetNodeArgs(args map[string]any) (string, error) {
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

// ClampLimit clamps an int limit between lo and hi inclusive.
func ClampLimit(v, lo, hi, def int) int {
	if v < lo {
		if def >= lo && def <= hi {
			return def
		}
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
