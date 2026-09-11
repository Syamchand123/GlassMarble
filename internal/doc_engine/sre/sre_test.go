package sre

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

func TestGenerateErrorCatalog(t *testing.T) {
	tempDir := t.TempDir()
	pkgDir := filepath.Join(tempDir, "errors")
	_ = os.MkdirAll(pkgDir, 0755)

	code := `package errors
import "errors"

// ErrResourceTimeout is returned when an operation times out.
var ErrResourceTimeout = errors.New("operation timed out waiting for lock")

// ErrNotFound is returned when key does not exist.
var ErrNotFound = errors.New("resource not found")
`
	_ = os.WriteFile(filepath.Join(pkgDir, "sentinels.go"), []byte(code), 0644)

	catalog, err := GenerateErrorCatalog(tempDir)
	if err != nil {
		t.Fatalf("GenerateErrorCatalog failed: %v", err)
	}

	if !strings.Contains(catalog, "ErrResourceTimeout") {
		t.Errorf("missing ErrResourceTimeout in catalog:\n%s", catalog)
	}
	if !strings.Contains(catalog, "ErrNotFound") {
		t.Errorf("missing ErrNotFound in catalog:\n%s", catalog)
	}

	dest := filepath.Join(tempDir, "docs", "errors.md")
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Errorf("docs/errors.md file not created")
	}
}

