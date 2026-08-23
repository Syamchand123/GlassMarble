package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/product/ids"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// RenderDiagram converts a LayoutTree into Mermaid diagram markup for the given DiagramType with default theme.
func RenderDiagram(tree *types.LayoutTree, t types.DiagramType) string {
	return RenderDiagramWithTheme(tree, t, "modern", "auto")
}

// RenderDiagramWithTheme converts a LayoutTree into Mermaid diagram markup using the specified Theme and Direction.
func RenderDiagramWithTheme(tree *types.LayoutTree, t types.DiagramType, themeName, direction string) string {
	theme := GetTheme(themeName)
	var sb strings.Builder

	switch t {
	case types.UMLClass:
		renderClassDiagram(tree, &sb)
	case types.UMLObject:
		renderObjectDiagram(tree, &sb)
	case types.UMLComponent:
		renderFlowchartGraph(tree, &sb, theme, direction, "Component Diagram")
	case types.UMLDeployment:
		renderFlowchartGraph(tree, &sb, theme, direction, "Deployment Diagram")
	case types.UMLPackage:
		renderFlowchartGraph(tree, &sb, theme, direction, "Package Diagram")
	case types.UMLComposite:
		renderFlowchartGraph(tree, &sb, theme, direction, "Composite Structure Diagram")
	case types.UMLProfile:
		renderFlowchartGraph(tree, &sb, theme, direction, "Profile Diagram")
	case types.UMLUsecase:
		renderFlowchartGraph(tree, &sb, theme, direction, "Use Case Diagram")
	case types.UMLActivity:
		renderActivityDiagram(tree, &sb, theme, direction)
	case types.Flowchart:
		renderFlowchartGraph(tree, &sb, theme, direction, "Flowchart")
	case types.UMLState:
		renderStateDiagram(tree, &sb)
	case types.UMLSequence:
		renderSequenceDiagramWithTheme(tree, &sb, theme)
	case types.UMLCommunication:
		renderCommunicationDiagram(tree, &sb, theme, direction)
	case types.UMLInteractionOverview:
		renderInteractionOverviewDiagram(tree, &sb, theme, direction)
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
		renderDataFlowDiagram(tree, &sb, theme, direction)
	case types.Mindmap:
		renderMindmapDiagram(tree, &sb)
	case types.DependencyGraph:
		renderFlowchartGraph(tree, &sb, theme, direction, "Dependency Graph")
	case types.HotspotComplexity:
		renderFlowchartGraph(tree, &sb, theme, direction, "Hotspot Complexity Diagram")
	case types.CallGraph:
		renderFlowchartGraph(tree, &sb, theme, direction, "Call Graph")
	case types.LayeredArchitecture:
		renderFlowchartGraph(tree, &sb, theme, direction, "Layered Architecture Diagram")
	case types.ChangeImpact:
		renderFlowchartGraph(tree, &sb, theme, direction, "Change Impact Diagram")
	case types.Infrastructure:
		renderInfrastructureDiagram(tree, &sb)
	default:
		renderFlowchartGraph(tree, &sb, theme, direction, "Architecture Diagram")
	}

	if t != types.Mindmap {
		renderSummaryFooter(tree, &sb)
	}
	return sb.String()
}

func nodeSubtext(node *types.LayoutNode) string {
	if node == nil {
		return ""
	}
	var parts []string
	if node.PrimitiveType != "" && node.PrimitiveType != "Go/Core Component" {
		clean := strings.TrimPrefix(node.PrimitiveType, ont.PrefixGM)
		parts = append(parts, clean)
	}
	if node.IsHotspot {
		parts = append(parts, "Complexity Hotspot")
	}
	if node.IsGodObject {
		parts = append(parts, "God Object")
	}
	if node.PageRank > 0.05 {
		parts = append(parts, fmt.Sprintf("PR: %.2f", node.PageRank))
	}
	if len(parts) > 0 {
		return strings.Join(parts, " · ")
	}
	return ""
}

