package aggregate

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// RenderPlantUMLDiagram renders the layout tree as a PlantUML diagram with C4 includes and UML stencils.
func RenderPlantUMLDiagram(tree *types.LayoutTree, t types.DiagramType) string {
	return RenderPlantUMLDiagramWithTheme(tree, t, "modern")
}

// RenderPlantUMLDiagramWithTheme renders with explicit theme skinparameters.
func RenderPlantUMLDiagramWithTheme(tree *types.LayoutTree, t types.DiagramType, themeName string) string {
	theme := GetTheme(themeName)
	var sb strings.Builder
	sb.WriteString("@startuml\n")
	sb.WriteString(theme.EmitPlantUMLSkinparams())

	if tree != nil {
		writeC4Includes(t, &sb)

		switch t {
		case types.UMLClass, types.UMLObject, types.UMLProfile, types.C4Code:
			renderPlantUMLClassDiagram(tree, &sb)
		case types.UMLComponent, types.UMLComposite, types.UMLPackage, types.UMLDeployment:
			renderPlantUMLComponentDiagram(tree, &sb)
		case types.C4Context:
			renderPlantUMLC4ContextDiagram(tree, &sb)
		case types.C4Container:
			renderPlantUMLC4ContainerDiagram(tree, &sb)
		case types.C4Component:
			renderPlantUMLC4ComponentDiagram(tree, &sb)
		case types.C4Landscape:
			renderPlantUMLC4LandscapeDiagram(tree, &sb)
		case types.C4Dynamic:
			renderPlantUMLC4DynamicDiagram(tree, &sb)
		case types.C4Deployment:
			renderPlantUMLC4DeploymentDiagram(tree, &sb)
		// Every remaining DiagramType routes here explicitly so the switch
		// covers all 31 (GAP-L-03): these structure-heavy diagrams
		// (usecase, activity, state, sequence, ER, flow, mindmap, ...) share
		// the generic rectangle/arrow stencil, which is valid PlantUML —
		// never a Mermaid-style fallback.
		case types.UMLUsecase, types.UMLActivity, types.UMLState,
			types.UMLSequence, types.UMLCommunication, types.UMLInteractionOverview,
			types.UMLTiming, types.ERDiagram, types.DataFlow, types.Mindmap,
			types.Flowchart, types.DependencyGraph, types.HotspotComplexity,
			types.CallGraph, types.LayeredArchitecture, types.ChangeImpact,
			types.Infrastructure:
			renderPlantUMLGenericDiagram(tree, &sb)
		default:
			renderPlantUMLGenericDiagram(tree, &sb)
		}
	}

	renderPlantUMLSummaryFooter(tree, &sb)

	sb.WriteString("@enduml\n")
	return sb.String()
}

func writeC4Includes(t types.DiagramType, sb *strings.Builder) {
	switch t {
	case types.C4Context, types.C4Landscape:
		sb.WriteString("!include <C4/C4_Context>\n")
	case types.C4Container:
		sb.WriteString("!include <C4/C4_Container>\n")
	case types.C4Component:
		sb.WriteString("!include <C4/C4_Component>\n")
	case types.C4Dynamic:
		sb.WriteString("!include <C4/C4_Context>\n")
	case types.C4Deployment:
		sb.WriteString("!include <C4/C4_Deployment>\n")
	}
}

func renderPlantUMLClassDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	for _, node := range collectAllNodes(tree) {
		name := sanitizeMermaidLabel(node.Name)
		if name == "" {
			name = sanitizeName(node.ID)
		}
		alias := sanitizeName(node.ID)
		code := node.Code
		if code == "" && node.Properties != nil {
			code = node.Properties["content"]
		}
		pFields, pMethods := parseMembersFromCode(code)

		switch node.Kind {
		case ont.PredTypeDecl, ont.PredStruct, ont.PredClass:
			keyword := "class"
			if strings.Contains(strings.ToLower(node.PrimitiveType), "interface") || strings.Contains(strings.ToLower(node.Name), "iface") {
				keyword = "interface"
			}
			if len(pFields) > 0 || len(pMethods) > 0 {
				sb.WriteString(fmt.Sprintf("%s \"%s\" as %s {\n", keyword, name, alias))
				for _, f := range pFields {
					prefix := memberVisibilityPrefix(f.visibility, f.label)
					if f.typeName != "" {
						sb.WriteString(fmt.Sprintf("    %s%s : %s\n", prefix, f.label, f.typeName))
					} else {
						sb.WriteString(fmt.Sprintf("    %s%s\n", prefix, f.label))
					}
				}
				for _, m := range pMethods {
					prefix := memberVisibilityPrefix(m.visibility, m.label)
					if m.typeName != "" {
						sb.WriteString(fmt.Sprintf("    %s%s() : %s\n", prefix, m.label, m.typeName))
					} else {
						sb.WriteString(fmt.Sprintf("    %s%s()\n", prefix, m.label))
					}
				}
				sb.WriteString("}\n")
			} else {
				sb.WriteString(fmt.Sprintf("%s \"%s\" as %s\n", keyword, name, alias))
			}
		case ont.PredInterface:
			if len(pMethods) > 0 {
				sb.WriteString(fmt.Sprintf("interface \"%s\" as %s {\n", name, alias))
				for _, m := range pMethods {
					prefix := memberVisibilityPrefix(m.visibility, m.label)
					sb.WriteString(fmt.Sprintf("    %s%s()\n", prefix, m.label))
				}
				sb.WriteString("}\n")
			} else {
				sb.WriteString(fmt.Sprintf("interface \"%s\" as %s\n", name, alias))
			}
		case ont.PredExecutable, ont.PredFunction, ont.PredMethod:
			sb.WriteString(fmt.Sprintf("class \"%s\" as %s <<method>>\n", name, alias))
		default:
			if node.Kind != ont.PredMember && node.Kind != "FIELD" {
				sb.WriteString(fmt.Sprintf("rectangle \"%s\" as %s\n", name, alias))
			}
		}
	}

	arrowMap := map[string]string{
		ont.PredInheritsFrom: " --|> ",
		ont.PredExtends:      " --|> ",
		ont.PredImplements:   " ..|> ",
		ont.PredComposes:     " --* ",
		ont.PredAggregates:   " --o ",
		ont.PredReferences:   " ..> ",
		ont.PredCalls:        " -> ",
		ont.PredDependsOn:    " ..> ",
		ont.PredHasMember:    " -- ",
		ont.PredHasField:     " -- ",
		ont.PredContains:     " --> ",
	}

	for _, edge := range tree.Edges {
		src := sanitizeName(edge.SourceID)
		tgt := sanitizeName(edge.TargetID)
		arrow, ok := arrowMap[edge.Predicate]
		if !ok {
			arrow = " ..> "
		}
		label := shortPredicate(edge.Predicate)
		sb.WriteString(fmt.Sprintf("%s%s%s : %s\n", src, arrow, tgt, label))
	}
}

func renderPlantUMLComponentDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	var walk func(t *types.LayoutTree, indent string)
	walk = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		if t.BoundaryName != "Root" {
			alias := sanitizeName(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%spackage \"%s\" as %s {\n", indent, sanitizeMermaidLabel(t.BoundaryName), alias))
			indent += "    "
		}
		for _, node := range t.Nodes {
			alias := sanitizeName(node.ID)
			name := sanitizeMermaidLabel(node.Name)
			if isDatabase(node) {
				sb.WriteString(fmt.Sprintf("%sdatabase \"%s\" as %s\n", indent, name, alias))
			} else {
				sb.WriteString(fmt.Sprintf("%scomponent \"%s\" as %s\n", indent, name, alias))
			}
		}
		for _, child := range t.Children {
			walk(child, indent)
		}
		if t.BoundaryName != "Root" {
			indent = indent[4:]
			sb.WriteString(fmt.Sprintf("%s}\n", indent))
		}
	}
	walk(tree, "")

	for _, edge := range tree.Edges {
		src := sanitizeName(edge.SourceID)
		tgt := sanitizeName(edge.TargetID)
		sb.WriteString(fmt.Sprintf("%s ..> %s : %s\n", src, tgt, shortPredicate(edge.Predicate)))
	}
}

func renderPlantUMLGenericDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	for _, node := range collectAllNodes(tree) {
		alias := sanitizeName(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		if name == "" {
			name = alias
		}
		sb.WriteString(fmt.Sprintf("rectangle \"%s\" as %s\n", name, alias))
	}
	for _, edge := range tree.Edges {
		src := sanitizeName(edge.SourceID)
		tgt := sanitizeName(edge.TargetID)
		sb.WriteString(fmt.Sprintf("%s --> %s : %s\n", src, tgt, shortPredicate(edge.Predicate)))
	}
}

func renderPlantUMLC4ContextDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	title := getDiagramTitle(tree, "System Context Diagram")
	sb.WriteString(fmt.Sprintf("title %s\n", title))

	for _, node := range collectNodesByKind(tree, ont.PredUser) {
		sb.WriteString(fmt.Sprintf("Person(%s, \"%s\", \"User / Client\")\n",
			sanitizeName(node.ID), sanitizeMermaidLabel(node.Name)))
	}

	if len(tree.Children) > 0 {
		for _, boundary := range tree.Children {
			alias := sanitizeName(boundary.BoundaryName)
			sb.WriteString(fmt.Sprintf("System(%s, \"%s\", \"System Module\")\n",
				alias, sanitizeMermaidLabel(boundary.BoundaryName)))
		}
	} else if len(tree.Nodes) > 0 {
		alias := sanitizeName(tree.BoundaryName)
		sb.WriteString(fmt.Sprintf("System(%s, \"%s\", \"Core System\")\n",
			alias, sanitizeMermaidLabel(tree.BoundaryName)))
	}

	for _, node := range collectExternalNodes(tree) {
		sb.WriteString(fmt.Sprintf("SystemExt(%s, \"%s\", \"External System\")\n",
			sanitizeName(node.ID), sanitizeMermaidLabel(node.Name)))
	}

	for _, node := range collectAllNodes(tree) {
		if isDatabase(node) {
			sb.WriteString(fmt.Sprintf("SystemDb(%s, \"%s\", \"Database\")\n",
				sanitizeName(node.ID), sanitizeMermaidLabel(node.Name)))
		}
	}

	renderPlantUMLC4Edges(tree, sb)
}

