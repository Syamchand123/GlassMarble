// Package renderer — archetypes.go
// Implements the 10 built-in document archetypes from Section 7.2 of the Master Plan.
// Each archetype pre-populates a complete DocSpec with industry-standard sections,
// default diagrams, and grounding directives.
package renderer

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// ArchetypeNames lists all 10 supported built-in document archetypes.
var ArchetypeNames = []string{
	"architecture",
	"module",
	"runbook",
	"onboarding",
	"migration",
	"adr",
	"api",
	"security",
	"database",
	"config",
}

// GetArchetype returns a pre-configured template DocSpec for the named archetype.
// Returns (spec, true) if found, or (DocSpec{}, false) if unknown.
func GetArchetype(name string) (config.DocSpec, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "architecture":
		return config.DocSpec{
			Archetype: "architecture",
			Title:     "System Architecture & Data Flow",
			Purpose:   "Complete guide to internal system architecture, subsystems, and component data flow",
			Audience:  "Senior contributors, maintainers, and system architects",
			Mode:      config.ModeManagedSections,
			Diagrams: []config.DiagramRef{
				{Type: "c4container"},
				{Type: "layered"},
				{Type: "dependency", Scope: "global"},
			},
			Sections: []config.SectionSpec{
				{
					ID:          "overview",
					Title:       "System Overview",
					Instruction: "Describe the high-level system architecture, entry points, and component topology.",
					GroundWith:  []string{"arch_intelligence", "components"},
					Managed:     true,
				},
				{
					ID:          "components",
					Title:       "Core Subsystems",
					Instruction: "Detail the primary subsystems, their responsibilities, and directory mappings.",
					GroundWith:  []string{"components", "arch_intelligence"},
					Managed:     true,
				},
				{
					ID:          "data-flow",
					Title:       "Data Flow & Request Lifecycle",
					Instruction: "Explain request processing, call sequences, and event dispatch pipelines.",
					GroundWith:  []string{"callgraph", "c4container"},
					Managed:     true,
				},
				{
					ID:          "storage",
					Title:       "Storage & Persistence Model",
					Instruction: "Explain the database/storage contract, atomic writes, and state consistency guarantees.",
					GroundWith:  []string{"signatures", "callgraph", "comments"},
					Managed:     true,
				},
				{
					ID:          "security-boundary",
					Title:       "Security Boundaries",
					Instruction: "Outline trust zones, authentication checkpoints, and boundary validations.",
					GroundWith:  []string{"ingress_points", "egress_calls", "crypto_primitives"},
					Managed:     true,
				},
			},
		}, true

	case "module":
		return config.DocSpec{
			Archetype: "module",
			Title:     "Module & Package Reference",
			Purpose:   "Technical reference for package exported surface, concurrency contracts, and error modes",
			Audience:  "Developers integrating with or modifying this package",
			Mode:      config.ModeFullOwned,
			Diagrams: []config.DiagramRef{
				{Type: "callgraph"},
				{Type: "dependency"},
			},
			Sections: []config.SectionSpec{
				{
					ID:          "interface",
					Title:       "Exported Interface & Types",
					Instruction: "Table of exported types, interfaces, functions, and public contracts.",
					GroundWith:  []string{"exported_symbols", "comments", "callgraph"},
					Managed:     true,
				},
				{
					ID:          "concurrency",
					Title:       "Concurrency & Thread Safety",
					Instruction: "Document mutex locks, goroutine lifecycles, and synchronization invariants.",
					GroundWith:  []string{"concurrency_primitives", "signatures"},
					Managed:     true,
				},
				{
					ID:          "errors",
					Title:       "Error Catalog & Sentinels",
					Instruction: "Table of sentinel errors with trigger conditions and caller context.",
					GroundWith:  []string{"error_returns", "sentinels"},
					Managed:     true,
				},
				{
					ID:          "callers",
					Title:       "Integration & Callers",
					Instruction: "Document key upstream callers and usage patterns for this module.",
					GroundWith:  []string{"callgraph", "callers"},
					Managed:     true,
				},
			},
		}, true

	case "runbook":
		return config.DocSpec{
			Archetype: "runbook",
			Title:     "Production Operations Runbook",
			Purpose:   "Operational guide for diagnosing, mitigating, and resolving production incidents",
			Audience:  "On-call SRE engineers and DevOps operators at 2 AM",
			Mode:      config.ModeManagedSections,
			Diagrams: []config.DiagramRef{
				{Type: "dependency", Scope: "global"},
			},
			Sections: []config.SectionSpec{
				{
					ID:          "health-checks",
					Title:       "Health Check Commands",
					Instruction: "List CLI diagnostic commands with exact flags and expected outputs.",
					GroundWith:  []string{"exported_symbols", "signatures"},
					Managed:     true,
				},
				{
					ID:          "error-triage",
					Title:       "Error Triage Reference",
					Instruction: "For each sentinel error: trigger condition, root cause, and remediation steps.",
					GroundWith:  []string{"sentinels", "error_returns", "callers"},
					Managed:     true,
				},
				{
					ID:          "dependencies",
					Title:       "Service Dependencies",
					Instruction: "Enumerate internal and external service dependencies and fallback behaviors.",
					GroundWith:  []string{"components", "callgraph"},
					Managed:     true,
				},
				{
					ID:          "config-matrix",
					Title:       "Runtime Configuration Matrix",
					Instruction: "Catalog environment variables and operational knobs governing behavior.",
					GroundWith:  []string{"config_vars", "signatures"},
					Managed:     true,
				},
			},
		}, true

	case "onboarding":
		return config.DocSpec{
			Archetype: "onboarding",
			Title:     "Engineering Onboarding Tour",
			Purpose:   "Codebase tour and architectural walkthrough for new team members",
			Audience:  "New engineering hires and first-time contributors",
			Mode:      config.ModeManagedSections,
			Diagrams: []config.DiagramRef{
				{Type: "layered"},
			},
			Sections: []config.SectionSpec{
				{
					ID:          "system-map",
					Title:       "System Map & Layout",
					Instruction: "Walk through repository layout, key directories, and architectural layers.",
					GroundWith:  []string{"components", "arch_intelligence"},
					Managed:     true,
				},
				{
					ID:          "request-lifecycle",
					Title:       "Request Lifecycle",
					Instruction: "Follow an end-to-end operation from CLI/API entry to persistence.",
					GroundWith:  []string{"callgraph"},
					Managed:     true,
				},
				{
					ID:          "key-packages",
					Title:       "Key Packages",
					Instruction: "Overview of core packages and their primary responsibilities.",
					GroundWith:  []string{"components"},
					Managed:     true,
				},
				{
					ID:          "where-to-start",
					Title:       "Where to Start & Contribution",
					Instruction: "Explain development environment setup, running tests, and code conventions.",
					GroundWith:  []string{"arch_intelligence"},
					Managed:     true,
				},
			},
		}, true

	case "migration":
		return config.DocSpec{
			Archetype: "migration",
			Title:     "Version Migration Guide",
			Purpose:   "Upgrade instructions detailing breaking changes, API deprecations, and schema shifts",
			Audience:  "Engineers upgrading between major or minor versions",
			Mode:      config.ModeManagedSections,
			Sections: []config.SectionSpec{
				{
					ID:          "breaking-changes",
					Title:       "Breaking Changes",
					Instruction: "Document removed or modified symbols and behavioral incompatibilities.",
					GroundWith:  []string{"symbol_deltas"},
					Managed:     true,
				},
				{
					ID:          "renamed-identifiers",
					Title:       "Renamed & Relocated Identifiers",
					Instruction: "List old identifiers and their replacement names and packages.",
					GroundWith:  []string{"symbol_deltas"},
					Managed:     true,
				},
				{
					ID:          "config-changes",
					Title:       "Configuration Changes",
					Instruction: "Highlight added, renamed, or obsolete environment variables and flags.",
					GroundWith:  []string{"config_var_changes", "config_vars"},
					Managed:     true,
				},
				{
					ID:          "code-examples",
					Title:       "Code Upgrade Patterns",
					Instruction: "Provide before-and-after code translation snippets for common workflows.",
					GroundWith:  []string{"symbol_deltas"},
					Managed:     true,
				},
			},
		}, true

	case "adr":
		return config.DocSpec{
			Archetype: "adr",
			Title:     "Architectural Decision Record",
			Purpose:   "Formal architectural decision record capturing problem, options, and chosen path",
			Audience:  "Architecture reviewers, security auditors, and future maintainers",
			Mode:      config.ModeManagedSections,
			Sections: []config.SectionSpec{
				{
					ID:          "context",
					Title:       "Context & Problem Statement",
					Instruction: "State the architectural context, constraints, and business driver.",
					GroundWith:  []string{"arch_events", "timeline"},
					Managed:     true,
				},
				{
					ID:          "decision",
					Title:       "Decision & Chosen Approach",
					Instruction: "Document the selected architecture design and justification.",
					GroundWith:  []string{"commit_reasoning", "arch_events"},
					Managed:     true,
				},
				{
					ID:          "consequences",
					Title:       "Consequences & Ripple Effects",
					Instruction: "List positive impacts, operational trade-offs, and technical debt risks.",
					GroundWith:  []string{"arch_intelligence"},
					Managed:     true,
				},
				{
					ID:          "alternatives",
					Title:       "Rejected Alternatives",
					Instruction: "Summarize alternative architectures considered and rationale for rejection.",
					GroundWith:  []string{"arch_events"},
					Managed:     true,
				},
			},
		}, true

	case "api":
		return config.DocSpec{
			Archetype: "api",
			Title:     "API Surface Reference",
			Purpose:   "Complete reference of external API endpoints, RPC procedures, schemas, and status codes",
			Audience:  "Frontend engineers, API consumers, and SDK developers",
			Mode:      config.ModeFullOwned,
			Sections: []config.SectionSpec{
				{
					ID:          "endpoints",
					Title:       "Endpoints & Route Handlers",
					Instruction: "Table of route paths, HTTP methods, handler functions, and descriptions.",
					GroundWith:  []string{"http_handlers", "exported_symbols"},
					Managed:     true,
				},
				{
					ID:          "schemas",
					Title:       "Request & Response Schemas",
					Instruction: "Document payload structures, required fields, and type contracts.",
					GroundWith:  []string{"exported_symbols", "signatures"},
					Managed:     true,
				},
				{
					ID:          "auth",
					Title:       "Authentication & Access Control",
					Instruction: "Detail token verification, required headers, and role permissions.",
					GroundWith:  []string{"exported_symbols", "signatures"},
					Managed:     true,
				},
				{
					ID:          "error-codes",
					Title:       "Error Responses & Codes",
					Instruction: "Catalog error payloads, status codes, and recovery suggestions.",
					GroundWith:  []string{"sentinels", "error_returns"},
					Managed:     true,
				},
			},
		}, true

	case "security":
		return config.DocSpec{
			Archetype: "security",
			Title:     "Security Architecture & Threat Model",
			Purpose:   "Threat model and security posture analysis for compliance and security reviews",
			Audience:  "Security engineers, penetration testers, and compliance auditors",
			Mode:      config.ModeManagedSections,
			Sections: []config.SectionSpec{
				{
					ID:          "trust-boundaries",
					Title:       "Trust Boundaries & Isolation",
					Instruction: "Map system trust zones, network perimeter boundaries, and process boundaries.",
					GroundWith:  []string{"ingress_points", "arch_intelligence"},
					Managed:     true,
				},
				{
					ID:          "data-flow",
					Title:       "Data Flow & Secret Handling",
					Instruction: "Trace sensitive data movement, sanitization rules, and encryption points.",
					GroundWith:  []string{"egress_calls", "callgraph"},
					Managed:     true,
				},
				{
					ID:          "cryptography",
					Title:       "Cryptography & Key Management",
					Instruction: "Document hashing algorithms, cipher modes, and key rotation policies.",
					GroundWith:  []string{"crypto_primitives"},
					Managed:     true,
				},
				{
					ID:          "cve-exposure",
					Title:       "Attack Surface & External Ingress",
					Instruction: "Analyze third-party dependencies, exposed ports, and input validation sinks.",
					GroundWith:  []string{"components"},
					Managed:     true,
				},
			},
		}, true

	case "database":
		return config.DocSpec{
			Archetype: "database",
			Title:     "Database Schema & Entity Relationship",
			Purpose:   "Complete database schema reference, entity relations, indexes, and migrations",
			Audience:  "Backend engineers, DBA operators, and data analysts",
			Mode:      config.ModeFullOwned,
			Diagrams: []config.DiagramRef{
				{Type: "er"},
			},
			Sections: []config.SectionSpec{
				{
					ID:          "tables",
					Title:       "Tables & Entity Models",
					Instruction: "Document table names, columns, data types, and primary keys.",
					GroundWith:  []string{"db_schemas"},
					Managed:     true,
				},
				{
					ID:          "relationships",
					Title:       "Relationships & Cardinality",
					Instruction: "Map foreign keys, parent-child relations, and cascade behaviors.",
					GroundWith:  []string{"db_schemas"},
					Managed:     true,
				},
				{
					ID:          "indexes",
					Title:       "Indexes & Performance Constraints",
					Instruction: "Catalog composite indexes, unique constraints, and query access patterns.",
					GroundWith:  []string{"db_schemas"},
					Managed:     true,
				},
				{
					ID:          "migration-history",
					Title:       "Migration History & Evolution",
					Instruction: "Summarize database migration sequence and schema versions.",
					GroundWith:  []string{"migration_files"},
					Managed:     true,
				},
			},
		}, true

	case "config":
		return config.DocSpec{
			Archetype: "config",
			Title:     "Configuration & Environment Reference",
			Purpose:   "Comprehensive catalog of environment variables, configuration flags, and defaults",
			Audience:  "DevOps engineers, deployment automation, and developers",
			Mode:      config.ModeFullOwned,
			Sections: []config.SectionSpec{
				{
					ID:          "all-variables",
					Title:       "All Configuration Knobs",
					Instruction: "Table of all environment variables, flags, and configuration keys.",
					GroundWith:  []string{"config_vars", "env_getenv"},
					Managed:     true,
				},
				{
					ID:          "required-optional",
					Title:       "Required vs Optional Settings",
					Instruction: "Distinguish strictly required variables from those with defaults.",
					GroundWith:  []string{"config_vars"},
					Managed:     true,
				},
				{
					ID:          "defaults",
					Title:       "Default Values & Types",
					Instruction: "Detail default values, valid ranges, and expected value formats.",
					GroundWith:  []string{"config_vars"},
					Managed:     true,
				},
				{
					ID:          "file-source",
					Title:       "Configuration Source Code Sites",
					Instruction: "List source files and line locations where configuration is read.",
					GroundWith:  []string{"config_vars"},
					Managed:     true,
				},
			},
		}, true
	}

	return config.DocSpec{}, false
}

// ApplyArchetype populates default sections, title, purpose, audience, and diagrams
// from the named archetype if not already specified in spec.
func ApplyArchetype(spec *config.DocSpec) error {
	if spec.Archetype == "" {
		return nil
	}
	arch, ok := GetArchetype(spec.Archetype)
	if !ok {
		return fmt.Errorf("doc_engine/renderer: unknown archetype %q (valid: %s)",
			spec.Archetype, strings.Join(ArchetypeNames, ", "))
	}

	if spec.Title == "" {
		spec.Title = arch.Title
	}
	if spec.Purpose == "" {
		spec.Purpose = arch.Purpose
	}
	if spec.Audience == "" {
		spec.Audience = arch.Audience
	}
	if spec.Mode == "" {
		spec.Mode = arch.Mode
	}
	if len(spec.Diagrams) == 0 {
		spec.Diagrams = arch.Diagrams
	}
	if len(spec.Sections) == 0 {
		spec.Sections = arch.Sections
	}

	return nil
}
