package aggregate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
)

func TestNormalizeRelativePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{".", ""},
		{"foo/bar/baz.go", "foo/bar/baz.go"},
		{"./foo/bar/baz.go", "foo/bar/baz.go"},
		{"/foo/bar/baz.go", "foo/bar/baz.go"},
		{"C:/foo/bar/baz.go", "foo/bar/baz.go"},
		{`foo\bar\baz.go`, "foo/bar/baz.go"},
		{"./", ""},
		{"/", ""},
	}
	for _, tt := range tests {
		got := NormalizeRelativePath(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeRelativePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSplitPathToDirectories(t *testing.T) {
	tests := []struct {
		path     string
		wantDirs []string
		wantFile string
	}{
		{"src/core/database/postgres.go", []string{"src", "core", "database"}, "postgres.go"},
		{"main.go", nil, "main.go"},
		{"", nil, ""},
		{"a/b/c.go", []string{"a", "b"}, "c.go"},
	}
	for _, tt := range tests {
		dirs, file := SplitPathToDirectories(tt.path)
		if file != tt.wantFile {
			t.Errorf("SplitPathToDirectories(%q) file = %q, want %q", tt.path, file, tt.wantFile)
		}
		if len(dirs) != len(tt.wantDirs) {
			t.Errorf("SplitPathToDirectories(%q) dirs = %v, want %v", tt.path, dirs, tt.wantDirs)
			continue
		}
		for i := range dirs {
			if dirs[i] != tt.wantDirs[i] {
				t.Errorf("SplitPathToDirectories(%q) dir[%d] = %q, want %q", tt.path, i, dirs[i], tt.wantDirs[i])
			}
		}
	}
}

func TestNewDirectoryNode(t *testing.T) {
	node := NewDirectoryNode("database", "src/core/database")
	if node == nil {
		t.Fatal("NewDirectoryNode returned nil")
	}
	if node.FolderName != "database" {
		t.Errorf("FolderName = %q, want database", node.FolderName)
	}
	if node.RelativePath != "src/core/database" {
		t.Errorf("RelativePath = %q, want src/core/database", node.RelativePath)
	}
	if node.SubFolders == nil {
		t.Error("SubFolders should be initialized")
	}
	if node.Files == nil {
		t.Error("Files should be initialized")
	}
}

func TestDirectoryNodeAddFile(t *testing.T) {
	root := NewDirectoryNode("root", ".")
	root.Files["main.go"] = &FileBoundaryNode{
		FileName:     "main.go",
		RelativePath: "main.go",
		Language:     "go",
		LocalImports: []string{"fmt"},
	}

	if len(root.Files) != 1 {
		t.Errorf("len(Files) = %d, want 1", len(root.Files))
	}
	f := root.Files["main.go"]
	if f == nil {
		t.Fatal("main.go not found in Files")
	}
	if f.FileName != "main.go" {
		t.Errorf("FileName = %q, want main.go", f.FileName)
	}
	if len(f.LocalImports) != 1 || f.LocalImports[0] != "fmt" {
		t.Errorf("LocalImports = %v, want [fmt]", f.LocalImports)
	}
}

func TestDirectoryNodeAddSubFolder(t *testing.T) {
	root := NewDirectoryNode("root", ".")
	sub := NewDirectoryNode("src", "src")
	root.SubFolders["src"] = sub

	if len(root.SubFolders) != 1 {
		t.Errorf("len(SubFolders) = %d, want 1", len(root.SubFolders))
	}
	if root.SubFolders["src"].FolderName != "src" {
		t.Errorf("SubFolder name = %q, want src", root.SubFolders["src"].FolderName)
	}
}

func TestNewWorkspaceContext(t *testing.T) {
	wc := NewWorkspaceContext()
	if wc == nil {
		t.Fatal("NewWorkspaceContext returned nil")
	}
	if wc.Aliases == nil {
		t.Error("Aliases should be initialized")
	}
	if wc.ModuleBoundaries == nil {
		t.Error("ModuleBoundaries should be initialized")
	}
}

func TestWorkspaceContextGetModuleBoundary(t *testing.T) {
	wc := NewWorkspaceContext()
	wc.ModuleBoundaries = []string{"src/core", "src/api"}

	// Deepest matching boundary
	boundary := wc.GetModuleBoundary("src/core/database/postgres.go")
	if boundary != "src/core" {
		t.Errorf("GetModuleBoundary = %q, want src/core", boundary)
	}

	boundary = wc.GetModuleBoundary("src/api/handler.go")
	if boundary != "src/api" {
		t.Errorf("GetModuleBoundary = %q, want src/api", boundary)
	}

	// No match
	boundary = wc.GetModuleBoundary("cmd/main.go")
	if boundary != "" {
		t.Errorf("GetModuleBoundary = %q, want empty", boundary)
	}
}

func TestAggregateOutputStructure(t *testing.T) {
	out := &AggregateOutput{
		CommitHash:            "abc123",
		RootNode:              NewDirectoryNode(".", ""),
		GlobalDefinitionIndex: make(map[string][]*normalize.GASTNode),
		GlobalCallQueue:       make([]LinkedCallSite, 0),
		FileToSymbols:         make(map[string][]string),
		FileToCalls:           make(map[string][]LinkedCallSite),
		LocalTables:           make(map[string]*normalize.FileSymbolTable),
		WorkspaceCtx:          NewWorkspaceContext(),
		ExternalDependencies:  make(map[string]*normalize.GASTNode),
		GenericsRegistry:      make(map[string]string),
		EntrypointRegistry:    make([]string, 0),
	}

	if out.CommitHash != "abc123" {
		t.Errorf("CommitHash = %q, want abc123", out.CommitHash)
	}
	if out.RootNode == nil {
		t.Error("RootNode should not be nil")
	}
}

func TestLinkedCallSiteStructure(t *testing.T) {
	lcs := LinkedCallSite{
		SourceFileNodeID: "main.go::main",
		SourceFilePath:   "main.go",
		SourceFolderPath: ".",
		ReceiverName:     "fmt",
		MethodName:       "Println",
		LineNumber:       10,
		HasPrimitive:     false,
		LocalImports:     []string{"fmt"},
	}
	if lcs.SourceFileNodeID != "main.go::main" {
		t.Errorf("SourceFileNodeID = %q, want main.go::main", lcs.SourceFileNodeID)
	}
	if lcs.MethodName != "Println" {
		t.Errorf("MethodName = %q, want Println", lcs.MethodName)
	}
}

func TestIndexedNode(t *testing.T) {
	in := IndexedNode{
		Key:  "src.main.main",
		Node: &normalize.GASTNode{Name: "main", Type: normalize.GASTFunction},
	}
	if in.Key != "src.main.main" {
		t.Errorf("Key = %q, want src.main.main", in.Key)
	}
	if in.Node == nil || in.Node.Name != "main" {
		t.Errorf("Node.Name = %q, want main", in.Node.Name)
	}
}

func TestExtractCallsFromFile(t *testing.T) {
	st := &normalize.FileSymbolTable{
		RelPath: "main.go",
		Imports: []string{"fmt"},
		LocalCalls: []normalize.CallSite{
			{
				CallerNodeID: "main.go::main",
				ReceiverName: "fmt",
				MethodName:   "Println",
				LineNumber:   10,
			},
		},
	}
	calls := extractCallsFromFile("main.go", st)
	if len(calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(calls))
	}
	if calls[0].MethodName != "Println" {
		t.Errorf("MethodName = %q, want Println", calls[0].MethodName)
	}
	if calls[0].SourceFilePath != "main.go" {
		t.Errorf("SourceFilePath = %q, want main.go", calls[0].SourceFilePath)
	}
	if len(calls[0].LocalImports) != 1 {
		t.Errorf("len(LocalImports) = %d, want 1", len(calls[0].LocalImports))
	}
}

func TestExtractCallsFromFileEmpty(t *testing.T) {
	st := &normalize.FileSymbolTable{
		RelPath:    "empty.go",
		LocalCalls: nil,
	}
	calls := extractCallsFromFile("empty.go", st)
	if len(calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(calls))
	}
}

