package prompts

import (
	"fmt"
	"strings"
)

// Definition describes a builtin GlassMarble prompt (Master Plan §8).
type Definition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Arguments   []Argument  `json:"arguments,omitempty"`
	Template    string      `json:"-"`
}

// Argument describes a prompt argument.
type Argument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// List returns the six builtin prompts from Master Plan §8.
func List() []Definition {
	return []Definition{
		{
			Name:        "explain_architecture",
			Description: "Comprehensive architecture explanation",
			Arguments:   []Argument{{Name: "focus", Description: "Optional subsystem or topic focus"}},
			Template:    "Explain the architecture of this repository. Focus: {{focus}}\n1. Call gmb_render_diagram C4_CONTAINER\n2. Call gmb_patterns_smells\n3. Call gmb_memory_query focus={{focus}}\n4. Summarize high-level architecture, components, and decisions.",
		},
		{
			Name:        "analyze_impact",
			Description: "Blast radius of a proposed change",
			Arguments:   []Argument{{Name: "symbol", Description: "Symbol ID or file path", Required: true}},
			Template:    "Analyze blast radius of modifying {{symbol}}.\n1. Call gmb_impact_analysis target={{symbol}}\n2. Call akg_impact_radius id={{symbol}}\n3. Summarize risk score, dependents, and tests.",
		},
		{
			Name:        "find_technical_debt",
			Description: "Identify smells, dead code, God objects",
			Template:    "Find technical debt:\n1. Call gmb_patterns_smells include_smells=true\n2. Call akg_god_objects\n3. Call akg_orphans\n4. Prioritize remediation roadmap.",
		},
		{
			Name:        "explain_component",
			Description: "Deep dive into a component",
			Arguments:   []Argument{{Name: "component", Description: "Component name", Required: true}},
			Template:    "Explain component {{component}}.\n1. Call gmb_memory_component component={{component}}\n2. Call gmb_dependency_analysis target={{component}}\n3. Call gmb_render_diagram COMPONENT_GRAPH folder:{{component}}\n4. Provide responsibilities, coupling, history.",
		},
		{
			Name:        "generate_diagram",
			Description: "Generate a specific diagram type",
			Arguments:   []Argument{{Name: "diagram_type", Description: "Diagram type e.g. C4_CONTAINER", Required: true}},
			Template:    "Generate diagram {{diagram_type}}.\n1. Call gmb_render_diagram type={{diagram_type}} format=mermaid\n2. Present Mermaid diagram and explain subsystems.",
		},
		{
			Name:        "ci_gate_check",
			Description: "All governance checks for CI",
			Template:    "Run all architecture governance checks:\n1. Call gmb_drift_check\n2. Call gmb_arch_lint\n3. Call gmb_patterns_smells include_smells=true\n4. Summarize PASS/FAIL with failures listed.",
		},
	}
}

// Get returns the Definition for name, or an error if not found.
func Get(name string) (Definition, error) {
	for _, d := range List() {
		if d.Name == name {
			return d, nil
		}
	}
	return Definition{}, fmt.Errorf("prompt %q not found", name)
}

// Render substitutes {{key}} placeholders in the template with args values.
// Unknown placeholders are left as-is; required-arg validation is caller-side.
func Render(name string, args map[string]string) (string, error) {
	def, err := Get(name)
	if err != nil {
		return "", err
	}
	// Validate required args
	for _, a := range def.Arguments {
		if a.Required {
			if strings.TrimSpace(args[a.Name]) == "" {
				return "", fmt.Errorf("missing required argument %q for prompt %q", a.Name, name)
			}
		}
	}
	out := def.Template
	for k, v := range args {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	// Default empty focus to "whole system"
	out = strings.ReplaceAll(out, "{{focus}}", args["focus"])
	return out, nil
}

// Validate checks that all required args are present for a prompt.
func Validate(name string, args map[string]string) error {
	def, err := Get(name)
	if err != nil {
		return err
	}
	for _, a := range def.Arguments {
		if a.Required && strings.TrimSpace(args[a.Name]) == "" {
			return fmt.Errorf("missing required argument %q", a.Name)
		}
	}
	return nil
}
