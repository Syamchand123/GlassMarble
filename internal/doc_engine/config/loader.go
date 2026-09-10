// Package config — loader.go
// Loads and validates .glassmarble/docs.yaml and inline YAML frontmatter.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// DocsConfigFilename is the name of the documentation configuration file.
	DocsConfigFilename = "docs.yaml"

	// DocsStateFilename is the name of the documentation state persistence file.
	DocsStateFilename = "docs_state.json"

	// CurrentSchemaVersion is the schema version this loader understands.
	CurrentSchemaVersion = 1
)

// LoadDocsConfig loads and validates .glassmarble/docs.yaml for the given
// repository root. Returns a validated DocsConfig.
//
// If the file does not exist, an empty DocsConfig with Version=1 is returned
// (not an error). Callers can check len(cfg.Documents) == 0.
func LoadDocsConfig(repoRoot string) (*DocsConfig, error) {
	configPath := filepath.Join(repoRoot, ".glassmarble", DocsConfigFilename)

	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		// Fallback to repo root: docs.yaml
		rootPath := filepath.Join(repoRoot, DocsConfigFilename)
		rootData, rootErr := os.ReadFile(rootPath)
		if rootErr == nil {
			configPath = rootPath
			data = rootData
			err = nil
		} else {
			// docs.yaml does not exist — return an empty, valid config.
			return &DocsConfig{Version: CurrentSchemaVersion}, nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("doc_engine: reading %s: %w", configPath, err)
	}

	var cfg DocsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("doc_engine: parsing %s: %w", configPath, err)
	}

	if err := validateDocsConfig(&cfg, configPath); err != nil {
		return nil, err
	}

	// Apply defaults.
	applyDefaults(&cfg)

	// Non-fatal completeness warnings (Pillar 2 / plan §7.1): surface missing
	// high-signal fields to stderr without failing the load (P5).
	for _, w := range ValidateSpecCompleteness(&cfg) {
		fmt.Fprintf(os.Stderr, "doc_engine: config warning: %s\n", w.Error())
	}

	return &cfg, nil
}

// validateDocsConfig checks the loaded config for required fields and
// obvious mis-configurations. All errors are actionable.
func validateDocsConfig(cfg *DocsConfig, configPath string) error {
	if cfg.Version != CurrentSchemaVersion {
		return fmt.Errorf("doc_engine: %s: unsupported schema version %d (expected %d)",
			configPath, cfg.Version, CurrentSchemaVersion)
	}

	seenIDs := make(map[string]bool, len(cfg.Documents))

	for i, doc := range cfg.Documents {
		prefix := fmt.Sprintf("doc_engine: %s: documents[%d]", configPath, i)

		if doc.ID == "" {
			return fmt.Errorf("%s: 'id' is required", prefix)
		}
		if !isURLSafe(doc.ID) {
			return fmt.Errorf("%s: 'id' %q must use only lowercase letters, digits, and hyphens", prefix, doc.ID)
		}
		if seenIDs[doc.ID] {
			return fmt.Errorf("%s: duplicate document id %q", prefix, doc.ID)
		}
		seenIDs[doc.ID] = true

		if doc.TargetPath == "" {
			return fmt.Errorf("%s (id=%q): 'target' path is required", prefix, doc.ID)
		}
		if !strings.HasSuffix(doc.TargetPath, ".md") {
			return fmt.Errorf("%s (id=%q): 'target' must be a .md file", prefix, doc.ID)
		}

		if len(doc.Scope.Paths) == 0 && len(doc.Scope.EntryPoints) == 0 {
			return fmt.Errorf("%s (id=%q): 'scope' must specify at least one path or entry_point", prefix, doc.ID)
		}

		// Validate sections.
		seenSectionIDs := make(map[string]bool, len(doc.Sections))
		for j, sec := range doc.Sections {
			secPrefix := fmt.Sprintf("%s (id=%q) sections[%d]", prefix, doc.ID, j)
			if sec.ID == "" {
				return fmt.Errorf("%s: 'id' is required", secPrefix)
			}
			if !isURLSafe(sec.ID) {
				return fmt.Errorf("%s: section id %q must use only lowercase letters, digits, and hyphens", secPrefix, sec.ID)
			}
			if seenSectionIDs[sec.ID] {
				return fmt.Errorf("%s: duplicate section id %q in document %q", secPrefix, sec.ID, doc.ID)
			}
			seenSectionIDs[sec.ID] = true
			if sec.Title == "" {
				return fmt.Errorf("%s (section=%q): 'title' is required", secPrefix, sec.ID)
			}
		}
	}

	return nil
}