func TestImportResolvers(t *testing.T) {
	wc := NewWorkspaceContext()

	tests := []struct {
		name       string
		resolver   ImportResolver
		importPath string
		fromFile   string
		rootDir    string
		minResults int
	}{
		{"Generic", &GenericImportResolver{}, "github.com/user/repo/pkg", "main.go", ".", 1},
		{"Generic relative", &GenericImportResolver{}, "./utils", "main.go", ".", 1},
		{"Go", &GoImportResolver{}, "fmt", "main.go", ".", 1},
		{"Go with module", &GoImportResolver{}, "github.com/org/repo/pkg/auth", "main.go", ".", 1},
		{"Python", &PythonImportResolver{}, "os.path", "main.py", ".", 1},
		{"Java", &JavaImportResolver{}, "java.util.List", "Main.java", ".", 1},
		{"TS relative", &TSImportResolver{}, "./component", "app.ts", ".", 1},
		{"TS absolute", &TSImportResolver{}, "@/components/ui", "app.ts", ".", 1},
	}

	for _, tt := range tests {
		results := tt.resolver.Resolve(tt.importPath, tt.fromFile, tt.rootDir, wc)
		if len(results) < tt.minResults {
			t.Errorf("%s resolver.Resolve(%q) returned %d results, want at least %d; got %v",
				tt.name, tt.importPath, len(results), tt.minResults, results)
		}
	}
}

