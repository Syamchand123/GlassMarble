package stage4

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// BuildUniversalID creates a globally unique node ID formatted as `path::Symbol` or `path::Receiver::Method`.
// Example: path="src/store.go", receiver="PostgresStore", name="Save" -> "src/store.go::PostgresStore::Save"
// Example: path="sample.py", receiver="", name="DatabaseConnector" -> "sample.py::DatabaseConnector"
func BuildUniversalID(relPath, receiver, name string) string {
	cleanPath := stage3.NormalizeRelativePath(relPath)
	if cleanPath == "" {
		cleanPath = "root"
	}

	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		cleanName = "anonymous"
	}

	if receiver != "" {
		return fmt.Sprintf("%s::%s::%s", cleanPath, receiver, cleanName)
	}

	return fmt.Sprintf("%s::%s", cleanPath, cleanName)
}

// InitialGraphBuilder traverses Stage 3's DirectoryNode topology and registers all vertices into GraphNodes.
type InitialGraphBuilder struct {
	output *Stage4Output
	modSet map[string]bool
}

func (b *InitialGraphBuilder) registerNode(baseID string, node *ResolvedNode, gastNode *stage2.GASTNode) string {
	finalID := baseID
	
	// Step 4.1: Cryptographic AST Hashing for Delta Stability
	// If it's an anonymous function, shadowed variable, or collision, we hash the syntax structure.
	if strings.Contains(baseID, "anonymous") || gastNode.Name == "" || gastNode.Name == "anonymous" {
		hash := generateASTHash(gastNode)
		finalID = fmt.Sprintf("%s_%s", baseID, hash)
	} else if _, exists := b.output.GraphNodes[finalID]; exists {
		// Collision fallback using AST hash instead of fragile #1, #2 sequential counters
		hash := generateASTHash(gastNode)
		finalID = fmt.Sprintf("%s_%s", baseID, hash)
	}

	node.ID = finalID
	b.output.GraphNodes[finalID] = node
	return finalID
}

func generateASTHash(node *stage2.GASTNode) string {
	if node == nil {
		return "nil"
	}
	// Hash based on syntax structure, type, kind, and children (ignoring line numbers for Delta stability)
	var sb strings.Builder
	sb.WriteString(string(node.Type))
	sb.WriteString(":")
	sb.WriteString(node.Kind)
	sb.WriteString(":")
	sb.WriteString(node.DataType)
	sb.WriteString(":")
	for _, c := range node.Children {
		sb.WriteString(string(c.Type))
		sb.WriteString(c.Kind)
	}
	hash := sha256.Sum256([]byte(sb.String()))
	return fmt.Sprintf("%x", hash[:6]) // 12 hex chars is plenty for collision avoidance in a single file scope
}

// BuildInitialNodes populates Stage4Output.GraphNodes with Universal Namespace Prefixed IDs for modified files.
func BuildInitialNodes(stage3Out *stage3.Stage3Output, modifiedFiles []string) *Stage4Output {
	output := NewStage4Output(stage3Out.CommitHash)
	
	modSet := make(map[string]bool)
	for _, f := range modifiedFiles {
		modSet[stage3.NormalizeRelativePath(f)] = true
	}

	builder := &InitialGraphBuilder{output: output, modSet: modSet}

	if stage3Out.RootNode != nil {
		builder.traverseDirectory(stage3Out.RootNode)
	}

	for _, v := range stage3Out.EntrypointRegistry {
		output.EntrypointRegistry = append(output.EntrypointRegistry, v)
	}
	output.ModifiedFiles = modSet

	return output
}

