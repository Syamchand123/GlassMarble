package stage3

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func renderC4ContextDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("C4Context\n")
	title := getDiagramTitle(tree, "System Context Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	for _, boundary := range tree.Children {
		if isSystemBoundary(boundary) {
			alias := reg.boundary(boundary.BoundaryName)
			sb.WriteString(fmt.Sprintf("    System(%s, \"%s\", \"System\")\n",
				alias, sanitizeMermaidLabel(boundary.BoundaryName)))
		}
	}

	for _, node := range collectExternalNodes(tree) {
		tags := getNodeTags(node)
		sb.WriteString(fmt.Sprintf("    SystemExt(%s, \"%s\", \"External System%s\")\n",
			reg.alias(node.ID), sanitizeMermaidLabel(node.Name), tags))
	}

	for _, node := range collectAllNodes(tree) {
		if isDatabase(node) {
			tags := getNodeTags(node)
			sb.WriteString(fmt.Sprintf("    SystemDb(%s, \"%s\", \"Database%s\")\n",
				reg.alias(node.ID), sanitizeMermaidLabel(node.Name), tags))
		}
	}

	renderC4Edges(tree, reg, sb)
}

func renderC4ContainerDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("C4Container\n")
	title := getDiagramTitle(tree, "Container Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	for _, boundary := range tree.Children {
		alias := reg.boundary(boundary.BoundaryName)
		name := sanitizeMermaidLabel(boundary.BoundaryName)
		sb.WriteString(fmt.Sprintf("    System_Boundary(%s_sys, \"%s System\") {\n", alias, name))

		for _, subBoundary := range boundary.Children {
			subAlias := reg.boundary(subBoundary.BoundaryName)
			subName := sanitizeMermaidLabel(subBoundary.BoundaryName)
			tech := detectContainerTechnology(subBoundary)
			sb.WriteString(fmt.Sprintf("        Container(%s, \"%s\", \"%s\", \"%s\")\n",
				subAlias, subName, tech, getContainerDescription(subBoundary)))
		}

		for _, node := range boundary.Nodes {
			nodeAlias := reg.alias(node.ID)
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

	renderC4Edges(tree, reg, sb)
}

func renderC4ComponentDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("C4Component\n")
	title := getDiagramTitle(tree, "C4 Component Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	var renderTree func(t *types.LayoutTree, indent string)
	renderTree = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}

		if t.BoundaryName != "Root" {
			boundaryAlias := reg.boundary(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%sContainer_Boundary(%s, \"%s\") {\n", indent, boundaryAlias, sanitizeMermaidLabel(t.BoundaryName)))
			indent += "    "
		}

		for _, node := range t.Nodes {
			nodeAlias := reg.alias(node.ID)
			name := sanitizeMermaidLabel(node.Name)
			if name == "" {
				name = sanitizeMermaidLabel(node.ID)
			}
			kind := getShortKind(node.Kind)
			tech := node.PrimitiveType
			if tech == "" {
				tech = "Go/Core Component"
			}
			if isDatabase(node) {
				sb.WriteString(fmt.Sprintf("%sComponentDb(%s, \"%s\", \"%s\", \"Relational / NoSQL Data Store\")\n", indent, nodeAlias, name, tech))
			} else if strings.Contains(node.PrimitiveType, "CACHE") {
				sb.WriteString(fmt.Sprintf("%sComponentDb(%s, \"%s\", \"%s\", \"In-Memory Cache Store\")\n", indent, nodeAlias, name, tech))
			} else if strings.Contains(node.PrimitiveType, "MESSAGE_QUEUE") {
				sb.WriteString(fmt.Sprintf("%sComponentExt(%s, \"%s\", \"%s\", \"Async Message Queue / Event Bus\")\n", indent, nodeAlias, name, tech))
			} else if strings.Contains(node.PrimitiveType, "AI_LLM") {
				sb.WriteString(fmt.Sprintf("%sComponentExt(%s, \"%s\", \"%s\", \"AI / LLM Vector Service\")\n", indent, nodeAlias, name, tech))
			} else if isExternalSystem(node) {
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
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		predLabel := sanitizeMermaidLabel(strings.TrimPrefix(edge.Predicate, ont.PrefixGM))

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
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	for _, boundary := range tree.Children {
		if !isSystemBoundary(boundary) {
			continue
		}
		alias := reg.boundary(boundary.BoundaryName)
		name := sanitizeMermaidLabel(boundary.BoundaryName)
		sb.WriteString(fmt.Sprintf("    Enterprise_Boundary(%s_ent, \"%s Enterprise\") {\n", alias, name))
		sb.WriteString(fmt.Sprintf("        System(%s, \"%s\", \"System Module\")\n", alias, name))
		for _, node := range boundary.Nodes {
			if isExternalSystem(node) {
				sb.WriteString(fmt.Sprintf("        SystemExt(%s, \"%s\", \"External System\")\n",
					reg.alias(node.ID), sanitizeMermaidLabel(node.Name)))
			} else if isDatabase(node) {
				sb.WriteString(fmt.Sprintf("        SystemDb(%s, \"%s\", \"Database\")\n",
					reg.alias(node.ID), sanitizeMermaidLabel(node.Name)))
			}
		}
		sb.WriteString("    }\n")
	}

	renderC4Edges(tree, reg, sb)
}

func renderC4DynamicDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	// C4Dynamic (UML sequence semantics) is rendered as a Mermaid
	// sequenceDiagram, which is the closest valid native equivalent
	// (AUDIT Issue 2 Phase 2C-14).
	sb.WriteString("sequenceDiagram\n")
	title := getDiagramTitle(tree, "Dynamic Flow Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	participants := make(map[string]bool)
	for _, edge := range tree.Edges {
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		if !participants[srcAlias] {
			sb.WriteString(fmt.Sprintf("    participant %s as %s\n", srcAlias, getParticipantLabel(edge.SourceID)))
			participants[srcAlias] = true
		}
		if !participants[tgtAlias] {
			sb.WriteString(fmt.Sprintf("    participant %s as %s\n", tgtAlias, getParticipantLabel(edge.TargetID)))
			participants[tgtAlias] = true
		}
	}

	edges := make([]types.LayoutEdge, len(tree.Edges))
	copy(edges, tree.Edges)
	sort.Slice(edges, func(i, j int) bool {
		return edges[i].LineNumber < edges[j].LineNumber
	})
	for _, edge := range edges {
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		label := sanitizeMermaidLabel(shortPredicate(edge.Predicate))
		if label == "" {
			label = "calls"
		}
		sb.WriteString(fmt.Sprintf("    %s->>+%s: %s\n", srcAlias, tgtAlias, label))
		sb.WriteString(fmt.Sprintf("    %s-->>-%s: return\n", tgtAlias, srcAlias))
	}
}

func renderC4DeploymentDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	// Deployment_Node is only valid inside C4Deployment diagrams; the
	// previous C4Container header produced invalid Mermaid
	// (AUDIT Issue 2 Phase 2A-2).
	sb.WriteString("C4Deployment\n")
	title := getDiagramTitle(tree, "C4 Deployment Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	for _, boundary := range tree.Children {
		if !isSystemBoundary(boundary) {
			continue
		}
		alias := reg.boundary(boundary.BoundaryName)
		name := sanitizeMermaidLabel(boundary.BoundaryName)
		sb.WriteString(fmt.Sprintf("    Deployment_Node(%s_node, \"%s Node\", \"Deployment Environment\") {\n", alias, name))
		for _, node := range boundary.Nodes {
			nodeAlias := reg.alias(node.ID)
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

	renderC4Edges(tree, reg, sb)
}
