package stages_test

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
)

// findNode walks a GAST tree looking for the first node matching pred.
func findNode(root *stage2.GASTNode, pred func(*stage2.GASTNode) bool) *stage2.GASTNode {
	if root == nil {
		return nil
	}
	if pred(root) {
		return root
	}
	for _, child := range root.Children {
		if hit := findNode(child, pred); hit != nil {
			return hit
		}
	}
	return nil
}

func TestStage2NormalizeBuildsTrees(t *testing.T) {
	sb := newSampleSandbox(t)
	_, payload, _, _ := runStages123(t, sb, "cafe1234cafe1234")

	if payload.CommitHash != "cafe1234cafe1234" {
		t.Errorf("payload.CommitHash = %q, want cafe1234cafe1234", payload.CommitHash)
	}
	for _, want := range []string{
		"cmd/api/main.go",
		"internal/service/service.go",
		"internal/repo/repo.go",
		"internal/cache/cache.go",
	} {
		if tree, _ := lookupPath(payload.UpsertedTrees, want); tree == nil {
			t.Errorf("UpsertedTrees missing %q", want)
		}
	}

	table, _ := lookupPath(payload.LocalSymbolTables, "internal/service/service.go")
	if table == nil {
		t.Fatalf("LocalSymbolTables missing internal/service/service.go")
	}
	// PackageName comes from detectPackageName: the Go translator emits no
	// package_clause token, so the directory fallback applies and the name
	// is the dotted dir path ("internal.service"), not the bare clause.
	if table.PackageName != "internal.service" {
		t.Errorf("service.go PackageName = %q, want internal.service (directory fallback)", table.PackageName)
	}
	found := false
	for _, imp := range table.Imports {
		if imp == "example.com/shop/internal/repo" {
			found = true
		}
	}
	if !found {
		t.Errorf("service.go Imports missing example.com/shop/internal/repo: %v", table.Imports)
	}

	if len(payload.DeletedPaths) != 0 {
		t.Errorf("DeletedPaths = %v, want empty on first pass", payload.DeletedPaths)
	}
}

func TestStage2NormalizeMainTreeHasFunctions(t *testing.T) {
	sb := newSampleSandbox(t)
	_, payload, _, _ := runStages123(t, sb, "hash")

	root, _ := lookupPath(payload.UpsertedTrees, "cmd/api/main.go")
	if root == nil {
		t.Fatalf("no GAST tree for cmd/api/main.go")
	}
	if root.Type != stage2.GASTFileRoot {
		t.Errorf("main.go root Type = %q, want %q", root.Type, stage2.GASTFileRoot)
	}
	if len(root.Children) == 0 {
		t.Fatalf("main.go root has no children, want functions")
	}
	main := findNode(root, func(n *stage2.GASTNode) bool {
		return n.Type == stage2.GASTFunction && n.Name == "main"
	})
	if main == nil {
		t.Errorf("no FUNCTION node named main in cmd/api/main.go tree")
	}
}

func TestStage2NormalizeNilTolerance(t *testing.T) {
	payload, err := stage2.Normalize(nil, "hash")
	if err != nil {
		t.Fatalf("stage2.Normalize(nil, hash): %v", err)
	}
	if payload == nil {
		t.Fatal("Normalize(nil) returned nil payload")
	}
	if len(payload.UpsertedTrees) != 0 || len(payload.LocalSymbolTables) != 0 {
		t.Errorf("nil input produced trees: %v / %v", payload.UpsertedTrees, payload.LocalSymbolTables)
	}
	if len(payload.DeletedPaths) != 0 {
		t.Errorf("nil input produced deletes: %v", payload.DeletedPaths)
	}
}

func TestStage2MultiLanguageDispatch(t *testing.T) {
	sb := newSampleSandbox(t)
	_, payload, _, _ := runStages123(t, sb, "hash")

	pyRoot, _ := lookupPath(payload.UpsertedTrees, "scripts/ingest.py")
	if pyRoot == nil {
		t.Fatalf("no GAST tree for scripts/ingest.py")
	}
	if pyRoot.Properties["language"] != "python" {
		t.Errorf("ingest.py root language = %q, want python", pyRoot.Properties["language"])
	}
	jsRoot, _ := lookupPath(payload.UpsertedTrees, "web/app.js")
	if jsRoot == nil {
		t.Fatalf("no GAST tree for web/app.js")
	}
	if jsRoot.Properties["language"] != "javascript" {
		t.Errorf("app.js root language = %q, want javascript", jsRoot.Properties["language"])
	}

	if _, ok := stage2.Dispatcher(stage1.LangPython).(*stage2.PythonTranslator); !ok {
		t.Error("Dispatcher(LangPython) is not a *PythonTranslator")
	}
	if _, ok := stage2.Dispatcher(stage1.LangJS).(*stage2.JSTranslator); !ok {
		t.Error("Dispatcher(LangJS) is not a *JSTranslator")
	}
	if _, ok := stage2.Dispatcher(stage1.LangGo).(*stage2.GoTranslator); !ok {
		t.Error("Dispatcher(LangGo) is not a *GoTranslator")
	}
}
