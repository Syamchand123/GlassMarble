package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateThreatModel(t *testing.T) {
	tempDir := t.TempDir()
	apiPkg := filepath.Join(tempDir, "api")
	_ = os.MkdirAll(apiPkg, 0755)

	code := `package api
import (
	"net/http"
	"crypto/sha256"
	"os"
)

func Register() {
	http.HandleFunc("/login", nil)
	key := os.Getenv("API_SECRET_KEY")
	_ = sha256.Sum256([]byte(key))
}
`
	_ = os.WriteFile(filepath.Join(apiPkg, "routes.go"), []byte(code), 0644)

	threatModel, err := GenerateThreatModel(tempDir)
	if err != nil {
		t.Fatalf("GenerateThreatModel failed: %v", err)
	}

	if !strings.Contains(threatModel, "System Threat Model & Security Architecture") {
		t.Errorf("missing title in threat model:\n%s", threatModel)
	}
	if !strings.Contains(threatModel, "API_SECRET_KEY") {
		t.Errorf("missing API_SECRET_KEY in threat model:\n%s", threatModel)
	}

	dest := filepath.Join(tempDir, "docs", "security", "threat_model.md")
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Errorf("docs/security/threat_model.md not created")
	}
}

func TestGenerateConfigDictionary(t *testing.T) {
	tempDir := t.TempDir()
	pkgDir := filepath.Join(tempDir, "cfg")
	_ = os.MkdirAll(pkgDir, 0755)

	code := `package cfg
import "os"

func Init() {
	_ = os.Getenv("PORT")
	_ = os.Getenv("DATABASE_URL")
}
`
	_ = os.WriteFile(filepath.Join(pkgDir, "env.go"), []byte(code), 0644)

	dict, err := GenerateConfigDictionary(tempDir)
	if err != nil {
		t.Fatalf("GenerateConfigDictionary failed: %v", err)
	}

	if !strings.Contains(dict, "DATABASE_URL") {
		t.Errorf("missing DATABASE_URL in config dictionary:\n%s", dict)
	}
	if !strings.Contains(dict, "PORT") {
		t.Errorf("missing PORT in config dictionary:\n%s", dict)
	}

	dest := filepath.Join(tempDir, "docs", "configuration.md")
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Errorf("docs/configuration.md not created")
	}
}

func TestGenerateSBOM(t *testing.T) {
	tempDir := t.TempDir()

	goModContent := `module example.com/testapp

go 1.22

require (
	github.com/spf13/cobra v1.8.0
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
)
`
	_ = os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goModContent), 0644)

	sbom, err := GenerateSBOM(tempDir)
	if err != nil {
		t.Fatalf("GenerateSBOM failed: %v", err)
	}

	if !strings.Contains(sbom, "github.com/spf13/cobra") {
		t.Errorf("missing cobra in SBOM:\n%s", sbom)
	}
	if !strings.Contains(sbom, "Direct") {
		t.Errorf("missing Direct classification in SBOM:\n%s", sbom)
	}

	dest := filepath.Join(tempDir, "docs", "compliance", "dependencies.md")
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Errorf("docs/compliance/dependencies.md not created")
	}
}

func TestCheckAPISurfaceBoundaries(t *testing.T) {
	tempDir := t.TempDir()
	cmdDir := filepath.Join(tempDir, "cmd")
	_ = os.MkdirAll(cmdDir, 0755)

	code := `package cmd
type internalHelper struct{}

func PublicEndpoint() internalHelper {
	return internalHelper{}
}
`
	_ = os.WriteFile(filepath.Join(cmdDir, "root.go"), []byte(code), 0644)

	report, err := CheckAPISurfaceBoundaries(tempDir)
	if err != nil {
		t.Fatalf("CheckAPISurfaceBoundaries failed: %v", err)
	}

	if report.PublicEdgeCount != 1 {
		t.Errorf("expected 1 public edge func, got %d", report.PublicEdgeCount)
	}
	if len(report.Violations) == 0 {
		t.Errorf("expected violation for leaked unexported type internalHelper")
	}
}
