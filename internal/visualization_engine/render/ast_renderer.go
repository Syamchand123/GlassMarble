package aggregate

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// RenderDiagramFromAST renders a validated DiagramAST into the target format.
func RenderDiagramFromAST(ast *types.DiagramAST, format string) string {
	if ast == nil {
		return ""
	}

	ast = ValidateAndOptimizeAST(ast)

	switch strings.ToLower(format) {
	case "plantuml":
		return renderASTPlantUML(ast)
	case "dot", "graphviz":
		return renderASTDOT(ast)
	case "html":
		mermaidCode := renderASTMermaid(ast)
		return RenderHTMLStudio(mermaidCode, ast.Type, ast.Summary, "modern")
	default:
		return renderASTMermaid(ast)
	}
}

func renderASTMermaid(ast *types.DiagramAST) string {
	var sb strings.Builder

	switch ast.Type {
	case types.Mindmap:
		renderASTMermaidMindmap(ast, &sb)
		return sb.String()
	case types.ERDiagram:
		renderASTMermaidER(ast, &sb)
	case types.UMLSequence, types.C4Dynamic:
		renderASTMermaidSequence(ast, &sb)
	case types.C4Context, types.C4Landscape:
		renderASTMermaidC4Context(ast, &sb)
	case types.C4Container:
		renderASTMermaidC4Container(ast, &sb)
	case types.C4Component:
		renderASTMermaidC4Component(ast, &sb)
	case types.C4Deployment, types.UMLDeployment:
		renderASTMermaidC4Deployment(ast, &sb)
	case types.UMLClass, types.C4Code:
		renderASTMermaidClass(ast, &sb)
	default:
		renderASTMermaidFlowchart(ast, &sb)
	}

	renderASTSummaryFooter(ast, &sb)
	return sb.String()
}

func renderASTMermaidMindmap(ast *types.DiagramAST, sb *strings.Builder) {
	sb.WriteString("mindmap\n")
	rootName := "Project"
	if ast.Root != nil && ast.Root.Label != "" && ast.Root.Label != "Root" {
		rootName = ast.Root.Label
	}
	sb.WriteString(fmt.Sprintf("    root((%s))\n", rootName))

	var renderMindmapBoundary func(b *types.ASTBoundary, indent string)
	renderMindmapBoundary = func(b *types.ASTBoundary, indent string) {
		if b == nil {
			return
		}
		for _, child := range b.Children {
			sb.WriteString(fmt.Sprintf("%s(\"%s\")\n", indent, child.Label))
			renderMindmapBoundary(child, indent+"    ")
		}
		for _, elem := range b.Elements {
			sb.WriteString(fmt.Sprintf("%s[\"%s\"]\n", indent, elem.Name))
		}
	}
	renderMindmapBoundary(ast.Root, "        ")
}

func renderASTMermaidER(ast *types.DiagramAST, sb *strings.Builder) {
	sb.WriteString("erDiagram\n")
	elements := ast.CollectAllElements()
	var entities []*types.ASTElement
	for _, elem := range elements {
		if elem.Kind == types.ElemStruct || elem.Kind == types.ElemClass || elem.Kind == types.ElemDatabase {
			entities = append(entities, elem)
		}
	}
	if len(entities) == 0 {
		entities = elements
	}
	for _, elem := range entities {
		sb.WriteString(fmt.Sprintf("    %s {\n", elem.ID))
		sb.WriteString("        string id PK\n")
		if len(elem.Fields) > 0 {
			for _, f := range elem.Fields {
				fType := sanitizeERType(f.Type)
				if fType == "" {
					fType = "string"
				}
				sb.WriteString(fmt.Sprintf("        %s %s\n", fType, f.Name))
			}
		} else {
			sb.WriteString(fmt.Sprintf("        string name \"%s\"\n", elem.Name))
		}
		sb.WriteString("    }\n")
	}
	for _, edge := range ast.Edges {
		label := edge.Label
		if label == "" {
			label = "relates"
		}
		sb.WriteString(fmt.Sprintf("    %s ||--o{ %s : %s\n", edge.SourceID, edge.TargetID, sanitizeMermaidLabel(label)))
	}
}

