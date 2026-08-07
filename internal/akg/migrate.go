package akg

import (
	"fmt"
	"os"
	"path/filepath"
)

// AutoMigrateOnLoad checks the loaded AKG schema version and automatically
// migrates schema v1/v2 databases to schema v3, backing up the original TTL file
// as akg_state.v<version>.ttl.bak.
func AutoMigrateOnLoad(storageDir string, graph *CodePropertyGraph) (string, error) {
	if graph == nil {
		return "", fmt.Errorf("cannot migrate nil graph")
	}

	if graph.SchemaVersion >= CurrentSchemaVersion {
		return "", nil
	}

	oldVersion := graph.SchemaVersion
	if oldVersion <= 0 {
		oldVersion = 2
	}

	StatePath := filepath.Join(storageDir, "akg_state.ttl")
	var backupPath string
	if _, err := os.Stat(StatePath); err == nil {
		bak, err := CreateSchemaBackup(storageDir, oldVersion)
		if err != nil {
			return "", fmt.Errorf("failed to backup schema before migration: %w", err)
		}
		backupPath = bak
	}

	if err := MigrateToSchemaV3(graph); err != nil {
		return backupPath, fmt.Errorf("schema v3 migration failed: %w", err)
	}

	return backupPath, nil
}
