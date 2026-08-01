// Package testutil provides shared fixtures for the AI engine tests:
// a synthetic in-memory AKG seeded through the real transaction manager
// plus small source files for the code tools.
package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

// Node IDs used by the synthetic fixture.
const (
	NodeDBStore = "src/db.go::DBStore"
	NodeSave    = "src/db.go::DBStore::Save"
	NodeMain    = "src/app.go::main"
	NodeHelper  = "src/util.go::helper"
)

// SeedAKG writes a small synthetic AKG into dir/.glassmarble through the
// real transaction manager and returns the storage directory.
//
// Graph:
//
//	main (FUNCTION, entrypoint) ──CALLS──▶ Save (METHOD)
//	Save ──CALLS──▶ helper (FUNCTION)
//	helper ──CALLS──▶ Save        (cycle)
func SeedAKG(t *testing.T, dir string) string {
	t.Helper()
	storageDir := filepath.Join(dir, ".glassmarble")
	tm, err := akg.NewAKGTransactionManager(storageDir)
	if err != nil {
		t.Fatalf("NewAKGTransactionManager: %v", err)
	}
	payload := &stage4.Stage4Output{
		CommitHash: "abc1234",
		GraphNodes: map[string]*stage4.ResolvedNode{
			NodeDBStore: {
				ID:   NodeDBStore,
				Kind: "STRUCT",
				Name: "DBStore",
				FileSpec: stage4.LocationMeta{
					Path:      "src/db.go",
					LineStart: 10,
					LineEnd:   60,
				},
			},
			NodeSave: {
				ID:   NodeSave,
				Kind: "METHOD",
				Name: "Save",
				FileSpec: stage4.LocationMeta{
					Path:      "src/db.go",
					LineStart: 15,
					LineEnd:   21,
				},
				// role is a persisted property; macro_rules is derived data and
				// is intentionally NOT persisted (AUDIT Phase 3C-12).
				Properties: map[string]string{"role": "persistence", "macro_rules": "data_layer|persistence"},
			},
			NodeMain: {
				ID:   NodeMain,
				Kind: "FUNCTION",
				Name: "main",
				FileSpec: stage4.LocationMeta{
					Path:      "src/app.go",
					LineStart: 1,
					LineEnd:   5,
				},
			},
			NodeHelper: {
				ID:   NodeHelper,
				Kind: "FUNCTION",
				Name: "helper",
				FileSpec: stage4.LocationMeta{
					Path:      "src/util.go",
					LineStart: 30,
					LineEnd:   40,
				},
			},
		},
		OutboundEdges: map[string][]stage4.ResolvedEdge{
			NodeMain: {
				{SourceID: NodeMain, TargetID: NodeSave, Type: stage4.EdgeCalls, LineNumber: 3, Confidence: 1.0},
			},
			NodeSave: {
				{SourceID: NodeSave, TargetID: NodeHelper, Type: stage4.EdgeCalls, LineNumber: 17, Confidence: 1.0},
			},
			NodeHelper: {
				{SourceID: NodeHelper, TargetID: NodeSave, Type: stage4.EdgeCalls, LineNumber: 33, Confidence: 1.0},
			},
		},
		InboundEdges: map[string][]stage4.ResolvedEdge{
			NodeSave: {
				{SourceID: NodeMain, TargetID: NodeSave, Type: stage4.EdgeCalls, LineNumber: 3, Confidence: 1.0},
				{SourceID: NodeHelper, TargetID: NodeSave, Type: stage4.EdgeCalls, LineNumber: 33, Confidence: 1.0},
			},
			NodeHelper: {
				{SourceID: NodeSave, TargetID: NodeHelper, Type: stage4.EdgeCalls, LineNumber: 17, Confidence: 1.0},
			},
		},
		EntrypointRegistry: []string{NodeMain},
	}
	if err := tm.ExecuteDeltaTransaction(payload, []string{"src/db.go", "src/app.go", "src/util.go"}); err != nil {
		t.Fatalf("ExecuteDeltaTransaction: %v", err)
	}
	tm.Close()
	return storageDir
}

// WriteFile writes rel inside dir (creating parents) for code-tool tests.
func WriteFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// DBStoreSource returns a 60-line Go source file. The Save method occupies
// lines 15-21 and the DBStore struct lines 10-60, mirroring the AKG fixture
// node spans.
func DBStoreSource() string {
	var sb strings.Builder
	line := func(format string, args ...any) {
		sb.WriteString(fmt.Sprintf(format+"\n", args...))
	}
	line("package db")
	line("")
	line("import \"fmt\"")
	line("")
	line("// db.go — synthetic source used by the AI engine tests.")
	line("// The line numbers below mirror the AKG fixture node spans.")
	line("")
	line("")
	line("")
	line("// DBStore persists records to disk.")
	line("type DBStore struct {")
	line("\tpath string")
	line("}")
	line("")
	line("func (s *DBStore) Save(rec string) error {")
	line("\tif rec == \"\" {")
	line("\t\treturn fmt.Errorf(\"empty record\")")
	line("\t}")
	line("\thelper(rec)")
	line("\treturn nil")
	line("}")
	line("")
	line("func (s *DBStore) Load() (string, error) {")
	line("\treturn \"\", nil")
	line("}")
	for i := 26; i <= 60; i++ {
		line("// (padding)")
	}
	return sb.String()
}

// AppSource is the 5-line main file matching the NodeMain span (lines 1-5).
const AppSource = `package main

func main() {
	// entrypoint
}
`

// UtilSource is a 40-line helper file with the helper function at lines 30-40.
func UtilSource() string {
	var sb strings.Builder
	for i := 1; i <= 29; i++ {
		sb.WriteString("// (padding)\n")
	}
	sb.WriteString("package util\n\n")
	sb.WriteString("func helper(rec string) {\n\t// noop\n}\n")
	for i := 37; i <= 40; i++ {
		sb.WriteString("// (padding)\n")
	}
	return sb.String()
}