func renderASTMermaidSequence(ast *types.DiagramAST, sb *strings.Builder) {
	sb.WriteString("sequenceDiagram\n")
	sb.WriteString("    autonumber\n")
	elements := ast.CollectAllElements()
	for _, elem := range elements {
		stereotype := "«SERVICE»"
		if elem.Kind == types.ElemActor {
			stereotype = "«ACTOR»"
		} else if elem.Kind == types.ElemDatabase {
			stereotype = "«DATASTORE»"
		}
		label := fmt.Sprintf("\"<small>%s</small><br/>%s\"", stereotype, elem.Name)
		sb.WriteString(fmt.Sprintf("    participant %s as %s\n", elem.ID, label))
	}
	if len(ast.Edges) == 0 && len(elements) > 0 {
		first := elements[0].ID
		sb.WriteString(fmt.Sprintf("    %s->>%s: (idle)\n", first, first))
		return
	}
	sb.WriteString("    rect rgba(240, 245, 255, 0.6)\n")
	for _, edge := range ast.Edges {
		label := edge.Label
		if label == "" {
			label = "calls"
		}
		sb.WriteString(fmt.Sprintf("        %s->>+%s: %s\n", edge.SourceID, edge.TargetID, label))
		sb.WriteString(fmt.Sprintf("        %s-->>-%s: return\n", edge.TargetID, edge.SourceID))
	}
	sb.WriteString("    end\n")
}

func renderASTMermaidC4Context(ast *types.DiagramAST, sb *strings.Builder) {
	sb.WriteString("C4Context\n")
	sb.WriteString(fmt.Sprintf("    title %s\n", ast.Title))
	declared := make(map[string]bool)

	if ast.Root != nil {
		for _, b := range ast.Root.Children {
			declared[b.ID] = true
			sb.WriteString(fmt.Sprintf("    Enterprise_Boundary(%s_ent, \"%s Enterprise\") {\n", b.ID, b.Label))
			sb.WriteString(fmt.Sprintf("        System(%s, \"%s\", \"System Module\")\n", b.ID, b.Label))
			for _, elem := range b.Elements {
				declared[elem.ID] = true
				if elem.IsExternal {
					sb.WriteString(fmt.Sprintf("        System_Ext(%s, \"%s\", \"External System\")\n", elem.ID, elem.Name))
				} else if elem.Kind == types.ElemDatabase {
					sb.WriteString(fmt.Sprintf("        SystemDb(%s, \"%s\", \"Database\")\n", elem.ID, elem.Name))
				}
			}
			sb.WriteString("    }\n")
		}
		for _, elem := range ast.Root.Elements {
			declared[elem.ID] = true
			if elem.Kind == types.ElemActor {
				sb.WriteString(fmt.Sprintf("    Person(%s, \"%s\", \"User / Client\")\n", elem.ID, elem.Name))
			}
		}
	}

	for _, edge := range ast.Edges {
		if !declared[edge.SourceID] || !declared[edge.TargetID] {
			continue
		}
		label := edge.Label
		if label == "" {
			label = "relates"
		}
		sb.WriteString(fmt.Sprintf("    Rel(%s, %s, \"%s\")\n", edge.SourceID, edge.TargetID, label))
	}
}

func renderASTMermaidC4Container(ast *types.DiagramAST, sb *strings.Builder) {
	sb.WriteString("C4Container\n")
	sb.WriteString(fmt.Sprintf("    title %s\n", ast.Title))
	declared := make(map[string]bool)

	if ast.Root != nil {
		for _, b := range ast.Root.Children {
			sb.WriteString(fmt.Sprintf("    System_Boundary(%s_sys, \"%s System\") {\n", b.ID, b.Label))
			for _, sub := range b.Children {
				declared[sub.ID] = true
				tech := sub.Tech
				if tech == "" {
					tech = "Go Module"
				}
				sb.WriteString(fmt.Sprintf("        Container(%s, \"%s\", \"%s\", \"Application Container\")\n", sub.ID, sub.Label, tech))
			}
			for _, elem := range b.Elements {
				declared[elem.ID] = true
				tech := elem.Tech
				if tech == "" {
					tech = "Go"
				}
				if elem.Kind == types.ElemDatabase {
					sb.WriteString(fmt.Sprintf("        ContainerDb(%s, \"%s\", \"%s\", \"Data Store\")\n", elem.ID, elem.Name, tech))
				} else {
					sb.WriteString(fmt.Sprintf("        Container(%s, \"%s\", \"%s\", \"Container Component\")\n", elem.ID, elem.Name, tech))
				}
			}
			sb.WriteString("    }\n")
		}
		if len(ast.Root.Children) == 0 && len(ast.Root.Elements) > 0 {
			sb.WriteString(fmt.Sprintf("    System_Boundary(sb_root_sys, \"%s System\") {\n", ast.Root.Label))
			for _, elem := range ast.Root.Elements {
				declared[elem.ID] = true
				sb.WriteString(fmt.Sprintf("        Container(%s, \"%s\", \"Go\", \"Component\")\n", elem.ID, elem.Name))
			}
			sb.WriteString("    }\n")
		}
	}

	for _, edge := range ast.Edges {
		if !declared[edge.SourceID] || !declared[edge.TargetID] {
			continue
		}
		label := edge.Label
		if label == "" {
			label = "uses"
		}
		sb.WriteString(fmt.Sprintf("    Rel(%s, %s, \"%s\")\n", edge.SourceID, edge.TargetID, label))
	}
}

