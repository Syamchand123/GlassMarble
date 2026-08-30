package arch_linter

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultRulesTemplate generates a comprehensive, documented starter rules.yaml.
const DefaultRulesTemplate = `# GlassMarble Architectural Governance Rules
# Schema documentation: https://github.com/Syamchand123/GlassMarble#architecture-linter
version: "1"
name: "Clean Architecture & Domain Boundary Rules"
description: "Enforces strict layer boundaries, prevents circular dependencies, and limits architectural debt."

exclude:
  - "**/testdata/**"
  - "**/tests/**"
  - "**/vendor/**"
  - "**/node_modules/**"

rules:
  # 1. Domain Layer Isolation (Clean Architecture / DDD)
  - id: "domain-isolation"
    name: "Domain Layer Must Not Import Infrastructure or Web APIs"
    severity: "ERROR"
    description: "Core business logic and domain entities must remain pure and free from framework/database dependencies."
    from: "internal/domain/**"
    deny_imports:
      - "internal/infrastructure/**"
      - "internal/api/**"
      - "internal/delivery/**"
      - "cmd/**"

  # 2. Prevent Circular Package Dependencies
  - id: "no-circular-dependencies"
    name: "Acyclic Package Dependencies"
    severity: "ERROR"
    description: "Packages across the internal codebase must form a directed acyclic graph (DAG)."
    prevent_cycles: true
    scope: "internal/**"

  # 3. Encapsulate Database / Data Access
  - id: "encapsulate-database"
    name: "Database Access Must Be Encapsulated in Repository/Storage Layer"
    severity: "WARN"
    description: "Controllers and Handlers should not directly manipulate raw database connections."
    from: "internal/api/**"
    deny_imports:
      - "internal/db/raw/**"
      - "internal/storage/driver/**"

  # 4. Limit Excessive Package Coupling (Max Fan-Out)
  - id: "limit-core-fanout"
    name: "Core Packages Coupling Limit"
    severity: "WARN"
    description: "Core components should have focused, single responsibilities."
    max_fan_out: 12
    scope: "internal/core/**"
`

// ScaffoldRules creates a starter rules file at destPath if it does not already exist.
func ScaffoldRules(destPath string) (string, error) {
	if destPath == "" {
		destPath = filepath.Join(".glassmarble", "rules.yaml")
	}

	if _, err := os.Stat(destPath); err == nil {
		return destPath, fmt.Errorf("rules file already exists at %s", destPath)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return "", fmt.Errorf("failed to create directory for %s: %w", destPath, err)
	}

	if err := os.WriteFile(destPath, []byte(DefaultRulesTemplate), 0644); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", destPath, err)
	}

	return destPath, nil
}