func TestImportResolverEmpty(t *testing.T) {
	wc := NewWorkspaceContext()
	resolver := &GenericImportResolver{}
	results := resolver.Resolve("", "main.go", ".", wc)
	if results != nil {
		t.Errorf("empty import should return nil, got %v", results)
	}
}

func TestGoImportResolverWithModulePrefix(t *testing.T) {
	wc := NewWorkspaceContext()
	wc.ModulePrefix = "github.com/org/repo"

	resolver := &GoImportResolver{}
	results := resolver.Resolve("github.com/org/repo/pkg/auth", "main.go", ".", wc)
	if len(results) < 1 {
		t.Fatal("expected at least 1 result")
	}
	// Should find local path "pkg/auth"
	foundLocal := false
	for _, r := range results {
		if r == "pkg/auth" {
			foundLocal = true
			break
		}
	}
	if !foundLocal {
		t.Errorf("expected 'pkg/auth' in results, got %v", results)
	}
}

func TestTSImportResolverWithAliases(t *testing.T) {
	wc := NewWorkspaceContext()
	wc.Aliases["@"] = "src"
	wc.Aliases["@components"] = "src/components"

	resolver := &TSImportResolver{}
	results := resolver.Resolve("@/components/ui/button", "app.ts", ".", wc)
	if len(results) < 1 {
		t.Fatal("expected at least 1 result")
	}
}

func TestAggregateNilInput(t *testing.T) {
	out, err := Aggregate(nil, nil, ".")
	if err != nil {
		t.Fatalf("Aggregate(nil, nil) returned error: %v", err)
	}
	if out == nil {
		t.Fatal("Aggregate(nil, nil) returned nil output")
	}
	if out.CommitHash != "" {
		t.Errorf("CommitHash = %q, want empty", out.CommitHash)
	}
	if out.RootNode == nil {
		t.Error("RootNode should be initialized")
	}
}

func TestAggregateEmptyPayload(t *testing.T) {
	payload := &normalize.NormalizeOutput{
		CommitHash:        "abc",
		UpsertedTrees:     make(map[string]*normalize.GASTNode),
		LocalSymbolTables: make(map[string]*normalize.FileSymbolTable),
	}
	out, err := Aggregate(payload, nil, ".")
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}
	if out.CommitHash != "abc" {
		t.Errorf("CommitHash = %q, want abc", out.CommitHash)
	}
}