func renderASTMermaidC4Component(ast *types.DiagramAST, sb *strings.Builder) {
	sb.WriteString("C4Component\n")
	sb.WriteString(fmt.Sprintf("    title %s\n", ast.Title))
	declared := make(map[string]bool)

	var renderTree func(b *types.ASTBoundary, indent string)
	renderTree = func(b *types.ASTBoundary, indent string) {
		if b == nil || !b.HasElements() {
			return
		}
		if b.RawName != "Root" {
			sb.WriteString(fmt.Sprintf("%sContainer_Boundary(%s, \"%s\") {\n", indent, b.ID, b.Label))
			indent += "    "
		}
		for _, elem := range b.Elements {
			declared[elem.ID] = true
			tech := elem.Tech
			if tech == "" {
				tech = "Go/Core Component"
			}
			if elem.Kind == types.ElemDatabase {
				sb.WriteString(fmt.Sprintf("%sComponentDb(%s, \"%s\", \"%s\", \"Data Store\")\n", indent, elem.ID, elem.Name, tech))
			} else if elem.IsExternal {
				sb.WriteString(fmt.Sprintf("%sComponent_Ext(%s, \"%s\", \"%s\", \"External Integration\")\n", indent, elem.ID, elem.Name, tech))
			} else {
				sb.WriteString(fmt.Sprintf("%sComponent(%s, \"%s\", \"%s\", \"Architectural Component\")\n", indent, elem.ID, elem.Name, tech))
			}
		}
		for _, child := range b.Children {
			renderTree(child, indent)
		}
		if b.RawName != "Root" {
			indent = indent[:len(indent)-4]
			sb.WriteString(fmt.Sprintf("%s}\n", indent))
		}
	}

	renderTree(ast.Root, "    ")

	for _, edge := range ast.Edges {
		if !declared[edge.SourceID] || !declared[edge.TargetID] {
			continue
		}
		label := edge.Label
		if label == "" {
			label = "calls"
		}
		sb.WriteString(fmt.Sprintf("    Rel(%s, %s, \"%s\")\n", edge.SourceID, edge.TargetID, label))
	}
}

func renderASTMermaidC4Deployment(ast *types.DiagramAST, sb *strings.Builder) {
	sb.WriteString("C4Deployment\n")
	sb.WriteString(fmt.Sprintf("    title %s\n", ast.Title))
	declared := make(map[string]bool)

	if ast.Root != nil {
		for _, b := range ast.Root.Children {
			sb.WriteString(fmt.Sprintf("    Deployment_Node(%s_node, \"%s Node\", \"Execution Environment\") {\n", b.ID, b.Label))
			for _, elem := range b.Elements {
				declared[elem.ID] = true
				tech := elem.Tech
				if tech == "" {
					tech = "Go Runtime"
				}
				if elem.Kind == types.ElemDatabase {
					sb.WriteString(fmt.Sprintf("        ContainerDb(%s, \"%s\", \"%s\", \"Data Store\")\n", elem.ID, elem.Name, tech))
				} else {
					sb.WriteString(fmt.Sprintf("        Container(%s, \"%s\", \"%s\", \"Deployment Artifact\")\n", elem.ID, elem.Name, tech))
				}
			}
			sb.WriteString("    }\n")
		}
	}

	for _, edge := range ast.Edges {
		if !declared[edge.SourceID] || !declared[edge.TargetID] {
			continue
		}
		label := edge.Label
		if label == "" {
			label = "serves"
		}
		sb.WriteString(fmt.Sprintf("    Rel(%s, %s, \"%s\")\n", edge.SourceID, edge.TargetID, label))
	}
}