func renderPlantUMLC4ContainerDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	title := getDiagramTitle(tree, "Container Diagram")
	sb.WriteString(fmt.Sprintf("title %s\n", title))

	if len(tree.Children) == 0 && len(tree.Nodes) > 0 {
		alias := sanitizeName(tree.BoundaryName)
		name := sanitizeMermaidLabel(tree.BoundaryName)
		sb.WriteString(fmt.Sprintf("System_Boundary(%s_sys, \"%s System\") {\n", alias, name))
		for _, node := range tree.Nodes {
			nodeAlias := sanitizeName(node.ID)
			nodeName := sanitizeMermaidLabel(node.Name)
			tech := detectNodeTechnology(node)
			if isDatabase(node) {
				sb.WriteString(fmt.Sprintf("    ContainerDb(%s, \"%s\", \"%s\", \"%s\")\n",
					nodeAlias, nodeName, tech, getNodeDescription(node)))
			} else {
				sb.WriteString(fmt.Sprintf("    Container(%s, \"%s\", \"%s\", \"%s\")\n",
					nodeAlias, nodeName, tech, getNodeDescription(node)))
			}
		}
		sb.WriteString("}\n")
	} else {
		for _, boundary := range tree.Children {
			alias := sanitizeName(boundary.BoundaryName)
			name := sanitizeMermaidLabel(boundary.BoundaryName)
			sb.WriteString(fmt.Sprintf("System_Boundary(%s_sys, \"%s System\") {\n", alias, name))

			for _, subBoundary := range boundary.Children {
				subAlias := sanitizeName(subBoundary.BoundaryName)
				subName := sanitizeMermaidLabel(subBoundary.BoundaryName)
				tech := detectContainerTechnology(subBoundary)
				sb.WriteString(fmt.Sprintf("    Container(%s, \"%s\", \"%s\", \"%s\")\n",
					subAlias, subName, tech, getContainerDescription(subBoundary)))
			}

			for _, node := range boundary.Nodes {
				nodeAlias := sanitizeName(node.ID)
				nodeName := sanitizeMermaidLabel(node.Name)
				tech := detectNodeTechnology(node)
				if isDatabase(node) {
					sb.WriteString(fmt.Sprintf("    ContainerDb(%s, \"%s\", \"%s\", \"%s\")\n",
						nodeAlias, nodeName, tech, getNodeDescription(node)))
				} else {
					sb.WriteString(fmt.Sprintf("    Container(%s, \"%s\", \"%s\", \"%s\")\n",
						nodeAlias, nodeName, tech, getNodeDescription(node)))
				}
			}

			sb.WriteString("}\n")
		}
	}

	renderPlantUMLC4Edges(tree, sb)
}

func renderPlantUMLC4ComponentDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	title := getDiagramTitle(tree, "C4 Component Diagram")
	sb.WriteString(fmt.Sprintf("title %s\n", title))

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
				kind := getShortKind(node.Kind)
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
	renderTree(tree, "")

	renderPlantUMLC4Edges(tree, sb)
}

func renderPlantUMLC4LandscapeDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	title := getDiagramTitle(tree, "System Landscape Diagram")
	sb.WriteString(fmt.Sprintf("title %s\n", title))

	if len(tree.Children) == 0 && len(tree.Nodes) > 0 {
		alias := sanitizeName(tree.BoundaryName)
		name := sanitizeMermaidLabel(tree.BoundaryName)
		sb.WriteString(fmt.Sprintf("Enterprise_Boundary(%s_ent, \"%s Enterprise\") {\n", alias, name))
		sb.WriteString(fmt.Sprintf("    System(%s, \"%s\", \"System Module\")\n", alias, name))
		for _, node := range tree.Nodes {
			if isExternalSystem(node) {
				sb.WriteString(fmt.Sprintf("    SystemExt(%s, \"%s\", \"External System\")\n",
					sanitizeName(node.ID), sanitizeMermaidLabel(node.Name)))
			} else if isDatabase(node) {
				sb.WriteString(fmt.Sprintf("    SystemDb(%s, \"%s\", \"Database\")\n",
					sanitizeName(node.ID), sanitizeMermaidLabel(node.Name)))
			}
		}
		sb.WriteString("}\n")
	} else {
		for _, boundary := range tree.Children {
			alias := sanitizeName(boundary.BoundaryName)
			name := sanitizeMermaidLabel(boundary.BoundaryName)
			sb.WriteString(fmt.Sprintf("Enterprise_Boundary(%s_ent, \"%s Enterprise\") {\n", alias, name))
			sb.WriteString(fmt.Sprintf("    System(%s, \"%s\", \"System Module\")\n", alias, name))
			for _, node := range boundary.Nodes {
				if isExternalSystem(node) {
					sb.WriteString(fmt.Sprintf("    SystemExt(%s, \"%s\", \"External System\")\n",
						sanitizeName(node.ID), sanitizeMermaidLabel(node.Name)))
				} else if isDatabase(node) {
					sb.WriteString(fmt.Sprintf("    SystemDb(%s, \"%s\", \"Database\")\n",
						sanitizeName(node.ID), sanitizeMermaidLabel(node.Name)))
				}
			}
			sb.WriteString("}\n")
		}
	}

	renderPlantUMLC4Edges(tree, sb)
}

