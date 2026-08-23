package layout

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/product/ids"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// BuildDiagramAST converts a LayoutTree into a typed DiagramAST.
func BuildDiagramAST(tree *types.LayoutTree, diagType types.DiagramType, opts types.QueryOptions) *types.DiagramAST {
	if tree == nil {
		return &types.DiagramAST{
			Type:      diagType,
			Scope:     opts.Scope,
			ScopePath: opts.ScopePath,
			Direction: "TB",
			Root:      &types.ASTBoundary{ID: "root", RawName: "Root", Label: "Project", Kind: types.BoundarySystem},
		}
	}

	ast := &types.DiagramAST{
		Title:     getASTTitle(tree, diagType),
		Type:      diagType,
		Scope:     opts.Scope,
		ScopePath: opts.ScopePath,
		Direction: getASTDirection(diagType),
		Summary:   tree.Summary,
		Format:    opts.Format,
	}

	ast.Root = buildASTBoundary(tree, diagType)
	ast.Edges = buildASTEdges(tree.Edges, diagType)

	return ast
}

func getASTTitle(tree *types.LayoutTree, dt types.DiagramType) string {
	base := strings.ReplaceAll(string(dt), "_", " ")
	if tree.BoundaryName != "" && tree.BoundaryName != "Root" {
		return fmt.Sprintf("%s - %s", tree.BoundaryName, base)
	}
	return base
}

func getASTDirection(dt types.DiagramType) string {
	switch dt {
	case types.UMLComposite, types.DataFlow:
		return "LR"
	default:
		return "TB"
	}
}

func buildASTBoundary(tree *types.LayoutTree, dt types.DiagramType) *types.ASTBoundary {
	if tree == nil {
		return nil
	}

	b := &types.ASTBoundary{
		ID:      sanitizeASTID(tree.BoundaryName),
		RawName: tree.BoundaryName,
		Label:   tree.BoundaryName,
		Kind:    determineBoundaryKind(tree.BoundaryName, dt),
	}

	if b.Label == "" || b.Label == "Root" {
		b.Label = "Project"
	}

	for _, node := range tree.Nodes {
		b.Elements = append(b.Elements, buildASTElement(node, dt))
	}

	for _, child := range tree.Children {
		childB := buildASTBoundary(child, dt)
		if childB != nil {
			b.Children = append(b.Children, childB)
		}
	}

	return b
}

func determineBoundaryKind(name string, dt types.DiagramType) types.BoundaryKind {
	switch dt {
	case types.C4Context, types.C4Landscape:
		return types.BoundaryEnterprise
	case types.C4Container:
		return types.BoundarySystem
	case types.C4Component:
		return types.BoundaryContainer
	case types.UMLDeployment, types.C4Deployment:
		return types.BoundaryDevice
	case types.UMLPackage:
		return types.BoundaryPackage
	default:
		return types.BoundaryCluster
	}
}

func buildASTElement(node *types.LayoutNode, dt types.DiagramType) *types.ASTElement {
	if node == nil {
		return nil
	}

	elem := &types.ASTElement{
		ID:        sanitizeASTID(node.ID),
		RawID:     node.ID,
		Name:      node.Name,
		Kind:      mapNodeKindToElementKind(node.Kind, node.PrimitiveType),
		Tech:      node.PrimitiveType,
		IsHotspot: node.IsHotspot,
	}

	if elem.Name == "" {
		elem.Name = extractNameFromID(node.ID)
	}

	extractMembers(node, elem)
	return elem
}

