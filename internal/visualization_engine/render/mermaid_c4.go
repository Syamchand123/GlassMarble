package aggregate

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

	for _, node := range collectAllNodes(tree) {
		if node.Kind == ont.PredUser {
			alias := reg.alias(node.ID)
			reg.markDeclared(alias)
			sb.WriteString(fmt.Sprintf("    Person(%s, \"%s\", \"User / Client\")\n",
				alias, sanitizeMermaidLabel(node.Name)))
		}
	}

	if len(tree.Children) > 0 {
		for _, boundary := range tree.Children {
			if !hasTreeNodes(boundary) {
				continue
			}
			alias := reg.boundary(boundary.BoundaryName)
			reg.markDeclared(alias)
			sb.WriteString(fmt.Sprintf("    System(%s, \"%s\", \"System\")\n",
				alias, sanitizeMermaidLabel(boundary.BoundaryName)))
		}
	} else if len(tree.Nodes) > 0 {
		rootAlias := reg.boundary(tree.BoundaryName)
		reg.markDeclared(rootAlias)
		sb.WriteString(fmt.Sprintf("    System(%s, \"%s\", \"Core System\")\n",
			rootAlias, sanitizeMermaidLabel(tree.BoundaryName)))
	}

	for _, node := range collectExternalNodes(tree) {
		alias := reg.alias(node.ID)
		reg.markDeclared(alias)
		tags := getNodeTags(node)
		sb.WriteString(fmt.Sprintf("    System_Ext(%s, \"%s\", \"External System%s\")\n",
			alias, sanitizeMermaidLabel(node.Name), tags))
	}

	for _, node := range collectAllNodes(tree) {
		if isDatabase(node) {
			alias := reg.alias(node.ID)
			reg.markDeclared(alias)
			tags := getNodeTags(node)
			sb.WriteString(fmt.Sprintf("    SystemDb(%s, \"%s\", \"Database%s\")\n",
				alias, sanitizeMermaidLabel(node.Name), tags))
		}
	}

	renderC4Edges(tree, reg, sb)
}

func hasTreeNodes(t *types.LayoutTree) bool {
	if t == nil {
		return false
	}
	if len(t.Nodes) > 0 {
		return true
	}
	for _, child := range t.Children {
		if hasTreeNodes(child) {
			return true
		}
	}
	return false
}