func TestAggregateWithExistingState(t *testing.T) {
	existing := &AggregateOutput{
		CommitHash:            "prev",
		RootNode:              NewDirectoryNode(".", ""),
		GlobalDefinitionIndex: make(map[string][]*normalize.GASTNode),
		LocalTables:           make(map[string]*normalize.FileSymbolTable),
		WorkspaceCtx:          NewWorkspaceContext(),
	}
	payload := &normalize.NormalizeOutput{
		CommitHash:        "new",
		UpsertedTrees:     make(map[string]*normalize.GASTNode),
		LocalSymbolTables: make(map[string]*normalize.FileSymbolTable),
	}
	out, err := Aggregate(payload, existing, ".")
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}
	if out.CommitHash != "new" {
		t.Errorf("CommitHash = %q, want new", out.CommitHash)
	}
	// Should reuse existing RootNode
	if out.RootNode != existing.RootNode {
		t.Error("should reuse existing RootNode")
	}
}

func TestAggregateWithOneFile(t *testing.T) {
	gastRoot := &normalize.GASTNode{
		Type: normalize.GASTFileRoot,
		Name: "main.go",
		Children: []*normalize.GASTNode{
			{
				Type: normalize.GASTFunction,
				Name: "main",
				Properties: map[string]string{
					"fully_qualified_name": "main.main",
				},
			},
		},
	}
	symTable := &normalize.FileSymbolTable{
		FilePath: "/root/main.go",
		RelPath:  "main.go",
		Language: "go",
		Imports:  []string{"fmt"},
	}

	payload := &normalize.NormalizeOutput{
		CommitHash:        "abc",
		UpsertedTrees:     map[string]*normalize.GASTNode{"main.go": gastRoot},
		LocalSymbolTables: map[string]*normalize.FileSymbolTable{"main.go": symTable},
	}
	out, err := Aggregate(payload, nil, ".")
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}
	if out.CommitHash != "abc" {
		t.Errorf("CommitHash = %q, want abc", out.CommitHash)
	}
	if _, exists := out.LocalTables["main.go"]; !exists {
		t.Error("LocalTables should contain main.go")
	}
	if len(out.FileToSymbols["main.go"]) == 0 {
		t.Error("FileToSymbols should have symbols for main.go")
	}
	// GlobalCallQueue should include calls
	if out.GlobalCallQueue == nil {
		t.Error("GlobalCallQueue should be initialized")
	}
}

func TestSynthesizeGlobalCallQueue(t *testing.T) {
	localTables := map[string]*normalize.FileSymbolTable{
		"main.go": {
			RelPath: "main.go",
			LocalCalls: []normalize.CallSite{
				{CallerNodeID: "main", ReceiverName: "fmt", MethodName: "Println", LineNumber: 10},
			},
		},
	}

	queue := SynthesizeGlobalCallQueue(localTables)
	if len(queue) != 1 {
		t.Fatalf("len(queue) = %d, want 1", len(queue))
	}
	if queue[0].MethodName != "Println" {
		t.Errorf("MethodName = %q, want Println", queue[0].MethodName)
	}
}

func TestSynthesizeGlobalCallQueueEmpty(t *testing.T) {
	queue := SynthesizeGlobalCallQueue(nil)
	if len(queue) != 0 {
		t.Errorf("empty input should return empty queue, got %d", len(queue))
	}
}

func TestSynthesizeGlobalDefinitionIndex(t *testing.T) {
	root := NewDirectoryNode(".", "")
	root.Files["main.go"] = &FileBoundaryNode{
		FileName:     "main.go",
		RelativePath: "main.go",
		GASTRoot: &normalize.GASTNode{
			Type: normalize.GASTFileRoot,
			Children: []*normalize.GASTNode{
				{
					Type: normalize.GASTFunction,
					Name: "main",
					Properties: map[string]string{
						"fully_qualified_name": "main.main",
						"file_path":            "main.go",
					},
				},
			},
		},
	}

	index := SynthesizeGlobalDefinitionIndex(root)
	if len(index) == 0 {
		t.Error("index should not be empty for a file with functions")
	}
	// Should find the function indexed
	_, exists := index["main.main"]
	if !exists {
		t.Error("index should contain main.main")
	}
}

