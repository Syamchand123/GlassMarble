package stage3

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/product/ont"
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

	// Mindmap rejects comments, so its footer (and any other comment) must not
	// be emitted (AUDIT Issue 2 Phase 2C-11).
	if t != types.Mindmap {
		renderSummaryFooter(tree, &sb)
	}
	return sb.String()
}

func renderClassDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("classDiagram\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	classes := make(map[string]*types.LayoutNode)
	methods := make(map[string][]string)

	var collectNodes func(t *types.LayoutTree)
	collectNodes = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			switch node.Kind {
			case ont.PredTypeDecl, ont.PredStruct, ont.PredClass, ont.PredInterface:
				classes[node.ID] = node
			}
		}
		for _, child := range t.Children {
			collectNodes(child)
		}
	}
	collectNodes(tree)

	var collectMethods func(t *types.LayoutTree)
	collectMethods = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			switch node.Kind {
			case ont.PredExecutable, ont.PredFunction, ont.PredMethod:
				_, rec, sym := parseFQN(node.ID)
				if rec != "" {
					parentID := findParentClassID(node.ID, classes)
					if parentID != "" {
						methods[parentID] = append(methods[parentID], sym)
					}
				}
			case ont.PredMember:
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
			collectMethods(child)
		}
	}
	collectMethods(tree)

	for id, class := range classes {
		classAlias := reg.alias(id)
		sb.WriteString(fmt.Sprintf("    class %s {\n", classAlias))
		kind := strings.TrimPrefix(class.Kind, ont.PrefixGM)
		switch kind {
		case "Struct", "Class", "Interface":
			sb.WriteString(fmt.Sprintf("        <<%s>>\n", strings.ToLower(kind)))
		default:
			sb.WriteString("        <<type>>\n")
		}
		lbl := sanitizeMermaidLabel(class.Name)
		if lbl != "" {
			sb.WriteString(fmt.Sprintf("        %s\n", lbl))
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
					srcAlias := reg.alias(src)
					tgtAlias := reg.alias(tgt)
					switch edge.Predicate {
					case ont.PredInheritsFrom:
						sb.WriteString(fmt.Sprintf("    %s --|> %s : inherits\n", srcAlias, tgtAlias))
					case ont.PredExtends:
						sb.WriteString(fmt.Sprintf("    %s --|> %s : extends\n", srcAlias, tgtAlias))
					case ont.PredImplements:
						sb.WriteString(fmt.Sprintf("    %s ..|> %s : implements\n", srcAlias, tgtAlias))
					case ont.PredMixes:
						sb.WriteString(fmt.Sprintf("    %s ..|> %s : mixes\n", srcAlias, tgtAlias))
					case ont.PredComposes:
						sb.WriteString(fmt.Sprintf("    %s --* %s : composes\n", srcAlias, tgtAlias))
					case ont.PredAggregates:
						sb.WriteString(fmt.Sprintf("    %s --o %s : aggregates\n", srcAlias, tgtAlias))
					case ont.PredHasMember, ont.PredHasField:
						sb.WriteString(fmt.Sprintf("    %s --* %s : has\n", srcAlias, tgtAlias))
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
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	var renderObjects func(t *types.LayoutTree)
	renderObjects = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			switch node.Kind {
			case ont.PredTypeDecl, ont.PredStruct, ont.PredClass, ont.PredInterface:
				alias := reg.alias(node.ID)
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
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s --- %s : link\n", srcAlias, tgtAlias))
	}
}

func renderUMLComponentDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("flowchart TB\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	var renderComp func(t *types.LayoutTree, indent string)
	renderComp = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		if t.BoundaryName != "Root" {
			boundaryAlias := reg.boundary(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%ssubgraph %s [\"<<component>> %s\"]\n", indent, boundaryAlias, t.BoundaryName))
			indent += "    "
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
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
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s ..> %s\n", srcAlias, tgtAlias))
	}
}

func renderUMLDeploymentDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("flowchart TB\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	var renderNode func(t *types.LayoutTree, indent string)
	renderNode = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		if t.BoundaryName != "Root" {
			boundaryAlias := reg.boundary(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%ssubgraph %s [\"<<device>> %s\"]\n", indent, boundaryAlias, t.BoundaryName))
			indent += "    "
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
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
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s ===|protocol| %s\n", srcAlias, tgtAlias))
	}
}

func renderUMLPackageDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("flowchart TB\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	var renderPack func(t *types.LayoutTree, indent string)
	renderPack = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		if t.BoundaryName != "Root" {
			boundaryAlias := reg.boundary(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%ssubgraph %s [\"folder: %s\"]\n", indent, boundaryAlias, t.BoundaryName))
			indent += "    "
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
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
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s -.->|import| %s\n", srcAlias, tgtAlias))
	}
}

func renderUMLCompositeDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("flowchart LR\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	var renderComposite func(t *types.LayoutTree, indent string)
	renderComposite = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		if t.BoundaryName != "Root" {
			boundaryAlias := reg.boundary(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%ssubgraph %s [\"composite structure: %s\"]\n", indent, boundaryAlias, t.BoundaryName))
			indent += "    "
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
			name := sanitizeMermaidLabel(node.Name)
			if node.Kind == ont.PredInterface || node.Kind == ont.PredPort {
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
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s --- %s\n", srcAlias, tgtAlias))
	}
}

func renderUMLProfileDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("classDiagram\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	var renderProfile func(t *types.LayoutTree)
	renderProfile = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
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
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	var renderUsecases func(t *types.LayoutTree)
	renderUsecases = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
			name := sanitizeMermaidLabel(node.Name)
			if node.Kind == ont.PredAnnotation {
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
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s --> %s\n", srcAlias, tgtAlias))
	}
}

func renderActivityDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("flowchart TB\n")
	sb.WriteString("    Start([Start])\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	var firstNodeAlias string
	var lastNodeAlias string
	var renderFlow func(t *types.LayoutTree)
	renderFlow = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
			if firstNodeAlias == "" {
				firstNodeAlias = alias
			}
			lastNodeAlias = alias
			name := sanitizeMermaidLabel(node.Name)
			if name == "" {
				name = sanitizeMermaidLabel(node.ID)
			}
			if node.Kind == ont.PredControlStructure {
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
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		if edge.Predicate == ont.PredSpawnsConcurrent {
			sb.WriteString(fmt.Sprintf("    %s -->|fork| %s\n", srcAlias, tgtAlias))
		} else if edge.Predicate == ont.PredControlFlowToTrue {
			sb.WriteString(fmt.Sprintf("    %s -->|true| %s\n", srcAlias, tgtAlias))
		} else if edge.Predicate == ont.PredControlFlowToFalse {
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
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	states := make(map[string]bool)
	hasIncoming := make(map[string]bool)
	hasOutgoing := make(map[string]bool)
	var renderStates func(t *types.LayoutTree)
	renderStates = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
			name := sanitizeMermaidLabel(node.Name)
			if name == "" {
				name = sanitizeMermaidLabel(node.ID)
			}
			sb.WriteString(fmt.Sprintf("    state %s as \"%s\"\n", alias, name))
			states[alias] = true
		}
		for _, child := range t.Children {
			renderStates(child)
		}
	}
	renderStates(tree)

	// Initial/final transitions come from graph structure, never fabricated:
	// the initial state is a state with no incoming edges, the final states
	// have no outgoing edges (AUDIT Issue 2 Phase 2C-14).
	if len(states) > 0 {
		initialWritten := false
		for _, edge := range tree.Edges {
			srcAlias := reg.alias(edge.SourceID)
			tgtAlias := reg.alias(edge.TargetID)
			if states[srcAlias] {
				hasOutgoing[srcAlias] = true
			}
			if states[tgtAlias] {
				hasIncoming[tgtAlias] = true
			}
		}
		for _, edge := range tree.Edges {
			srcAlias := reg.alias(edge.SourceID)
			tgtAlias := reg.alias(edge.TargetID)
			if states[srcAlias] && states[tgtAlias] && srcAlias != tgtAlias {
				label := sanitizeMermaidLabel(strings.TrimPrefix(edge.Predicate, ont.PrefixGM))
				sb.WriteString(fmt.Sprintf("    %s --> %s : %s\n", srcAlias, tgtAlias, label))
			}
		}
		// Deterministic order: sorted by alias.
		var ids []string
		for alias := range states {
			ids = append(ids, alias)
		}
		sort.Strings(ids)
		for _, alias := range ids {
			if !hasIncoming[alias] {
				sb.WriteString(fmt.Sprintf("    [*] --> %s\n", alias))
				initialWritten = true
			}
			if !hasOutgoing[alias] {
				sb.WriteString(fmt.Sprintf("    %s --> [*]\n", alias))
			}
		}
		_ = initialWritten
	} else {
		sb.WriteString("    [*] --> Idle\n")
		sb.WriteString("    Idle --> [*]\n")
	}
}

func renderSequenceDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("sequenceDiagram\n")
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
		_, _, symbol := parseFQN(edge.TargetID)
		symbolLabel := sanitizeMermaidLabel(symbol)
		if symbolLabel == "" {
			symbolLabel = sanitizeMermaidLabel(edge.TargetID)
		}
		arrow := "->>+"
		if edge.Predicate == ont.PredSpawnsConcurrent {
			arrow = "-)+"
		} else if edge.Predicate == ont.PredDispatchesEvent {
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
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	var renderComm func(t *types.LayoutTree)
	renderComm = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
			sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", alias, sanitizeMermaidLabel(node.Name)))
		}
		for _, child := range t.Children {
			renderComm(child)
		}
	}
	renderComm(tree)
	for i, edge := range tree.Edges {
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		_, _, symbol := parseFQN(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s -->|%d: %s()| %s\n", srcAlias, i+1, symbol, tgtAlias))
	}
}

func renderInteractionOverviewDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("flowchart TB\n")
	sb.WriteString("    Start([Start])\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	var renderOverview func(t *types.LayoutTree)
	renderOverview = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
			sb.WriteString(fmt.Sprintf("    %s[\"ref: %s()\"]\n", alias, sanitizeMermaidLabel(node.Name)))
		}
		for _, child := range t.Children {
			renderOverview(child)
		}
	}
	renderOverview(tree)
	for _, edge := range tree.Edges {
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s --> %s\n", srcAlias, tgtAlias))
	}
}

func renderTimingDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	// Mermaid has no native UML timing diagram; `timeline` is its closest
	// valid time-oriented diagram (AUDIT Issue 2 Phase 2C-14). Each
	// participant becomes a section with its states in execution order.
	sb.WriteString("timeline\n")
	sb.WriteString("    title UML Timing Diagram\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	participants := make(map[string][]string)
	var collectSteps func(t *types.LayoutTree)
	collectSteps = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
			name := sanitizeMermaidLabel(node.Name)
			if name == "" {
				name = sanitizeMermaidLabel(node.ID)
			}
			participants[alias] = append(participants[alias], name)
		}
		for _, child := range t.Children {
			collectSteps(child)
		}
	}
	collectSteps(tree)

	if len(participants) == 0 {
		sb.WriteString("    section System\n")
		sb.WriteString("        Idle : Active\n")
		return
	}

	var aliases []string
	for alias := range participants {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	step := 0
	for _, alias := range aliases {
		sb.WriteString(fmt.Sprintf("    section %s\n", alias))
		for _, name := range participants[alias] {
			sb.WriteString(fmt.Sprintf("        %s : %d\n", name, step))
			step++
		}
	}
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
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	var renderDFD func(t *types.LayoutTree)
	renderDFD = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
			name := sanitizeMermaidLabel(node.Name)
			if node.Kind == ont.PredVariable || node.Kind == ont.PredParameter {
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
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		label := shortPredicate(edge.Predicate)
		// Valid arrow grammar: link text markers must be closed (AUDIT Issue
		// 2 Phase 2A-1). `==>|label|` is only valid on the first of a pair,
		// so taint/mutation edges emit the doubled form.
		if edge.Predicate == ont.PredMutatesGlobal || edge.Predicate == ont.PredVulnerableTaint {
			sb.WriteString(fmt.Sprintf("    %s ==>|%s| %s\n", srcAlias, label, tgtAlias))
		} else {
			sb.WriteString(fmt.Sprintf("    %s -.->|%s| %s\n", srcAlias, label, tgtAlias))
		}
	}
}

func renderERDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("erDiagram\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	for _, node := range collectAllNodes(tree) {
		alias := reg.alias(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		sb.WriteString(fmt.Sprintf("    %s {\n        string id PK\n", alias))
		if name != "" {
			sb.WriteString("        string name\n")
		}
		sb.WriteString("    }\n")
	}
	for _, edge := range tree.Edges {
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s ||--o{ %s : has\n", srcAlias, tgtAlias))
	}
}

func renderMindmapDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("mindmap\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	// Mindmap requires a single root node (AUDIT Issue 2 Phase 2C-14).
	if tree == nil || (len(tree.Nodes) == 0 && len(tree.Children) == 0) {
		sb.WriteString("    root((Project))\n")
		return
	}
	var rootName string
	if tree != nil && tree.BoundaryName != "" && tree.BoundaryName != "Root" {
		rootName = tree.BoundaryName
	} else {
		rootName = "Project"
	}
	sb.WriteString(fmt.Sprintf("    root((%s))\n", sanitizeMermaidLabel(rootName)))
	var renderMindmap func(t *types.LayoutTree, indent string)
	renderMindmap = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
			sb.WriteString(fmt.Sprintf("%s%s[ %s ]\n", indent, alias, sanitizeMermaidLabel(node.Name)))
		}
		for _, child := range t.Children {
			sb.WriteString(fmt.Sprintf("%s%s[ %s ]\n", indent, reg.boundary(child.BoundaryName), sanitizeMermaidLabel(child.BoundaryName)))
			renderMindmap(child, indent+"    ")
		}
	}
	renderMindmap(tree, "    ")
}

