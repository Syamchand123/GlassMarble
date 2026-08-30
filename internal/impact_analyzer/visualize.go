package impact_analyzer

import (
	"fmt"
	"strings"
)

// RenderMermaidImpact generates a Mermaid flowchart highlighting the blast radius.
func RenderMermaidImpact(rep *ImpactReport) string {
	if rep == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("flowchart BT\n")
	sb.WriteString("  %% GlassMarble Refactoring Blast-Radius Diagram\n\n")

	// Styles
	sb.WriteString("  classDef target fill:#ef4444,stroke:#991b1b,stroke-width:3px,color:#ffffff,font-weight:bold;\n")
	sb.WriteString("  classDef direct fill:#f97316,stroke:#c2410c,stroke-width:2px,color:#ffffff;\n")
	sb.WriteString("  classDef transitive fill:#6366f1,stroke:#4338ca,stroke-width:1px,color:#ffffff;\n")
	sb.WriteString("  classDef test fill:#a855f7,stroke:#7e22ce,stroke-width:1px,color:#ffffff;\n")
	sb.WriteString("  classDef entry fill:#10b981,stroke:#047857,stroke-width:2px,color:#ffffff;\n\n")

	// Target Node
	targetID := sanitizeMermaidID(rep.TargetNodeID)
	sb.WriteString(fmt.Sprintf("  %s[\"🎯 %s (%s)\"]:::target\n", targetID, rep.TargetName, rep.TargetKind))

	// Direct Dependents
	for _, d := range rep.DirectDependents {
		nodeID := sanitizeMermaidID(d.ID)
		cls := "direct"
		prefix := "⚡"
		if d.IsTest {
			cls = "test"
			prefix = "🧪"
		} else if d.IsEntry {
			cls = "entry"
			prefix = "🚀"
		}
		sb.WriteString(fmt.Sprintf("  %s[\"%s %s\"]:::%s\n", nodeID, prefix, d.Name, cls))
		sb.WriteString(fmt.Sprintf("  %s -->|%s| %s\n", nodeID, d.EdgeType, targetID))
	}

	// Transitive Dependents (limit to 15 to keep diagram legible)
	limit := 15
	if len(rep.TransitiveDependents) < limit {
		limit = len(rep.TransitiveDependents)
	}
	for i := 0; i < limit; i++ {
		t := rep.TransitiveDependents[i]
		nodeID := sanitizeMermaidID(t.ID)
		cls := "transitive"
		prefix := "↳"
		if t.IsTest {
			cls = "test"
			prefix = "🧪"
		} else if t.IsEntry {
			cls = "entry"
			prefix = "🚀"
		}
		sb.WriteString(fmt.Sprintf("  %s[\"%s %s\"]:::%s\n", nodeID, prefix, t.Name, cls))
	}

	return sb.String()
}

func sanitizeMermaidID(id string) string {
	r := strings.NewReplacer(
		"::", "_",
		"/", "_",
		".", "_",
		"-", "_",
		" ", "_",
		"\\", "_",
	)
	clean := r.Replace(id)
	if clean == "" {
		return "node"
	}
	return "n_" + clean
}