func TestComputeVisibilityEnclave(t *testing.T) {
	wc := NewWorkspaceContext()
	node := &normalize.GASTNode{
		Type:       normalize.GASTFunction,
		Name:       "myFunc",
		Visibility: "public",
		Children: []*normalize.GASTNode{
			{Type: normalize.GASTVariable, Name: "localVar", Visibility: "internal"},
		},
	}

	ComputeVisibilityEnclave(node, "src/main.go", wc)

	if node.Properties == nil || node.Properties["namespace_scope"] != "Public" {
		t.Errorf("parent scope = %q, want Public", node.Properties["namespace_scope"])
	}
	// Child should also be processed
	child := node.Children[0]
	if child.Properties == nil || child.Properties["namespace_scope"] != "PackagePrivate" {
		t.Errorf("child scope = %q, want PackagePrivate", child.Properties["namespace_scope"])
	}
}

func TestEntrypointDetector(t *testing.T) {
	output := &AggregateOutput{
		GlobalDefinitionIndex: map[string][]*normalize.GASTNode{
			"cmd.main": {
				{
					Name: "main",
					Type: normalize.GASTFunction,
					Properties: map[string]string{
						"fully_qualified_name": "cmd.main",
					},
				},
			},
		},
		EntrypointRegistry: make([]string, 0),
	}

	IndexEntrypoints(output)

	if len(output.EntrypointRegistry) == 0 {
		t.Error("main should be detected as entrypoint")
	}
	found := false
	for _, ep := range output.EntrypointRegistry {
		if ep == "cmd.main" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("cmd.main not found in EntrypointRegistry: %v", output.EntrypointRegistry)
	}
}

func TestFileBoundaryNode(t *testing.T) {
	fbn := FileBoundaryNode{
		FileName:     "postgres.go",
		RelativePath: "src/database/postgres.go",
		Language:     "go",
		LocalImports: []string{"database/sql", "fmt"},
	}
	if fbn.FileName != "postgres.go" {
		t.Errorf("FileName = %q, want postgres.go", fbn.FileName)
	}
	if len(fbn.LocalImports) != 2 {
		t.Errorf("len(LocalImports) = %d, want 2", len(fbn.LocalImports))
	}
}

func TestDirectoryNodePrimitiveZone(t *testing.T) {
	node := NewDirectoryNode("database", "src/core/database")
	node.PrimitiveZone = "DATABASE"
	if node.PrimitiveZone != "DATABASE" {
		t.Errorf("PrimitiveZone = %q, want DATABASE", node.PrimitiveZone)
	}
}

func TestCollectExportedGASTNodes(t *testing.T) {
	root := &normalize.GASTNode{
		Type: normalize.GASTFileRoot,
		Name: "main.go",
		Children: []*normalize.GASTNode{
			{
				Type: normalize.GASTFunction,
				Name: "main",
				Properties: map[string]string{
					"fully_qualified_name": "main.main",
				},
			},
			{
				Type: normalize.GASTVariable,
				Name: "globalVar",
			},
		},
	}

	var nodes []IndexedNode
	var localSyms []string
	collectExportedGASTNodes(root, "main.go", &nodes, &localSyms)

	if len(nodes) == 0 {
		t.Error("should collect exported nodes")
	}
	if len(localSyms) == 0 {
		t.Error("should collect local symbols")
	}
}

// TestAggregateDiscoversWorkspaceFromRootDir verifies Aggregate scans the
// provided rootDir (not the process CWD) for workspace config. Regression for
// the bug where ScanWorkspace(".") resolved module boundaries relative to the
// working directory, breaking monorepo aliases when gmb runs elsewhere.
func TestAggregateDiscoversWorkspaceFromRootDir(t *testing.T) {
	root := t.TempDir()
	modFile := filepath.Join(root, "go.mod")
	if err := os.WriteFile(modFile, []byte("module github.com/acme/widget\n"), 0644); err != nil {
		t.Fatal(err)
	}

	payload := &normalize.NormalizeOutput{
		CommitHash:        "abc",
		UpsertedTrees:     make(map[string]*normalize.GASTNode),
		LocalSymbolTables: make(map[string]*normalize.FileSymbolTable),
	}
	out, err := Aggregate(payload, nil, root)
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}
	if out.WorkspaceCtx == nil || out.WorkspaceCtx.ModulePrefix != "github.com/acme/widget" {
		t.Errorf("ModulePrefix = %q, want github.com/acme/widget", out.WorkspaceCtx.ModulePrefix)
	}
}
