package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerPrompts binds reusable architecture analysis prompt workflows to the MCP server.
func (s *Server) registerPrompts() {
	// 1. Pre-Commit / CI Gate Checks (ci_gate_check & gmb_pre_commit_audit)
	s.MCPServer().AddPrompt(
		mcp.NewPrompt(
			"ci_gate_check",
			mcp.WithPromptDescription("Run all architecture governance checks (drift, lint, smells) for CI/CD gates"),
			mcp.WithArgument("scope", mcp.ArgumentDescription("Optional path or component scope")),
		),
		s.handlePreCommitAuditPrompt,
	)
	s.MCPServer().AddPrompt(
		mcp.NewPrompt(
			"gmb_pre_commit_audit",
			mcp.WithPromptDescription("Evaluate current uncommitted changes for architecture drift, layer violations, and blast-radius risk"),
			mcp.WithArgument("scope", mcp.ArgumentDescription("Optional path or component scope")),
		),
		s.handlePreCommitAuditPrompt,
	)

	// 2. Impact Analysis / Refactoring Advisor (analyze_impact & gmb_refactor_advisor)
	s.MCPServer().AddPrompt(
		mcp.NewPrompt(
			"analyze_impact",
			mcp.WithPromptDescription("Compute blast radius and architectural risk for a proposed change"),
			mcp.WithArgument("symbol", mcp.RequiredArgument(), mcp.ArgumentDescription("Symbol ID or file path")),
		),
		s.handleAnalyzeImpactPrompt,
	)
	s.MCPServer().AddPrompt(
		mcp.NewPrompt(
			"gmb_refactor_advisor",
			mcp.WithPromptDescription("Analyze a symbol or file and formulate a step-by-step safe architectural refactoring plan"),
			mcp.WithArgument("target", mcp.RequiredArgument(), mcp.ArgumentDescription("Symbol ID or file path to refactor")),
		),
		s.handleRefactorAdvisorPrompt,
	)

	// 3. Architecture Explanation (explain_architecture & gmb_explain_architecture)
	s.MCPServer().AddPrompt(
		mcp.NewPrompt(
			"explain_architecture",
			mcp.WithPromptDescription("Comprehensive architecture walkthrough with C4 diagrams, invariants, and decisions"),
			mcp.WithArgument("focus", mcp.ArgumentDescription("Optional subsystem or topic focus")),
		),
		s.handleExplainArchitecturePrompt,
	)
	s.MCPServer().AddPrompt(
		mcp.NewPrompt(
			"gmb_explain_architecture",
			mcp.WithPromptDescription("Generate an architectural deep dive with diagrams, patterns, and decisions for the repository or a component"),
			mcp.WithArgument("component", mcp.ArgumentDescription("Optional component name (leave empty for whole system)")),
		),
		s.handleExplainArchitecturePrompt,
	)

	// 4. Onboarding Guide (onboard_developer & gmb_onboarding_guide)
	s.MCPServer().AddPrompt(
		mcp.NewPrompt(
			"onboard_developer",
			mcp.WithPromptDescription("Produce a comprehensive architectural onboarding guide for a new engineer"),
		),
		s.handleOnboardingGuidePrompt,
	)
	s.MCPServer().AddPrompt(
		mcp.NewPrompt(
			"gmb_onboarding_guide",
			mcp.WithPromptDescription("Produce a comprehensive architectural onboarding walk-through for a new engineer joining the project"),
		),
		s.handleOnboardingGuidePrompt,
	)

	// 5. Technical Debt Finder (find_technical_debt)
	s.MCPServer().AddPrompt(
		mcp.NewPrompt(
			"find_technical_debt",
			mcp.WithPromptDescription("Identify high-severity architectural smells, dead code, and God objects"),
		),
		s.handleTechnicalDebtPrompt,
	)

	// 6. Component Deep Dive (explain_component)
	s.MCPServer().AddPrompt(
		mcp.NewPrompt(
			"explain_component",
			mcp.WithPromptDescription("Deep dive into a specific component's responsibilities, coupling, and history"),
			mcp.WithArgument("component", mcp.RequiredArgument(), mcp.ArgumentDescription("Component name or ID")),
		),
		s.handleExplainComponentPrompt,
	)

	// 7. Diagram Generation Prompt (generate_diagram)
	s.MCPServer().AddPrompt(
		mcp.NewPrompt(
			"generate_diagram",
			mcp.WithPromptDescription("Generate and explain a specific architectural diagram"),
			mcp.WithArgument("diagram_type", mcp.RequiredArgument(), mcp.ArgumentDescription("Diagram type: C4_CONTEXT, C4_CONTAINER, UML_CLASS, DEPENDENCY_GRAPH, etc.")),
		),
		s.handleGenerateDiagramPrompt,
	)
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

func (s *Server) handleAnalyzeImpactPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	symbol := ""
	if req.Params.Arguments != nil {
		symbol = req.Params.Arguments["symbol"]
		if symbol == "" {
			symbol = req.Params.Arguments["target"]
		}
	}
	if strings.TrimSpace(symbol) == "" {
		return nil, fmt.Errorf("missing required argument 'symbol'")
	}

	promptText := fmt.Sprintf(`You are analyzing the architectural blast radius of modifying %q.

Please execute the following steps:
1. Call 'gmb_impact_analysis' with target=%q to calculate direct and transitive dependents.
2. Call 'akg_impact_radius' with id=%q for raw graph impact closure.
3. Call 'gmb_code_references' for %q to inspect actual usage sites.

Summarize:
- Blast Radius Risk Score & Level (LOW/MEDIUM/HIGH/CRITICAL)
- Direct & Transitive Dependents Breakdown
- Impacted Test Suites to Execute
- Potential Architectural Risks.`, symbol, symbol, symbol, symbol)

	return mcp.NewGetPromptResult(
		fmt.Sprintf("Impact Analysis for %s", symbol),
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(promptText)),
		},
	), nil
}

func (s *Server) handleRefactorAdvisorPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	target := ""
	if req.Params.Arguments != nil {
		target = req.Params.Arguments["target"]
		if target == "" {
			target = req.Params.Arguments["symbol"]
		}
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
		if component == "" {
			component = req.Params.Arguments["focus"]
		}
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

func (s *Server) handleTechnicalDebtPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	promptText := `You are conducting an Architectural Technical Debt Audit.

Please execute the following discovery using GlassMarble tools:
1. Call 'gmb_patterns_smells' with include_smells=true to find all architectural smells.
2. Call 'akg_god_objects' to locate god objects / classes with excessive coupling.
3. Call 'akg_orphans' to detect dead or unreachable code.
4. Call 'akg_cycles' to locate cyclic dependencies.

Synthesize an Actionable Technical Debt Report:
- Critical Architectural Smells (Cyclic deps, tight coupling, unstable abstractions)
- God Objects & Bloated Modules
- Dead / Orphaned Code Cleanups
- Prioritized Remediation Roadmap.`

	return mcp.NewGetPromptResult(
		"GlassMarble Technical Debt Audit",
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(promptText)),
		},
	), nil
}

func (s *Server) handleExplainComponentPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	component := ""
	if req.Params.Arguments != nil {
		component = req.Params.Arguments["component"]
	}
	if strings.TrimSpace(component) == "" {
		return nil, fmt.Errorf("missing required argument 'component'")
	}

	promptText := fmt.Sprintf(`You are explaining the %q component in detail.

Please execute the following steps:
1. Call 'gmb_memory_component' with component=%q to read longitudinal memory and history.
2. Call 'gmb_dependency_analysis' with target=%q to inspect inbound/outbound couplings.
3. Call 'gmb_render_diagram' with type="COMPONENT_GRAPH" and scope="folder:%s" to visualize structure.

Provide a detailed component breakdown:
- Core Responsibilities & Boundaries
- Inbound Callers & Outbound Dependencies
- Historical Evolutions & Recorded Knowledge Claims
- Key Invariants.`, component, component, component, component)

	return mcp.NewGetPromptResult(
		fmt.Sprintf("Component Deep Dive: %s", component),
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(promptText)),
		},
	), nil
}

func (s *Server) handleGenerateDiagramPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	diagType := "C4_CONTAINER"
	if req.Params.Arguments != nil && req.Params.Arguments["diagram_type"] != "" {
		diagType = req.Params.Arguments["diagram_type"]
	}

	promptText := fmt.Sprintf(`Please render and explain an architectural diagram of type %q.

1. Call 'gmb_render_diagram' with type=%q and format="mermaid".
2. Present the Mermaid diagram in a clean markdown code fence.
3. Explain the primary subsystems, relationships, and data flows illustrated by the diagram.`, diagType, diagType)

	return mcp.NewGetPromptResult(
		fmt.Sprintf("Architectural Diagram: %s", diagType),
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
