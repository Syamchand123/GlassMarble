package stages_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// findNodesInFile collects node IDs whose FileSpec.Path matches relPath.
func findNodesInFile(out *stage4.Stage4Output, relPath string) []string {
	var ids []string
	for id, node := range out.GraphNodes {
		if stage3.NormalizeRelativePath(node.FileSpec.Path) == relPath {
			ids = append(ids, id)
		}
	}
	return ids
}

// callEdgeBetween reports whether any CALLS edge connects a node in srcFile
// to a node in dstFile. Edge endpoints are FQN/symbol IDs (no file paths),
// so both endpoint sets are resolved through GraphNodes' FileSpec first.
// Same-file calls resolve at LevelArchitecture; cross-file Go CALLS edges do
// not (only DEPENDS_ON between file nodes does — see TestStage4LinkArchitecture).
func callEdgeBetween(out *stage4.Stage4Output, srcFile, dstFile string) bool {
	toSet := func(ids []string) map[string]bool {
		set := make(map[string]bool, len(ids))
		for _, id := range ids {
			set[id] = true
		}
		return set
	}
	srcIDs := toSet(findNodesInFile(out, srcFile))
	dstIDs := toSet(findNodesInFile(out, dstFile))
	for _, edges := range out.OutboundEdges {
		for _, e := range edges {
			if e.Type != stage4.EdgeCalls {
				continue
			}
			if srcIDs[e.SourceID] && dstIDs[e.TargetID] {
				return true
			}
		}
	}
	return false
}

// edgeBetweenFileFiles reports whether an edge of any of the given types
// connects the two file nodes directly (file-level dependency edges).
func edgeBetweenFileFiles(out *stage4.Stage4Output, srcFile, dstFile string, types ...stage4.RelationshipType) bool {
	want := make(map[stage4.RelationshipType]bool, len(types))
	for _, ty := range types {
		want[ty] = true
	}
	for _, edges := range out.OutboundEdges {
		for _, e := range edges {
			if !want[e.Type] {
				continue
			}
			if slashed(e.SourceID) == "file:"+srcFile && slashed(e.TargetID) == "file:"+dstFile {
				return true
			}
		}
	}
	return false
}

func TestStage4LinkArchitecture(t *testing.T) {
	sb := newSampleSandbox(t)
	linked := runPipeline(t, sb, "feedfacefeedface", stage4.LevelArchitecture)

	if len(linked.GraphNodes) == 0 {
		t.Fatal("Link produced zero graph nodes")
	}
	if len(findNodesInFile(linked, "internal/service/service.go")) == 0 {
		t.Error("no graph node for internal/service/service.go")
	}
	if linked.GraphNodes["module:internal/service"] == nil {
		t.Error("missing MODULE node for internal/service")
	}
	if linked.GraphNodes["file:internal/service/service.go"] == nil {
		t.Error("missing FILE node for internal/service/service.go")
	}
	// Architecture level resolves the main -> service link as a file-level
	// DEPENDS_ON edge (verified: no cross-file Go CALLS edges are produced;
	// same-file CALLS do resolve, e.g. web/app.js -> fetchGreeting).
	if !edgeBetweenFileFiles(linked, "cmd/api/main.go", "internal/service/service.go", stage4.EdgeDependsOn) {
		t.Error("no DEPENDS_ON edge from cmd/api/main.go to internal/service/service.go")
	}
	if !callEdgeBetween(linked, "web/app.js", "web/app.js") {
		t.Error("no same-file CALLS edge in web/app.js")
	}
	if len(linked.EntrypointRegistry) == 0 {
		t.Error("EntrypointRegistry empty in stage 4 output")
	}
}

func TestStage4LevelFullHasMoreNodes(t *testing.T) {
	sb := newSampleSandbox(t)
	arch := runPipeline(t, sb, "archhash", stage4.LevelArchitecture)
	full := runPipeline(t, sb, "fullhash", stage4.LevelFull)

	if len(full.GraphNodes) < len(arch.GraphNodes) {
		t.Errorf("LevelFull produced %d nodes, fewer than architecture level %d", len(full.GraphNodes), len(arch.GraphNodes))
	}
}

