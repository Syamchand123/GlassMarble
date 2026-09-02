package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/arch_linter"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/drift"
	"github.com/mark3labs/mcp-go/mcp"
	"gopkg.in/yaml.v3"
)

// registerGovernanceTools binds drift, lint, pattern detection, and coupling stats tools.
func (s *Server) registerGovernanceTools() {
	// 1. gmb_drift_check Tool
	driftTool := mcp.NewTool("gmb_drift_check",
		mcp.WithDescription("Detect architecture drift against declared layering and cycle budgets in config.yaml."),
		mcp.WithString("config_file",
			mcp.Description("Optional path to custom config.yaml file"),
		),
	)
	s.RegisterTool(driftTool, s.handleDriftTool)

	// 2. gmb_arch_lint Tool
	lintTool := mcp.NewTool("gmb_arch_lint",
		mcp.WithDescription("Lint repository against architectural rules and layer boundaries declared in rules.yaml."),
		mcp.WithString("rules_file",
			mcp.Description("Optional path to custom rules.yaml file (default: .glassmarble/rules.yaml)"),
		),
	)
	s.RegisterTool(lintTool, s.handleLintTool)

	// 3. gmb_patterns_smells Tool
	patternsTool := mcp.NewTool("gmb_patterns_smells",
		mcp.WithDescription("Detect architectural design patterns (Clean Arch, DDD, Event-Driven) and structural smells."),
		mcp.WithBoolean("include_smells",
			mcp.Description("Include architectural code & structural smells in output (default: true)"),
		),
		mcp.WithBoolean("include_components",
			mcp.Description("Include inferred component boundaries in output (default: true)"),
		),
	)
	s.RegisterTool(patternsTool, s.handlePatternsTool)

	// 4. gmb_arch_stats Tool
	archStatsTool := mcp.NewTool("gmb_arch_stats",
		mcp.WithDescription("Compute component coupling health metrics: Afferent Coupling (Ca), Efferent Coupling (Ce), and Instability."),
	)
	s.RegisterTool(archStatsTool, s.handleArchStatsTool)
}

func (s *Server) handleDriftTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	graph, err := s.bridge.Snapshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("AKG database unavailable: %v — run 'gmb analyze' first", err)), nil
	}

	configPath := getStringArg(req, "config_file", "")
	if configPath == "" {
		configPath = filepath.Join(s.bridge.StorageDir(), "config.yaml")
	}

	cfg := config.Config{}
	if data, rerr := os.ReadFile(configPath); rerr == nil {
		var local config.Config
		_ = yaml.Unmarshal(data, &local)
		cfg = local
	}

	rep := drift.Analyze(graph, cfg.Drift)

	passed := !rep.ExceedsBudget() && rep.ForbiddenEdges == 0

	result := map[string]any{
		"passed":          passed,
		"forbidden_edges": rep.ForbiddenEdges,
		"cycle_count":     rep.CycleCount,
		"cycle_budget":    rep.CycleBudget,
		"exceeds_budget":  rep.ExceedsBudget(),
		"violations":      rep.Violations,
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to serialize drift report: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func (s *Server) handleLintTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	graph, err := s.bridge.Snapshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("AKG database unavailable: %v — run 'gmb analyze' first", err)), nil
	}

	rulesPath := getStringArg(req, "rules_file", "")
	if rulesPath == "" {
		candidates := []string{
			filepath.Join(s.bridge.StorageDir(), "rules.yaml"),
			filepath.Join(s.bridge.StorageDir(), "rules.yml"),
			filepath.Join(s.bridge.RootDir(), ".gmb-rules.yaml"),
			filepath.Join(s.bridge.RootDir(), "gmb-rules.yaml"),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				rulesPath = c
				break
			}
		}
	}

	if rulesPath == "" {
		out, _ := json.MarshalIndent(map[string]any{
			"passed":     true,
			"violations": []any{},
			"message":    "No rules.yaml file found. To enforce custom rules, create .glassmarble/rules.yaml or use 'gmb lint --init'.",
		}, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}

	ruleset, err := arch_linter.LoadRules(rulesPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load rules from %s: %v", rulesPath, err)), nil
	}

	report, err := arch_linter.Lint(graph, ruleset)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("linting failed: %v", err)), nil
	}

	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to serialize lint report: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func (s *Server) handlePatternsTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	graph, err := s.bridge.Snapshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("AKG database unavailable: %v — run 'gmb analyze' first", err)), nil
	}

	includeSmells := true
	includeComponents := true
	m := getArgMap(req)
	if val, ok := m["include_smells"]; ok {
		if b, ok := val.(bool); ok {
			includeSmells = b
		}
	}
	if val, ok := m["include_components"]; ok {
		if b, ok := val.(bool); ok {
			includeComponents = b
		}
	}

	cfg := config.DefaultIntelligenceConfig()
	opts := []arch_intelligence.EngineOption{
		arch_intelligence.WithConfig(cfg),
	}

	engine := arch_intelligence.NewEngineWithOptions(graph, opts...)
	res := engine.Run()

	response := map[string]any{
		"metrics":  res.Metrics,
		"patterns": res.Patterns,
	}

	if includeSmells {
		response["smells"] = res.Smells
	}
	if includeComponents {
		response["components"] = res.Components
		response["component_coupling"] = res.ComponentCoupling
	}

	out, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to serialize patterns report: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func (s *Server) handleArchStatsTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	graph, err := s.bridge.Snapshot()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("AKG database unavailable: %v — run 'gmb analyze' first", err)), nil
	}

	cfg := config.DefaultIntelligenceConfig()
	opts := []arch_intelligence.EngineOption{
		arch_intelligence.WithConfig(cfg),
	}

	engine := arch_intelligence.NewEngineWithOptions(graph, opts...)
	res := engine.Run()

	stats := map[string]any{
		"metrics":            res.Metrics,
		"component_coupling": res.ComponentCoupling,
		"patterns":           res.Patterns,
		"smells":             res.Smells,
	}

	out, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to serialize architecture stats: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}
