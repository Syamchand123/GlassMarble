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
	// 1. Status Resources (gmb://status & glassmarble://status)
	s.MCPServer().AddResource(
		mcp.NewResource("gmb://status", "AKG Status", mcp.WithResourceDescription("Real-time metadata of the active AKG"), mcp.WithMIMEType("application/json")),
		s.handleStatusResource,
	)
	s.MCPServer().AddResource(
		mcp.NewResource("glassmarble://status", "GlassMarble Status", mcp.WithResourceDescription("Real-time metadata of the active AKG"), mcp.WithMIMEType("application/json")),
		s.handleStatusResource,
	)

	// 2. Intelligence Resource (glassmarble://intelligence)
	s.MCPServer().AddResource(
		mcp.NewResource("glassmarble://intelligence", "Architecture Intelligence", mcp.WithResourceDescription("Latest architecture intelligence report (.glassmarble/intelligence/latest.json)"), mcp.WithMIMEType("application/json")),
		s.handleIntelligenceResource,
	)

	// 3. Memory Resources (glassmarble://memory & gmb://memory/summary)
	s.MCPServer().AddResource(
		mcp.NewResource("gmb://memory/summary", "Developer Memory Summary", mcp.WithResourceDescription("Developer memory summary"), mcp.WithMIMEType("application/json")),
		s.handleMemorySummaryResource,
	)
	s.MCPServer().AddResource(
		mcp.NewResource("glassmarble://memory", "Developer Memory Overview", mcp.WithResourceDescription("Developer memory summary"), mcp.WithMIMEType("application/json")),
		s.handleMemorySummaryResource,
	)

	// 4. Timeline Resources (glassmarble://timeline & gmb://timeline/latest)
	s.MCPServer().AddResource(
		mcp.NewResource("gmb://timeline/latest", "Architecture Evolution Timeline", mcp.WithResourceDescription("Recent chronological architecture evolution events"), mcp.WithMIMEType("application/json")),
		s.handleTimelineResource,
	)
	s.MCPServer().AddResource(
		mcp.NewResource("glassmarble://timeline", "Architecture Timeline JSON", mcp.WithResourceDescription("Architecture timeline file (.glassmarble/memory/timeline.json)"), mcp.WithMIMEType("application/json")),
		s.handleTimelineFileResource,
	)

	// 5. Conventions Resource (glassmarble://conventions)
	s.MCPServer().AddResource(
		mcp.NewResource("glassmarble://conventions", "Learned Project Conventions", mcp.WithResourceDescription("Learned architecture conventions (.glassmarble/memory/conventions.json)"), mcp.WithMIMEType("application/json")),
		s.handleConventionsResource,
	)

	// 6. Telemetry Resource (glassmarble://telemetry)
	s.MCPServer().AddResource(
		mcp.NewResource("glassmarble://telemetry", "Pipeline Telemetry", mcp.WithResourceDescription("GlassMarble pipeline performance telemetry (.glassmarble/telemetry.json)"), mcp.WithMIMEType("application/json")),
		s.handleTelemetryResource,
	)

	// 7. Config Resources (gmb://config & glassmarble://config)
	s.MCPServer().AddResource(
		mcp.NewResource("gmb://config", "GlassMarble Configuration", mcp.WithResourceDescription("Current project configuration (.glassmarble/config.yaml)"), mcp.WithMIMEType("text/yaml")),
		s.handleConfigResource,
	)
	s.MCPServer().AddResource(
		mcp.NewResource("glassmarble://config", "GlassMarble Configuration", mcp.WithResourceDescription("Current project configuration (.glassmarble/config.yaml)"), mcp.WithMIMEType("text/yaml")),
		s.handleConfigResource,
	)

	// 8. Rules Resources (gmb://rules & glassmarble://rules)
	s.MCPServer().AddResource(
		mcp.NewResource("gmb://rules", "Architecture Rules", mcp.WithResourceDescription("Declarative architecture rules (.glassmarble/rules.yaml)"), mcp.WithMIMEType("text/yaml")),
		s.handleRulesResource,
	)
	s.MCPServer().AddResource(
		mcp.NewResource("glassmarble://rules", "Architecture Rules", mcp.WithResourceDescription("Declarative architecture rules (.glassmarble/rules.yaml)"), mcp.WithMIMEType("text/yaml")),
		s.handleRulesResource,
	)
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

func (s *Server) handleIntelligenceResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	path := filepath.Join(s.bridge.StorageDir(), "intelligence", "latest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     "{}",
			},
		}, nil
	}
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     string(data),
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

func (s *Server) handleTimelineFileResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	path := filepath.Join(s.bridge.StorageDir(), "memory", "timeline.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     "[]",
			},
		}, nil
	}
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     string(data),
		},
	}, nil
}

func (s *Server) handleConventionsResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	path := filepath.Join(s.bridge.StorageDir(), "memory", "conventions.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     "{}",
			},
		}, nil
	}
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     string(data),
		},
	}, nil
}

func (s *Server) handleTelemetryResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	path := filepath.Join(s.bridge.StorageDir(), "telemetry.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     "{}",
			},
		}, nil
	}
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     string(data),
		},
	}, nil
}