func TestStage4QualityMetrics(t *testing.T) {
	sb := newSampleSandbox(t)
	linked := runPipeline(t, sb, "qualityhash", stage4.LevelArchitecture)

	q := stage4.MeasureQuality(linked)
	if q.TotalNodes == 0 {
		t.Error("MeasureQuality TotalNodes = 0")
	}
	if q.TotalEdges == 0 {
		t.Error("MeasureQuality TotalEdges = 0")
	}

	// Persisted-graph quality view after a transaction commit.
	tm, err := akg.NewAKGTransactionManager(sb.GmDir)
	if err != nil {
		t.Fatalf("akg.NewAKGTransactionManager: %v", err)
	}
	_, _, _, modified := runStages123(t, sb, "qualityhash")
	if err := tm.ExecuteDeltaTransaction(linked, modified); err != nil {
		t.Fatalf("tm.ExecuteDeltaTransaction: %v", err)
	}
	gq := akg.MeasureGraphQuality(tm.GetActiveGraph())
	if gq.TotalNodes == 0 {
		t.Error("akg.MeasureGraphQuality TotalNodes = 0")
	}
	if gq.DanglingEdges != 0 {
		t.Errorf("merged graph has %d dangling edges", gq.DanglingEdges)
	}
}

func TestStage4TransactionManager(t *testing.T) {
	sb := newSampleSandbox(t)
	hash := "beefcafebeefcafe"
	_, _, agg, modified := runStages123(t, sb, hash)
	linked, err := stage4.Link(agg, modified, akg.NewCodePropertyGraph(hash), stage4.LinkerConfig{LevelOfDetail: stage4.LevelArchitecture})
	if err != nil {
		t.Fatalf("stage4.Link: %v", err)
	}

	tm, err := akg.NewAKGTransactionManager(sb.GmDir)
	if err != nil {
		t.Fatalf("akg.NewAKGTransactionManager: %v", err)
	}

	if err := tm.ExecuteDeltaTransaction(linked, modified); err != nil {
		t.Fatalf("first ExecuteDeltaTransaction: %v", err)
	}
	graph := tm.GetActiveGraph()
	if graph.Nodes.Len() == 0 {
		t.Fatal("active graph has no nodes after transaction")
	}
	if !sb.Exists(filepath.Join(".glassmarble", "akg.json")) {
		t.Error("akg.json was not written to .glassmarble")
	}
	v1 := graph.Version
	if v1 == 0 {
		t.Error("graph version is 0 after first transaction")
	}

	// Re-applying the identical delta is idempotent-ish: no error, version grows.
	if err := tm.ExecuteDeltaTransaction(linked, modified); err != nil {
		t.Fatalf("second ExecuteDeltaTransaction: %v", err)
	}
	v2 := tm.GetActiveGraph().Version
	if v2 <= v1 {
		t.Errorf("graph version did not increase across transactions: %d -> %d", v1, v2)
	}

	commitHash, schemaVersion, version, err := akg.StateMetadata(sb.GmDir)
	if err != nil {
		t.Fatalf("akg.StateMetadata: %v", err)
	}
	if commitHash != hash {
		t.Errorf("StateMetadata commitHash = %q, want %q", commitHash, hash)
	}
	if schemaVersion != akg.CurrentSchemaVersion {
		t.Errorf("StateMetadata schemaVersion = %d, want %d", schemaVersion, akg.CurrentSchemaVersion)
	}
	if version == 0 {
		t.Error("StateMetadata version = 0, want > 0")
	}
}