func renderPlantUMLC4DynamicDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	title := getDiagramTitle(tree, "Dynamic Flow Diagram")
	sb.WriteString(fmt.Sprintf("title %s\n", title))

	for _, node := range collectAllNodes(tree) {
		alias := sanitizeName(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		if name == "" {
			name = sanitizeName(node.ID)
		}
		sb.WriteString(fmt.Sprintf("System(%s, \"%s\", \"Execution Node\")\n", alias, name))
	}

	for i, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		label := sanitizeMermaidLabel(shortPredicate(edge.Predicate))
		if label == "" {
			label = "calls"
		}
		sb.WriteString(fmt.Sprintf("Rel(%s, %s, \"%d: %s\")\n", srcAlias, tgtAlias, i+1, label))
	}
}

func renderPlantUMLC4DeploymentDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	title := getDiagramTitle(tree, "C4 Deployment Diagram")
	sb.WriteString(fmt.Sprintf("title %s\n", title))

	if len(tree.Children) == 0 && len(tree.Nodes) > 0 {
		alias := sanitizeName(tree.BoundaryName)
		name := sanitizeMermaidLabel(tree.BoundaryName)
		sb.WriteString(fmt.Sprintf("Deployment_Node(%s_node, \"%s Node\", \"Deployment Environment\") {\n", alias, name))
		for _, node := range tree.Nodes {
			nodeAlias := sanitizeName(node.ID)
			nodeName := sanitizeMermaidLabel(node.Name)
			if isDatabase(node) {
				sb.WriteString(fmt.Sprintf("    ContainerDb(%s, \"%s\", \"%s\", \"Data Store\")\n",
					nodeAlias, nodeName, detectNodeTechnology(node)))
			} else {
				sb.WriteString(fmt.Sprintf("    Container(%s, \"%s\", \"%s\", \"%s\")\n",
					nodeAlias, nodeName, detectNodeTechnology(node), getNodeDescription(node)))
			}
		}
		sb.WriteString("}\n")
	} else {
		for _, boundary := range tree.Children {
			alias := sanitizeName(boundary.BoundaryName)
			name := sanitizeMermaidLabel(boundary.BoundaryName)
			sb.WriteString(fmt.Sprintf("Deployment_Node(%s_node, \"%s Node\", \"Deployment Environment\") {\n", alias, name))
			for _, node := range boundary.Nodes {
				nodeAlias := sanitizeName(node.ID)
				nodeName := sanitizeMermaidLabel(node.Name)
				if isDatabase(node) {
					sb.WriteString(fmt.Sprintf("    ContainerDb(%s, \"%s\", \"%s\", \"Data Store\")\n",
						nodeAlias, nodeName, detectNodeTechnology(node)))
				} else {
					sb.WriteString(fmt.Sprintf("    Container(%s, \"%s\", \"%s\", \"%s\")\n",
						nodeAlias, nodeName, detectNodeTechnology(node), getNodeDescription(node)))
				}
			}
			sb.WriteString("}\n")
		}
	}

	renderPlantUMLC4Edges(tree, sb)
}

func renderPlantUMLC4Edges(tree *types.LayoutTree, sb *strings.Builder) {
	drawn := make(map[string]bool)
	for _, edge := range tree.Edges {
		src := sanitizeName(edge.SourceID)
		tgt := sanitizeName(edge.TargetID)
		key := src + "->" + tgt
		if drawn[key] {
			continue
		}
		drawn[key] = true
		label := sanitizeMermaidLabel(shortPredicate(edge.Predicate))
		sb.WriteString(fmt.Sprintf("Rel(%s, %s, \"%s\")\n", src, tgt, label))
	}
}
