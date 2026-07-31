package stage3

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func renderC4ContextDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("C4Context\n")
	title := getDiagramTitle(tree, "System Context Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))

	for _, node := range collectNodesByKind(tree, "gm:User") {
		tags := getNodeTags(node)
		sb.WriteString(fmt.Sprintf("    Person(%s, \"%s\", \"External Actor%s\")\n",
			sanitizeName(node.ID), sanitizeMermaidLabel(node.Name), tags))
	}

	for _, boundary := range tree.Children {
		if isSystemBoundary(boundary) {
			alias := sanitizeName(boundary.BoundaryName)
			sb.WriteString(fmt.Sprintf("    System(%s, \"%s\", \"System\")\n",
				alias, sanitizeMermaidLabel(boundary.BoundaryName)))
		}
	}

	for _, node := range collectNodesByKind(tree, "gm:ExternalSystem") {
		tags := getNodeTags(node)
		sb.WriteString(fmt.Sprintf("    SystemExt(%s, \"%s\", \"External System%s\")\n",
			sanitizeName(node.ID), sanitizeMermaidLabel(node.Name), tags))
	}

	for _, node := range collectNodesByPrimitive(tree, "DATABASE") {
		tags := getNodeTags(node)
		sb.WriteString(fmt.Sprintf("    SystemDb(%s, \"%s\", \"Database%s\")\n",
			sanitizeName(node.ID), sanitizeMermaidLabel(node.Name), tags))
	}

	renderC4Edges(tree, sb)
}

func renderC4ContainerDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("C4Container\n")
	title := getDiagramTitle(tree, "Container Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))

	for _, boundary := range tree.Children {
		alias := sanitizeName(boundary.BoundaryName)
		name := sanitizeMermaidLabel(boundary.BoundaryName)
		sb.WriteString(fmt.Sprintf("    System_Boundary(%s_sys, \"%s System\") {\n", alias, name))

		for _, subBoundary := range boundary.Children {
			subAlias := sanitizeName(subBoundary.BoundaryName)
			subName := sanitizeMermaidLabel(subBoundary.BoundaryName)
			tech := detectContainerTechnology(subBoundary)
			sb.WriteString(fmt.Sprintf("        Container(%s, \"%s\", \"%s\", \"%s\")\n",
				subAlias, subName, tech, getContainerDescription(subBoundary)))
		}

		for _, node := range boundary.Nodes {
			nodeAlias := sanitizeName(node.ID)
			nodeName := sanitizeMermaidLabel(node.Name)
			tech := detectNodeTechnology(node)
			if isDatabase(node) {
				sb.WriteString(fmt.Sprintf("        ContainerDb(%s, \"%s\", \"%s\", \"%s\")\n",
					nodeAlias, nodeName, tech, getNodeDescription(node)))
			} else {
				sb.WriteString(fmt.Sprintf("        Container(%s, \"%s\", \"%s\", \"%s\")\n",
					nodeAlias, nodeName, tech, getNodeDescription(node)))
			}
		}

		sb.WriteString("    }\n")
	}

	renderC4Edges(tree, sb)
}

func renderC4ComponentDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("C4Component\n")
	title := getDiagramTitle(tree, "C4 Component Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))

	var renderTree func(t *types.LayoutTree, indent string)
	renderTree = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}

		if t.BoundaryName != "Root" {
			boundaryAlias := sanitizeName(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%sContainer_Boundary(%s, \"%s\") {\n", indent, boundaryAlias, sanitizeMermaidLabel(t.BoundaryName)))
			indent += "    "
		}

		for _, node := range t.Nodes {
			nodeAlias := sanitizeName(node.ID)
			name := sanitizeMermaidLabel(node.Name)
			if name == "" {
				name = sanitizeMermaidLabel(node.ID)
			}
			kind := getShortKind(node.Kind)
			tech := node.PrimitiveType
			if tech == "" {
				tech = "Go/Core Component"
			}
			if node.Kind == "gm:Database" || strings.Contains(node.PrimitiveType, "DATABASE") {
				sb.WriteString(fmt.Sprintf("%sComponentDb(%s, \"%s\", \"%s\", \"Relational / NoSQL Data Store\")\n", indent, nodeAlias, name, tech))
			} else if strings.Contains(node.PrimitiveType, "CACHE") {
				sb.WriteString(fmt.Sprintf("%sComponentDb(%s, \"%s\", \"%s\", \"In-Memory Cache Store\")\n", indent, nodeAlias, name, tech))
			} else if strings.Contains(node.PrimitiveType, "MESSAGE_QUEUE") {
				sb.WriteString(fmt.Sprintf("%sComponentExt(%s, \"%s\", \"%s\", \"Async Message Queue / Event Bus\")\n", indent, nodeAlias, name, tech))
			} else if strings.Contains(node.PrimitiveType, "AI_LLM") {
				sb.WriteString(fmt.Sprintf("%sComponentExt(%s, \"%s\", \"%s\", \"AI / LLM Vector Service\")\n", indent, nodeAlias, name, tech))
			} else if node.Kind == "gm:ExternalSystem" || strings.Contains(node.PrimitiveType, "NETWORK_IO") || strings.Contains(node.PrimitiveType, "CLOUD_SDK") || strings.Contains(node.PrimitiveType, "RPC") {
				sb.WriteString(fmt.Sprintf("%sComponentExt(%s, \"%s\", \"%s\", \"External Cloud / Service Integration\")\n", indent, nodeAlias, name, tech))
			} else {
				sb.WriteString(fmt.Sprintf("%sComponent(%s, \"%s\", \"%s\", \"%s Architectural Component\")\n", indent, nodeAlias, name, tech, kind))
			}
		}

		for _, child := range t.Children {
			renderTree(child, indent)
		}

		if t.BoundaryName != "Root" {
			indent = indent[4:]
			sb.WriteString(fmt.Sprintf("%s}\n", indent))
		}
	}
	renderTree(tree, "    ")

	drawn := make(map[string]bool)
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		predLabel := sanitizeMermaidLabel(strings.TrimPrefix(edge.Predicate, "gm:"))

		relKey := srcAlias + "->" + tgtAlias
		if drawn[relKey] {
			continue
		}
		drawn[relKey] = true

		if edge.Weight > 1 {
			sb.WriteString(fmt.Sprintf("    Rel(%s, %s, \"%s (calls: %d)\")\n", srcAlias, tgtAlias, predLabel, edge.Weight))
		} else {
			sb.WriteString(fmt.Sprintf("    Rel(%s, %s, \"%s\")\n", srcAlias, tgtAlias, predLabel))
		}
	}
}