func TestStage4SubscribeCommitEvent(t *testing.T) {
	sb := newSampleSandbox(t)
	hash := "c0ffee00c0ffee00"
	_, _, agg, modified := runStages123(t, sb, hash)
	linked, err := stage4.Link(agg, modified, akg.NewCodePropertyGraph(hash), stage4.LinkerConfig{LevelOfDetail: stage4.LevelArchitecture})
	if err != nil {
		t.Fatalf("stage4.Link: %v", err)
	}

	tm, err := akg.NewAKGTransactionManager(sb.GmDir)
	if err != nil {
		t.Fatalf("akg.NewAKGTransactionManager: %v", err)
	}
	sub := tm.Subscribe()
	if err := tm.ExecuteDeltaTransaction(linked, modified); err != nil {
		t.Fatalf("ExecuteDeltaTransaction: %v", err)
	}

	select {
	case ev := <-sub:
		if ev.CommitHash != hash {
			t.Errorf("event CommitHash = %q, want %q", ev.CommitHash, hash)
		}
		if ev.NodeCount == 0 {
			t.Error("event NodeCount = 0")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no AKGCommitEvent received within 5s")
	}
}

func TestStage4ReplaceGraphStateMetadata(t *testing.T) {
	sb := harness.NewSandbox(t)
	tm, err := akg.NewAKGTransactionManager(sb.GmDir)
	if err != nil {
		t.Fatalf("akg.NewAKGTransactionManager: %v", err)
	}

	g := akg.NewCodePropertyGraph("replacehash1234")
	g.Nodes = g.Nodes.Set("internal/service/service.go::New", &stage4.ResolvedNode{
		ID:   "internal/service/service.go::New",
		Kind: "FUNCTION",
		Name: "New",
		FileSpec: stage4.LocationMeta{
			Path:      "internal/service/service.go",
			LineStart: 1,
			LineEnd:   2,
		},
	})
	if err := tm.ReplaceGraph(g); err != nil {
		t.Fatalf("tm.ReplaceGraph: %v", err)
	}

	if n := tm.GetActiveGraph().Nodes.Len(); n != 1 {
		t.Errorf("active graph node count = %d, want 1", n)
	}

	commitHash, schemaVersion, version, err := akg.StateMetadata(sb.GmDir)
	if err != nil {
		t.Fatalf("akg.StateMetadata: %v", err)
	}
	if commitHash != "replacehash1234" {
		t.Errorf("StateMetadata commitHash = %q, want replacehash1234", commitHash)
	}
	if schemaVersion != akg.CurrentSchemaVersion {
		t.Errorf("StateMetadata schemaVersion = %d, want %d", schemaVersion, akg.CurrentSchemaVersion)
	}
	if version == 0 {
		t.Error("StateMetadata version = 0, want > 0")
	}
}

func TestStage4DiffGraphs(t *testing.T) {
	base := akg.NewCodePropertyGraph("basecommit")
	base.Nodes = base.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "A"})
	base.Nodes = base.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "B"})
	base.OutboundEdges = base.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls, LineNumber: 1}})

	head := akg.NewCodePropertyGraph("headcommit")
	head.Nodes = head.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "A"})
	head.Nodes = head.Nodes.Set("c", &stage4.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "C"})
	head.OutboundEdges = head.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "c", Type: stage4.EdgeCalls, LineNumber: 1}})

	diff := akg.DiffGraphs(base, head)
	if diff.BaseCommit != "basecommit" || diff.HeadCommit != "headcommit" {
		t.Errorf("diff commits = %q -> %q", diff.BaseCommit, diff.HeadCommit)
	}
	if len(diff.NodesAdded) != 1 || diff.NodesAdded[0].ID != "c" {
		t.Errorf("NodesAdded = %+v, want [c]", diff.NodesAdded)
	}
	if len(diff.NodesRemoved) != 1 || diff.NodesRemoved[0].ID != "b" {
		t.Errorf("NodesRemoved = %+v, want [b]", diff.NodesRemoved)
	}
	if len(diff.EdgesAdded) != 1 || diff.EdgesAdded[0].TargetID != "c" {
		t.Errorf("EdgesAdded = %+v, want [a->c]", diff.EdgesAdded)
	}
	if len(diff.EdgesRemoved) != 1 || diff.EdgesRemoved[0].TargetID != "b" {
		t.Errorf("EdgesRemoved = %+v, want [a->b]", diff.EdgesRemoved)
	}
}