func renderFlowchartGraph(tree *types.LayoutTree, sb *strings.Builder, theme *Theme, dir, diagTitle string) {
	if dir == "" || dir == "auto" {
		dir = "TB"
	}
	sb.WriteString(fmt.Sprintf("flowchart %s\n", dir))
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	var renderNodes func(t *types.LayoutTree, indent string)
	renderNodes = func(t *types.LayoutTree, indent string) {
		if t == nil {
			return
		}
		if t.BoundaryName != "Root" && t.BoundaryName != "" && hasTreeNodes(t) {
			boundaryAlias := reg.boundary(t.BoundaryName)
			sb.WriteString(fmt.Sprintf("%ssubgraph %s [\"«MODULE» %s\"]\n", indent, boundaryAlias, sanitizeMermaidLabel(t.BoundaryName)))
			indent += "    "
		}
		for _, node := range t.Nodes {
			alias := reg.alias(node.ID)
			name := sanitizeMermaidLabel(node.Name)
			if name == "" {
				name = sanitizeMermaidLabel(node.ID)
			}
			arch := ClassifyNodeArchetype(node)
			subtext := nodeSubtext(node)
			nodeStmt := FormatMermaidNode(arch, alias, name, subtext)
			sb.WriteString(fmt.Sprintf("%s%s\n", indent, nodeStmt))
		}
		for _, child := range t.Children {
			renderNodes(child, indent)
		}
		if t.BoundaryName != "Root" && t.BoundaryName != "" && hasTreeNodes(t) {
			indent = indent[4:]
			sb.WriteString(fmt.Sprintf("%send\n", indent))
		}
	}
	renderNodes(tree, "    ")

	drawn := make(map[string]bool)
	for _, edge := range tree.Edges {
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		var buf strings.Builder
		renderEdgeStyles(edge, srcAlias, tgtAlias, &buf)
		block := strings.TrimRight(buf.String(), "\n")
		if block == "" || drawn[block] {
			continue
		}
		drawn[block] = true
		sb.WriteString(buf.String())
	}

	sb.WriteString(theme.EmitMermaidClassDefs())
}

type classMember struct {
	label      string
	typeName   string
	visibility string
}

func parseMembersFromCode(code string) (fields []classMember, methods []classMember) {
	if code == "" {
		return nil, nil
	}
	lines := strings.Split(code, "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "*") {
			continue
		}
		if idx := strings.Index(line, "//"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		if strings.Contains(line, "(") && strings.Contains(line, ")") {
			openP := strings.Index(line, "(")
			closeP := strings.LastIndex(line, ")")
			if openP > 0 && closeP > openP {
				namePart := strings.TrimSpace(line[:openP])
				fieldsInName := strings.Fields(namePart)
				if len(fieldsInName) > 0 {
					mName := fieldsInName[len(fieldsInName)-1]
					mName = strings.TrimPrefix(mName, "*")
					vis := "+"
					if len(mName) > 0 && mName[0] >= 'a' && mName[0] <= 'z' {
						vis = "-"
					}
					if strings.Contains(namePart, "private") {
						vis = "-"
					} else if strings.Contains(namePart, "public") {
						vis = "+"
					}
					ret := strings.TrimSpace(line[closeP+1:])
					ret = strings.TrimSuffix(ret, ";")
					ret = strings.TrimSuffix(ret, "{")
					ret = strings.TrimSpace(ret)
					if mName != "" && mName != "struct" && mName != "interface" && mName != "type" && mName != "func" {
						methods = append(methods, classMember{
							label:      mName,
							typeName:   ret,
							visibility: vis,
						})
						continue
					}
				}
			}
		}

		parts := strings.Fields(line)
		if len(parts) >= 2 {
			fName := parts[0]
			fType := parts[1]
			if strings.Contains(fType, "`") {
				fType = fType[:strings.Index(fType, "`")]
			}
			fType = strings.TrimSuffix(fType, ";")
			fType = strings.TrimSuffix(fType, ",")
			fName = strings.TrimSuffix(fName, ":")
			vis := "+"
			if len(fName) > 0 && fName[0] >= 'a' && fName[0] <= 'z' {
				vis = "-"
			}
			if fName == "private" || fName == "public" || fName == "protected" {
				if len(parts) >= 3 {
					vis = "+"
					if fName == "private" {
						vis = "-"
					}
					fType = parts[1]
					fName = strings.TrimSuffix(parts[2], ";")
				}
			}
			if fType == "struct" || fType == "interface" || fType == "{" || fType == "}" ||
				fName == "type" || fName == "struct" || fName == "interface" || fName == "{" || fName == "}" ||
				strings.HasPrefix(fName, "//") {
				continue
			}

			// Clean type annotations
			fType = strings.ReplaceAll(fType, "()", "[]")
			if strings.HasPrefix(fType, "[]") {
				fType = fType[2:] + "[]"
			}

			if fName != "" {
				fields = append(fields, classMember{
					label:      fName,
					typeName:   fType,
					visibility: vis,
				})
			}
		} else if len(parts) == 1 {
			emb := strings.TrimSuffix(parts[0], ";")
			emb = strings.TrimSuffix(emb, "{")
			emb = strings.TrimSuffix(emb, "}")
			if emb != "" && emb != "{" && emb != "}" && emb != "type" && emb != "struct" && emb != "interface" && !strings.HasPrefix(emb, "//") {
				fields = append(fields, classMember{
					label:      emb,
					typeName:   emb,
					visibility: "+",
				})
			}
		}
	}
	return fields, methods
}

