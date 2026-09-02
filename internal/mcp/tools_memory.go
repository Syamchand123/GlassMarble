package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/learning"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerMemoryTools binds developer memory and architecture timeline tools to the MCP server.
func (s *Server) registerMemoryTools() {
	// 1. gmb_memory_overview Tool
	memoryOverviewTool := mcp.NewTool("gmb_memory_overview",
		mcp.WithDescription("Get an overview of developer memory: total architectural events, claims, and components."),
	)
	s.RegisterTool(memoryOverviewTool, s.handleMemoryOverviewTool)

	// 2. gmb_memory_query Tool
	memoryQueryTool := mcp.NewTool("gmb_memory_query",
		mcp.WithDescription("Query developer memory for architectural rationale, claims, decisions, and component knowledge."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Topic, architectural concept, component name, or question to search memory for"),
		),
	)
	s.RegisterTool(memoryQueryTool, s.handleMemoryQueryTool)

	// 3. gmb_memory_component Tool
	memoryComponentTool := mcp.NewTool("gmb_memory_component",
		mcp.WithDescription("Inspect the longitudinal architectural history and timeline events for a specific component."),
		mcp.WithString("component",
			mcp.Required(),
			mcp.Description("Name or substring of the component to inspect"),
		),
	)
	s.RegisterTool(memoryComponentTool, s.handleMemoryComponentTool)

	// 4. gmb_arch_timeline Tool
	archTimelineTool := mcp.NewTool("gmb_arch_timeline",
		mcp.WithDescription("Retrieve the chronological architecture evolution timeline (refactorings, ADRs, component changes)."),
		mcp.WithString("from",
			mcp.Description("Start timestamp (RFC3339) or duration (e.g. '2026-01-01', '30d')"),
		),
		mcp.WithString("to",
			mcp.Description("End timestamp (RFC3339) or 'HEAD'"),
		),
		mcp.WithString("component",
			mcp.Description("Optional component name filter"),
		),
	)
	s.RegisterTool(archTimelineTool, s.handleArchTimelineTool)
}

func (s *Server) handleMemoryOverviewTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	store, err := s.bridge.MemoryStore()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Memory store unavailable: %v", err)), nil
	}

	mem, err := store.LoadMemory()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to load memory: %v", err)), nil
	}

	learner := learning.NewLearnerForRepo(s.bridge.RootDir())
	proj, applied, _ := learner.OverlayMemory(mem)

	type compSummary struct {
		Name      string    `json:"name"`
		State     string    `json:"state"`
		FirstSeen time.Time `json:"first_seen"`
		LastSeen  time.Time `json:"last_seen"`
	}

	components := make([]compSummary, 0, len(proj.ComponentMemory))
	for _, c := range proj.ComponentMemory {
		components = append(components, compSummary{
			Name:      c.Name,
			State:     string(c.State),
			FirstSeen: c.FirstSeen,
			LastSeen:  c.LastSeen,
		})
	}

	result := map[string]any{
		"total_events":        proj.TotalEvents,
		"total_claims":        len(proj.GlobalMemory),
		"total_components":    len(proj.ComponentMemory),
		"components":          components,
		"corrections_applied": len(applied),
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Serialization error: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func (s *Server) handleMemoryQueryTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, err := requireStringArg(req, "query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	store, err := s.bridge.MemoryStore()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Memory store unavailable: %v", err)), nil
	}

	mem, err := store.LoadMemory()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to load memory: %v", err)), nil
	}

	learner := learning.NewLearnerForRepo(s.bridge.RootDir())
	proj, applied, _ := learner.OverlayMemory(mem)

	lowerQ := strings.ToLower(query)

	type matchedClaim struct {
		ID         string  `json:"id"`
		Subject    string  `json:"subject"`
		Predicate  string  `json:"predicate"`
		Object     string  `json:"object"`
		Kind       string  `json:"kind"`
		Confidence float64 `json:"confidence"`
		Freshness  float64 `json:"freshness"`
		State      string  `json:"state"`
	}

	type matchedEvent struct {
		ID        string    `json:"id"`
		Kind      string    `json:"kind"`
		Title     string    `json:"title"`
		Timestamp time.Time `json:"timestamp"`
	}

	var claims []matchedClaim
	for _, c := range proj.GlobalMemory {
		if strings.Contains(strings.ToLower(c.Subject), lowerQ) ||
			strings.Contains(strings.ToLower(c.Predicate), lowerQ) ||
			strings.Contains(strings.ToLower(c.Object), lowerQ) {
			claims = append(claims, matchedClaim{
				ID:         c.ID,
				Subject:    c.Subject,
				Predicate:  c.Predicate,
				Object:     c.Object,
				Kind:       string(c.ClaimKind),
				Confidence: float64(c.Evidence.AggConfidence),
				Freshness:  c.FreshnessScore,
				State:      string(c.State),
			})
		}
	}

	var events []matchedEvent
	for _, e := range proj.Events {
		if strings.Contains(strings.ToLower(e.Title), lowerQ) ||
			strings.Contains(strings.ToLower(string(e.Kind)), lowerQ) {
			events = append(events, matchedEvent{
				ID:        e.ID,
				Kind:      string(e.Kind),
				Title:     e.Title,
				Timestamp: e.Timestamp,
			})
		}
	}

	result := map[string]any{
		"query":               query,
		"matched_claims":      claims,
		"matched_events":      events,
		"corrections_applied": len(applied),
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Serialization error: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func (s *Server) handleMemoryComponentTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	component, err := requireStringArg(req, "component")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	store, err := s.bridge.MemoryStore()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Memory store unavailable: %v", err)), nil
	}

	mem, err := store.LoadMemory()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to load memory: %v", err)), nil
	}

	learner := learning.NewLearnerForRepo(s.bridge.RootDir())
	proj, applied, _ := learner.OverlayMemory(mem)

	lowerC := strings.ToLower(component)
	var foundHistory *developer_memory.ComponentHistory
	for _, ch := range proj.ComponentMemory {
		if strings.EqualFold(ch.Name, component) || strings.Contains(strings.ToLower(ch.Name), lowerC) {
			copyH := ch
			foundHistory = &copyH
			break
		}
	}

	timeline := developer_memory.GetComponentTimelineFromMemory(proj, component)

	result := map[string]any{
		"query":               component,
		"found":               foundHistory != nil,
		"history":             foundHistory,
		"timeline":            timeline,
		"corrections_applied": len(applied),
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Serialization error: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func (s *Server) handleArchTimelineTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	store, err := s.bridge.MemoryStore()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Memory store unavailable: %v", err)), nil
	}

	mem, err := store.LoadMemory()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to load memory: %v", err)), nil
	}

	component := getStringArg(req, "component", "")
	var entries []archmodel.TimelineEntry

	if component != "" {
		entries = developer_memory.GetComponentTimelineFromMemory(mem, component)
	} else {
		entries = developer_memory.GetFullTimelineFromMemory(mem, time.Time{}, time.Time{})
	}

	if entries == nil {
		entries = []archmodel.TimelineEntry{}
	}

	out, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Serialization error: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}
