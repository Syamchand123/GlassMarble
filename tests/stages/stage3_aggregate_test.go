package stages_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// findFileNode walks the DirectoryNode tree to locate a FileBoundaryNode by
// its slash-normalized relative path.
func findFileNode(t *testing.T, out *stage3.Stage3Output, relPath string) *stage3.FileBoundaryNode {
	t.Helper()
	var hit *stage3.FileBoundaryNode
	var walk func(dir *stage3.DirectoryNode)
	walk = func(dir *stage3.DirectoryNode) {
		if dir == nil {
			return
		}
		for _, file := range dir.Files {
			if stage3.NormalizeRelativePath(file.RelativePath) == relPath {
				hit = file
				return
			}
		}
		for _, sub := range dir.SubFolders {
			walk(sub)
		}
	}
	walk(out.RootNode)
	return hit
}

// aggregateFingerprint reduces an output to a comparable, deterministic form
// (file paths, index keys, entrypoint registry) for equality assertions.
func aggregateFingerprint(out *stage3.Stage3Output) []string {
	var keys []string
	var walk func(dir *stage3.DirectoryNode)
	walk = func(dir *stage3.DirectoryNode) {
		if dir == nil {
			return
		}
		for _, file := range dir.Files {
			keys = append(keys, "file:"+stage3.NormalizeRelativePath(file.RelativePath))
		}
		for _, sub := range dir.SubFolders {
			keys = append(keys, "dir:"+stage3.NormalizeRelativePath(sub.RelativePath))
			walk(sub)
		}
	}
	walk(out.RootNode)
	for key := range out.GlobalDefinitionIndex {
		keys = append(keys, "sym:"+key)
	}
	keys = append(keys, out.EntrypointRegistry...)
	sort.Strings(keys)
	return keys
}

func TestStage3AggregateTopology(t *testing.T) {
	sb := newSampleSandbox(t)
	_, _, agg, _ := runStages123(t, sb, "feedfacefeedface")

	if agg.RootNode == nil {
		t.Fatal("Aggregate produced nil RootNode")
	}

	service := findFileNode(t, agg, "internal/service/service.go")
	if service == nil {
		t.Fatalf("no FileBoundaryNode for internal/service/service.go")
	}
	if service.GASTRoot == nil {
		t.Error("service.go FileBoundaryNode has nil GASTRoot")
	}
	found := false
	for _, imp := range service.LocalImports {
		if imp == "example.com/shop/internal/repo" {
			found = true
		}
	}
	if !found {
		t.Errorf("service.go LocalImports missing example.com/shop/internal/repo: %v", service.LocalImports)
	}
	if service.Language != "go" {
		t.Errorf("service.go Language = %q, want go", service.Language)
	}

	if agg.LocalTables["internal/service/service.go"] == nil {
		t.Error("LocalTables missing internal/service/service.go")
	}
	syms := agg.FileToSymbols["internal/service/service.go"]
	if len(syms) == 0 {
		t.Errorf("FileToSymbols empty for internal/service/service.go")
	} else {
		// Method symbols are receiver-qualified ("Service.Greet"), not bare
		// names (verified against stage3.collectExportedGASTNodes).
		for _, want := range []string{"New", "Service", "Service.Greet"} {
			seen := false
			for _, sym := range syms {
				if sym == want {
					seen = true
				}
			}
			if !seen {
				t.Errorf("FileToSymbols[service.go] missing %q: %v", want, syms)
			}
		}
	}
	if agg.CommitHash != "feedfacefeedface" {
		t.Errorf("agg.CommitHash = %q, want feedfacefeedface", agg.CommitHash)
	}
}

func TestStage3PathHelpers(t *testing.T) {
	sb := newSampleSandbox(t)
	_, _, agg, _ := runStages123(t, sb, "hash")

	for _, tc := range []struct {
		in, want string
	}{
		{in: "G:/repo/src/main.go", want: "repo/src/main.go"},
		{in: "src\\core\\database.go", want: "src/core/database.go"},
		{in: "./cmd/api/main.go", want: "cmd/api/main.go"},
		{in: "main.go", want: "main.go"},
	} {
		if got := stage3.NormalizeRelativePath(tc.in); got != tc.want {
			t.Errorf("NormalizeRelativePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	dirs, name := stage3.SplitPathToDirectories("src/core/database/postgres.go")
	if len(dirs) != 3 || dirs[0] != "src" || dirs[2] != "database" || name != "postgres.go" {
		t.Errorf("SplitPathToDirectories(src/core/database/postgres.go) = (%v, %q)", dirs, name)
	}
	dirs, name = stage3.SplitPathToDirectories("main.go")
	if len(dirs) != 0 || name != "main.go" {
		t.Errorf("SplitPathToDirectories(main.go) = (%v, %q), want (nil, main.go)", dirs, name)
	}

	svc := agg.RootNode.SubFolders["internal"].SubFolders["service"]
	if svc == nil {
		t.Fatalf("aggregate tree missing internal/service directory")
	}
	if svc.RelativePath != "internal/service" {
		t.Errorf("service dir RelativePath = %q, want internal/service", svc.RelativePath)
	}
	if svc.Files["service.go"] == nil {
		t.Errorf("service dir missing service.go file node")
	}
}

func TestStage3FindEntryPoints(t *testing.T) {
	sb := newSampleSandbox(t)
	_, _, agg, _ := runStages123(t, sb, "hash")

	eps := stage3.FindEntryPoints(agg)
	found := false
	for _, ep := range eps {
		if ep.Kind == stage3.EntryPointMain && stage3.NormalizeRelativePath(ep.FilePath) == "cmd/api/main.go" && ep.Name == "main" {
			found = true
		}
	}
	if !found {
		t.Errorf("FindEntryPoints did not report cmd/api/main.go main(): %+v", eps)
	}
	if len(agg.EntrypointRegistry) == 0 {
		t.Error("EntrypointRegistry empty, IndexEntrypoints produced no entries")
	}
}

func TestStage3AggregateNilPreservesState(t *testing.T) {
	sb := newSampleSandbox(t)
	_, _, agg, _ := runStages123(t, sb, "hash")

	got, err := stage3.Aggregate(nil, agg, sb.Root)
	if err != nil {
		t.Fatalf("stage3.Aggregate(nil, existing): %v", err)
	}
	if got != agg {
		t.Error("Aggregate(nil, existing) did not return the existing output instance")
	}
	if len(agg.FileToSymbols) == 0 {
		t.Error("existing state lost its FileToSymbols")
	}
	if agg.RootNode == nil {
		t.Error("existing state lost its RootNode")
	}
}

func TestStage3AggregateDeterministic(t *testing.T) {
	sb := newSampleSandbox(t)
	_, payload, _, _ := runStages123(t, sb, "hash")

	first, err := stage3.Aggregate(payload, nil, sb.Root)
	if err != nil {
		t.Fatalf("stage3.Aggregate run 1: %v", err)
	}
	second, err := stage3.Aggregate(payload, nil, sb.Root)
	if err != nil {
		t.Fatalf("stage3.Aggregate run 2: %v", err)
	}

	fp1 := aggregateFingerprint(first)
	fp2 := aggregateFingerprint(second)
	if !reflect.DeepEqual(fp1, fp2) {
		t.Errorf("two Aggregate runs differ:\nrun1: %v\nrun2: %v", fp1, fp2)
	}
	if !reflect.DeepEqual(first.FileToSymbols, second.FileToSymbols) {
		t.Errorf("FileToSymbols differs between runs:\nrun1: %v\nrun2: %v", first.FileToSymbols, second.FileToSymbols)
	}
}
