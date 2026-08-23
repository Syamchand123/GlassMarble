package render

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

	// Root-level package nodes (cmd, main.go, ...) have no parent directory
	// and land directly on the root tree; without this they would silently
	// disappear from the context view while their edges reference them
	// (GAP-C4-01).
	for _, node := range tree.Nodes {
		if isExternalSystem(node) || isDatabase(node) {
			continue
		}
		alias := reg.alias(node.ID)
		reg.markDeclared(alias)
		sb.WriteString(fmt.Sprintf("    System(%s, \"%s\", \"System\")\n",
			alias, sanitizeMermaidLabel(node.Name)))
	}

	for _, node := range collectExternalNodes(tree) {
		alias := reg.alias(node.ID)
		reg.markDeclared(alias)
		tags := getNodeTags(node)
		sb.WriteString(fmt.Sprintf("    System_Ext(%s, \"%s\", \"External System%s\")\n",
			alias, sanitizeMermaidLabel(node.Name), tags))
	}

	for _, node := range collectAllNodes(tree) {
		if isDatabase(node) && node.Kind != ont.PredPackage {
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

// countSubtreeNodes returns the total number of layout nodes in the subtree.
func countSubtreeNodes(t *types.LayoutTree) int {
	if t == nil {
		return 0
	}
	n := len(t.Nodes)
	for _, c := range t.Children {
		n += countSubtreeNodes(c)
	}
	return n
}

// c4BlockRenderer drives the recursive C4 block renderer shared by the
// container / landscape / deployment / infrastructure diagrams (GAP-C4-03).
type c4BlockRenderer struct {
	indent       string
	blockAlias   func(base string) string
	openBlock    func(alias, name string) string
	closeBlock   func(indent string) string
	selfLine     func(alias, name string) string
	renderNode   func(node *types.LayoutNode, reg *aliasRegistry) string
	renderFolded func(boundary *types.LayoutTree, count int, reg *aliasRegistry) string
}

// renderC4Blocks renders one outer block per content-bearing level-1
// boundary. Every deeper boundary folds its whole subtree into a single
// element, so deep boundary trees (folder/file scope) never silently drop
// nodes while global-scope output keeps its established shape (GAP-C4-03).
func renderC4Blocks(tree *types.LayoutTree, reg *aliasRegistry, sb *strings.Builder, r *c4BlockRenderer) {
	emitBlock := func(t *types.LayoutTree, indent string) {
		base := reg.boundary(t.BoundaryName)
		emitted := base
		if r.blockAlias != nil {
			emitted = r.blockAlias(base)
		}
		// A boundary name can appear at more than one tree position (e.g.
		// file and package scope subtrees); emit a unique alias so no
		// declaration is duplicated.
		emitted = reg.uniqueAlias(emitted)
		reg.markDeclared(emitted)
		var bind func(st *types.LayoutTree)
		bind = func(st *types.LayoutTree) {
			for _, n := range st.Nodes {
				reg.bindBoundary(n.ID, emitted)
			}
			for _, c := range st.Children {
				bind(c)
			}
		}
		bind(t)
		sb.WriteString(r.openBlock(emitted, sanitizeMermaidLabel(t.BoundaryName)))
		inner := indent + "    "
		if r.selfLine != nil {
			selfAlias := reg.uniqueAlias(base)
			reg.markDeclared(selfAlias)
			sb.WriteString(inner + r.selfLine(selfAlias, sanitizeMermaidLabel(t.BoundaryName)))
		}
		for _, node := range t.Nodes {
			sb.WriteString(inner + r.renderNode(node, reg))
		}
		for _, child := range t.Children {
			if !hasTreeNodes(child) {
				continue
			}
			sb.WriteString(inner + r.renderFolded(child, countSubtreeNodes(child), reg))
		}
		sb.WriteString(r.closeBlock(indent))
	}

	if len(tree.Children) == 0 && len(tree.Nodes) > 0 {
		emitBlock(tree, r.indent)
		return
	}
	for _, boundary := range tree.Children {
		if !hasTreeNodes(boundary) {
			continue
		}
		emitBlock(boundary, r.indent)
	}
}

// renderContainerElement renders a single layout node as a C4 Container
// (or ContainerDb for data stores), used by the container, deployment and
// infrastructure diagrams.
func renderContainerElement(node *types.LayoutNode, reg *aliasRegistry) string {
	alias := reg.alias(node.ID)
	reg.markDeclared(alias)
	name := sanitizeMermaidLabel(node.Name)
	if name == "" {
		name = sanitizeMermaidLabel(node.ID)
	}
	if isDatabase(node) {
		return fmt.Sprintf("ContainerDb(%s, \"%s\", \"%s\", \"%s\")\n",
			alias, name, detectNodeTechnology(node), getNodeDescription(node))
	}
	return fmt.Sprintf("Container(%s, \"%s\", \"%s\", \"%s\")\n",
		alias, name, detectNodeTechnology(node), getNodeDescription(node))
}

// renderFoldedContainerElement folds a deeper boundary (and its whole
// subtree) into a single Container element (GAP-C4-03).
func renderFoldedContainerElement(boundary *types.LayoutTree, count int, reg *aliasRegistry) string {
	alias := reg.uniqueAlias(reg.boundary(boundary.BoundaryName))
	reg.markDeclared(alias)
	name := sanitizeMermaidLabel(boundary.BoundaryName)
	desc := getContainerDescription(boundary)
	if len(boundary.Nodes) == 0 {
		desc = fmt.Sprintf("Package Container (%d nodes)", count)
	}
	return fmt.Sprintf("Container(%s, \"%s\", \"%s\", \"%s\")\n",
		alias, name, detectContainerTechnology(boundary), desc)
}

func renderC4ContainerDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("C4Container\n")
	title := getDiagramTitle(tree, "Container Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	renderC4Blocks(tree, reg, sb, &c4BlockRenderer{
		indent: "    ",
		blockAlias: func(base string) string {
			return base + "_sys"
		},
		openBlock: func(alias, name string) string {
			return fmt.Sprintf("    System_Boundary(%s, \"%s System\") {\n", alias, name)
		},
		closeBlock: func(indent string) string {
			return indent + "}\n"
		},
		renderNode:   renderContainerElement,
		renderFolded: renderFoldedContainerElement,
	})

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

	renderC4Blocks(tree, reg, sb, &c4BlockRenderer{
		indent: "    ",
		blockAlias: func(base string) string {
			return base + "_ent"
		},
		openBlock: func(alias, name string) string {
			return fmt.Sprintf("    Enterprise_Boundary(%s, \"%s Enterprise\") {\n", alias, name)
		},
		closeBlock: func(indent string) string {
			return indent + "}\n"
		},
		selfLine: func(alias, name string) string {
			return fmt.Sprintf("System(%s, \"%s\", \"System Module\")\n", alias, name)
		},
		renderNode: func(node *types.LayoutNode, reg *aliasRegistry) string {
			nodeAlias := reg.alias(node.ID)
			reg.markDeclared(nodeAlias)
			name := sanitizeMermaidLabel(node.Name)
			if isExternalSystem(node) {
				return fmt.Sprintf("System_Ext(%s, \"%s\", \"External System\")\n", nodeAlias, name)
			}
			if isDatabase(node) && node.Kind != ont.PredPackage {
				return fmt.Sprintf("SystemDb(%s, \"%s\", \"Database\")\n", nodeAlias, name)
			}
			return fmt.Sprintf("System(%s, \"%s\", \"System Module\")\n", nodeAlias, name)
		},
		renderFolded: func(boundary *types.LayoutTree, count int, reg *aliasRegistry) string {
			alias := reg.uniqueAlias(reg.boundary(boundary.BoundaryName))
			reg.markDeclared(alias)
			return fmt.Sprintf("System(%s, \"%s\", \"System Module (%d nodes)\")\n",
				alias, sanitizeMermaidLabel(boundary.BoundaryName), count)
		},
	})

	// Root-level package nodes have no parent directory; render them as
	// top-level systems so they are not silently dropped (GAP-C4-01).
	for _, node := range tree.Nodes {
		if isExternalSystem(node) || isDatabase(node) {
			continue
		}
		nodeAlias := reg.alias(node.ID)
		reg.markDeclared(nodeAlias)
		sb.WriteString(fmt.Sprintf("    System(%s, \"%s\", \"System Module\")\n",
			nodeAlias, sanitizeMermaidLabel(node.Name)))
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

	renderC4Blocks(tree, reg, sb, &c4BlockRenderer{
		indent: "    ",
		blockAlias: func(base string) string {
			return base + "_node"
		},
		openBlock: func(alias, name string) string {
			return fmt.Sprintf("    Deployment_Node(%s, \"%s Node\", \"Deployment Environment\") {\n", alias, name)
		},
		closeBlock: func(indent string) string {
			return indent + "}\n"
		},
		renderNode:   renderContainerElement,
		renderFolded: renderFoldedContainerElement,
	})

	renderC4Edges(tree, reg, sb)
}