// applyDefaults fills in zero-value fields with sensible defaults.
func applyDefaults(cfg *DocsConfig) {
	if cfg.TargetPlatform == "" {
		cfg.TargetPlatform = "github_flat"
	}
	if cfg.DocsDir == "" {
		cfg.DocsDir = "docs"
	}
	if cfg.Constraints.MaxDocUpdatesPerCommit == 0 {
		cfg.Constraints.MaxDocUpdatesPerCommit = 10
	}
	if cfg.Constraints.MaxTokensPerRun == 0 {
		cfg.Constraints.MaxTokensPerRun = 100000
	}
	if cfg.Constraints.MinFreshnessThreshold == 0 {
		cfg.Constraints.MinFreshnessThreshold = 80
	}
	if cfg.Constraints.MinFreshnessFail == 0 {
		cfg.Constraints.MinFreshnessFail = 60
	}
	if cfg.Style.Voice == "" {
		cfg.Style.Voice = "active, second-person, present tense"
	}

	for i := range cfg.Documents {
		doc := &cfg.Documents[i]
		if doc.Mode == "" {
			doc.Mode = ModeManagedSections
		}
		// Sections default to managed=true.
		for j := range doc.Sections {
			sec := &doc.Sections[j]
			// yaml.Unmarshal sets bool to false by default.
			// We want managed to default to true for explicitly listed sections.
			// Since we can't distinguish "not set" from "set false" with yaml,
			// we apply: if Instruction is non-empty and Managed is false, assume
			// the user forgot to set it. This is the safe default.
			if sec.Instruction != "" && !sec.Managed && !sec.Freeze {
				sec.Managed = true
			}
		}
	}
}

// isURLSafe reports whether s contains only lowercase letters, digits, and hyphens.
func isURLSafe(s string) bool {
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}
	return len(s) > 0
}

// DocByID returns the DocSpec with the given ID, or nil if not found.
func (cfg *DocsConfig) DocByID(id string) *DocSpec {
	for i := range cfg.Documents {
		if cfg.Documents[i].ID == id {
			return &cfg.Documents[i]
		}
	}
	return nil
}

// DocByTarget returns the DocSpec whose TargetPath matches the given path,
// or nil if not found. The comparison is case-insensitive on Windows.
func (cfg *DocsConfig) DocByTarget(targetPath string) *DocSpec {
	norm := filepath.ToSlash(strings.ToLower(targetPath))
	for i := range cfg.Documents {
		if strings.ToLower(filepath.ToSlash(cfg.Documents[i].TargetPath)) == norm {
			return &cfg.Documents[i]
		}
	}
	return nil
}

// StorageDirPath returns the .glassmarble directory path for a given repo root.
func StorageDirPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".glassmarble")
}

// DocsConfigPath returns the full path to docs.yaml for a given repo root.
func DocsConfigPath(repoRoot string) string {
	return filepath.Join(StorageDirPath(repoRoot), DocsConfigFilename)
}

// DocsStatePath returns the full path to docs_state.json for a given repo root.
func DocsStatePath(repoRoot string) string {
	return filepath.Join(StorageDirPath(repoRoot), DocsStateFilename)
}

// WriteDefaultDocsConfig writes a minimal docs.yaml scaffold to the given
// repository's .glassmarble directory. It does not overwrite an existing file.
func WriteDefaultDocsConfig(repoRoot string) error {
	configPath := DocsConfigPath(repoRoot)
	if _, err := os.Stat(configPath); err == nil {
		// Already exists — do not overwrite.
		return nil
	}

	content := `# GlassMarble Documentation Intelligence Engine configuration.
# Generated by 'gmb doc init'.
# Reference: https://github.com/Syamchand123/GlassMarble#documentation-engine
version: 1
docs_dir: "docs"

# Global style applied to all documents unless overridden per-document.
style:
  voice: "active, second-person, present tense"
  jargon_blacklist:
    - "simply"
    - "obviously"
    - "leverage"
    - "utilize"

# Global budget and freshness constraints.
constraints:
  max_doc_updates_per_commit: 10
  max_tokens_per_run: 100000
  min_freshness_threshold: 80  # CI warning below this
  min_freshness_fail: 60       # CI hard fail below this

# Target publishing platform: github_flat | vitepress | docusaurus | mkdocs
target_platform: "github_flat"

# Add managed documents below.
# Run 'gmb doc init docs/<name>.md --scope "<path>/**"' to scaffold a document.
documents: []
`

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("doc_engine: creating .glassmarble dir: %w", err)
	}
	return os.WriteFile(configPath, []byte(content), 0644)
}
