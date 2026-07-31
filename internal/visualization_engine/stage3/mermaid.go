package stage3

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// RenderDiagram converts a LayoutTree into Mermaid diagram markup for the given DiagramType.
func RenderDiagram(tree *types.LayoutTree, t types.DiagramType) string {
	var sb strings.Builder

	switch t {
	case types.UMLClass:
		renderClassDiagram(tree, &sb)
	case types.UMLObject:
		renderObjectDiagram(tree, &sb)
	case types.UMLComponent:
		renderUMLComponentDiagram(tree, &sb)
	case types.UMLDeployment:
		renderUMLDeploymentDiagram(tree, &sb)
	case types.UMLPackage:
		renderUMLPackageDiagram(tree, &sb)
	case types.UMLComposite:
		renderUMLCompositeDiagram(tree, &sb)
	case types.UMLProfile:
		renderUMLProfileDiagram(tree, &sb)
	case types.UMLUsecase:
		renderUsecaseDiagram(tree, &sb)
	case types.UMLActivity:
		renderActivityDiagram(tree, &sb)
	case types.Flowchart:
		renderFlowchartFallback(tree, &sb)
	case types.UMLState:
		renderStateDiagram(tree, &sb)
	case types.UMLSequence:
		renderSequenceDiagram(tree, &sb)
	case types.UMLCommunication:
		renderCommunicationDiagram(tree, &sb)
	case types.UMLInteractionOverview:
		renderInteractionOverviewDiagram(tree, &sb)
	case types.UMLTiming:
		renderTimingDiagram(tree, &sb)
	case types.C4Context:
		renderC4ContextDiagram(tree, &sb)
	case types.C4Container:
		renderC4ContainerDiagram(tree, &sb)
	case types.C4Component:
		renderC4ComponentDiagram(tree, &sb)
	case types.C4Code:
		renderC4CodeDiagram(tree, &sb)
	case types.C4Landscape:
		renderC4LandscapeDiagram(tree, &sb)
	case types.C4Dynamic:
		renderC4DynamicDiagram(tree, &sb)
	case types.C4Deployment:
		renderC4DeploymentDiagram(tree, &sb)
	case types.ERDiagram:
		renderERDiagram(tree, &sb)
	case types.DataFlow:
		renderDataFlowDiagram(tree, &sb)
	case types.Mindmap:
		renderMindmapDiagram(tree, &sb)
	case types.DependencyGraph:
		renderDependencyGraphDiagram(tree, &sb)
	case types.HotspotComplexity:
		renderHotspotComplexityDiagram(tree, &sb)
	case types.CallGraph:
		renderCallGraphDiagram(tree, &sb)
	case types.LayeredArchitecture:
		renderLayeredArchitectureDiagram(tree, &sb)
	case types.ChangeImpact:
		renderChangeImpactDiagram(tree, &sb)
	case types.Infrastructure:
		renderInfrastructureDiagram(tree, &sb)
	default:
		renderFlowchartFallback(tree, &sb)
	}

	renderSummaryFooter(tree, &sb)
	return sb.String()
}

func renderClassDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("classDiagram\n")
	classes := make(map[string]*types.LayoutNode)
	methods := make(map[string][]string)

	var collectNodes func(t *types.LayoutTree)
	collectNodes = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			if node.Kind == "gm:TypeDecl" {
				classes[node.ID] = node
			} else if node.Kind == "gm:Executable" {
				_, rec, sym := parseFQN(node.ID)
				if rec != "" {
					parentID := findParentClassID(node.ID, classes)
					if parentID != "" {
						methods[parentID] = append(methods[parentID], sym)
					}
				}
			} else if node.Kind == "gm:Member" {
				_, rec, sym := parseFQN(node.ID)
				if rec != "" {
					parentID := findParentClassID(node.ID, classes)
					if parentID != "" {
						if node.PrimitiveType != "" {
							sym = node.PrimitiveType + " " + sym
						}
						methods[parentID] = append(methods[parentID], sym)
					}
				}
			}
		}
		for _, child := range t.Children {
			collectNodes(child)
		}
	}
	collectNodes(tree)

	for id, class := range classes {
		classAlias := sanitizeName(id)
		sb.WriteString(fmt.Sprintf("    class %s {\n", classAlias))
		lbl := sanitizeMermaidLabel(class.Name)
		if lbl != "" {
			sb.WriteString(fmt.Sprintf("        <<%s>>\n", lbl))
		}
		if mList, exists := methods[id]; exists {
			for _, m := range mList {
				sb.WriteString(fmt.Sprintf("        +%s()\n", sanitizeMermaidLabel(m)))
			}
		}
		if class.PrimitiveType != "" {
			sb.WriteString(fmt.Sprintf("        %s\n", sanitizeMermaidLabel(class.PrimitiveType)))
		}
		sb.WriteString("    }\n")
	}

	drawnRelations := make(map[string]bool)
	for _, edge := range tree.Edges {
		srcClasses := resolveNodeToClass(edge.SourceID, classes)
		tgtClasses := resolveNodeToClass(edge.TargetID, classes)
		for _, src := range srcClasses {
			for _, tgt := range tgtClasses {
				if src != tgt {
					relationKey := fmt.Sprintf("%s->%s", src, tgt)
					if drawnRelations[relationKey] {
						continue
					}
					drawnRelations[relationKey] = true
					srcAlias := sanitizeName(src)
					tgtAlias := sanitizeName(tgt)
					switch edge.Predicate {
					case "gm:inheritsFrom":
						sb.WriteString(fmt.Sprintf("    %s --|> %s : inherits\n", srcAlias, tgtAlias))
					case "gm:implements":
						sb.WriteString(fmt.Sprintf("    %s ..|> %s : implements\n", srcAlias, tgtAlias))
					case "gm:composes":
						sb.WriteString(fmt.Sprintf("    %s --* %s : composes\n", srcAlias, tgtAlias))
					case "gm:aggregates":
						sb.WriteString(fmt.Sprintf("    %s --o %s : aggregates\n", srcAlias, tgtAlias))
					default:
						sb.WriteString(fmt.Sprintf("    %s ..> %s : uses\n", srcAlias, tgtAlias))
					}
				}
			}
		}
	}
}

func renderObjectDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("classDiagram\n")
	var renderObjects func(t *types.LayoutTree)
	renderObjects = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			if node.Kind == "gm:TypeDecl" {
				alias := sanitizeName(node.ID)
				sb.WriteString(fmt.Sprintf("    class %s {\n", alias))
				sb.WriteString(fmt.Sprintf("        %s : Instance\n", sanitizeMermaidLabel(node.Name)))
				sb.WriteString("    }\n")
			}
		}
		for _, child := range t.Children {
			renderObjects(child)
		}
	}
	renderObjects(tree)
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s --- %s : link\n", srcAlias, tgtAlias))
	}
}

func renderUMLComponentDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("flowchart TB\n")
	var renderComp func(t *types.LayoutTree, indent string)
	renderComp = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		if t.BoundaryName != "Root" {
			boundaryAlias := sanitizeName(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%ssubgraph %s [\"<<component>> %s\"]\n", indent, boundaryAlias, t.BoundaryName))
			indent += "    "
		}
		for _, node := range t.Nodes {
			alias := sanitizeName(node.ID)
			sb.WriteString(fmt.Sprintf("%s%s[[\"%s\"]]\n", indent, alias, node.Name))
		}
		for _, child := range t.Children {
			renderComp(child, indent)
		}
		if t.BoundaryName != "Root" {
			indent = indent[4:]
			sb.WriteString(fmt.Sprintf("%send\n", indent))
		}
	}
	renderComp(tree, "")
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s ..> %s\n", srcAlias, tgtAlias))
	}
}

func renderUMLDeploymentDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("flowchart TB\n")
	var renderNode func(t *types.LayoutTree, indent string)
	renderNode = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		if t.BoundaryName != "Root" {
			boundaryAlias := sanitizeName(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%ssubgraph %s [\"<<device>> %s\"]\n", indent, boundaryAlias, t.BoundaryName))
			indent += "    "
		}
		for _, node := range t.Nodes {
			alias := sanitizeName(node.ID)
			if node.PrimitiveType == "DATABASE" {
				sb.WriteString(fmt.Sprintf("%s%s[(\"%s (db)\")]\n", indent, alias, node.Name))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s[\"%s (execution)\"]\n", indent, alias, node.Name))
			}
		}
		for _, child := range t.Children {
			renderNode(child, indent)
		}
		if t.BoundaryName != "Root" {
			indent = indent[4:]
			sb.WriteString(fmt.Sprintf("%send\n", indent))
		}
	}
	renderNode(tree, "")
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s ===|protocol| %s\n", srcAlias, tgtAlias))
	}
}

func renderUMLPackageDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("flowchart TB\n")
	var renderPack func(t *types.LayoutTree, indent string)
	renderPack = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		if t.BoundaryName != "Root" {
			boundaryAlias := sanitizeName(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%ssubgraph %s [\"folder: %s\"]\n", indent, boundaryAlias, t.BoundaryName))
			indent += "    "
		}
		for _, node := range t.Nodes {
			alias := sanitizeName(node.ID)
			sb.WriteString(fmt.Sprintf("%s%s[\"%s\"]\n", indent, alias, node.Name))
		}
		for _, child := range t.Children {
			renderPack(child, indent)
		}
		if t.BoundaryName != "Root" {
			indent = indent[4:]
			sb.WriteString(fmt.Sprintf("%send\n", indent))
		}
	}
	renderPack(tree, "")
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s -.->|import| %s\n", srcAlias, tgtAlias))
	}
}

func renderUMLCompositeDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("flowchart LR\n")
	var renderComposite func(t *types.LayoutTree, indent string)
	renderComposite = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		if t.BoundaryName != "Root" {
			boundaryAlias := sanitizeName(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%ssubgraph %s [\"composite structure: %s\"]\n", indent, boundaryAlias, t.BoundaryName))
			indent += "    "
		}
		for _, node := range t.Nodes {
			alias := sanitizeName(node.ID)
			name := sanitizeMermaidLabel(node.Name)
			if node.Kind == "gm:Interface" || node.Kind == "gm:Port" {
				sb.WriteString(fmt.Sprintf("%s%s([\"Port: %s\"])\n", indent, alias, name))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s[\"Part: %s\"]\n", indent, alias, name))
			}
		}
		for _, child := range t.Children {
			renderComposite(child, indent)
		}
		if t.BoundaryName != "Root" {
			indent = indent[4:]
			sb.WriteString(fmt.Sprintf("%send\n", indent))
		}
	}
	renderComposite(tree, "")
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s --- %s\n", srcAlias, tgtAlias))
	}
}

func renderUMLProfileDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("classDiagram\n")
	var renderProfile func(t *types.LayoutTree)
	renderProfile = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := sanitizeName(node.ID)
			sb.WriteString(fmt.Sprintf("    class %s {\n", alias))
			stereo := node.PrimitiveType
			if stereo == "" {
				stereo = sanitizeMermaidLabel(node.Name)
			}
			sb.WriteString(fmt.Sprintf("        <<%s>>\n", stereo))
			sb.WriteString("    }\n")
		}
		for _, child := range t.Children {
			renderProfile(child)
		}
	}
	renderProfile(tree)
}

func renderUsecaseDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("graph LR\n")
	sb.WriteString("    Actor((Client))\n")
	var renderUsecases func(t *types.LayoutTree)
	renderUsecases = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := sanitizeName(node.ID)
			name := sanitizeMermaidLabel(node.Name)
			if node.Kind == "gm:Annotation" {
				sb.WriteString(fmt.Sprintf("    %s((\"Use Case: %s\"))\n", alias, name))
				sb.WriteString(fmt.Sprintf("    Actor --> %s\n", alias))
			} else {
				sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", alias, name))
			}
		}
		for _, child := range t.Children {
			renderUsecases(child)
		}
	}
	renderUsecases(tree)
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s --> %s\n", srcAlias, tgtAlias))
	}
}

func renderActivityDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("flowchart TB\n")
	sb.WriteString("    Start([Start])\n")
	var firstNodeAlias string
	var lastNodeAlias string
	var renderFlow func(t *types.LayoutTree)
	renderFlow = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := sanitizeName(node.ID)
			if firstNodeAlias == "" {
				firstNodeAlias = alias
			}
			lastNodeAlias = alias
			name := sanitizeMermaidLabel(node.Name)
			if name == "" {
				name = sanitizeMermaidLabel(node.ID)
			}
			if node.Kind == "gm:ControlStructure" {
				sb.WriteString(fmt.Sprintf("    %s{\"%s\"}\n", alias, name))
			} else {
				sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", alias, name))
			}
		}
		for _, child := range t.Children {
			renderFlow(child)
		}
	}
	renderFlow(tree)
	if firstNodeAlias != "" {
		sb.WriteString(fmt.Sprintf("    Start --> %s\n", firstNodeAlias))
	}
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		if edge.Predicate == "gm:spawnsConcurrent" {
			sb.WriteString(fmt.Sprintf("    %s -->|fork| %s\n", srcAlias, tgtAlias))
		} else if edge.Predicate == "gm:controlFlowToTrue" {
			sb.WriteString(fmt.Sprintf("    %s -->|true| %s\n", srcAlias, tgtAlias))
		} else if edge.Predicate == "gm:controlFlowToFalse" {
			sb.WriteString(fmt.Sprintf("    %s -->|false| %s\n", srcAlias, tgtAlias))
		} else {
			sb.WriteString(fmt.Sprintf("    %s --> %s\n", srcAlias, tgtAlias))
		}
	}
	if lastNodeAlias != "" {
		sb.WriteString("    End([End])\n")
		sb.WriteString(fmt.Sprintf("    %s --> End\n", lastNodeAlias))
	} else {
		sb.WriteString("    End([End])\n")
		sb.WriteString("    Start --> End\n")
	}
}

func renderStateDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("stateDiagram-v2\n")
	states := make(map[string]bool)
	var firstState string
	var renderStates func(t *types.LayoutTree)
	renderStates = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := sanitizeName(node.ID)
			name := sanitizeMermaidLabel(node.Name)
			if name == "" {
				name = sanitizeMermaidLabel(node.ID)
			}
			if firstState == "" {
				firstState = alias
			}
			sb.WriteString(fmt.Sprintf("    state %s as \"%s\"\n", alias, name))
			states[alias] = true
		}
		for _, child := range t.Children {
			renderStates(child)
		}
	}
	renderStates(tree)
	if firstState != "" {
		sb.WriteString(fmt.Sprintf("    [*] --> %s\n", firstState))
	} else {
		sb.WriteString("    [*] --> Idle\n")
		sb.WriteString("    Idle --> [*]\n")
	}
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		if states[srcAlias] && states[tgtAlias] && srcAlias != tgtAlias {
			label := sanitizeMermaidLabel(strings.TrimPrefix(edge.Predicate, "gm:"))
			sb.WriteString(fmt.Sprintf("    %s --> %s : %s\n", srcAlias, tgtAlias, label))
		}
	}
}

func renderSequenceDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("sequenceDiagram\n")
	participants := make(map[string]bool)
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
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
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		_, _, symbol := parseFQN(edge.TargetID)
		symbolLabel := sanitizeMermaidLabel(symbol)
		if symbolLabel == "" {
			symbolLabel = sanitizeMermaidLabel(edge.TargetID)
		}
		arrow := "->>+"
		if edge.Predicate == "gm:spawnsConcurrent" {
			arrow = "-)+"
		} else if edge.Predicate == "gm:dispatchesEvent" {
			arrow = "--)+"
		}
		cycleSuffix := ""
		if edge.IsCycle {
			cycleSuffix = " [CYCLE]"
		}
		sb.WriteString(fmt.Sprintf("    %s%s%s: %s%s\n", srcAlias, arrow, tgtAlias, symbolLabel, cycleSuffix))
		sb.WriteString(fmt.Sprintf("    %s-->>-%s: return\n", tgtAlias, srcAlias))
	}
}

func renderCommunicationDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("flowchart LR\n")
	var renderComm func(t *types.LayoutTree)
	renderComm = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := sanitizeName(node.ID)
			sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", alias, sanitizeMermaidLabel(node.Name)))
		}
		for _, child := range t.Children {
			renderComm(child)
		}
	}
	renderComm(tree)
	for i, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		_, _, symbol := parseFQN(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s -->|%d: %s()| %s\n", srcAlias, i+1, symbol, tgtAlias))
	}
}

func renderInteractionOverviewDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("flowchart TB\n")
	sb.WriteString("    Start([Start])\n")
	var renderOverview func(t *types.LayoutTree)
	renderOverview = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := sanitizeName(node.ID)
			sb.WriteString(fmt.Sprintf("    %s[\"ref: %s()\"]\n", alias, sanitizeMermaidLabel(node.Name)))
		}
		for _, child := range t.Children {
			renderOverview(child)
		}
	}
	renderOverview(tree)
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s --> %s\n", srcAlias, tgtAlias))
	}
}

func renderTimingDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("timeline\n")
	sb.WriteString("    title UML Timing Diagram\n")
	var renderTimeline func(t *types.LayoutTree, step int) int
	renderTimeline = func(t *types.LayoutTree, step int) int {
		if t == nil {
			return step
		}
		for _, node := range t.Nodes {
			sb.WriteString(fmt.Sprintf("    section %s\n", sanitizeMermaidLabel(node.Name)))
			sb.WriteString(fmt.Sprintf("        State %d : Active\n", step))
			step++
		}
		for _, child := range t.Children {
			step = renderTimeline(child, step)
		}
		return step
	}
	renderTimeline(tree, 0)
}

