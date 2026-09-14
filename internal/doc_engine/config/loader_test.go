// Package config — loader_test.go
// Tests for docs.yaml loading and validation.
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDocsConfig_FileNotExist(t *testing.T) {
	// When docs.yaml does not exist, LoadDocsConfig returns an empty
	// but valid config (not an error).
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".glassmarble"), 0755)

	cfg, err := LoadDocsConfig(dir)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, CurrentSchemaVersion, cfg.Version)
	assert.Empty(t, cfg.Documents)
}

func TestLoadDocsConfig_MinimalValid(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".glassmarble"), 0755)

	content := `
version: 1
docs_dir: "docs"
documents:
  - id: "auth"
    target: "docs/auth.md"
    title: "Auth Module"
    purpose: "Debug guide for auth engineers"
    audience: "Backend engineers"
    scope:
      paths:
        - "internal/auth/**"
    sections:
      - id: "interface"
        title: "Exported Interface"
        instruction: "Table of exported types"
        managed: true
        ground_with:
          - exported_symbols
`
	writeConfig(t, dir, content)

	cfg, err := LoadDocsConfig(dir)
	require.NoError(t, err)
	require.Len(t, cfg.Documents, 1)

	doc := cfg.Documents[0]
	assert.Equal(t, "auth", doc.ID)
	assert.Equal(t, "docs/auth.md", doc.TargetPath)
	assert.Equal(t, ModeManagedSections, doc.Mode) // default applied
	require.Len(t, doc.Sections, 1)
	assert.Equal(t, "interface", doc.Sections[0].ID)
	assert.Equal(t, "Exported Interface", doc.Sections[0].Title)
}

func TestLoadDocsConfig_DefaultsApplied(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".glassmarble"), 0755)

	content := `
version: 1
documents:
  - id: "arch"
    target: "docs/architecture.md"
    title: "Architecture"
    purpose: "System overview"
    audience: "Contributors"
    scope:
      paths:
        - "internal/**"
`
	writeConfig(t, dir, content)

	cfg, err := LoadDocsConfig(dir)
	require.NoError(t, err)

	// Global defaults
	assert.Equal(t, "github_flat", cfg.TargetPlatform)
	assert.Equal(t, "docs", cfg.DocsDir)
	assert.Equal(t, 10, cfg.Constraints.MaxDocUpdatesPerCommit)
	assert.Equal(t, 100000, cfg.Constraints.MaxTokensPerRun)
	assert.Equal(t, 80, cfg.Constraints.MinFreshnessThreshold)
	assert.Equal(t, 60, cfg.Constraints.MinFreshnessFail)
	assert.Equal(t, "active, second-person, present tense", cfg.Style.Voice)

	// Doc default
	assert.Equal(t, ModeManagedSections, cfg.Documents[0].Mode)
}

func TestLoadDocsConfig_InvalidVersion(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".glassmarble"), 0755)

	content := `
version: 99
documents: []
`
	writeConfig(t, dir, content)

	_, err := LoadDocsConfig(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported schema version")
}

func TestLoadDocsConfig_MissingID(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".glassmarble"), 0755)

	content := `
version: 1
documents:
  - target: "docs/arch.md"
    title: "Architecture"
    purpose: "Overview"
    audience: "Contributors"
    scope:
      paths: ["internal/**"]
`
	writeConfig(t, dir, content)

	_, err := LoadDocsConfig(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'id' is required")
}

func TestLoadDocsConfig_DuplicateID(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".glassmarble"), 0755)

	content := `
version: 1
documents:
  - id: "arch"
    target: "docs/arch.md"
    title: "Arch"
    purpose: "Overview"
    audience: "Devs"
    scope:
      paths: ["internal/**"]
  - id: "arch"
    target: "docs/arch2.md"
    title: "Arch 2"
    purpose: "Overview 2"
    audience: "Devs"
    scope:
      paths: ["cmd/**"]
`
	writeConfig(t, dir, content)

	_, err := LoadDocsConfig(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate document id")
}

func TestLoadDocsConfig_InvalidID(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".glassmarble"), 0755)

	content := `
version: 1
documents:
  - id: "My_Doc!"
    target: "docs/doc.md"
    title: "Doc"
    purpose: "Overview"
    audience: "Devs"
    scope:
      paths: ["internal/**"]
`
	writeConfig(t, dir, content)

	_, err := LoadDocsConfig(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lowercase letters")
}

func TestLoadDocsConfig_MissingTarget(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".glassmarble"), 0755)

	content := `
version: 1
documents:
  - id: "arch"
    title: "Architecture"
    purpose: "Overview"
    audience: "Contributors"
    scope:
      paths: ["internal/**"]
`
	writeConfig(t, dir, content)

	_, err := LoadDocsConfig(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'target' path is required")
}

func TestLoadDocsConfig_NonMDTarget(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".glassmarble"), 0755)

	content := `
version: 1
documents:
  - id: "arch"
    target: "docs/architecture.txt"
    title: "Architecture"
    purpose: "Overview"
    audience: "Contributors"
    scope:
      paths: ["internal/**"]
`
	writeConfig(t, dir, content)

	_, err := LoadDocsConfig(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".md file")
}

