package handlers

import (
	"strings"
)

// Governance tool names (Master Plan §6 governance category).
const (
	ToolDriftCheck     = "gmb_drift_check"
	ToolArchLint       = "gmb_arch_lint"
	ToolPatternsSmells = "gmb_patterns_smells"
	ToolArchStats      = "gmb_arch_stats"
)

// GovernanceToolNames returns the governance tool set.
func GovernanceToolNames() []string {
	return []string{ToolDriftCheck, ToolArchLint, ToolPatternsSmells, ToolArchStats}
}

// DriftArgs holds validated args for gmb_drift_check.
type DriftArgs struct {
	ConfigFile string
}

// ValidateDriftArgs validates gmb_drift_check args.
func ValidateDriftArgs(args map[string]any) DriftArgs {
	cf, _ := args["config_file"].(string)
	return DriftArgs{ConfigFile: strings.TrimSpace(cf)}
}

// LintArgs holds validated args for gmb_arch_lint.
type LintArgs struct {
	RulesFile string
}

// ValidateLintArgs validates gmb_arch_lint args.
func ValidateLintArgs(args map[string]any) LintArgs {
	rf, _ := args["rules_file"].(string)
	return LintArgs{RulesFile: strings.TrimSpace(rf)}
}

// PatternsArgs holds validated args for gmb_patterns_smells.
type PatternsArgs struct {
	IncludeSmells     bool
	IncludeComponents bool
}

// ValidatePatternsArgs validates gmb_patterns_smells args, defaulting both to true.
func ValidatePatternsArgs(args map[string]any) PatternsArgs {
	out := PatternsArgs{IncludeSmells: true, IncludeComponents: true}
	if v, ok := args["include_smells"].(bool); ok {
		out.IncludeSmells = v
	}
	if v, ok := args["include_components"].(bool); ok {
		out.IncludeComponents = v
	}
	return out
}