func getNodeTags(node *types.LayoutNode) string {
	var tags []string
	if node.IsGodObject {
		tags = append(tags, "WARNING: God Object")
	}
	if node.IsBottleneck {
		tags = append(tags, "Bridge: High Betweenness")
	}
	if node.IsHotspot {
		tags = append(tags, "Hotspot")
	}
	if node.PageRank > 0.1 {
		tags = append(tags, "Important")
	}
	if len(tags) > 0 {
		return " [" + strings.Join(tags, ", ") + "]"
	}
	return ""
}

func renderDataFlowDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("graph LR\n")
	var renderDFD func(t *types.LayoutTree)
	renderDFD = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := sanitizeName(node.ID)
			name := sanitizeMermaidLabel(node.Name)
			if node.Kind == "gm:Variable" || node.Kind == "gm:Parameter" {
				sb.WriteString(fmt.Sprintf("    %s((\"%s\"))\n", alias, name))
			} else {
				sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", alias, name))
			}
		}
		for _, child := range t.Children {
			renderDFD(child)
		}
	}
	renderDFD(tree)
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		arrow := " -.->|"
		if edge.Predicate == "gm:mutatesGlobal" || edge.Predicate == "gm:vulnerableTaint" {
			arrow = " ==>" + strings.TrimPrefix(edge.Predicate, "gm:") + "|| "
		}
		sb.WriteString(fmt.Sprintf("    %s %s %s\n", srcAlias, arrow, tgtAlias))
	}
}

func renderERDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("erDiagram\n")
	for _, node := range collectAllNodes(tree) {
		alias := sanitizeName(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		sb.WriteString(fmt.Sprintf("    %s {\n        string id PK\n", alias))
		if name != "" {
			sb.WriteString(fmt.Sprintf("        string name\n"))
		}
		sb.WriteString("    }\n")
	}
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s ||--o{ %s : has\n", srcAlias, tgtAlias))
	}
}

func renderMindmapDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("mindmap\n")
	var renderMindmap func(t *types.LayoutTree, indent string)
	renderMindmap = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := sanitizeName(node.ID)
			sb.WriteString(fmt.Sprintf("%s%s[ %s ]\n", indent, alias, sanitizeMermaidLabel(node.Name)))
		}
		for _, child := range t.Children {
			renderMindmap(child, indent+"    ")
		}
	}
	renderMindmap(tree, "")
}

func renderFlowchartFallback(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("flowchart TB\n")
	var renderFlow func(t *types.LayoutTree, indent string)
	renderFlow = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		if t.BoundaryName != "Root" {
			boundaryAlias := sanitizeName(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%ssubgraph %s[\"%s\"]\n", indent, boundaryAlias, t.BoundaryName))
			indent += "    "
		}
		for _, node := range t.Nodes {
			alias := sanitizeName(node.ID)
			name := sanitizeMermaidLabel(node.Name)
			if name == "" {
				name = sanitizeMermaidLabel(node.ID)
			}
			sb.WriteString(fmt.Sprintf("%s%s[\"%s\"]\n", indent, alias, name))
		}
		for _, child := range t.Children {
			renderFlow(child, indent)
		}
		if t.BoundaryName != "Root" {
			indent = indent[4:]
			sb.WriteString(fmt.Sprintf("%send\n", indent))
		}
	}
	renderFlow(tree, "")
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		renderEdgeStyles(edge, srcAlias, tgtAlias, sb)
	}
}

func renderInfrastructureDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("C4Context\n")
	title := getDiagramTitle(tree, "Infrastructure Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))
	for _, boundary := range tree.Children {
		if !isSystemBoundary(boundary) {
			continue
		}
		alias := sanitizeName(boundary.BoundaryName)
		name := sanitizeMermaidLabel(boundary.BoundaryName)
		sb.WriteString(fmt.Sprintf("    System_Boundary(%s_sys, \"%s Infrastructure\") {\n", alias, name))
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
	for _, node := range collectNodesByKind(tree, "gm:ExternalSystem") {
		tags := getNodeTags(node)
		sb.WriteString(fmt.Sprintf("    SystemExt(%s, \"%s\", \"External System%s\")\n",
			sanitizeName(node.ID), sanitizeMermaidLabel(node.Name), tags))
	}
	renderC4Edges(tree, sb)
}

func renderDependencyGraphDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("graph TD\n")
	for _, node := range collectAllNodes(tree) {
		alias := sanitizeName(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", alias, name))
	}
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s --> %s\n", srcAlias, tgtAlias))
	}
}

func renderHotspotComplexityDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("graph TD\n")
	for _, node := range collectAllNodes(tree) {
		alias := sanitizeName(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		if name == "" {
			name = alias
		}
		sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", alias, name))
	}
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s -.-> %s\n", srcAlias, tgtAlias))
	}
}

func renderCallGraphDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("graph TD\n")
	for _, node := range collectAllNodes(tree) {
		alias := sanitizeName(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", alias, name))
	}
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s --> %s\n", srcAlias, tgtAlias))
	}
}

func renderLayeredArchitectureDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("graph TD\n")
	var renderLayers func(t *types.LayoutTree, indent string)
	renderLayers = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		if t.BoundaryName != "Root" {
			boundaryAlias := sanitizeName(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%ssubgraph %s[\"%s\"]\n", indent, boundaryAlias, t.BoundaryName))
			indent += "    "
		}
		for _, node := range t.Nodes {
			alias := sanitizeName(node.ID)
			name := sanitizeMermaidLabel(node.Name)
			sb.WriteString(fmt.Sprintf("%s%s[\"%s\"]\n", indent, alias, name))
		}
		for _, child := range t.Children {
			renderLayers(child, indent)
		}
		if t.BoundaryName != "Root" {
			indent = indent[4:]
			sb.WriteString(fmt.Sprintf("%send\n", indent))
		}
	}
	renderLayers(tree, "")
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s -.-> %s\n", srcAlias, tgtAlias))
	}
}

func renderChangeImpactDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("graph TD\n")
	for _, node := range collectAllNodes(tree) {
		alias := sanitizeName(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		if node.IsHotspot {
			sb.WriteString(fmt.Sprintf("    %s[\"%s\"]:::hotspot\n", alias, name))
		} else {
			sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", alias, name))
		}
	}
	for _, edge := range tree.Edges {
		srcAlias := sanitizeName(edge.SourceID)
		tgtAlias := sanitizeName(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s --> %s\n", srcAlias, tgtAlias))
	}
	sb.WriteString("    classDef hotspot fill:#ffcccc\n")
}

func renderEdgesWithStyles(tree *types.LayoutTree, sb *strings.Builder) {
	for _, edge := range tree.Edges {
		src := sanitizeName(edge.SourceID)
		tgt := sanitizeName(edge.TargetID)
		renderEdgeStyles(edge, src, tgt, sb)
	}
}

func aliasEdgeStyle(_ types.LayoutEdge, src, tgt string, sb *strings.Builder) {
	sb.WriteString(fmt.Sprintf("    %s -.->[aliases] %s\n", src, tgt))
}

func eventEdgeStyle(_ types.LayoutEdge, src, tgt string, sb *strings.Builder) {
	sb.WriteString(fmt.Sprintf("    %s ==> %s\n", src, tgt))
}

// renderEdgeStyles writes the appropriate Mermaid edge syntax for the given edge into sb.
// IsCycle is evaluated first because it is not predicate-based and overrides all other styling.
func renderEdgeStyles(edge types.LayoutEdge, src, tgt string, sb *strings.Builder) {
	if edge.IsCycle {
		sb.WriteString(fmt.Sprintf("    %s x--x %s:::CYCLIC\n", src, tgt))
		sb.WriteString("    linkStyle default stroke:#ff0000\n")
		return
	}
	if strings.Contains(edge.Predicate, "vulnerable") || strings.Contains(edge.Predicate, "taint") {
		eventEdgeStyle(edge, src, tgt, sb)
		return
	}
	if edge.Predicate == "gm:aliasesType" || edge.Predicate == "gm:instantiatesGeneric" || edge.Predicate == "gm:pointsTo" {
		aliasEdgeStyle(edge, src, tgt, sb)
		return
	}
	if strings.HasPrefix(edge.Predicate, "gm:ffi") || strings.HasPrefix(edge.Predicate, "gm:cgo") {
		sb.WriteString(fmt.Sprintf("    %s =====|FFI: %s| %s\n", src, shortPredicate(edge.Predicate), tgt))
		return
	}
	if edge.Predicate == "gm:diInjects" {
		sb.WriteString(fmt.Sprintf("    %s --o %s\n", src, tgt))
		sb.WriteString(fmt.Sprintf("    %s -.->[injects] %s\n", src, tgt))
		return
	}
	sb.WriteString(fmt.Sprintf("    %s --> %s\n", src, tgt))
}