func renderASTMermaidClass(ast *types.DiagramAST, sb *strings.Builder) {
	sb.WriteString("classDiagram\n")
	elements := ast.CollectAllElements()
	if len(elements) == 0 {
		sb.WriteString("    class EmptyScope {\n        <<empty>>\n    }\n")
		return
	}

	structCount := 0
	for _, elem := range elements {
		if elem.Kind == types.ElemStruct || elem.Kind == types.ElemClass || elem.Kind == types.ElemInterface {
			structCount++
			sb.WriteString(fmt.Sprintf("    class %s {\n", elem.ID))
			if elem.Kind == types.ElemInterface {
				sb.WriteString("        <<interface>>\n")
			} else {
				sb.WriteString("        <<struct>>\n")
			}
			for _, m := range elem.Methods {
				sb.WriteString(fmt.Sprintf("        %s%s()\n", m.Visibility, m.Name))
			}
			for _, f := range elem.Fields {
				sb.WriteString(fmt.Sprintf("        %s%s : %s\n", f.Visibility, f.Name, f.Type))
			}
			sb.WriteString("    }\n")
		}
	}

	if structCount == 0 {
		var functions []*types.ASTElement
		for _, elem := range elements {
			if elem.Kind == types.ElemFunction || elem.Kind == types.ElemMethod || elem.Kind == types.ElemGeneric {
				functions = append(functions, elem)
			}
		}
		if len(functions) > 0 {
			modName := "Module"
			if ast.Title != "" {
				modName = sanitizeMermaidLabel(ast.Title)
			}
			sb.WriteString(fmt.Sprintf("    class %s {\n", modName))
			sb.WriteString("        <<module>>\n")
			for _, fn := range functions {
				sb.WriteString(fmt.Sprintf("        +%s()\n", fn.Name))
			}
			sb.WriteString("    }\n")
			return
		}

		sb.WriteString("    class EmptyScope {\n        <<empty>>\n    }\n")
		return
	}

	for _, edge := range ast.Edges {
		switch edge.ArrowKind {
		case types.ArrowInherit:
			sb.WriteString(fmt.Sprintf("    %s --|> %s : inherits\n", edge.SourceID, edge.TargetID))
		case types.ArrowCompose:
			sb.WriteString(fmt.Sprintf("    %s --* %s : composes\n", edge.SourceID, edge.TargetID))
		case types.ArrowAggregate:
			sb.WriteString(fmt.Sprintf("    %s --o %s : aggregates\n", edge.SourceID, edge.TargetID))
		case types.ArrowDependency:
			sb.WriteString(fmt.Sprintf("    %s ..> %s : uses\n", edge.SourceID, edge.TargetID))
		default:
			sb.WriteString(fmt.Sprintf("    %s --> %s\n", edge.SourceID, edge.TargetID))
		}
	}
}

func renderASTMermaidFlowchart(ast *types.DiagramAST, sb *strings.Builder) {
	dir := ast.Direction
	if dir == "" {
		dir = "TB"
	}
	sb.WriteString(fmt.Sprintf("flowchart %s\n", dir))

	var renderFlowBoundary func(b *types.ASTBoundary, indent string)
	renderFlowBoundary = func(b *types.ASTBoundary, indent string) {
		if b == nil || !b.HasElements() {
			return
		}
		if b.RawName != "Root" {
			sb.WriteString(fmt.Sprintf("%ssubgraph %s[\"«MODULE» %s\"]\n", indent, b.ID, b.Label))
			indent += "    "
		}
		for _, elem := range b.Elements {
			if elem.Kind == types.ElemDatabase {
				sb.WriteString(fmt.Sprintf("%s%s[(\"<small>«DATASTORE»</small><br/><b>%s</b>\")]:::datastore\n", indent, elem.ID, elem.Name))
			} else if elem.Kind == types.ElemActor {
				sb.WriteString(fmt.Sprintf("%s%s([\"<small>«ENTRYPOINT»</small><br/><b>%s</b>\"]):::entrypoint\n", indent, elem.ID, elem.Name))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s[\"<small>«SERVICE»</small><br/><b>%s</b>\"]:::service\n", indent, elem.ID, elem.Name))
			}
		}
		for _, child := range b.Children {
			renderFlowBoundary(child, indent)
		}
		if b.RawName != "Root" {
			indent = indent[:len(indent)-4]
			sb.WriteString(fmt.Sprintf("%send\n", indent))
		}
	}

	renderFlowBoundary(ast.Root, "    ")

	for _, edge := range ast.Edges {
		if edge.IsCycle {
			sb.WriteString(fmt.Sprintf("    %s ==>|«CYCLE»| %s\n", edge.SourceID, edge.TargetID))
		} else if edge.Style == types.EdgeDashed {
			sb.WriteString(fmt.Sprintf("    %s -.-> %s\n", edge.SourceID, edge.TargetID))
		} else if edge.Style == types.EdgeThick {
			sb.WriteString(fmt.Sprintf("    %s ==> %s\n", edge.SourceID, edge.TargetID))
		} else {
			sb.WriteString(fmt.Sprintf("    %s --> %s\n", edge.SourceID, edge.TargetID))
		}
	}

	sb.WriteString(ThemeModern.EmitMermaidClassDefs())
}