func mapNodeKindToElementKind(kind string, prim string) types.ElementKind {
	if prim == "DATABASE" {
		return types.ElemDatabase
	}
	if prim == "MESSAGE_QUEUE" {
		return types.ElemQueue
	}
	if prim == "USER" || kind == ont.PredUser {
		return types.ElemActor
	}

	switch kind {
	case ont.PredStruct:
		return types.ElemStruct
	case ont.PredInterface:
		return types.ElemInterface
	case ont.PredClass:
		return types.ElemClass
	case ont.PredFunction:
		return types.ElemFunction
	case ont.PredMethod:
		return types.ElemMethod
	case ont.PredPackage:
		return types.ElemPackage
	case ont.PredModule:
		return types.ElemModule
	case ont.PredFile:
		return types.ElemFile
	default:
		return types.ElemGeneric
	}
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

func extractMembers(node *types.LayoutNode, elem *types.ASTElement) {
	if elem.Kind == types.ElemInterface || elem.Kind == types.ElemStruct || elem.Kind == types.ElemClass {
		code := node.Code
		if code == "" && node.Properties != nil {
			code = node.Properties["content"]
		}
		if code != "" {
			pFields, pMethods := parseMembersFromCodeAST(code)
			for _, pf := range pFields {
				elem.Fields = append(elem.Fields, types.ASTMember{
					Name:       pf.name,
					Type:       pf.typeName,
					Visibility: pf.vis,
				})
			}
			for _, pm := range pMethods {
				elem.Methods = append(elem.Methods, types.ASTMember{
					Name:       pm.name,
					Type:       pm.returnType,
					Visibility: pm.vis,
				})
			}
		}
		if len(elem.Methods) == 0 && elem.Kind == types.ElemInterface {
			vis := types.VisibilityPublic
			if node.Visibility == "private" || (len(node.Name) > 0 && node.Name[0] >= 'a' && node.Name[0] <= 'z') {
				vis = types.VisibilityPrivate
			}
			elem.Methods = append(elem.Methods, types.ASTMember{
				Name:       elem.Name,
				Visibility: vis,
			})
		}
	}
}

type astFieldInfo struct {
	name     string
	typeName string
	vis      types.MemberVisibility
}

type astMethodInfo struct {
	name       string
	returnType string
	vis        types.MemberVisibility
}

func parseMembersFromCodeAST(code string) ([]astFieldInfo, []astMethodInfo) {
	var fields []astFieldInfo
	var methods []astMethodInfo
	if code == "" {
		return fields, methods
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
					vis := types.VisibilityPublic
					if len(mName) > 0 && mName[0] >= 'a' && mName[0] <= 'z' {
						vis = types.VisibilityPrivate
					}
					ret := strings.TrimSpace(line[closeP+1:])
					ret = strings.TrimSuffix(ret, ";")
					ret = strings.TrimSuffix(ret, "{")
					ret = strings.TrimSpace(ret)
					if mName != "" && mName != "struct" && mName != "interface" && mName != "type" && mName != "func" {
						methods = append(methods, astMethodInfo{
							name:       mName,
							returnType: ret,
							vis:        vis,
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
			vis := types.VisibilityPublic
			if len(fName) > 0 && fName[0] >= 'a' && fName[0] <= 'z' {
				vis = types.VisibilityPrivate
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
				fields = append(fields, astFieldInfo{
					name:     fName,
					typeName: fType,
					vis:      vis,
				})
			}
		} else if len(parts) == 1 {
			emb := strings.TrimSuffix(parts[0], ";")
			emb = strings.TrimSuffix(emb, "{")
			emb = strings.TrimSuffix(emb, "}")
			if emb != "" && emb != "{" && emb != "}" && emb != "type" && emb != "struct" && emb != "interface" && !strings.HasPrefix(emb, "//") {
				fields = append(fields, astFieldInfo{
					name:     emb,
					typeName: emb,
					vis:      types.VisibilityPublic,
				})
			}
		}
	}
	return fields, methods
}

func buildASTEdges(edges []types.LayoutEdge, dt types.DiagramType) []types.ASTEdge {
	var astEdges []types.ASTEdge
	for _, edge := range edges {
		style, arrow := mapPredicateToStyleAndArrow(edge.Predicate, edge.IsCycle)
		label := strings.TrimPrefix(edge.Predicate, ont.PrefixGM)

		astEdges = append(astEdges, types.ASTEdge{
			SourceID:   sanitizeASTID(edge.SourceID),
			TargetID:   sanitizeASTID(edge.TargetID),
			Predicate:  edge.Predicate,
			Label:      label,
			Style:      style,
			ArrowKind:  arrow,
			Weight:     edge.Weight,
			IsCycle:    edge.IsCycle,
			LineNumber: edge.LineNumber,
		})
	}
	return astEdges
}

func mapPredicateToStyleAndArrow(pred string, isCycle bool) (types.EdgeStyle, types.ArrowKind) {
	if isCycle {
		return types.EdgeCross, types.ArrowBidirect
	}

	switch pred {
	case ont.PredInheritsFrom, ont.PredExtends:
		return types.EdgeSolid, types.ArrowInherit
	case ont.PredImplements:
		return types.EdgeDashed, types.ArrowInherit
	case ont.PredComposes, ont.PredContains:
		return types.EdgeSolid, types.ArrowCompose
	case ont.PredAggregates, ont.PredHasMember:
		return types.EdgeSolid, types.ArrowAggregate
	case ont.PredDependsOn, ont.PredImports, ont.PredReferences:
		return types.EdgeDashed, types.ArrowDependency
	case ont.PredDispatchesAsync, ont.PredSpawnsConcurrent:
		return types.EdgeThick, types.ArrowAsync
	default:
		return types.EdgeSolid, types.ArrowNormal
	}
}

func sanitizeASTID(id string) string {
	var sb strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	res := sb.String()
	for strings.Contains(res, "__") {
		res = strings.ReplaceAll(res, "__", "_")
	}
	res = strings.Trim(res, "_")
	if res == "" {
		res = "elem"
	}
	if res[0] >= '0' && res[0] <= '9' {
		res = "n_" + res
	}
	return res
}