func TestGenerateErrorCatalogCallers(t *testing.T) {
	tempDir := t.TempDir()
	pkgDir := filepath.Join(tempDir, "svc")
	_ = os.MkdirAll(pkgDir, 0755)

	decl := `package svc
import "errors"

// ErrUnavailable is returned when downstream is down.
var ErrUnavailable = errors.New("downstream unavailable")
`
	caller := `package svc
import "fmt"

func Poll() error {
	if true {
		fmt.Println(ErrUnavailable)
		return ErrUnavailable
	}
	return nil
}
`
	other := `package svc
func Nop() {}
`
	_ = os.WriteFile(filepath.Join(pkgDir, "sentinels.go"), []byte(decl), 0644)
	_ = os.WriteFile(filepath.Join(pkgDir, "caller.go"), []byte(caller), 0644)
	_ = os.WriteFile(filepath.Join(pkgDir, "other.go"), []byte(other), 0644)

	facts, err := scanSentinelErrors(tempDir)
	if err != nil {
		t.Fatalf("scanSentinelErrors failed: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("expected 1 sentinel, got %d", len(facts))
	}
	if len(facts[0].Callers) == 0 {
		t.Fatalf("expected callers for ErrUnavailable, got none")
	}
	for _, c := range facts[0].Callers {
		if strings.Contains(c, "sentinels.go") {
			t.Errorf("caller list must exclude the declaration file, got %q", c)
		}
		if !strings.Contains(c, "caller.go#") {
			t.Errorf("expected caller.go#line entry, got %q", c)
		}
	}
	if len(facts[0].Callers) > 5 {
		t.Errorf("expected at most 5 callers, got %d", len(facts[0].Callers))
	}

	catalog, err := GenerateErrorCatalog(tempDir)
	if err != nil {
		t.Fatalf("GenerateErrorCatalog failed: %v", err)
	}
	if !strings.Contains(catalog, "Callers") {
		t.Errorf("expected Callers column in catalog:\n%s", catalog)
	}
	if !strings.Contains(catalog, "caller.go#") {
		t.Errorf("expected caller reference in catalog:\n%s", catalog)
	}
}

func TestGenerateErrorCatalogWithNilGraphMatches(t *testing.T) {
	tempDir := t.TempDir()
	pkgDir := filepath.Join(tempDir, "errors")
	_ = os.MkdirAll(pkgDir, 0755)

	code := `package errors
import "errors"

// ErrGone is returned when gone.
var ErrGone = errors.New("gone")
`
	_ = os.WriteFile(filepath.Join(pkgDir, "sentinels.go"), []byte(code), 0644)

	plain, err := GenerateErrorCatalog(tempDir)
	if err != nil {
		t.Fatalf("GenerateErrorCatalog failed: %v", err)
	}
	// Remove the generated docs/errors.md so the second call regenerates it.
	_ = os.Remove(filepath.Join(tempDir, "docs", "errors.md"))
	withNil, err := GenerateErrorCatalogWithGraph(tempDir, nil)
	if err != nil {
		t.Fatalf("GenerateErrorCatalogWithGraph(nil) failed: %v", err)
	}
	if plain != withNil {
		t.Errorf("nil graph must reproduce the ident-scan catalog exactly:\n--- plain ---\n%s\n--- withNil ---\n%s", plain, withNil)
	}
}

func TestGenerateErrorCatalogWithGraphCallers(t *testing.T) {
	tempDir := t.TempDir()
	pkgDir := filepath.Join(tempDir, "svc")
	_ = os.MkdirAll(pkgDir, 0755)

	decl := `package svc
import "errors"

// ErrUnavailable is returned when downstream is down.
var ErrUnavailable = errors.New("downstream unavailable")
`
	_ = os.WriteFile(filepath.Join(pkgDir, "sentinels.go"), []byte(decl), 0644)

	// CPG: sentinel node + one inbound edge from a caller node whose
	// FileSpec carries the caller location.
	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("svc/sentinels.go::ErrUnavailable", &link.ResolvedNode{
		ID:   "svc/sentinels.go::ErrUnavailable",
		Kind: "VAR",
		Name: "ErrUnavailable",
		FileSpec: link.LocationMeta{
			Path:      "svc/sentinels.go",
			LineStart: 5,
			LineEnd:   5,
		},
	})
	graph.Nodes = graph.Nodes.Set("svc/caller.go::Poll", &link.ResolvedNode{
		ID:   "svc/caller.go::Poll",
		Kind: "FUNCTION",
		Name: "Poll",
		FileSpec: link.LocationMeta{
			Path:      "svc/caller.go",
			LineStart: 9,
			LineEnd:   20,
		},
	})
	graph.InboundEdges = graph.InboundEdges.Set("svc/sentinels.go::ErrUnavailable", []link.ResolvedEdge{
		{SourceID: "svc/caller.go::Poll", TargetID: "svc/sentinels.go::ErrUnavailable", Type: link.EdgeCalls, LineNumber: 12},
	})

	facts, err := scanSentinelErrorsWithGraph(tempDir, graph)
	if err != nil {
		t.Fatalf("scanSentinelErrorsWithGraph failed: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("expected 1 sentinel, got %d", len(facts))
	}
	if len(facts[0].Callers) != 1 || facts[0].Callers[0] != "svc/caller.go#9" {
		t.Errorf("expected graph caller svc/caller.go#9, got %v", facts[0].Callers)
	}

	catalog, err := GenerateErrorCatalogWithGraph(tempDir, graph)
	if err != nil {
		t.Fatalf("GenerateErrorCatalogWithGraph failed: %v", err)
	}
	if !strings.Contains(catalog, "svc/caller.go#9") {
		t.Errorf("expected graph caller in catalog:\n%s", catalog)
	}
}

func TestGenerateErrorCatalogWithGraphUnknownSentinel(t *testing.T) {
	tempDir := t.TempDir()
	pkgDir := filepath.Join(tempDir, "svc")
	_ = os.MkdirAll(pkgDir, 0755)

	decl := `package svc
import "errors"

// ErrMissing is unknown to the graph.
var ErrMissing = errors.New("missing")
`
	_ = os.WriteFile(filepath.Join(pkgDir, "sentinels.go"), []byte(decl), 0644)

	// Empty graph: authoritative, no ident-scan mixing → no callers.
	graph := akg.NewCodePropertyGraph("test")
	facts, err := scanSentinelErrorsWithGraph(tempDir, graph)
	if err != nil {
		t.Fatalf("scanSentinelErrorsWithGraph failed: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("expected 1 sentinel, got %d", len(facts))
	}
	if len(facts[0].Callers) != 0 {
		t.Errorf("graph-unknown sentinel must keep empty callers, got %v", facts[0].Callers)
	}
}