func (b *InitialGraphBuilder) traverseDirectory(dir *stage3.DirectoryNode) {
	if dir == nil {
		return
	}

	// Register Directory Node as a MODULE node
	if dir.RelativePath != "" && dir.RelativePath != "." {
		modID := "module:" + stage3.NormalizeRelativePath(dir.RelativePath)
		b.output.GraphNodes[modID] = &ResolvedNode{
			ID:   modID,
			Kind: "MODULE",
			Name: dir.FolderName,
			FileSpec: LocationMeta{
				Path: stage3.NormalizeRelativePath(dir.RelativePath),
			},
		}
		
		// Map the Folder Zone if it exists
		if dir.PrimitiveZone != "" {
			b.output.FolderZones[modID] = string(dir.PrimitiveZone)
		}
	}

	// Traverse Files
	for _, file := range dir.Files {
		if file == nil || file.GASTRoot == nil {
			continue
		}

		normPath := stage3.NormalizeRelativePath(file.RelativePath)
		
		// 1. Check if this file was modified. If modSet is empty, we process everything (Full Mode).
		if len(b.modSet) > 0 && !b.modSet[normPath] {
			continue
		}

		fileID := "file:" + normPath
		b.output.GraphNodes[fileID] = &ResolvedNode{
			ID:   fileID,
			Kind: "FILE",
			Name: file.FileName,
			FileSpec: LocationMeta{
				Path: normPath,
			},
		}

		b.extractNodesFromGAST(file.GASTRoot, normPath, "")
	}

	// Traverse SubFolders
	for _, subDir := range dir.SubFolders {
		b.traverseDirectory(subDir)
	}
}

func (b *InitialGraphBuilder) extractNodesFromGAST(node *stage2.GASTNode, relPath string, parentType string) {
	if node == nil {
		return
	}

	var primaryPrimitive string
	if len(node.Primitives) > 0 {
		primaryPrimitive = string(node.Primitives[0])
	}

	switch node.Type {
	case stage2.GASTTypeDeclaration:
		kind := strings.ToUpper(node.Kind)
		if kind == "" {
			kind = "STRUCT"
		}

		// Use node.ID as fallback name if Name is empty (e.g. test fixtures built manually)
		typeName := node.Name
		if typeName == "" && node.ID != "" {
			segments := strings.Split(node.ID, "::")
			typeName = segments[len(segments)-1]
		}

		universalID := BuildUniversalID(relPath, "", typeName)
		resolvedNode := &ResolvedNode{
			ID:        universalID,
			Kind:      kind,
			Name:      typeName,
			Primitive: primaryPrimitive,
			FileSpec: LocationMeta{
				Path:      relPath,
				LineStart: int(node.StartLine),
				LineEnd:   int(node.EndLine),
			},
			Properties: node.Properties,
		}
		b.registerNode(universalID, resolvedNode, node)

		// Recurse into children passing typeName as parentType
		for _, child := range node.Children {
			b.extractNodesFromGAST(child, relPath, typeName)
		}
		return

	case stage2.GASTFunction:
		kind := "FUNCTION"
		if node.Kind == "method" || node.ReceiverType != "" || parentType != "" {
			kind = "METHOD"
		}

		receiver := node.ReceiverType
		if receiver == "" {
			receiver = parentType
		}

		// Use node.ID as fallback name if Name is empty (e.g. test fixtures built manually)
		nodeName := node.Name
		if nodeName == "" && node.ID != "" {
			// Extract last segment from the ID e.g. "src/b.go::BFunc" -> "BFunc"
			segments := strings.Split(node.ID, "::")
			nodeName = segments[len(segments)-1]
		}

		universalID := BuildUniversalID(relPath, receiver, nodeName)
		resolvedNode := &ResolvedNode{
			ID:        universalID,
			Kind:      kind,
			Name:      nodeName,
			Primitive: primaryPrimitive,
			FileSpec: LocationMeta{
				Path:      relPath,
				LineStart: int(node.StartLine),
				LineEnd:   int(node.EndLine),
			},
			Properties: node.Properties,
		}
		b.registerNode(universalID, resolvedNode, node)

		for _, child := range node.Children {
			b.extractNodesFromGAST(child, relPath, receiver)
		}
		return
	}

	for _, child := range node.Children {
		b.extractNodesFromGAST(child, relPath, parentType)
	}
}
