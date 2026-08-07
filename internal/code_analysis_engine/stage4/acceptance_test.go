package stage4

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// Helper: build a Stage3Output from a map of filename->source.
func buildStage3FromSource(t *testing.T, sources map[string]string) *stage3.Stage3Output {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range sources {
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
	payload, err := stage2.Normalize(out, "acceptance")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	s3out, err := stage3.Aggregate(payload, nil, "")
	if err != nil {
		t.Fatalf("stage3.Aggregate failed: %v", err)
	}
	return s3out
}

// Helper: run the full linker on a Stage3Output with given modified files.
func link(t *testing.T, stage3Out *stage3.Stage3Output, modified []string) *Stage4Output {
	t.Helper()
	out, err := Link(stage3Out, modified, nil)
	if err != nil {
		t.Fatalf("Link failed: %v", err)
	}
	return out
}

// Helper: count edges of a given type in the CPG.
func countEdgeType(cpg *Stage4Output, typ RelationshipType) int {
	n := 0
	for _, edges := range cpg.OutboundEdges {
		for _, e := range edges {
			if e.Type == typ {
				n++
			}
		}
	}
	return n
}

// Helper: check if any edge of a given type exists.
func hasEdgeType(cpg *Stage4Output, typ RelationshipType) bool {
	for _, edges := range cpg.OutboundEdges {
		for _, e := range edges {
			if e.Type == typ {
				return true
			}
		}
	}
	return false
}

// TestMemberLinker_GoStruct: fixture struct with fields+methods → hasField ×n, hasReceiver ×m, all with provenance `ast`.
func TestMemberLinker_GoStruct(t *testing.T) {
	sources := map[string]string{
		"order.go": `package main

type Order struct {
	ID    int
	Price float64
}

func (o *Order) Save() error {
	return nil
}
`,
	}
	stage3Out := buildStage3FromSource(t, sources)
	cpg := link(t, stage3Out, []string{"order.go"})

	// Count HAS_FIELD edges (should be 2: ID and Price)
	hasFieldCount := countEdgeType(cpg, EdgeHasField)
	if hasFieldCount != 2 {
		t.Errorf("expected 2 HAS_FIELD edges, got %d", hasFieldCount)
	}
	// Count HAS_RECEIVER edges (should be 1: Save)
	hasReceiverCount := countEdgeType(cpg, EdgeHasReceiver)
	if hasReceiverCount != 1 {
		t.Errorf("expected 1 HAS_RECEIVER edge, got %d", hasReceiverCount)
	}
	// Verify provenance is "ast" for each such edge.
	for _, edges := range cpg.OutboundEdges {
		for _, e := range edges {
			if e.Type == EdgeHasField || e.Type == EdgeHasReceiver {
				if prov, ok := e.Properties[ont.PredProvenance]; !ok || prov != "ast" {
					t.Errorf("edge %v missing or incorrect provenance (expected ast, got %q)", e, prov)
				}
			}
		}
	}
}

// TestMemberLinker_GoEmbedding: `type B struct { A }` → `extends` A with `gm:embedding "true"`.
func TestMemberLinker_GoEmbedding(t *testing.T) {
	sources := map[string]string{
		"mix.go": `package main

type A struct{}

type B struct {
	A
}
`,
	}
	stage3Out := buildStage3FromSource(t, sources)
	cpg := link(t, stage3Out, []string{"mix.go"})

	// Look for EdgeExtends from B to A with PredEmbedding "true".
	found := false
	for _, edges := range cpg.OutboundEdges {
		for _, e := range edges {
			if e.Type == EdgeExtends && e.SourceID == "mix.go::B" && e.TargetID == "mix.go::A" {
				if emb, ok := e.Properties[ont.PredEmbedding]; ok && emb == "true" {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Errorf("expected EdgeExtends with gm:embedding=true from B to A")
	}
}

// TestInterfaceLinker_FullRebuild: `--full` (empty ModifiedFiles) now emits implements edges for a fixture interface/struct pair.
func TestInterfaceLinker_FullRebuild(t *testing.T) {
	sources := map[string]string{
		"repo.go": `package main

type Runner interface {
	Run() error
}

type Robot struct{}

func (r Robot) Run() error {
	return nil
}
`,
	}
	stage3Out := buildStage3FromSource(t, sources)
	cpg := link(t, stage3Out, nil) // empty ModifiedFiles => full rebuild

	// Expects IMPLEMENTS edge from Robot to Runner.
	found := false
	for _, edges := range cpg.OutboundEdges {
		for _, e := range edges {
			if e.Type == EdgeImplements && e.SourceID == "repo.go::Robot" && e.TargetID == "repo.go::Runner" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("expected IMPLEMENTS edge from Robot to Runner in full rebuild")
	}
}

// TestSignatureEdges: `EdgeReturns`/`EdgeHasParam` emitted for fixture Go method with params+return.
func TestSignatureEdges(t *testing.T) {
	sources := map[string]string{
		"svc.go": `package main

type Report struct{}

func Process(data string, count int) Report {
	return Report{}
}
`,
	}
	stage3Out := buildStage3FromSource(t, sources)
	cpg := link(t, stage3Out, []string{"svc.go"})

	// We expect EdgeHasParam for the two parameters and EdgeReturns for the return type.
	// Note: EdgeReturns only fires if the return type resolves to a node in the graph.
	// Here we have a struct Report, so it should resolve.
	hasParamCount := countEdgeType(cpg, EdgeHasParam)
	returnsCount := countEdgeType(cpg, EdgeReturns)
	if hasParamCount != 2 {
		t.Errorf("expected 2 EdgeHasParam, got %d", hasParamCount)
	}
	if returnsCount != 1 {
		t.Errorf("expected 1 EdgeReturns, got %d", returnsCount)
	}
	// Check provenance.
	for _, edges := range cpg.OutboundEdges {
		for _, e := range edges {
			if e.Type == EdgeHasParam || e.Type == EdgeReturns {
				if prov, ok := e.Properties[ont.PredProvenance]; !ok || prov != "ast" {
					t.Errorf("signature edge %v missing or incorrect provenance (expected ast, got %q)", e, prov)
				}
			}
		}
	}
}

// TestAllEdgeConstantsProduced: table-driven: every one of the 44 RelationshipType constants has ≥1 production producer.
func TestAllEdgeConstantsProduced(t *testing.T) {
	// List of all RelationshipType constant names (must match those in type.go).
	constantNames := []string{
		"EdgeContains",
		"EdgeBelongsTo",
		"EdgeDependsOn",
		"EdgeImplements",
		"EdgeExtends",
		"EdgeMixes",
		"EdgeComposes",
		"EdgeHasField",
		"EdgeHasParam",
		"EdgeReturns",
		"EdgeHasReceiver",
		"EdgeCalls",
		"EdgeContextCall",
		"EdgeVirtualContext",
		"EdgeSpawnsConcurrent",
		"EdgeDefers",
		"EdgeCatches",
		"EdgeThrows",
		"EdgeReferences",
		"EdgeInstantiates",
		"EdgeDispatchesEvent",
		"EdgePublishes",
		"EdgeSubscribes",
		"EdgeSendsTo",
		"EdgeReceivesFrom",
		"EdgeQueriesDB",
		"EdgeCallsCloudAPI",
		"EdgeExposesEndpoint",
		"EdgeFFICall",
		"EdgeInjects",
		"EdgeConsumesResource",
		"EdgeMutatesGlobal",
		"EdgeNetworkCall",
		"EdgeControlFlow",
		"EdgeConditionalBranch",
		"EdgeLoopBranch",
		"EdgeSwitchBranch",
		"EdgeConstraint",
		"EdgeDataFlow",
		"EdgePointsTo",
		"EdgeHeapAlias",
		"EdgeAliases",
		"EdgeAliasesType",
		"EdgeCyclic",
		"EdgeVulnerable",
		"EdgeEscapesToHeap",
		"EdgeSecuritySink",
	}

	// Gather all non-test Go files in stage4 (excluding type.go itself, as it only contains declarations).
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	var prodFiles []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "type.go" {
			continue
		}
		prodFiles = append(prodFiles, f)
	}
	if len(prodFiles) == 0 {
		t.Fatalf("no production files found in stage4")
	}

	// For each constant name, check that it appears at least once in the production files.
	missing := []string{}
	for _, name := range constantNames {
		found := false
		for _, f := range prodFiles {
			data, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			if strings.Contains(string(data), name) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("the following edge constants have no production producer: %v", missing)
	}
}

// TestNoFuzzyResolution: interface/type linking produces no edges resolved via `strings.Contains` (grep-guard test fails the build if reintroduced).
func TestNoFuzzyResolution(t *testing.T) {
	// This test ensures that we do not re-introduce fuzzy matching via strings.Contains in the linking passes.
	// We'll search for the pattern "strings.Contains" in the linker passes (member_linker, type_linker, interface_linker).
	// Note: strings.Contains may appear in comments or strings; we ignore those by checking that it's not in a comment or string literal.
	// For simplicity, we just grep for "strings.Contains" and fail if any are found in the specified files.
	files := []string{
		"member_linker.go",
		"type_linker.go",
		"interface_linker.go",
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("failed to read %s: %v", f, err)
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			idx := strings.Index(line, "strings.Contains")
			if idx == -1 {
				continue
			}
			// Check if the occurrence is inside a comment.
			// Simple heuristic: if there is "//" before the occurrence on the same line, treat as comment.
			if commentIdx := strings.Index(line, "//"); commentIdx != -1 && commentIdx < idx {
				continue
			}
			// Also skip if the line is a block comment start/end? Not needed for now.
			t.Fatalf("fuzzy strings.Contains found in %s at line %d (this is prohibited by A-17): %s", f, i+1, strings.TrimSpace(line))
		}
	}
}
