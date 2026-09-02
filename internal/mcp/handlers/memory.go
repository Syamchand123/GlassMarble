package handlers

import (
	"fmt"
	"strings"
)

// Memory tool names (Master Plan §6 memory category).
const (
	ToolMemoryOverview  = "gmb_memory_overview"
	ToolMemoryQuery     = "gmb_memory_query"
	ToolMemoryComponent = "gmb_memory_component"
)

// MemoryToolNames returns the memory tool set (excludes timeline which lives in timeline.go).
func MemoryToolNames() []string {
	return []string{ToolMemoryOverview, ToolMemoryQuery, ToolMemoryComponent}
}

// ValidateMemoryQueryArgs validates gmb_memory_query.
func ValidateMemoryQueryArgs(args map[string]any) (string, error) {
	q, _ := args["query"].(string)
	q = strings.TrimSpace(q)
	if q == "" {
		return "", fmt.Errorf("missing required parameter \"query\"")
	}
	if len(q) > 1000 {
		return "", fmt.Errorf("query too long (%d chars, max 1000)", len(q))
	}
	return q, nil
}

// ValidateMemoryComponentArgs validates gmb_memory_component.
func ValidateMemoryComponentArgs(args map[string]any) (string, error) {
	c, _ := args["component"].(string)
	c = strings.TrimSpace(c)
	if c == "" {
		return "", fmt.Errorf("missing required parameter \"component\"")
	}
	if len(c) > 500 {
		return "", fmt.Errorf("component too long (%d chars, max 500)", len(c))
	}
	return c, nil
}

// MemoryOverviewResult is a helper for formatting memory overview.
type MemoryOverviewResult struct {
	TotalEvents       int `json:"total_events"`
	TotalClaims       int `json:"total_claims"`
	TotalComponents   int `json:"total_components"`
	CorrectionsApplied int `json:"corrections_applied"`
}
