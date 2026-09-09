package federation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
)

func TestFormatForPlatform(t *testing.T) {
	input := `# Title

> [!NOTE]
> This is a note callout.

> [!TIP]
> This is a helpful tip.

> [!WARNING]
> This is a warning.
`

	// 1. VitePress
	vp := FormatForPlatform(input, "vitepress")
	if !strings.Contains(vp, "::: info") || !strings.Contains(vp, "::: tip") || !strings.Contains(vp, "::: warning") {
		t.Errorf("expected VitePress custom containers:\n%s", vp)
	}

	// 2. Docusaurus
	docu := FormatForPlatform(input, "docusaurus")
	if !strings.Contains(docu, ":::note") || !strings.Contains(docu, ":::tip") || !strings.Contains(docu, ":::caution") {
		t.Errorf("expected Docusaurus custom containers:\n%s", docu)
	}

	// 3. MkDocs
	mk := FormatForPlatform(input, "mkdocs")
	if !strings.Contains(mk, "!!! note") || !strings.Contains(mk, "!!! tip") || !strings.Contains(mk, "!!! warning") {
		t.Errorf("expected MkDocs admonitions:\n%s", mk)
	}

	// 4. Default / GitHub Flat
	gh := FormatForPlatform(input, "github_flat")
	if !strings.Contains(gh, "> [!NOTE]") {
		t.Errorf("expected original github_flat format:\n%s", gh)
	}
}

func TestSnapshotDocSuite(t *testing.T) {
	tempDir := t.TempDir()

	docsDir := filepath.Join(tempDir, "docs")
	_ = os.MkdirAll(docsDir, 0755)
	_ = os.WriteFile(filepath.Join(docsDir, "guide.md"), []byte("# Live Guide\nProse content\n"), 0644)

	res, err := SnapshotDocSuite(tempDir, "v1.2.0")
	if err != nil {
		t.Fatalf("SnapshotDocSuite failed: %v", err)
	}

	if res.FilesCount != 1 {
		t.Errorf("expected 1 snapshot file, got %d", res.FilesCount)
	}

	snapTarget := filepath.Join(tempDir, "docs", "versions", "v1.2.0", "guide.md")
	if _, err := os.Stat(snapTarget); os.IsNotExist(err) {
		t.Errorf("snapshot file does not exist at %s", snapTarget)
	}

	indexFile := filepath.Join(tempDir, "docs", "versions", "index.md")
	if _, err := os.Stat(indexFile); os.IsNotExist(err) {
		t.Errorf("version index file not created at %s", indexFile)
	}
}

func TestExportAndImportFederationManifest(t *testing.T) {
	tempDir := t.TempDir()

	// Setup minimal docs.yaml
	cfgPath := config.DocsConfigPath(tempDir)
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0755)
	_, _ = storage.AtomicWriteFile(cfgPath, []byte("version: 1\ndocs_dir: docs\n"))

	manifestPath, err := ExportFederationManifest(tempDir)
	if err != nil {
		t.Fatalf("ExportFederationManifest failed: %v", err)
	}

	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Errorf("manifest file not created at %s", manifestPath)
	}

	docPath, err := ImportFederationManifest(tempDir, "upstream-service", manifestPath)
	if err != nil {
		t.Fatalf("ImportFederationManifest failed: %v", err)
	}

	if _, err := os.Stat(docPath); os.IsNotExist(err) {
		t.Errorf("federated doc was not written to %s", docPath)
	}
}

func TestGenerateI18nInventory(t *testing.T) {
	tempDir := t.TempDir()

	// Create locale bundle
	locDir := filepath.Join(tempDir, "locales")
	_ = os.MkdirAll(locDir, 0755)
	bundleJSON := `{"welcome": "Welcome", "login": "Log In"}`
	_ = os.WriteFile(filepath.Join(locDir, "es.json"), []byte(bundleJSON), 0644)

	res, err := GenerateI18nInventory(tempDir)
	if err != nil {
		t.Fatalf("GenerateI18nInventory failed: %v", err)
	}

	if !strings.Contains(res, "Internationalization (i18n) & Localization Inventory") {
		t.Errorf("missing title in i18n inventory:\n%s", res)
	}

	dest := filepath.Join(tempDir, "docs", "i18n.md")
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Errorf("docs/i18n.md file not created")
	}
}

func TestGenerateDebtReport(t *testing.T) {
	tempDir := t.TempDir()

	// Write docs.yaml
	cfgPath := config.DocsConfigPath(tempDir)
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0755)
	_, _ = storage.AtomicWriteFile(cfgPath, []byte("version: 1\ndocs_dir: docs\n"))

	rep, err := GenerateDebtReport(tempDir)
	if err != nil {
		t.Fatalf("GenerateDebtReport failed: %v", err)
	}

	if rep.GlobalFreshness != 100 {
		t.Errorf("expected 100 freshness for empty baseline, got %d", rep.GlobalFreshness)
	}
}