func renderFlowchartFallback(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("flowchart TB\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	var renderFlow func(t *types.LayoutTree, indent string)
	renderFlow = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		if t.BoundaryName != "Root" {
			boundaryAlias := reg.boundary(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%ssubgraph %s[\"%s\"]\n", indent, boundaryAlias, t.BoundaryName))
			indent += "    "
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
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
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		renderEdgeStyles(edge, srcAlias, tgtAlias, sb)
	}
}

func renderInfrastructureDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("C4Context\n")
	title := getDiagramTitle(tree, "Infrastructure Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	for _, boundary := range tree.Children {
		if !isSystemBoundary(boundary) {
			continue
		}
		alias := reg.boundary(boundary.BoundaryName)
		name := sanitizeMermaidLabel(boundary.BoundaryName)
		sb.WriteString(fmt.Sprintf("    System_Boundary(%s_sys, \"%s Infrastructure\") {\n", alias, name))
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
	for _, node := range collectExternalNodes(tree) {
		tags := getNodeTags(node)
		sb.WriteString(fmt.Sprintf("    SystemExt(%s, \"%s\", \"External System%s\")\n",
			reg.alias(node.ID), sanitizeMermaidLabel(node.Name), tags))
	}
	renderC4Edges(tree, reg, sb)
}

func renderDependencyGraphDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("graph TD\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	for _, node := range collectAllNodes(tree) {
		alias := reg.alias(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", alias, name))
	}
	for _, edge := range tree.Edges {
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s --> %s\n", srcAlias, tgtAlias))
	}
}

func renderHotspotComplexityDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("graph TD\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	for _, node := range collectAllNodes(tree) {
		alias := reg.alias(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		if name == "" {
			name = alias
		}
		sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", alias, name))
	}
	for _, edge := range tree.Edges {
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s -.-> %s\n", srcAlias, tgtAlias))
	}
}

func renderCallGraphDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("graph TD\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	for _, node := range collectAllNodes(tree) {
		alias := reg.alias(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", alias, name))
	}
	for _, edge := range tree.Edges {
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s --> %s\n", srcAlias, tgtAlias))
	}
}

func renderLayeredArchitectureDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("graph TD\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	var renderLayers func(t *types.LayoutTree, indent string)
	renderLayers = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		if t.BoundaryName != "Root" {
			boundaryAlias := reg.boundary(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%ssubgraph %s[\"%s\"]\n", indent, boundaryAlias, t.BoundaryName))
			indent += "    "
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
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
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s -.-> %s\n", srcAlias, tgtAlias))
	}
}

func renderChangeImpactDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("graph TD\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)
	for _, node := range collectAllNodes(tree) {
		alias := reg.alias(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		if node.IsHotspot {
			sb.WriteString(fmt.Sprintf("    %s[\"%s\"]:::hotspot\n", alias, name))
		} else {
			sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", alias, name))
		}
	}
	for _, edge := range tree.Edges {
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
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
	if edge.Predicate == ont.PredAliasesType || edge.Predicate == ont.PredInstantiatesGeneric || edge.Predicate == ont.PredPointsTo {
		aliasEdgeStyle(edge, src, tgt, sb)
		return
	}
	if strings.HasPrefix(edge.Predicate, ont.PredFfi) || strings.HasPrefix(edge.Predicate, ont.PredCgo) {
		sb.WriteString(fmt.Sprintf("    %s =====|FFI: %s| %s\n", src, shortPredicate(edge.Predicate), tgt))
		return
	}
	if edge.Predicate == ont.PredDiInjects {
		sb.WriteString(fmt.Sprintf("    %s --o %s\n", src, tgt))
		sb.WriteString(fmt.Sprintf("    %s -.->[injects] %s\n", src, tgt))
		return
	}
	sb.WriteString(fmt.Sprintf("    %s --> %s\n", src, tgt))
}