func renderASTPlantUML(ast *types.DiagramAST) string {
	var sb strings.Builder
	sb.WriteString("@startuml\n")
	sb.WriteString(ThemeModern.EmitPlantUMLSkinparams())
	sb.WriteString(fmt.Sprintf("title %s\n", ast.Title))

	elements := ast.CollectAllElements()
	for _, elem := range elements {
		if elem.Kind == types.ElemInterface {
			sb.WriteString(fmt.Sprintf("interface \"%s\" as %s\n", elem.Name, elem.ID))
		} else if elem.Kind == types.ElemDatabase {
			sb.WriteString(fmt.Sprintf("database \"%s\" as %s\n", elem.Name, elem.ID))
		} else {
			sb.WriteString(fmt.Sprintf("class \"%s\" as %s\n", elem.Name, elem.ID))
		}
	}

	for _, edge := range ast.Edges {
		sb.WriteString(fmt.Sprintf("%s --> %s : %s\n", edge.SourceID, edge.TargetID, edge.Label))
	}

	if ast.Summary != nil {
		s := ast.Summary
		sb.WriteString(fmt.Sprintf("' Graph Summary: %d nodes, %d edges, density=%.4f, diameter=%d, avg_path=%.2f, clusters=%d, largest_scc=%d, god_objects=%d, components=%d\n",
			s.NodeCount, s.EdgeCount, s.Density, s.Diameter, s.AvgPathLength, s.ClusterCount, s.LargestSCCSize, s.GodObjectCount, s.ConnectedComponents))
	}

	sb.WriteString("@enduml\n")
	return sb.String()
}

func renderASTDOT(ast *types.DiagramAST) string {
	var sb strings.Builder
	sb.WriteString("digraph G {\n")
	sb.WriteString("    rankdir=TB;\n")
	sb.WriteString(ThemeModern.EmitDOTGraphAttrs())

	elements := ast.CollectAllElements()
	for _, elem := range elements {
		sb.WriteString(fmt.Sprintf("    %s [label=\"%s\"];\n", elem.ID, elem.Name))
	}

	for _, edge := range ast.Edges {
		sb.WriteString(fmt.Sprintf("    %s -> %s [label=\"%s\"];\n", edge.SourceID, edge.TargetID, edge.Label))
	}

	if ast.Summary != nil {
		s := ast.Summary
		sb.WriteString(fmt.Sprintf("    // Graph Summary: %d nodes, %d edges, density=%.4f, diameter=%d, avg_path=%.2f, clusters=%d, largest_scc=%d, god_objects=%d, components=%d\n",
			s.NodeCount, s.EdgeCount, s.Density, s.Diameter, s.AvgPathLength, s.ClusterCount, s.LargestSCCSize, s.GodObjectCount, s.ConnectedComponents))
	}

	sb.WriteString("}\n")
	return sb.String()
}

func renderASTSummaryFooter(ast *types.DiagramAST, sb *strings.Builder) {
	if ast == nil || ast.Summary == nil || ast.Type == types.Mindmap {
		return
	}
	s := ast.Summary
	sb.WriteString(fmt.Sprintf("    %%%% Graph Summary: %d nodes, %d edges, density=%.4f, diameter=%d, avg_path=%.2f, clusters=%d, largest_scc=%d, god_objects=%d, components=%d\n",
		s.NodeCount, s.EdgeCount, s.Density, s.Diameter, s.AvgPathLength, s.ClusterCount, s.LargestSCCSize, s.GodObjectCount, s.ConnectedComponents))
}
