package akg

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// SchemaMigrationRecord records details about a schema migration version.
type SchemaMigrationRecord struct {
	Version     int
	Description string
}

// SchemaMigrations lists all historical schema version transitions.
var SchemaMigrations = []SchemaMigrationRecord{
	{Version: 1, Description: "Baseline schema: standard TTL serialization with reified edge triples."},
	{Version: 2, Description: "Schema v2: added commit hash metadata, MVCC snapshot bounds, and tombstone node blocks."},
	{Version: 3, Description: "Schema v3: single-statement RDF-star serialization (no double write), canonical ID support, view tags, content policy, and stale-kind consolidation."},
}

// CreateSchemaBackup copies the existing akg_state.ttl file to a backup file
// named akg_state.v<version>.ttl.bak before performing an in-place schema migration.
func CreateSchemaBackup(storageDir string, fromVersion int) (string, error) {
	StatePath := filepath.Join(storageDir, "akg_state.ttl")
	if _, err := os.Stat(StatePath); err != nil {
		return "", nil // No file to back up
	}
	bakPath := filepath.Join(storageDir, fmt.Sprintf("akg_state.v%d.ttl.bak", fromVersion))
	src, err := os.Open(StatePath)
	if err != nil {
		return "", fmt.Errorf("failed to open TTL for backup: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(bakPath)
	if err != nil {
		return "", fmt.Errorf("failed to create backup TTL file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("failed to copy TTL content to backup: %w", err)
	}
	_ = dst.Sync()
	return bakPath, nil
}

// MigrateToSchemaV3 upgrades an in-memory CodePropertyGraph from schema v1/v2 to schema v3.
// It consolidates legacy/stale node kinds (K-06) and ensures key symmetry (K-08).
func MigrateToSchemaV3(graph *CodePropertyGraph) error {
	if graph == nil {
		return fmt.Errorf("cannot migrate nil graph")
	}

	if graph.SchemaVersion >= CurrentSchemaVersion {
		return nil
	}

	// 1. Re-classify stale node kinds (K-06)
	newNodes := NewCowMap[string, *stage4.ResolvedNode]()
	newKindIndex := NewCowMap[string, map[string]bool]()

	graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		if node == nil {
			return
		}
		// Map stale/legacy kinds
		switch node.Kind {
		case "TYPE_DECL":
			node.Kind = "STRUCT"
			if node.Properties == nil {
				node.Properties = make(map[string]string)
			}
			node.Properties["kind"] = "typedef"
		case "EXECUTABLE":
			node.Kind = "FUNCTION"
		case "TYPE":
			node.Kind = "STRUCT"
		}

		// K-08: Symmetry check - remove legacy 'code' property key in favor of 'content'
		if node.Properties != nil {
			if codeVal, hasCode := node.Properties["code"]; hasCode {
				if _, hasContent := node.Properties["content"]; !hasContent {
					node.Properties["content"] = codeVal
				}
				delete(node.Properties, "code")
			}
		}

		newNodes = newNodes.Set(id, node)

		// Re-build KindIndex entry
		existingSet, _ := newKindIndex.Get(node.Kind)
		newSet := make(map[string]bool, len(existingSet)+1)
		for k, v := range existingSet {
			newSet[k] = v
		}
		newSet[id] = true
		newKindIndex = newKindIndex.Set(node.Kind, newSet)
	})

	graph.Nodes = newNodes
	graph.KindIndex = newKindIndex

	// 2. Ensure metadata
	if graph.CommitHash == "" {
		graph.CommitHash = "migrated_v3"
	}
	graph.SchemaVersion = CurrentSchemaVersion

	return nil
}

// CleanStaleKindMap returns clean ontology predicate string for a node kind,
// folding legacy kinds into standard ontology classes.
func CleanStaleKindMap(kind string) string {
	switch kind {
	case "TYPE_DECL", "TYPE":
		return ont.PredStruct
	case "EXECUTABLE":
		return ont.PredFunction
	default:
		return mapKindToClass(kind)
	}
}