func renderC4ContainerDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("C4Container\n")
	title := getDiagramTitle(tree, "Container Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	if len(tree.Children) == 0 && len(tree.Nodes) > 0 {
		alias := reg.boundary(tree.BoundaryName)
		name := sanitizeMermaidLabel(tree.BoundaryName)
		sb.WriteString(fmt.Sprintf("    System_Boundary(%s_sys, \"%s System\") {\n", alias, name))
		for _, node := range tree.Nodes {
			nodeAlias := reg.alias(node.ID)
			reg.markDeclared(nodeAlias)
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
	} else {
		for _, boundary := range tree.Children {
			if !hasTreeNodes(boundary) {
				continue
			}
			alias := reg.boundary(boundary.BoundaryName)
			name := sanitizeMermaidLabel(boundary.BoundaryName)
			sb.WriteString(fmt.Sprintf("    System_Boundary(%s_sys, \"%s System\") {\n", alias, name))

			for _, subBoundary := range boundary.Children {
				if !hasTreeNodes(subBoundary) {
					continue
				}
				subAlias := reg.boundary(subBoundary.BoundaryName)
				reg.markDeclared(subAlias)
				subName := sanitizeMermaidLabel(subBoundary.BoundaryName)
				tech := detectContainerTechnology(subBoundary)
				sb.WriteString(fmt.Sprintf("        Container(%s, \"%s\", \"%s\", \"%s\")\n",
					subAlias, subName, tech, getContainerDescription(subBoundary)))
			}

			for _, node := range boundary.Nodes {
				nodeAlias := reg.alias(node.ID)
				reg.markDeclared(nodeAlias)
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
		if t == nil || !hasTreeNodes(t) {
			return
		}

		if t.BoundaryName != "Root" {
			boundaryAlias := reg.boundary(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%sContainer_Boundary(%s, \"%s\") {\n", indent, boundaryAlias, sanitizeMermaidLabel(t.BoundaryName)))
			indent += "    "
		}

		for _, node := range t.Nodes {
			nodeAlias := reg.alias(node.ID)
			reg.markDeclared(nodeAlias)
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
				sb.WriteString(fmt.Sprintf("%sComponent_Ext(%s, \"%s\", \"%s\", \"Async Message Queue / Event Bus\")\n", indent, nodeAlias, name, tech))
			} else if strings.Contains(node.PrimitiveType, "AI_LLM") {
				sb.WriteString(fmt.Sprintf("%sComponent_Ext(%s, \"%s\", \"%s\", \"AI / LLM Vector Service\")\n", indent, nodeAlias, name, tech))
			} else if isExternalSystem(node) {
				sb.WriteString(fmt.Sprintf("%sComponent_Ext(%s, \"%s\", \"%s\", \"External Cloud / Service Integration\")\n", indent, nodeAlias, name, tech))
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

	renderC4Edges(tree, reg, sb)
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

	if len(tree.Children) == 0 && len(tree.Nodes) > 0 {
		alias := reg.boundary(tree.BoundaryName)
		reg.markDeclared(alias)
		name := sanitizeMermaidLabel(tree.BoundaryName)
		sb.WriteString(fmt.Sprintf("    Enterprise_Boundary(%s_ent, \"%s Enterprise\") {\n", alias, name))
		sb.WriteString(fmt.Sprintf("        System(%s, \"%s\", \"System Module\")\n", alias, name))
		for _, node := range tree.Nodes {
			nodeAlias := reg.alias(node.ID)
			reg.markDeclared(nodeAlias)
			if isExternalSystem(node) {
				sb.WriteString(fmt.Sprintf("        System_Ext(%s, \"%s\", \"External System\")\n",
					nodeAlias, sanitizeMermaidLabel(node.Name)))
			} else if isDatabase(node) {
				sb.WriteString(fmt.Sprintf("        SystemDb(%s, \"%s\", \"Database\")\n",
					nodeAlias, sanitizeMermaidLabel(node.Name)))
			}
		}
		sb.WriteString("    }\n")
	} else {
		for _, boundary := range tree.Children {
			if !hasTreeNodes(boundary) {
				continue
			}
			alias := reg.boundary(boundary.BoundaryName)
			reg.markDeclared(alias)
			name := sanitizeMermaidLabel(boundary.BoundaryName)
			sb.WriteString(fmt.Sprintf("    Enterprise_Boundary(%s_ent, \"%s Enterprise\") {\n", alias, name))
			sb.WriteString(fmt.Sprintf("        System(%s, \"%s\", \"System Module\")\n", alias, name))
			for _, node := range boundary.Nodes {
				nodeAlias := reg.alias(node.ID)
				reg.markDeclared(nodeAlias)
				if isExternalSystem(node) {
					sb.WriteString(fmt.Sprintf("        System_Ext(%s, \"%s\", \"External System\")\n",
						nodeAlias, sanitizeMermaidLabel(node.Name)))
				} else if isDatabase(node) {
					sb.WriteString(fmt.Sprintf("        SystemDb(%s, \"%s\", \"Database\")\n",
						nodeAlias, sanitizeMermaidLabel(node.Name)))
				}
			}
			sb.WriteString("    }\n")
		}
	}

	renderC4Edges(tree, reg, sb)
}

func renderC4DynamicDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("sequenceDiagram\n")
	title := getDiagramTitle(tree, "Dynamic Flow Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	if len(tree.Edges) == 0 {
		allNodes := collectAllNodes(tree)
		if len(allNodes) == 0 {
			sb.WriteString("    participant System as \"System\"\n    System->>System: (no dynamic calls)\n")
			return
		}
		for _, node := range allNodes {
			alias := reg.alias(node.ID)
			sb.WriteString(fmt.Sprintf("    participant %s as %s\n", alias, getParticipantLabel(node.ID)))
		}
		first := reg.alias(allNodes[0].ID)
		sb.WriteString(fmt.Sprintf("    %s->>%s: (idle)\n", first, first))
		return
	}

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
	sb.WriteString("C4Deployment\n")
	title := getDiagramTitle(tree, "C4 Deployment Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	if len(tree.Children) == 0 && len(tree.Nodes) > 0 {
		alias := reg.boundary(tree.BoundaryName)
		name := sanitizeMermaidLabel(tree.BoundaryName)
		sb.WriteString(fmt.Sprintf("    Deployment_Node(%s_node, \"%s Node\", \"Deployment Environment\") {\n", alias, name))
		for _, node := range tree.Nodes {
			nodeAlias := reg.alias(node.ID)
			reg.markDeclared(nodeAlias)
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
	} else {
		for _, boundary := range tree.Children {
			if !hasTreeNodes(boundary) {
				continue
			}
			alias := reg.boundary(boundary.BoundaryName)
			name := sanitizeMermaidLabel(boundary.BoundaryName)
			sb.WriteString(fmt.Sprintf("    Deployment_Node(%s_node, \"%s Node\", \"Deployment Environment\") {\n", alias, name))
			for _, node := range boundary.Nodes {
				nodeAlias := reg.alias(node.ID)
				reg.markDeclared(nodeAlias)
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
	}

	renderC4Edges(tree, reg, sb)
}
