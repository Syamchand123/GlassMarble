package stage2

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

// repoRoot resolves the GlassMarble repo root from the stage2 test dir
// (internal/code_analysis_engine/stage2 → repo root).
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return root
}

// RunStage2 writes the given files (relPath → content) into a temp dir and
// runs the full Stage 1 → Stage 2 pipeline over them.
func RunStage2(t *testing.T, files map[string]string) *Stage2Payload {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	out, err := stage1.RunIngestion(stage1.Config{RootDir: dir})
	if err != nil {
		t.Fatalf("RunIngestion: %v", err)
	}
	payload, err := Normalize(out, "acceptance")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	return payload
}

// loadRealFile copies a repo file into the temp corpus as a "fixture".
func loadRealFile(t *testing.T, files map[string]string, repoRel, corpusRel string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(repoRel)))
	if err != nil {
		t.Fatalf("read %s: %v", repoRel, err)
	}
	files[corpusRel] = string(content)
}

func collectNodes(root *GASTNode, out *[]*GASTNode) {
	if root == nil {
		return
	}
	*out = append(*out, root)
	for _, c := range root.Children {
		collectNodes(c, out)
	}
}

func findBy(root *GASTNode, pred func(*GASTNode) bool) *GASTNode {
	if root == nil {
		return nil
	}
	if pred(root) {
		return root
	}
	for _, c := range root.Children {
		if n := findBy(c, pred); n != nil {
			return n
		}
	}
	return nil
}

func fieldChildren(t *testing.T, typeNode *GASTNode) []*GASTNode {
	t.Helper()
	if typeNode == nil {
		t.Fatal("type node is nil")
	}
	var fields []*GASTNode
	for _, c := range typeNode.Children {
		if c.Type == GASTField {
			fields = append(fields, c)
		}
	}
	return fields
}

func methodChildren(typeNode *GASTNode) []*GASTNode {
	var methods []*GASTNode
	for _, c := range typeNode.Children {
		if c.Type == GASTFunction && c.Kind == "method" {
			methods = append(methods, c)
		}
	}
	return methods
}

// treeFor resolves a GAST tree by slash-normalized rel path (the engine
// keys UpsertedTrees by OS path separators on Windows).
func treeFor(t *testing.T, payload *Stage2Payload, slashRel string) *GASTNode {
	t.Helper()
	want := filepath.ToSlash(slashRel)
	for rel, root := range payload.UpsertedTrees {
		if filepath.ToSlash(rel) == want {
			return root
		}
	}
	t.Fatalf("no GAST tree for %s; keys=%v", slashRel, keys(payload))
	return nil
}

func keys(payload *Stage2Payload) []string {
	var ks []string
	for k := range payload.UpsertedTrees {
		ks = append(ks, filepath.ToSlash(k))
	}
	return ks
}

// TestGoGAST_Options is the §5.2.4 acceptance: the analyze program's
// Options struct produces 1 type node with field children carrying correct
// FieldType values sourced from tree-sitter field roles (no content regex).
func TestGoGAST_Options(t *testing.T) {
	files := map[string]string{}
	loadRealFile(t, files, "internal/tui/programs/analyze/program.go", "analyze/program.go")
	payload := RunStage2(t, files)

	root := treeFor(t, payload, "analyze/program.go")
	options := findBy(root, func(n *GASTNode) bool {
		return n.Type == GASTTypeDeclaration && n.Name == "Options" && n.Kind == "struct"
	})
	if options == nil {
		t.Fatal("no Options struct node in program.go GAST")
	}
	fields := fieldChildren(t, options)
	if len(fields) < 8 {
		t.Fatalf("Options has %d field children, want >= 8", len(fields))
	}
	wantTypes := map[string]string{
		"TargetDir":      "string",
		"CommitHash":     "string",
		"Full":           "bool",
		"Workers":        "int",
		"Verbose":        "bool",
		"LinkLevel":      "string",
		"MacroInference": "string",
		"MaxNodes":       "int",
		"AbortOnLimit":   "bool",
	}
	for _, f := range fields {
		if f.FieldType == "" {
			t.Errorf("Options field %q has empty FieldType", f.Name)
			continue
		}
		if want, ok := wantTypes[f.Name]; ok && f.FieldType != want {
			t.Errorf("Options field %s FieldType = %q, want %q", f.Name, f.FieldType, want)
		}
	}
}

