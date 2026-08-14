package link

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
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
//   - EdgeMixes for Ruby/C++ mixin / friend / trait composition
//   - EdgeImports for C/C++ #include imports
//
// and for every function/method:
//
//   - EdgeReturns (function → return-type node when resolvable)
//   - EdgeHasParam  (function → param node, param nodes registered)
//
// Targets resolve via GlobalDefinitionIndex + ownership; unresolvable
// members are skipped (no fabricated nodes beyond the field/param spines).
func LinkMembersAndReturns(aggregateOut *aggregate.AggregateOutput, cpg *LinkOutput) {
	if aggregateOut == nil || aggregateOut.RootNode == nil || cpg == nil {
		return
	}

	traverseForMembers(aggregateOut.RootNode, aggregateOut.GlobalDefinitionIndex, cpg)
}

func traverseForMembers(dir *aggregate.DirectoryNode, globalIndex map[string][]*normalize.GASTNode, cpg *LinkOutput) {
	if dir == nil {
		return
	}

	for _, file := range dir.Files {
		if file == nil || file.GASTRoot == nil {
			continue
		}
		if len(cpg.ModifiedFiles) > 0 && !cpg.ModifiedFiles[aggregate.NormalizeRelativePath(file.RelativePath)] {
			continue
		}
		linkMembersInGAST(file.GASTRoot, file.RelativePath, globalIndex, cpg)
		// EdgeBelongsTo: symbol -> file edge for every node in this file (provenance ast).
		fileID := "file:" + aggregate.NormalizeRelativePath(file.RelativePath)
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

func linkMembersInGAST(node *normalize.GASTNode, relPath string, globalIndex map[string][]*normalize.GASTNode, cpg *LinkOutput) {
	if node == nil {
		return
	}

	switch node.Type {
	case normalize.GASTTypeDeclaration:
		linkTypeMembers(node, relPath, globalIndex, cpg)
	case normalize.GASTFunction:
		linkFunctionMembers(node, relPath, globalIndex, cpg)
	case normalize.GASTImport:
		linkImportMembers(node, relPath, globalIndex, cpg)
	}

	for _, child := range node.Children {
		linkMembersInGAST(child, relPath, globalIndex, cpg)
	}
}

func linkTypeMembers(node *normalize.GASTNode, relPath string, globalIndex map[string][]*normalize.GASTNode, cpg *LinkOutput) {
	sourceID := BuildUniversalID(relPath, "", node.Name)
	if sourceID == "" {
		return
	}

	for _, child := range node.Children {
		if child == nil {
			continue
		}

		switch {
		case child.Type == normalize.GASTField && child.Kind == "embedding":
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

		case child.Type == normalize.GASTField:
			fieldID := BuildUniversalID(relPath, node.Name, child.Name)
			ensureMemberNode(cpg, fieldID, "FIELD", child.Name, relPath, int(child.StartLine))
			cpg.AddEdgeProperties(sourceID, fieldID, EdgeHasField, int(child.StartLine), 1.0,
				map[string]string{ont.PredProvenance: "ast"})

		case child.Type == normalize.GASTFunction && (child.Kind == "method" || child.Kind == "method_declaration" || child.Kind == "method_spec"):
			// Explicit receiver methods, Rust impl method specs, and interface method specs
			// both get explicit ownership edge (A-16 / Rust impl).
			isInterfaceMethod := child.ReceiverType == "" && (node.Kind == "interface" || node.Kind == "trait")
			if child.ReceiverType != node.Name && child.ReceiverType != "Self" && !isInterfaceMethod {
				continue
			}
			methodID := BuildUniversalID(relPath, node.Name, stripDottedName(child.Name))
			cpg.AddEdgeProperties(methodID, sourceID, EdgeHasReceiver, int(child.StartLine), 1.0,
				map[string]string{ont.PredProvenance: "ast"})
		}
	}

	// EdgeMixes producer: for mixin/trait composition (Ruby include/prepend/extend, C++ friend).
	if (node.Properties["mixin"] == "true" || node.Properties["trait"] == "true") && node.Type == normalize.GASTTypeDeclaration {
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
		if n, ok := cpg.GetNode(targetID); ok && (n.Kind == "INTERFACE" || n.Kind == "PROTOCOL" || n.Kind == "TRAIT") {
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

func linkFunctionMembers(node *normalize.GASTNode, relPath string, globalIndex map[string][]*normalize.GASTNode, cpg *LinkOutput) {
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
		if child.Type != normalize.GASTParameter {
			continue
		}
		paramID := funcID + "::param:" + child.Name
		ensureMemberNode(cpg, paramID, "PARAM", child.Name, relPath, int(child.StartLine))
		cpg.AddEdgeProperties(funcID, paramID, EdgeHasParam, int(child.StartLine), 1.0,
			map[string]string{ont.PredProvenance: "ast"})
	}
}

// linkImportMembers connects C/C++ #include imports and module imports to file nodes.
func linkImportMembers(node *normalize.GASTNode, relPath string, globalIndex map[string][]*normalize.GASTNode, cpg *LinkOutput) {
	if node == nil || node.Name == "" {
		return
	}
	fileID := "file:" + aggregate.NormalizeRelativePath(relPath)
	targetID := "file:" + aggregate.NormalizeRelativePath(node.Name)
	// NodeExists (not HasNode) is deliberate: the import target is usually an
	// unmodified file that lives only in the persisted graph, and this is a
	// pure lookup — no node is skipped from the delta.
	if cpg.NodeExists(targetID) {
		cpg.AddEdgeProperties(fileID, targetID, EdgeDependsOn, int(node.StartLine), 1.0,
			map[string]string{ont.PredProvenance: "ast"})
	}
}

// ensureMemberNode registers a structural spine node (FIELD/PARAM) without
// clobbering nodes already emitted in this delta. The persisted graph is
// deliberately not consulted: modified-file nodes are swept on commit, so a
// base hit would mean the node is never re-added.
func ensureMemberNode(cpg *LinkOutput, id, kind, name, relPath string, line int) {
	if cpg.HasNode(id) {
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
