package handlers

import (
	"fmt"
	"strings"
)

// Diagram tool names (Master Plan §6 diagram category).
const (
	ToolDiagramGenerate = "diagram_generate"
	ToolDiagramTypes    = "diagram_types"
	ToolRenderDiagram   = "gmb_render_diagram"
	ToolListDiagTypes   = "gmb_list_diagram_types"
)

// DiagramToolNames returns the diagram tool set.
func DiagramToolNames() []string {
	return []string{ToolDiagramGenerate, ToolDiagramTypes, ToolRenderDiagram, ToolListDiagTypes}
}

// KnownDiagramTypes mirrors the 31 supported diagram types (subset for validation stub).
var KnownDiagramTypes = []string{
	"C4_CONTEXT", "C4_CONTAINER", "C4_COMPONENT",
	"UML_CLASS", "UML_SEQUENCE", "DEPENDENCY_GRAPH",
	"CALL_GRAPH", "COMPONENT_GRAPH", "ER_DIAGRAM",
	"DATA_FLOW", "MINDMAP",
}

// DiagramArgs holds validated args for diagram_generate / gmb_render_diagram.
type DiagramArgs struct {
	Type      string
	Format    string
	Scope     string
	Entry     string
	Depth     int
	Pagerank  bool
	Community bool
	SCC       bool
}

// ValidateDiagramArgs validates diagram generation args.
func ValidateDiagramArgs(args map[string]any) (DiagramArgs, error) {
	t, _ := args["type"].(string)
	t = strings.TrimSpace(t)
	if t == "" {
		// Also check uppercase variant "diagram_type" used by some clients
		t, _ = args["diagram_type"].(string)
		t = strings.TrimSpace(t)
	}
	if t == "" {
		return DiagramArgs{}, fmt.Errorf("missing required parameter \"type\"")
	}
	t = strings.ToUpper(t)
	format, _ := args["format"].(string)
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "mermaid"
	}
	if format != "mermaid" && format != "plantuml" && format != "dot" {
		return DiagramArgs{}, fmt.Errorf("unsupported format %q (must be mermaid|plantuml|dot)", format)
	}
	scope, _ := args["scope"].(string)
	entry, _ := args["entry"].(string)
	depth := 0
	if v, ok := args["depth"]; ok {
		switch n := v.(type) {
		case float64:
			depth = int(n)
		case int:
			depth = n
		}
		if depth < 0 {
			depth = 0
		}
		if depth > 10 {
			depth = 10
		}
	}
	pagerank, _ := args["pagerank"].(bool)
	community, _ := args["community"].(bool)
	scc, _ := args["scc"].(bool)
	return DiagramArgs{
		Type:      t,
		Format:    format,
		Scope:     strings.TrimSpace(scope),
		Entry:     strings.TrimSpace(entry),
		Depth:     depth,
		Pagerank:  pagerank,
		Community: community,
		SCC:       scc,
	}, nil
}

// IsKnownDiagramType reports whether t is a known diagram family.
func IsKnownDiagramType(t string) bool {
	upper := strings.ToUpper(strings.TrimSpace(t))
	for _, k := range KnownDiagramTypes {
		if upper == k {
			return true
		}
	}
	return false
}