// TestGoGAST_Interfaces is the §5.2.4 acceptance: the Provider interface
// is an interface kind with 3 method children and no BaseTypes.
func TestGoGAST_Interfaces(t *testing.T) {
	files := map[string]string{}
	loadRealFile(t, files, "internal/ai_engine/provider/types.go", "provider/types.go")
	payload := RunStage2(t, files)

	root := treeFor(t, payload, "provider/types.go")
	provider := findBy(root, func(n *GASTNode) bool {
		return n.Type == GASTTypeDeclaration && n.Name == "Provider" && n.Kind == "interface"
	})
	if provider == nil {
		t.Fatal("no Provider interface node in types.go GAST")
	}
	if len(provider.BaseTypes) != 0 {
		t.Errorf("Provider BaseTypes = %v, want empty", provider.BaseTypes)
	}
	methods := methodChildren(provider)
	if len(methods) != 3 {
		t.Fatalf("Provider has %d method children, want 3", len(methods))
	}
	names := map[string]bool{}
	for _, m := range methods {
		names[m.Name] = true
		if m.ReceiverType != "" {
			t.Errorf("interface method %s ReceiverType = %q, want empty (interface-scoped)", m.Name, m.ReceiverType)
		}
	}
	for _, want := range []string{"Name", "Complete", "Ping"} {
		if !names[want] {
			t.Errorf("Provider missing interface method %s", want)
		}
	}
	if got := findMethodReturn(provider, "Complete"); got != "(*Response, error)" {
		t.Errorf("Complete ReturnType = %q, want (*Response, error)", got)
	}
}

func findMethodReturn(typeNode *GASTNode, name string) string {
	for _, m := range methodChildren(typeNode) {
		if m.Name == name {
			return m.ReturnType
		}
	}
	return ""
}

// TestGoGAST_Methods is the §5.2.4 acceptance: the Dispatcher struct has
// 5 method children with ReceiverType="Dispatcher" and correct ReturnType.
func TestGoGAST_Methods(t *testing.T) {
	files := map[string]string{}
	loadRealFile(t, files, "internal/ai_engine/agent/dispatcher.go", "agent/dispatcher.go")
	payload := RunStage2(t, files)

	root := treeFor(t, payload, "agent/dispatcher.go")
	dispatcher := findBy(root, func(n *GASTNode) bool {
		return n.Type == GASTTypeDeclaration && n.Name == "Dispatcher"
	})
	if dispatcher == nil {
		t.Fatal("no Dispatcher type node in dispatcher.go GAST")
	}
	methods := methodChildren(dispatcher)
	if len(methods) != 5 {
		t.Fatalf("Dispatcher has %d method children, want 5", len(methods))
	}
	for _, m := range methods {
		if m.ReceiverType != "Dispatcher" {
			t.Errorf("method %s ReceiverType = %q, want Dispatcher", m.Name, m.ReceiverType)
		}
	}
	want := map[string]string{
		"maxBytes": "int",
		"Dispatch": "[]provider.ToolResult",
		"dispatch": "provider.ToolResult",
		"find":     "(tools.Tool, bool)",
		"render":   "string",
	}
	for _, m := range methods {
		if w, ok := want[m.Name]; ok && m.ReturnType != w {
			t.Errorf("method %s ReturnType = %q, want %q", m.Name, m.ReturnType, w)
		}
		if m.Signature == "" {
			t.Errorf("method %s has empty Signature", m.Name)
		}
	}
}