func extractNameFromID(id string) string {
	norm := ids.NormalizeLegacyID(id)
	if c, err := ids.ParseCanonicalID(norm); err == nil && c.Symbol != "" {
		return c.Symbol
	}
	if idx := strings.LastIndex(id, "::"); idx != -1 {
		return id[idx+2:]
	}
	if idx := strings.LastIndex(id, "/"); idx != -1 {
		return id[idx+1:]
	}
	return id
}

func sanitizeERType(t string) string {
	if t == "" {
		return "string"
	}
	t = strings.TrimPrefix(t, "*")
	t = strings.TrimPrefix(t, "&")
	if strings.HasPrefix(t, "[]") {
		return sanitizeERType(t[2:]) + "_Array"
	}
	if strings.HasSuffix(t, "[]") {
		// parseMembersFromCode normalizes "[]X" to "X[]"; render both the
		// same way instead of mangling the bracket into a trailing "_".
		return sanitizeERType(strings.TrimSuffix(t, "[]")) + "_Array"
	}
	t = strings.ReplaceAll(t, ".", "_")
	t = strings.ReplaceAll(t, "[", "_")
	t = strings.ReplaceAll(t, "]", "")
	t = strings.ReplaceAll(t, " ", "_")
	t = strings.ReplaceAll(t, "*", "")
	var b strings.Builder
	for _, r := range t {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	res := b.String()
	if res == "" {
		return "string"
	}
	return res
}

func resolveNodeToEntity(id string, entities map[string]*types.LayoutNode) string {
	if _, ok := entities[id]; ok {
		return id
	}
	if idx := strings.LastIndex(id, "::"); idx != -1 {
		parent := id[:idx]
		if _, ok := entities[parent]; ok {
			return parent
		}
	}
	return ""
}

func memberVisibilityPrefix(explicitVis, label string) string {
	switch explicitVis {
	case "public", "Public", "+":
		return "+"
	case "private", "Private", "-":
		return "-"
	case "protected", "Protected", "#":
		return "#"
	case "package", "Package", "~":
		return "~"
	}
	if len(label) > 0 && label[0] >= 'A' && label[0] <= 'Z' {
		return "+"
	}
	return "-"
}

func renderClassDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("classDiagram\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	classes := make(map[string]*types.LayoutNode)
	fields := make(map[string][]classMember)
	methods := make(map[string][]classMember)
	allNodes := collectAllNodes(tree)

	for _, node := range allNodes {
		switch node.Kind {
		case ont.PredTypeDecl, ont.PredStruct, ont.PredClass, ont.PredInterface,
			"Struct", "Class", "Interface", "TypeDecl", "STRUCT", "CLASS", "INTERFACE", "TYPE_DECL":
			classes[node.ID] = node
			code := node.Code
			if code == "" && node.Properties != nil {
				code = node.Properties["content"]
			}
			if code != "" {
				pFields, pMethods := parseMembersFromCode(code)
				fields[node.ID] = append(fields[node.ID], pFields...)
				methods[node.ID] = append(methods[node.ID], pMethods...)
			}
		case ont.PredMember, "FIELD", "Field", "Member", "MEMBER":
			if idx := strings.LastIndex(node.ID, "::"); idx != -1 {
				parentID := node.ID[:idx]
				fields[parentID] = append(fields[parentID], classMember{
					label:      node.Name,
					typeName:   node.PrimitiveType,
					visibility: node.Visibility,
				})
			}
		case ont.PredMethod, "METHOD", "Method":
			if idx := strings.LastIndex(node.ID, "::"); idx != -1 {
				parentID := node.ID[:idx]
				methods[parentID] = append(methods[parentID], classMember{
					label:      node.Name,
					typeName:   node.PrimitiveType,
					visibility: node.Visibility,
				})
			}
		}
	}

	if len(classes) == 0 {
		var functions []*types.LayoutNode
		for _, node := range allNodes {
			switch node.Kind {
			case ont.PredFunction, ont.PredMethod, ont.PredExecutable,
				"FUNCTION", "METHOD", "EXECUTABLE", "Function", "Method":
				functions = append(functions, node)
			}
		}
		if len(functions) > 0 {
			modName := "Module"
			if tree != nil && tree.BoundaryName != "" && tree.BoundaryName != "Root" {
				modName = sanitizeMermaidLabel(tree.BoundaryName)
			} else if len(functions) > 0 && functions[0].ID != "" {
				if idx := strings.Index(functions[0].ID, "::"); idx != -1 {
					modName = sanitizeMermaidLabel(functions[0].ID[:idx])
				}
			}
			modAlias := reg.alias(modName)
			sb.WriteString(fmt.Sprintf("    class %s {\n", modAlias))
			sb.WriteString("        <<module>>\n")
			sort.Slice(functions, func(i, j int) bool { return functions[i].Name < functions[j].Name })
			for _, fn := range functions {
				fnName := sanitizeMermaidLabel(fn.Name)
				if fnName == "" {
					fnName = sanitizeMermaidLabel(extractNameFromID(fn.ID))
				}
				prefix := memberVisibilityPrefix(fn.Visibility, fnName)
				sb.WriteString(fmt.Sprintf("        %s%s()\n", prefix, fnName))
			}
			sb.WriteString("    }\n")
			return
		}

		sb.WriteString("    class EmptyScope {\n        <<empty>>\n    }\n")
		return
	}

	classIDs := make([]string, 0, len(classes))
	for id := range classes {
		classIDs = append(classIDs, id)
	}
	sort.Strings(classIDs)

	for _, id := range classIDs {
		class := classes[id]
		classAlias := reg.alias(id)
		sb.WriteString(fmt.Sprintf("    class %s {\n", classAlias))
		kind := strings.TrimPrefix(class.Kind, ont.PrefixGM)
		switch kind {
		case "Struct", "Class", "Interface", "TypeDecl", "STRUCT", "CLASS", "INTERFACE", "TYPE_DECL":
			sb.WriteString(fmt.Sprintf("        <<%s>>\n", strings.ToLower(kind)))
		default:
			sb.WriteString("        <<type>>\n")
		}

		// Render fields
		if fList, exists := fields[id]; exists {
			seenF := make(map[string]bool)
			for _, f := range fList {
				fLabel := sanitizeMermaidLabel(f.label)
				if fLabel == "" || seenF[fLabel] {
					continue
				}
				seenF[fLabel] = true
				prefix := memberVisibilityPrefix(f.visibility, fLabel)
				fType := f.typeName
				if fType != "" {
					sb.WriteString(fmt.Sprintf("        %s%s %s\n", prefix, sanitizeMermaidLabel(fType), fLabel))
				} else {
					sb.WriteString(fmt.Sprintf("        %s%s\n", prefix, fLabel))
				}
			}
		}

		// Render methods
		if mList, exists := methods[id]; exists {
			seenM := make(map[string]bool)
			for _, m := range mList {
				mLabel := sanitizeMermaidLabel(m.label)
				if mLabel == "" || seenM[mLabel] {
					continue
				}
				seenM[mLabel] = true
				prefix := memberVisibilityPrefix(m.visibility, mLabel)
				if m.typeName != "" {
					sb.WriteString(fmt.Sprintf("        %s%s() %s\n", prefix, mLabel, sanitizeMermaidLabel(m.typeName)))
				} else {
					sb.WriteString(fmt.Sprintf("        %s%s()\n", prefix, mLabel))
				}
			}
		}

		if class.PrimitiveType != "" {
			sb.WriteString(fmt.Sprintf("        %s\n", sanitizeMermaidLabel(class.PrimitiveType)))
		}
		sb.WriteString("    }\n")
	}

	sortedEdges := make([]types.LayoutEdge, len(tree.Edges))
	copy(sortedEdges, tree.Edges)
	sort.Slice(sortedEdges, func(i, j int) bool {
		if sortedEdges[i].SourceID != sortedEdges[j].SourceID {
			return sortedEdges[i].SourceID < sortedEdges[j].SourceID
		}
		if sortedEdges[i].TargetID != sortedEdges[j].TargetID {
			return sortedEdges[i].TargetID < sortedEdges[j].TargetID
		}
		return sortedEdges[i].Predicate < sortedEdges[j].Predicate
	})

	classIdx := buildClassIndex(classes)
	drawnRelations := make(map[string]bool)
	for _, edge := range sortedEdges {
		srcClasses := resolveNodeToClass(edge.SourceID, classes, classIdx)
		tgtClasses := resolveNodeToClass(edge.TargetID, classes, classIdx)
		for _, src := range srcClasses {
			for _, tgt := range tgtClasses {
				if src != tgt {
					relationKey := fmt.Sprintf("%s|%s|%s", src, edge.Predicate, tgt)
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

func renderActivityDiagram(tree *types.LayoutTree, sb *strings.Builder, theme *Theme, dir string) {
	if dir == "" || dir == "auto" {
		dir = "TB"
	}
	sb.WriteString(fmt.Sprintf("flowchart %s\n", dir))
	sb.WriteString("    Start([● Start]):::entrypoint\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	for _, node := range collectAllNodes(tree) {
		alias := reg.alias(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		if node.Kind == ont.PredFunction || node.Kind == ont.PredMethod || node.Kind == ont.PredExecutable {
			sb.WriteString(fmt.Sprintf("    %s[\"⚡ %s\"]:::service\n", alias, name))
		} else {
			sb.WriteString(fmt.Sprintf("    %s{\"⚖️ %s\"}:::gateway\n", alias, name))
		}
	}

	for _, edge := range tree.Edges {
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s --> %s\n", srcAlias, tgtAlias))
	}
	sb.WriteString("    End([■ End]):::entrypoint\n")
	sb.WriteString(theme.EmitMermaidClassDefs())
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
			arch := ClassifyNodeArchetype(node)
			sb.WriteString(fmt.Sprintf("    state \"%s %s\" as %s\n", arch.Stereotype(), name, alias))
			states[alias] = true
		}
		for _, child := range t.Children {
			renderStates(child)
		}
	}
	renderStates(tree)

	if len(states) == 0 {
		// The AKG has no state-transition nodes for this query; an empty
		// stateDiagram-v2 renders as a blank box. Emit an explicit note
		// instead of pretending there are states (GAP-ST-01).
		sb.WriteString("    state \"no state transitions detected in model\" as _no_state_data\n")
		sb.WriteString("    [*] --> _no_state_data\n")
		return
	}

	if len(states) > 0 {
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

		initialWritten := false
		for alias := range states {
			if !hasIncoming[alias] && !initialWritten {
				sb.WriteString(fmt.Sprintf("    [*] --> %s\n", alias))
				initialWritten = true
			}
		}
		if !initialWritten && len(states) > 0 {
			var first string
			for alias := range states {
				first = alias
				break
			}
			sb.WriteString(fmt.Sprintf("    [*] --> %s\n", first))
		}

		for _, edge := range tree.Edges {
			srcAlias := reg.alias(edge.SourceID)
			tgtAlias := reg.alias(edge.TargetID)
			label := sanitizeMermaidLabel(shortPredicate(edge.Predicate))
			if label != "" {
				sb.WriteString(fmt.Sprintf("    %s --> %s: %s\n", srcAlias, tgtAlias, label))
			} else {
				sb.WriteString(fmt.Sprintf("    %s --> %s\n", srcAlias, tgtAlias))
			}
		}

		for alias := range states {
			if !hasOutgoing[alias] {
				sb.WriteString(fmt.Sprintf("    %s --> [*]\n", alias))
			}
		}
	}
}

func renderSequenceDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	renderSequenceDiagramWithTheme(tree, sb, ThemeModern)
}

func renderSequenceDiagramWithTheme(tree *types.LayoutTree, sb *strings.Builder, theme *Theme) {
	sb.WriteString("sequenceDiagram\n")
	sb.WriteString("    autonumber\n")
	title := getDiagramTitle(tree, "Sequence Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	allNodes := collectAllNodes(tree)
	participants := make(map[string]bool)

	for _, node := range allNodes {
		alias := reg.alias(node.ID)
		if !participants[alias] {
			arch := ClassifyNodeArchetype(node)
			name := sanitizeMermaidLabel(node.Name)
			if name == "" {
				name = sanitizeMermaidLabel(node.ID)
			}
			label := fmt.Sprintf("\"<small>%s</small><br/>%s\"", arch.Stereotype(), name)
			if arch == ArchEntrypoint {
				sb.WriteString(fmt.Sprintf("    actor %s as %s\n", alias, label))
			} else {
				sb.WriteString(fmt.Sprintf("    participant %s as %s\n", alias, label))
			}
			participants[alias] = true
		}
	}

	if len(tree.Edges) == 0 {
		if len(allNodes) > 0 {
			first := reg.alias(allNodes[0].ID)
			sb.WriteString(fmt.Sprintf("    %s->>%s: (entry call)\n", first, first))
		}
		return
	}

	sb.WriteString("    rect rgba(240, 245, 255, 0.6)\n")
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
		sb.WriteString(fmt.Sprintf("        %s%s%s: %s%s\n", srcAlias, arrow, tgtAlias, symbolLabel, cycleSuffix))
		sb.WriteString(fmt.Sprintf("        %s-->>-%s: return\n", tgtAlias, srcAlias))
	}
	sb.WriteString("    end\n")
}

func renderCommunicationDiagram(tree *types.LayoutTree, sb *strings.Builder, theme *Theme, dir string) {
	if dir == "" || dir == "auto" {
		dir = "LR"
	}
	sb.WriteString(fmt.Sprintf("flowchart %s\n", dir))
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	for _, node := range collectAllNodes(tree) {
		alias := reg.alias(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		arch := ClassifyNodeArchetype(node)
		sb.WriteString(fmt.Sprintf("    %s\n", FormatMermaidNode(arch, alias, name, "")))
	}

	for i, edge := range tree.Edges {
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		_, _, symbol := parseFQN(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s -->|%d: %s()| %s\n", srcAlias, i+1, symbol, tgtAlias))
	}
	sb.WriteString(theme.EmitMermaidClassDefs())
}

func renderInteractionOverviewDiagram(tree *types.LayoutTree, sb *strings.Builder, theme *Theme, dir string) {
	if dir == "" || dir == "auto" {
		dir = "TB"
	}
	sb.WriteString(fmt.Sprintf("flowchart %s\n", dir))
	sb.WriteString("    Start([«START»]):::entrypoint\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	for _, node := range collectAllNodes(tree) {
		alias := reg.alias(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		sb.WriteString(fmt.Sprintf("    %s[[\"ref: %s()\"]]\n", alias, name))
	}

	for _, edge := range tree.Edges {
		srcAlias := reg.alias(edge.SourceID)
		tgtAlias := reg.alias(edge.TargetID)
		sb.WriteString(fmt.Sprintf("    %s --> %s\n", srcAlias, tgtAlias))
	}
	sb.WriteString(theme.EmitMermaidClassDefs())
}

func renderTimingDiagram(tree *types.LayoutTree, sb *strings.Builder) {
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

func renderDataFlowDiagram(tree *types.LayoutTree, sb *strings.Builder, theme *Theme, dir string) {
	if dir == "" || dir == "auto" {
		dir = "LR"
	}
	sb.WriteString(fmt.Sprintf("flowchart %s\n", dir))
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
				sb.WriteString(fmt.Sprintf("    %s((\"<small>«VAR»</small><br/>%s\")):::model\n", alias, name))
			} else {
				arch := ClassifyNodeArchetype(node)
				sb.WriteString(fmt.Sprintf("    %s\n", FormatMermaidNode(arch, alias, name, "")))
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
		if edge.Predicate == ont.PredMutatesGlobal || edge.Predicate == ont.PredVulnerableTaint {
			sb.WriteString(fmt.Sprintf("    %s ==>|%s| %s\n", srcAlias, label, tgtAlias))
		} else {
			sb.WriteString(fmt.Sprintf("    %s -.->|%s| %s\n", srcAlias, label, tgtAlias))
		}
	}
	sb.WriteString(theme.EmitMermaidClassDefs())
}

func renderERDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("erDiagram\n")
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	allNodes := collectAllNodes(tree)
	entities := make(map[string]*types.LayoutNode)
	fields := make(map[string][]classMember)

	for _, n := range allNodes {
		switch n.Kind {
		case ont.PredTypeDecl, ont.PredStruct, ont.PredClass, ont.PredInterface,
			"Struct", "Class", "Interface", "TypeDecl", "STRUCT", "CLASS", "INTERFACE", "TYPE_DECL",
			ont.PredVirtualDatabase, "VIRTUAL_DATABASE":
			entities[n.ID] = n
			code := n.Code
			if code == "" && n.Properties != nil {
				code = n.Properties["content"]
			}
			if code != "" {
				pFields, _ := parseMembersFromCode(code)
				fields[n.ID] = append(fields[n.ID], pFields...)
			}
		case ont.PredMember, "FIELD", "Field", "Member", "MEMBER":
			if idx := strings.LastIndex(n.ID, "::"); idx != -1 {
				parentID := n.ID[:idx]
				fields[parentID] = append(fields[parentID], classMember{
					label:      n.Name,
					typeName:   n.PrimitiveType,
					visibility: n.Visibility,
				})
			}
		}
	}

	if len(entities) == 0 {
		for _, n := range allNodes {
			if n.Kind != ont.PredMember && n.Kind != "FIELD" {
				entities[n.ID] = n
			}
		}
	}

	if len(entities) == 0 {
		sb.WriteString("    EMPTY_ENTITY {\n        string id PK\n    }\n")
		return
	}

	entityIDs := make([]string, 0, len(entities))
	for id := range entities {
		entityIDs = append(entityIDs, id)
	}
	sort.Strings(entityIDs)

	for _, id := range entityIDs {
		entity := entities[id]
		alias := reg.alias(id)
		name := sanitizeMermaidLabel(entity.Name)
		if name == "" {
			name = sanitizeMermaidLabel(extractNameFromID(id))
		}
		sb.WriteString(fmt.Sprintf("    %s {\n", alias))
		sb.WriteString("        string id PK\n")
		if eFields, ok := fields[id]; ok && len(eFields) > 0 {
			seen := make(map[string]bool)
			for _, f := range eFields {
				if f.label == "" || seen[f.label] {
					continue
				}
				seen[f.label] = true
				fType := sanitizeERType(f.typeName)
				if fType == "" {
					fType = "string"
				}
				fName := sanitizeMermaidLabel(f.label)
				sb.WriteString(fmt.Sprintf("        %s %s\n", fType, fName))
			}
		} else if name != "" {
			sb.WriteString(fmt.Sprintf("        string name \"%s\"\n", name))
		}
		sb.WriteString("    }\n")
	}

	drawn := make(map[string]bool)
	for _, edge := range tree.Edges {
		src := resolveNodeToEntity(edge.SourceID, entities)
		tgt := resolveNodeToEntity(edge.TargetID, entities)
		if src != "" && tgt != "" && src != tgt {
			relKey := src + "->" + tgt
			if drawn[relKey] {
				continue
			}
			drawn[relKey] = true
			srcAlias := reg.alias(src)
			tgtAlias := reg.alias(tgt)
			pred := shortPredicate(edge.Predicate)
			if pred == "" {
				pred = "relates"
			}
			sb.WriteString(fmt.Sprintf("    %s ||--o{ %s : \"%s\"\n", srcAlias, tgtAlias, sanitizeMermaidLabel(pred)))
		}
	}
}

func renderMindmapDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("mindmap\n")
	if tree == nil || (len(tree.Nodes) == 0 && len(tree.Children) == 0) {
		sb.WriteString("    root((Project))\n")
		return
	}
	var rootName string
	if tree.BoundaryName != "" && tree.BoundaryName != "Root" {
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
		for _, child := range t.Children {
			childName := sanitizeMermaidLabel(child.BoundaryName)
			if childName == "" {
				childName = "Package"
			}
			sb.WriteString(fmt.Sprintf("%s(\"%s\")\n", indent, childName))
			renderMindmap(child, indent+"    ")
		}
		for _, node := range t.Nodes {
			name := sanitizeMermaidLabel(node.Name)
			if name == "" {
				name = sanitizeMermaidLabel(node.ID)
			} else if node.ID != "" && node.ID != node.Name {
				name = fmt.Sprintf("%s (%s)", name, sanitizeMermaidLabel(node.ID))
			}
			sb.WriteString(fmt.Sprintf("%s[\"%s\"]\n", indent, name))
		}
	}
	renderMindmap(tree, "        ")
}

func renderInfrastructureDiagram(tree *types.LayoutTree, sb *strings.Builder) {
	sb.WriteString("C4Context\n")
	title := getDiagramTitle(tree, "Infrastructure Diagram")
	sb.WriteString(fmt.Sprintf("    title %s\n", title))
	reg := newAliasRegistry()
	registerTreeAliases(tree, reg)

	renderC4Blocks(tree, reg, sb, &c4BlockRenderer{
		indent: "    ",
		blockAlias: func(base string) string {
			return base + "_sys"
		},
		openBlock: func(alias, name string) string {
			return fmt.Sprintf("    System_Boundary(%s, \"%s Infrastructure\") {\n", alias, name)
		},
		closeBlock: func(indent string) string {
			return indent + "}\n"
		},
		renderNode:   renderContainerElement,
		renderFolded: renderFoldedContainerElement,
	})

	for _, node := range collectExternalNodes(tree) {
		tags := getNodeTags(node)
		nodeAlias := reg.alias(node.ID)
		reg.markDeclared(nodeAlias)
		sb.WriteString(fmt.Sprintf("    System_Ext(%s, \"%s\", \"External System%s\")\n",
			nodeAlias, sanitizeMermaidLabel(node.Name), tags))
	}
	renderC4Edges(tree, reg, sb)
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

func renderEdgesWithStyles(tree *types.LayoutTree, sb *strings.Builder) {
	for _, edge := range tree.Edges {
		src := sanitizeName(edge.SourceID)
		tgt := sanitizeName(edge.TargetID)
		renderEdgeStyles(edge, src, tgt, sb)
	}
}

func aliasEdgeStyle(_ types.LayoutEdge, src, tgt string, sb *strings.Builder) {
	sb.WriteString(fmt.Sprintf("    %s -.->|aliases| %s\n", src, tgt))
}

func eventEdgeStyle(_ types.LayoutEdge, src, tgt string, sb *strings.Builder) {
	sb.WriteString(fmt.Sprintf("    %s ==> %s\n", src, tgt))
}

func renderEdgeStyles(edge types.LayoutEdge, src, tgt string, sb *strings.Builder) {
	if edge.IsCycle {
		sb.WriteString(fmt.Sprintf("    %s ==>|«CYCLE»| %s\n", src, tgt))
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
		sb.WriteString(fmt.Sprintf("    %s ==>|FFI: %s| %s\n", src, shortPredicate(edge.Predicate), tgt))
		return
	}
	if edge.Predicate == ont.PredDiInjects {
		sb.WriteString(fmt.Sprintf("    %s --o %s\n", src, tgt))
		sb.WriteString(fmt.Sprintf("    %s -.->|injects| %s\n", src, tgt))
		return
	}
	sb.WriteString(fmt.Sprintf("    %s --> %s\n", src, tgt))
}
