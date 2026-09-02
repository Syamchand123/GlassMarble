package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerResources binds static and dynamic URI resources to the MCP server.
func (s *Server) registerResources() {
	// 1. gmb://status Resource
	statusRes := mcp.NewResource(
		"gmb://status",
		"Architecture Knowledge Graph Status",
		mcp.WithResourceDescription("Real-time metadata and node/edge metrics of the active Architecture Knowledge Graph"),
		mcp.WithMIMEType("application/json"),
	)
	s.MCPServer().AddResource(statusRes, s.handleStatusResource)

	// 2. gmb://config Resource
	configRes := mcp.NewResource(
		"gmb://config",
		"GlassMarble Configuration",
		mcp.WithResourceDescription("Current project configuration and layer definitions (.glassmarble/config.yaml)"),
		mcp.WithMIMEType("text/yaml"),
	)
	s.MCPServer().AddResource(configRes, s.handleConfigResource)

	// 3. gmb://rules Resource
	rulesRes := mcp.NewResource(
		"gmb://rules",
		"Declarative Architecture Rules",
		mcp.WithResourceDescription("Current architectural boundaries, forbidden dependencies, and lint rules (.glassmarble/rules.yaml)"),
		mcp.WithMIMEType("text/yaml"),
	)
	s.MCPServer().AddResource(rulesRes, s.handleRulesResource)

	// 4. gmb://memory/summary Resource
	memoryRes := mcp.NewResource(
		"gmb://memory/summary",
		"Developer Memory Summary",
		mcp.WithResourceDescription("Developer memory aggregate summary (events, claims, tracked components)"),
		mcp.WithMIMEType("application/json"),
	)
	s.MCPServer().AddResource(memoryRes, s.handleMemorySummaryResource)

	// 5. gmb://timeline/latest Resource
	timelineRes := mcp.NewResource(
		"gmb://timeline/latest",
		"Architecture Evolution Timeline",
		mcp.WithResourceDescription("Recent chronological architecture evolution events and refactorings"),
		mcp.WithMIMEType("application/json"),
	)
	s.MCPServer().AddResource(timelineRes, s.handleTimelineResource)
}

func (s *Server) handleStatusResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	graph, err := s.bridge.Snapshot()
	if err != nil {
		body, _ := json.MarshalIndent(map[string]any{
			"initialized": false,
			"error":       fmt.Sprintf("AKG database unavailable: %v", err),
		}, "", "  ")
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(body),
			},
		}, nil
	}

	body, _ := json.MarshalIndent(map[string]any{
		"initialized":            true,
		"nodes":                  graph.Nodes.Len(),
		"outbound_edge_mappings": graph.OutboundEdges.Len(),
		"inbound_edge_mappings":  graph.InboundEdges.Len(),
		"storage_dir":            s.bridge.StorageDir(),
	}, "", "  ")

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     string(body),
		},
	}, nil
}

func (s *Server) handleConfigResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	cfgPath := filepath.Join(s.bridge.StorageDir(), "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: "text/yaml",
				Text:     "# No .glassmarble/config.yaml found (using default configuration)",
			},
		}, nil
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/yaml",
			Text:     string(data),
		},
	}, nil
}

func (s *Server) handleRulesResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	rulesPath := filepath.Join(s.bridge.StorageDir(), "rules.yaml")
	data, err := os.ReadFile(rulesPath)
	if err != nil {
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: "text/yaml",
				Text:     "# No .glassmarble/rules.yaml found",
			},
		}, nil
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/yaml",
			Text:     string(data),
		},
	}, nil
}

func (s *Server) handleMemorySummaryResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	store, err := s.bridge.MemoryStore()
	if err != nil {
		body, _ := json.MarshalIndent(map[string]any{"error": err.Error()}, "", "  ")
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(body),
			},
		}, nil
	}

	mem, err := store.LoadMemory()
	if err != nil {
		body, _ := json.MarshalIndent(map[string]any{"error": err.Error()}, "", "  ")
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(body),
			},
		}, nil
	}

	body, _ := json.MarshalIndent(map[string]any{
		"total_events":     mem.TotalEvents,
		"total_claims":     len(mem.GlobalMemory),
		"total_components": len(mem.ComponentMemory),
		"last_updated":     mem.LastUpdated,
	}, "", "  ")

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     string(body),
		},
	}, nil
}

func (s *Server) handleTimelineResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	store, err := s.bridge.MemoryStore()
	if err != nil {
		body, _ := json.MarshalIndent([]any{}, "", "  ")
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(body),
			},
		}, nil
	}

	mem, err := store.LoadMemory()
	if err != nil || mem == nil {
		body, _ := json.MarshalIndent([]any{}, "", "  ")
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(body),
			},
		}, nil
	}

	timeline := mem.Timeline
	if len(timeline) > 20 {
		timeline = timeline[len(timeline)-20:]
	}

	body, _ := json.MarshalIndent(map[string]any{
		"count":    len(timeline),
		"timeline": timeline,
	}, "", "  ")

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     string(body),
		},
	}, nil
}
