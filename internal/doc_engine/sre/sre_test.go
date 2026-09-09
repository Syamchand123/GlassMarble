package sre

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComputeHealthProfile(t *testing.T) {
	tempDir := t.TempDir()
	subPkg := filepath.Join(tempDir, "pkg")
	_ = os.MkdirAll(subPkg, 0755)

	code := `package pkg
func ComplexLogic(a, b int) int {
	if a > 0 {
		for i := 0; i < b; i++ {
			if i % 2 == 0 {
				return i
			}
		}
	}
	return a + b
}
`
	_ = os.WriteFile(filepath.Join(subPkg, "logic.go"), []byte(code), 0644)

	profile := ComputeHealthProfile(tempDir, "pkg")
	if profile.TotalFiles != 1 {
		t.Errorf("expected 1 file, got %d", profile.TotalFiles)
	}
	if profile.MaxComplexity < 3 {
		t.Errorf("expected cyclomatic complexity >= 3, got %d", profile.MaxComplexity)
	}

	md := FormatHealthMarkdown(profile)
	if !strings.Contains(md, "Subsystem Health & Complexity Profile") {
		t.Errorf("expected health card header in markdown:\n%s", md)
	}
}

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

func TestGenerateConcurrencyContracts(t *testing.T) {
	tempDir := t.TempDir()
	pkgDir := filepath.Join(tempDir, "concurrent")
	_ = os.MkdirAll(pkgDir, 0755)

	code := `package concurrent
import "sync"

type ThreadSafeStore struct {
	mu sync.RWMutex
	items map[string]string
	events chan string
}
`
	_ = os.WriteFile(filepath.Join(pkgDir, "store.go"), []byte(code), 0644)

	contracts, err := GenerateConcurrencyContracts(tempDir)
	if err != nil {
		t.Fatalf("GenerateConcurrencyContracts failed: %v", err)
	}

	if !strings.Contains(contracts, "ThreadSafeStore") {
		t.Errorf("missing ThreadSafeStore in concurrency contracts:\n%s", contracts)
	}
	if !strings.Contains(contracts, "sync.RWMutex") {
		t.Errorf("missing sync.RWMutex in concurrency contracts:\n%s", contracts)
	}

	dest := filepath.Join(tempDir, "docs", "concurrency.md")
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Errorf("docs/concurrency.md file not created")
	}
}

func TestGenerateTestTopology(t *testing.T) {
	tempDir := t.TempDir()
	pkgDir := filepath.Join(tempDir, "suite")
	_ = os.MkdirAll(pkgDir, 0755)

	code := `package suite_test
import "testing"

func TestEngine_Success(t *testing.T) {}
func TestEngine_ConcurrentFailure(t *testing.T) {}
func BenchmarkEngine_FastPath(b *testing.B) {}
`
	_ = os.WriteFile(filepath.Join(pkgDir, "suite_test.go"), []byte(code), 0644)

	topology, err := GenerateTestTopology(tempDir)
	if err != nil {
		t.Fatalf("GenerateTestTopology failed: %v", err)
	}

	if !strings.Contains(topology, "Test Topology & Verification Coverage Matrix") {
		t.Errorf("missing title in test topology:\n%s", topology)
	}
	if !strings.Contains(topology, "TestEngine_ConcurrentFailure") {
		t.Errorf("missing edge case in test topology:\n%s", topology)
	}

	dest := filepath.Join(tempDir, "docs", "test_topology.md")
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Errorf("docs/test_topology.md file not created")
	}
}
