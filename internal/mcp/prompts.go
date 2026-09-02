package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerPrompts binds reusable architecture analysis prompt workflows to the MCP server.
func (s *Server) registerPrompts() {
	// 1. gmb_pre_commit_audit Prompt
	preCommitPrompt := mcp.NewPrompt(
		"gmb_pre_commit_audit",
		mcp.WithPromptDescription("Evaluate current uncommitted changes for architecture drift, layer violations, and blast-radius risk"),
		mcp.WithArgument("scope", mcp.ArgumentDescription("Optional path or component scope")),
	)
	s.MCPServer().AddPrompt(preCommitPrompt, s.handlePreCommitAuditPrompt)

	// 2. gmb_refactor_advisor Prompt
	refactorPrompt := mcp.NewPrompt(
		"gmb_refactor_advisor",
		mcp.WithPromptDescription("Analyze a symbol or file and formulate a step-by-step safe architectural refactoring plan"),
		mcp.WithArgument("target", mcp.RequiredArgument(), mcp.ArgumentDescription("Symbol ID or file path to refactor")),
	)
	s.MCPServer().AddPrompt(refactorPrompt, s.handleRefactorAdvisorPrompt)

	// 3. gmb_explain_architecture Prompt
	explainPrompt := mcp.NewPrompt(
		"gmb_explain_architecture",
		mcp.WithPromptDescription("Generate an architectural deep dive with diagrams, patterns, and decisions for the repository or a component"),
		mcp.WithArgument("component", mcp.ArgumentDescription("Optional component name (leave empty for whole system)")),
	)
	s.MCPServer().AddPrompt(explainPrompt, s.handleExplainArchitecturePrompt)

	// 4. gmb_onboarding_guide Prompt
	onboardingPrompt := mcp.NewPrompt(
		"gmb_onboarding_guide",
		mcp.WithPromptDescription("Produce a comprehensive architectural onboarding walk-through for a new engineer joining the project"),
	)
	s.MCPServer().AddPrompt(onboardingPrompt, s.handleOnboardingGuidePrompt)
}

func (s *Server) handlePreCommitAuditPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	scope := ""
	if req.Params.Arguments != nil {
		scope = req.Params.Arguments["scope"]
	}

	promptText := fmt.Sprintf(`You are conducting a strict GlassMarble Architectural Pre-Commit Audit.
Scope: %s

Please execute the following workflow using available GlassMarble MCP tools:
1. Call 'gmb_drift_check' to verify if any declared layering boundaries or cycle budgets are violated.
2. Call 'gmb_arch_lint' to check against declarative architecture rules.
3. If changes affect specific symbols or files, call 'gmb_impact_analysis' on those targets to calculate blast radius and impacted test suites.
4. Call 'gmb_patterns_smells' to identify any newly introduced code or structural smells.

Synthesize your findings into a crisp executive report:
- [PASS/FAIL] Architecture Drift Gate
- [PASS/FAIL] Rule Compliance
- Risk Score (0-100) and Blast Radius Summary
- Recommended tests to run before merging.`, defaultIfEmpty(scope, "whole repository"))

	return mcp.NewGetPromptResult(
		"GlassMarble Pre-Commit Architecture Audit",
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(promptText)),
		},
	), nil
}

func (s *Server) handleRefactorAdvisorPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	target := ""
	if req.Params.Arguments != nil {
		target = req.Params.Arguments["target"]
	}
	if strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("missing required argument 'target'")
	}

	promptText := fmt.Sprintf(`You are an expert Software Architect providing safe refactoring guidance.
Target: %s

Please execute the following steps using GlassMarble tools:
1. Call 'gmb_impact_analysis' with target="%s" to find all direct and transitive upstream dependents.
2. Call 'gmb_code_references' for "%s" to inspect actual call sites and usage patterns.
3. Call 'gmb_inspect_node' and 'gmb_code_definition' to review implementation details and properties.
4. Call 'gmb_arch_stats' to understand component coupling metrics (Ca, Ce, Instability).

Formulate a safe, incremental refactoring plan:
- Current Structural Assessment (coupling, smells, risk level)
- Inbound Dependents & Blast Radius Summary
- Step-by-Step Migration Strategy (preserve backward compatibility, extract interfaces, migrate callers)
- Regression Verification Plan (affected test suites).`, target, target, target)

	return mcp.NewGetPromptResult(
		fmt.Sprintf("Refactoring Plan for %s", target),
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(promptText)),
		},
	), nil
}

func (s *Server) handleExplainArchitecturePrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	component := ""
	if req.Params.Arguments != nil {
		component = req.Params.Arguments["component"]
	}

	promptText := fmt.Sprintf(`You are explaining the software architecture of this project.
Focus: %s

Please execute the following discovery using GlassMarble tools:
1. Call 'gmb_render_diagram' with type="C4_CONTAINER" (or "COMPONENT_GRAPH") to obtain the high-level architecture diagram.
2. Call 'gmb_patterns_smells' to examine detected architectural patterns and component boundaries.
3. Call 'gmb_memory_query' with query="%s" to retrieve recorded design rationale, decisions, and knowledge claims.
4. Call 'gmb_arch_timeline' to understand historical architectural milestones.

Provide a clear, structured architecture walkthrough:
- High-Level Architecture & Responsibilities (include Mermaid diagram)
- Key Components & Data Flow
- Invariants & Architectural Patterns
- Design Decisions & Historical Context.`, defaultIfEmpty(component, "Whole System"), defaultIfEmpty(component, "architecture"))

	return mcp.NewGetPromptResult(
		"GlassMarble Architecture Explanation",
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(promptText)),
		},
	), nil
}

func (s *Server) handleOnboardingGuidePrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	promptText := `You are creating an Architecture Onboarding Guide for a new software engineer joining the team.

Please execute the following discovery using GlassMarble tools:
1. Call 'gmb_server_info' and 'gmb_status' for repository health and graph statistics.
2. Call 'gmb_render_diagram' with type="C4_CONTEXT" and type="C4_CONTAINER" for top-level system topology.
3. Call 'akg_entrypoints' to locate primary application entry points and CLI commands.
4. Call 'gmb_hotspot_rankings' to identify critical, highly-connected core modules.
5. Call 'gmb_arch_timeline' to review recent major architectural refactorings.

Generate a structured Onboarding Guide:
1. Executive System Overview & Mental Model (with Mermaid diagrams)
2. Entry Points & Request Lifecycle
3. Core Hotspots & Key Subsystems
4. Architectural Invariants (Layering, Boundaries, Rules)
5. Recommended First Steps & Code Navigation Tour.`

	return mcp.NewGetPromptResult(
		"GlassMarble Developer Onboarding Guide",
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(promptText)),
		},
	), nil
}

func defaultIfEmpty(val, def string) string {
	if strings.TrimSpace(val) == "" {
		return def
	}
	return val
}
