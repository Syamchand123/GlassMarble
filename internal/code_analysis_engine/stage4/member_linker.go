package stage4

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// LinkMembersAndReturns is the STRUCTURAL spine producer
// (master_overhaul_plan.md §5.4.1 / W1-11, fixes A-06/A-16): for every
// type declaration it emits
//
//   - EdgeHasField    type → field (field nodes registered on demand)
//   - EdgeHasReceiver method → owner type (A-16)
//   - EdgeExtends/EdgeImplements from BaseTypes (target kind decides)
//   - EdgeImplements  from Implemented
//   - EdgeExtends with gm:embedding "true" for Go embedding (EmbeddedOf)
//
// and for every function/method:
//
//   - EdgeReturns (function → return-type node when resolvable)
//   - EdgeHasParam  (function → param node, param nodes registered)
//
// Targets resolve via GlobalDefinitionIndex + ownership; unresolvable
// members are skipped (no fabricated nodes beyond the field/param spines).
func LinkMembersAndReturns(stage3Out *stage3.Stage3Output, cpg *Stage4Output) {
	if stage3Out == nil || stage3Out.RootNode == nil || cpg == nil {
		return
	}

	traverseForMembers(stage3Out.RootNode, stage3Out.GlobalDefinitionIndex, cpg)
}

func traverseForMembers(dir *stage3.DirectoryNode, globalIndex map[string][]*stage2.GASTNode, cpg *Stage4Output) {
	if dir == nil {
		return
	}

	for _, file := range dir.Files {
		if file == nil || file.GASTRoot == nil {
			continue
		}
		if len(cpg.ModifiedFiles) > 0 && !cpg.ModifiedFiles[stage3.NormalizeRelativePath(file.RelativePath)] {
			continue
		}
		linkMembersInGAST(file.GASTRoot, file.RelativePath, globalIndex, cpg)
		// EdgeBelongsTo: symbol -> file edge for every node in this file (provenance ast).
		fileID := "file:" + stage3.NormalizeRelativePath(file.RelativePath)
		for nid, node := range cpg.GraphNodes {
			if node.FileSpec.Path == file.RelativePath && !strings.HasPrefix(nid, "file:") && !strings.HasPrefix(nid, "module:") && nid != fileID {
				cpg.AddEdgeProperties(nid, fileID, EdgeBelongsTo, 0, 1.0,
					map[string]string{ont.PredProvenance: "ast"})
			}
		}
	}

	for _, subDir := range dir.SubFolders {
		traverseForMembers(subDir, globalIndex, cpg)
	}
}