func renderC4CodeDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	renderClassDiagram(tree, sb)
}

func renderC4LandscapeDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("C4Context\n")
	title := getDiagramTitle(tree, "System Landscape Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))

	for _, boundary := range tree.Children {
		if !isSystemBoundary(boundary) {
			continue
		}
		alias := sanitizeName(boundary.BoundaryName)
		name := sanitizeMermaidLabel(boundary.BoundaryName)
		sb.WriteString(fmt.Sprintf("    Enterprise_Boundary(%s_ent, \"%s Enterprise\") {\n", alias, name))
		sb.WriteString(fmt.Sprintf("        System(%s, \"%s\", \"System Module\")\n", alias, name))
		for _, node := range boundary.Nodes {
			if node.Kind == "gm:ExternalSystem" {
				sb.WriteString(fmt.Sprintf("        SystemExt(%s, \"%s\", \"External System\")\n",
					sanitizeName(node.ID), sanitizeMermaidLabel(node.Name)))
			} else if isDatabase(node) {
				sb.WriteString(fmt.Sprintf("        SystemDb(%s, \"%s\", \"Database\")\n",
					sanitizeName(node.ID), sanitizeMermaidLabel(node.Name)))
			}
		}
		sb.WriteString("    }\n")
	}

	renderC4Edges(tree, sb)
}

func renderC4DynamicDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("C4Context\n")
	title := getDiagramTitle(tree, "Dynamic Flow Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))

	for _, node := range collectAllNodes(tree) {
		alias := sanitizeName(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		if name == "" {
			name = sanitizeName(node.ID)
		}
		sb.WriteString(fmt.Sprintf("    System(%s, \"%s\", \"Execution Node\")\n", alias, name))
	}

	for i, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		label := sanitizeMermaidLabel(shortPredicate(edge.Predicate))
		if label == "" {
			label = "calls"
		}
		sb.WriteString(fmt.Sprintf("    Rel(%s, %s, \"%d: %s\")\n", srcAlias, tgtAlias, i+1, label))
	}
}

func renderC4DeploymentDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("C4Container\n")
	title := getDiagramTitle(tree, "C4 Deployment Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))

	for _, boundary := range tree.Children {
		if !isSystemBoundary(boundary) {
			continue
		}
		alias := sanitizeName(boundary.BoundaryName)
		name := sanitizeMermaidLabel(boundary.BoundaryName)
		sb.WriteString(fmt.Sprintf("    Deployment_Node(%s_node, \"%s Node\", \"Deployment Environment\") {\n", alias, name))
		for _, node := range boundary.Nodes {
			nodeAlias := sanitizeName(node.ID)
			nodeName := sanitizeMermaidLabel(node.Name)
			if isDatabase(node) {
				sb.WriteString(fmt.Sprintf("        ContainerDb(%s, \"%s\", \"%s\", \"Data Store\")\n",
					nodeAlias, nodeName, detectNodeTechnology(node)))
			} else {
				sb.WriteString(fmt.Sprintf("        Container(%s, \"%s\", \"%s\", \"%s\")\n",
					nodeAlias, nodeName, detectNodeTechnology(node), getNodeDescription(node)))
			}
		}
		sb.WriteString("    }\n")
	}

	renderC4Edges(tree, sb)
}