// TestLoadDocsConfig_TargetPathTraversalRejected guards against a docs.yaml
// 'target' escaping the repository: every consumer (the writer, rag_export,
// permalink healing) joins this value onto repoRoot with no further
// validation, so an unvalidated ".."-escaping or absolute target would let
// the engine write or read outside the repo.
func TestLoadDocsConfig_TargetPathTraversalRejected(t *testing.T) {
	for _, target := range []string{
		"../../../etc/passwd.md",
		"../outside.md",
		"/etc/passwd.md",
	} {
		dir := t.TempDir()
		_ = os.MkdirAll(filepath.Join(dir, ".glassmarble"), 0755)
		content := `
version: 1
documents:
  - id: "arch"
    target: "` + target + `"
    title: "Architecture"
    purpose: "Overview"
    audience: "Contributors"
    scope:
      paths: ["internal/**"]
`
		writeConfig(t, dir, content)

		_, err := LoadDocsConfig(dir)
		require.Error(t, err, "target %q should be rejected", target)
		assert.Contains(t, err.Error(), "relative path inside the repository")
	}
}

func TestLoadDocsConfig_EmptyScope(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".glassmarble"), 0755)

	content := `
version: 1
documents:
  - id: "arch"
    target: "docs/architecture.md"
    title: "Architecture"
    purpose: "Overview"
    audience: "Contributors"
    scope: {}
`
	writeConfig(t, dir, content)

	_, err := LoadDocsConfig(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one path or entry_point")
}

func TestLoadDocsConfig_DuplicateSectionID(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".glassmarble"), 0755)

	content := `
version: 1
documents:
  - id: "auth"
    target: "docs/auth.md"
    title: "Auth"
    purpose: "Guide"
    audience: "Devs"
    scope:
      paths: ["internal/auth/**"]
    sections:
      - id: "interface"
        title: "Exported Interface"
        instruction: "List types"
        managed: true
      - id: "interface"
        title: "Interface Duplicate"
        instruction: "Duplicate"
        managed: true
`
	writeConfig(t, dir, content)

	_, err := LoadDocsConfig(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate section id")
}

func TestDocByID(t *testing.T) {
	cfg := &DocsConfig{
		Documents: []DocSpec{
			{ID: "auth", TargetPath: "docs/auth.md"},
			{ID: "arch", TargetPath: "docs/architecture.md"},
		},
	}

	doc := cfg.DocByID("auth")
	require.NotNil(t, doc)
	assert.Equal(t, "docs/auth.md", doc.TargetPath)

	assert.Nil(t, cfg.DocByID("missing"))
}

func TestDocByTarget(t *testing.T) {
	cfg := &DocsConfig{
		Documents: []DocSpec{
			{ID: "auth", TargetPath: "docs/auth.md"},
		},
	}

	doc := cfg.DocByTarget("docs/auth.md")
	require.NotNil(t, doc)
	assert.Equal(t, "auth", doc.ID)

	assert.Nil(t, cfg.DocByTarget("docs/other.md"))
}

func TestIsURLSafe(t *testing.T) {
	validCases := []string{"auth", "system-architecture", "ai-engine", "v1", "a1b2c3"}
	for _, s := range validCases {
		assert.True(t, isURLSafe(s), "expected %q to be URL-safe", s)
	}

	invalidCases := []string{"", "Auth", "my_doc", "my doc", "doc!", "UpperCase"}
	for _, s := range invalidCases {
		assert.False(t, isURLSafe(s), "expected %q to be NOT URL-safe", s)
	}
}

func TestWriteDefaultDocsConfig(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".glassmarble"), 0755)

	err := WriteDefaultDocsConfig(dir)
	require.NoError(t, err)

	// File should exist now.
	_, err = os.Stat(DocsConfigPath(dir))
	require.NoError(t, err)

	// Calling again should not overwrite (no error).
	err = WriteDefaultDocsConfig(dir)
	require.NoError(t, err)

	// The written file should be valid YAML parseable as DocsConfig.
	cfg, err := LoadDocsConfig(dir)
	require.NoError(t, err)
	assert.Equal(t, CurrentSchemaVersion, cfg.Version)
	assert.Empty(t, cfg.Documents) // scaffold has no documents
}

func TestLoadDocsConfig_FallbackToRepoRoot(t *testing.T) {
	dir := t.TempDir()
	content := `
version: 1
documents:
  - id: "root-doc"
    target: "docs/root.md"
    title: "Root Doc"
    purpose: "Testing root fallback"
    audience: "Developers"
    scope:
      paths: ["cmd/**"]
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, DocsConfigFilename), []byte(content), 0644))

	cfg, err := LoadDocsConfig(dir)
	require.NoError(t, err)
	require.Len(t, cfg.Documents, 1)
	assert.Equal(t, "root-doc", cfg.Documents[0].ID)
}

// writeConfig is a test helper that writes docs.yaml content to the
// .glassmarble directory of the given temp dir.
func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	path := filepath.Join(dir, ".glassmarble", DocsConfigFilename)
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}