func linkMembersInGAST(node *stage2.GASTNode, relPath string, globalIndex map[string][]*stage2.GASTNode, cpg *Stage4Output) {
	if node == nil {
		return
	}

	switch node.Type {
	case stage2.GASTTypeDeclaration:
		linkTypeMembers(node, relPath, globalIndex, cpg)
	case stage2.GASTFunction:
		linkFunctionMembers(node, relPath, globalIndex, cpg)
	}

	for _, child := range node.Children {
		linkMembersInGAST(child, relPath, globalIndex, cpg)
	}
}

	func linkTypeMembers(node *stage2.GASTNode, relPath string, globalIndex map[string][]*stage2.GASTNode, cpg *Stage4Output) {
		sourceID := BuildUniversalID(relPath, "", node.Name)
		if sourceID == "" {
			return
		}

		for _, child := range node.Children {
			if child == nil {
				continue
			}

			switch {
case child.Type == stage2.GASTField && child.Kind == "embedding":
			// Go embedding (W1-11, §5.4.1): EdgeExtends @ 1.0 with the
			// gm:embedding marker (Bubble Tea tea.Model gap).
			// The embedded type is the field's name (or FieldType), not EmbeddedOf.
			target := child.Name
			if target == "" {
				target = child.FieldType
			}
			if targetID := resolveTypeToFQN(target, relPath, globalIndex, cpg); targetID != "" && targetID != sourceID {
				cpg.AddEdgeProperties(sourceID, targetID, EdgeExtends, int(child.StartLine), 1.0,
					map[string]string{
						ont.PredEmbedding:  "true",
						ont.PredProvenance: "ast",
					})
			}

			case child.Type == stage2.GASTField:
				fieldID := BuildUniversalID(relPath, node.Name, child.Name)
				ensureMemberNode(cpg, fieldID, "FIELD", child.Name, relPath, int(child.StartLine))
				cpg.AddEdgeProperties(sourceID, fieldID, EdgeHasField, int(child.StartLine), 1.0,
					map[string]string{ont.PredProvenance: "ast"})

			case child.Type == stage2.GASTFunction && child.Kind == "method":
				// Explicit receiver methods (re-parented by the normalizer) and
				// interface method specs (empty receiver, §5.2.2) both get the
				// explicit ownership edge (A-16).
				isInterfaceMethod := child.ReceiverType == "" && node.Kind == "interface"
				if child.ReceiverType != node.Name && !isInterfaceMethod {
					continue
				}
				methodID := BuildUniversalID(relPath, node.Name, stripDottedName(child.Name))
				cpg.AddEdgeProperties(methodID, sourceID, EdgeHasReceiver, int(child.StartLine), 1.0,
					map[string]string{ont.PredProvenance: "ast"})
			}
		}

		// EdgeMixes producer: placeholder for mixin/trait composition.
		// This producer will fire if a stage2 translator sets Properties["mixin"] == "true"
		// on a type node (e.g., for Ruby include/prepend/extend or similar constructs).
		if node.Properties["mixin"] == "true" && node.Type == stage2.GASTTypeDeclaration {
			targetID := BuildUniversalID(relPath, "", "mixin::"+node.Name)
			ensureMemberNode(cpg, targetID, "TYPE", "mixin::"+node.Name, relPath, int(node.StartLine))
			cpg.AddEdgeProperties(sourceID, targetID, EdgeMixes, int(node.StartLine), 1.0,
				map[string]string{ont.PredProvenance: "ast"})
		}

		// BaseTypes → EdgeExtends/EdgeImplements depending on target kind (A-02).
		for _, bt := range node.BaseTypes {
			targetID := resolveTypeToFQN(bt, relPath, globalIndex, cpg)
			if targetID == "" || targetID == sourceID {
				continue
			}
			edgeType := EdgeExtends
			if n, ok := cpg.GetNode(targetID); ok && n.Kind == "INTERFACE" {
				edgeType = EdgeImplements
			}
			cpg.AddEdgeProperties(sourceID, targetID, edgeType, int(node.StartLine), 1.0,
				map[string]string{ont.PredProvenance: "ast"})
		}

		// Implemented → EdgeImplements.
		for _, impl := range node.Implemented {
			targetID := resolveTypeToFQN(impl, relPath, globalIndex, cpg)
			if targetID == "" || targetID == sourceID {
				continue
			}
			cpg.AddEdgeProperties(sourceID, targetID, EdgeImplements, int(node.StartLine), 1.0,
				map[string]string{ont.PredProvenance: "ast"})
		}
	}

func linkFunctionMembers(node *stage2.GASTNode, relPath string, globalIndex map[string][]*stage2.GASTNode, cpg *Stage4Output) {
	funcID := BuildUniversalID(relPath, node.ReceiverType, stripDottedName(node.Name))
	if funcID == "" {
		return
	}

	// EdgeReturns: function → return-type node when the type resolves
	// (v2 GAST ReturnType, content DataType fallback).
	returnType := node.ReturnType
	if returnType == "" {
		returnType = node.DataType
	}
	if returnType != "" {
		if targetID := resolveTypeToFQN(returnType, relPath, globalIndex, cpg); targetID != "" && targetID != funcID {
			cpg.AddEdgeProperties(funcID, targetID, EdgeReturns, int(node.StartLine), 1.0,
				map[string]string{ont.PredProvenance: "ast"})
		}
	}

	// EdgeHasParam: function → param node (GASTParameter children).
	for _, child := range node.Children {
		if child == nil || child.Name == "" {
			continue
		}
		if child.Type != stage2.GASTParameter {
			continue
		}
		paramID := funcID + "::param:" + child.Name
		ensureMemberNode(cpg, paramID, "PARAM", child.Name, relPath, int(child.StartLine))
		cpg.AddEdgeProperties(funcID, paramID, EdgeHasParam, int(child.StartLine), 1.0,
			map[string]string{ont.PredProvenance: "ast"})
	}
}

// ensureMemberNode registers a structural spine node (FIELD/PARAM) without
// clobbering existing nodes (base/delta/db all count).
func ensureMemberNode(cpg *Stage4Output, id, kind, name, relPath string, line int) {
	if cpg.NodeExists(id) {
		return
	}
	cpg.GraphNodes[id] = &ResolvedNode{
		ID:   id,
		Kind: kind,
		Name: name,
		FileSpec: LocationMeta{
			Path:      relPath,
			LineStart: line,
		},
	}
}

// stripDottedName removes Go translator qualifiers ("Dispatcher.maxBytes" →
// "maxBytes") so member IDs match the builder's universal IDs.
func stripDottedName(name string) string {
	if idx := strings.LastIndex(name, "."); idx != -1 {
		return name[idx+1:]
	}
	return name
}