// TestJavaGAST_Inheritance is the §5.2.4 acceptance: `class B extends A
// implements I` produces BaseTypes=["A"] and Implemented=["I"].
func TestJavaGAST_Inheritance(t *testing.T) {
	files := map[string]string{
		"app/Model.java": `package app;

interface I {
    void run();
}

class A {
    private int id;
}

class B extends A implements I {
    private String name;
}
`,
	}
	payload := RunStage2(t, files)

	root := treeFor(t, payload, "app/Model.java")
	b := findBy(root, func(n *GASTNode) bool {
		// Java translators carry FQN names ("app.Model.B").
		return n.Type == GASTTypeDeclaration && strings.HasSuffix(n.Name, ".B")
	})
	if b == nil {
		t.Fatal("no B class node")
	}
	if len(b.BaseTypes) != 1 || b.BaseTypes[0] != "A" {
		t.Errorf("B BaseTypes = %v, want [A]", b.BaseTypes)
	}
	if len(b.Implemented) != 1 || b.Implemented[0] != "I" {
		t.Errorf("B Implemented = %v, want [I]", b.Implemented)
	}
}

// TestNoEmptyDataType is the §5.2.4 acceptance (fixes A-01): every field /
// parameter node in the fixture corpus carries a non-empty FieldType /
// DataType.
func TestNoEmptyDataType(t *testing.T) {
	files := map[string]string{}
	loadRealFile(t, files, "internal/tui/programs/analyze/program.go", "analyze/program.go")
	loadRealFile(t, files, "internal/ai_engine/provider/types.go", "provider/types.go")
	loadRealFile(t, files, "internal/ai_engine/agent/dispatcher.go", "agent/dispatcher.go")
	files["app/Model.java"] = `package app;

class A {
    private int id;
    void run(int x, String s) {}
}
`
	payload := RunStage2(t, files)

	for rel, root := range payload.UpsertedTrees {
		var nodes []*GASTNode
		collectNodes(root, &nodes)
		for _, n := range nodes {
			if n.Type == GASTField && n.FieldType == "" {
				t.Errorf("%s: field %s has empty FieldType", rel, n.Name)
			}
			if n.Type == GASTParameter && n.DataType == "" {
				t.Errorf("%s: parameter %s has empty DataType", rel, n.Name)
			}
		}
	}
}

// TestV1PayloadCompatibility is the §5.2.4 acceptance (Rule 5.2.1.1): a v1
// GAST payload marshals, unmarshals into the v2 struct (new fields zero),
// and re-marshals to the identical JSON because new fields are omitempty.
func TestV1PayloadCompatibility(t *testing.T) {
	v1 := &GASTNode{
		ID:        "pkg.Foo",
		Type:      GASTTypeDeclaration,
		Name:      "Foo",
		Kind:      "struct",
		Namespace: "pkg",
		Children: []*GASTNode{
			{ID: "pkg.Foo.Bar", Type: GASTField, Name: "Bar", Kind: "field"},
		},
	}
	first, err := json.Marshal(v1)
	if err != nil {
		t.Fatalf("marshal v1: %v", err)
	}
	asV2 := &GASTNode{}
	if err := json.Unmarshal(first, asV2); err != nil {
		t.Fatalf("unmarshal as v2: %v", err)
	}
	if asV2.ReturnType != "" || asV2.BaseTypes != nil || asV2.Implemented != nil ||
		asV2.TypeParams != nil || asV2.FieldType != "" || asV2.EmbeddedOf != "" ||
		asV2.Signature != "" || asV2.IsVirtual || asV2.View != "" {
		t.Errorf("v1 payload leaked v2 fields: %+v", asV2)
	}
	second, err := json.Marshal(asV2)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("re-marshal drifted:\n v1: %s\n v2: %s", first, second)
	}
	if strings.Contains(string(second), "return_type") || strings.Contains(string(second), "base_types") {
		t.Errorf("v2 JSON contains empty new fields: %s", second)
	}
}
