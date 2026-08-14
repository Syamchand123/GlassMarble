package learning

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

func testGraph(t *testing.T) *akg.CodePropertyGraph {
	t.Helper()
	graph := &akg.CodePropertyGraph{
		Nodes:         akg.NewCowMap[string, *link.ResolvedNode](),
		FileNodeIndex: akg.NewCowMap[string, map[string]bool](),
	}
	graph.Nodes = graph.Nodes.Set("1", &link.ResolvedNode{Kind: "STRUCT", Name: "AuthService"})
	graph.Nodes = graph.Nodes.Set("2", &link.ResolvedNode{Kind: "STRUCT", Name: "UserService"})
	graph.Nodes = graph.Nodes.Set("3", &link.ResolvedNode{Kind: "STRUCT", Name: "PaymentHandler"})
	graph.Nodes = graph.Nodes.Set("4", &link.ResolvedNode{Kind: "STRUCT", Name: "Order"}) // no suffix
	graph.Nodes = graph.Nodes.Set("5", &link.ResolvedNode{Kind: "MODULE", Name: "checkout"})

	graph.FileNodeIndex = graph.FileNodeIndex.Set("internal/domain/auth_test.go", nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set("internal/domain/order.go", nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set("internal/api/routes_test.go", nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set("internal/api/svc_test.go", nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set("internal/infrastructure/db.go", nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set("internal/infrastructure/cache.go", nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set("docs/adr/001-init.md", nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set("docs/adr/002-cache.md", nil)
	return graph
}

func TestLearnConventionsFromGraph(t *testing.T) {
	conv := LearnConventions(testGraph(t), nil, WithLearnedAt(time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)))

	if conv.ServiceNamingPattern.Value != "*Service" {
		t.Errorf("service naming = %q, want *Service", conv.ServiceNamingPattern.Value)
	}
	if conv.ServiceNamingPattern.Evidence != 2 {
		t.Errorf("service naming evidence = %d, want 2", conv.ServiceNamingPattern.Evidence)
	}
	if conv.TestFilePattern.Value != "*_test.go" {
		t.Errorf("test pattern = %q, want *_test.go", conv.TestFilePattern.Value)
	}
	if conv.ADRDirectory.Value != "docs/adr" {
		t.Errorf("adr directory = %q, want docs/adr", conv.ADRDirectory.Value)
	}
	if conv.ADRDirectory.Confidence != 1.0 {
		t.Errorf("adr confidence = %f, want 1.0", conv.ADRDirectory.Confidence)
	}

	// Deterministic, sorted, gated by min evidence (2).
	wantLayers := []string{"api", "domain", "infrastructure"}
	if len(conv.LayerDirectories) != len(wantLayers) {
		t.Fatalf("layers = %v, want %v", conv.LayerDirectories, wantLayers)
	}
	for i, w := range wantLayers {
		if conv.LayerDirectories[i].Value != w {
			t.Errorf("layer[%d] = %q, want %q", i, conv.LayerDirectories[i].Value, w)
		}
	}
	if conv.LearnedAt.IsZero() {
		t.Error("learned_at must be set")
	}
}

func TestLearnConventionsMinEvidenceGate(t *testing.T) {
	// Only one service and one test file → nothing passes the evidence gate.
	graph := &akg.CodePropertyGraph{
		Nodes:         akg.NewCowMap[string, *link.ResolvedNode](),
		FileNodeIndex: akg.NewCowMap[string, map[string]bool](),
	}
	graph.Nodes = graph.Nodes.Set("1", &link.ResolvedNode{Kind: "STRUCT", Name: "AuthService"})
	graph.FileNodeIndex = graph.FileNodeIndex.Set("x_test.go", nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set("main.go", nil)

	conv := LearnConventions(graph, nil) // default min evidence 2
	if conv.ServiceNamingPattern.Evidence != 0 {
		t.Errorf("single occurrence must not become a convention: %+v", conv.ServiceNamingPattern)
	}
	if conv.TestFilePattern.Evidence != 0 {
		t.Errorf("single test file must not become a convention: %+v", conv.TestFilePattern)
	}

	// Explicit min evidence 1 reports it.
	conv = LearnConventions(graph, nil, WithMinEvidence(1))
	if conv.ServiceNamingPattern.Value != "*Service" || conv.TestFilePattern.Value != "*_test.go" {
		t.Errorf("min evidence 1 should report: %+v / %+v", conv.ServiceNamingPattern, conv.TestFilePattern)
	}
}

func TestLearnConventionsCrossPlatformPaths(t *testing.T) {
	// D7 regression: the graph can store Windows-style paths; the learner
	// must normalize them before splitting.
	graph := &akg.CodePropertyGraph{
		Nodes:         akg.NewCowMap[string, *link.ResolvedNode](),
		FileNodeIndex: akg.NewCowMap[string, map[string]bool](),
	}
	graph.FileNodeIndex = graph.FileNodeIndex.Set(`internal\domain\auth_test.go`, nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set(`internal\domain\order.go`, nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set(`internal\api\routes_test.go`, nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set(`internal\api\svc_test.go`, nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set(`internal\infrastructure\db.go`, nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set(`internal\infrastructure\cache.go`, nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set(`docs\adr\001-init.md`, nil)
	graph.FileNodeIndex = graph.FileNodeIndex.Set(`docs\adr\002-cache.md`, nil)

	conv := LearnConventions(graph, nil)
	if conv.ADRDirectory.Value != "docs/adr" {
		t.Errorf("adr directory from Windows path = %q, want docs/adr", conv.ADRDirectory.Value)
	}
	if conv.TestFilePattern.Value != "*_test.go" {
		t.Errorf("test pattern from Windows path = %q", conv.TestFilePattern.Value)
	}
	found := false
	for _, l := range conv.LayerDirectories {
		if l.Value == "domain" {
			found = true
		}
	}
	if !found {
		t.Errorf("domain layer lost from Windows path: %v", conv.LayerDirectories)
	}
}

func TestLearnConventionsFromMemory(t *testing.T) {
	// D6 regression: the memory parameter is used — patterns detected
	// repeatedly in history become LearnedPatterns, and ADR references in
	// claim evidence confirm the ADR directory.
	mem := &developer_memory.DeveloperMemory{
		Events: []archmodel.ArchEvent{
			{ID: "e1", Kind: archmodel.EventPatternDetected, Components: []string{"CLEAN_ARCHITECTURE"}},
			{ID: "e2", Kind: archmodel.EventPatternDetected, Components: []string{"CLEAN_ARCHITECTURE"}},
			{ID: "e3", Kind: archmodel.EventDependencyAdded, Components: []string{"a"}},
		},
		GlobalMemory: []developer_memory.KnowledgeClaim{
			{ID: "c1", Subject: "architecture", Evidence: testBundle("docs/adr/001-init.md")},
		},
	}
	conv := LearnConventions(testGraph(t), mem)
	if len(conv.LearnedPatterns) != 1 || conv.LearnedPatterns[0].Value != "CLEAN_ARCHITECTURE" {
		t.Fatalf("learned patterns = %v, want [CLEAN_ARCHITECTURE]", conv.LearnedPatterns)
	}
	if conv.LearnedPatterns[0].Evidence != 2 {
		t.Errorf("pattern evidence = %d, want 2", conv.LearnedPatterns[0].Evidence)
	}
	if conv.LearnedPatterns[0].Confidence != 1.0 {
		t.Errorf("pattern confidence = %f, want 1.0 (2 of 2 pattern events)", conv.LearnedPatterns[0].Confidence)
	}
}

func TestLearnConventionsPatternFeedback(t *testing.T) {
	conv := LearnConventions(testGraph(t), nil,
		WithPatternFeedback([]string{"CQRS"}, []string{"CLEAN_ARCHITECTURE"}))
	if len(conv.PreferredPatterns) != 1 || conv.PreferredPatterns[0] != "CQRS" {
		t.Errorf("preferred = %v", conv.PreferredPatterns)
	}
	if len(conv.RejectedPatterns) != 1 || conv.RejectedPatterns[0] != "CLEAN_ARCHITECTURE" {
		t.Errorf("rejected = %v", conv.RejectedPatterns)
	}

	// Deterministic: duplicate and unsorted input collapses to the same.
	conv2 := LearnConventions(testGraph(t), nil,
		WithPatternFeedback([]string{"CQRS", "CQRS", "SAGA"}, []string{}))
	if len(conv2.PreferredPatterns) != 2 || conv2.PreferredPatterns[0] != "CQRS" || conv2.PreferredPatterns[1] != "SAGA" {
		t.Errorf("dedupe/sort failed: %v", conv2.PreferredPatterns)
	}
}

func TestLearnConventionsNilGraphAndEmpty(t *testing.T) {
	conv := LearnConventions(nil, nil)
	if conv == nil {
		t.Fatal("nil graph must return empty conventions, not nil")
	}
	if conv.ServiceNamingPattern.Value != "" || len(conv.LayerDirectories) != 0 {
		t.Errorf("nil graph produced conventions: %+v", conv)
	}

	empty := &akg.CodePropertyGraph{
		Nodes:         akg.NewCowMap[string, *link.ResolvedNode](),
		FileNodeIndex: akg.NewCowMap[string, map[string]bool](),
	}
	conv = LearnConventions(empty, nil)
	if conv.ServiceNamingPattern.Value != "" || conv.TestFilePattern.Value != "" {
		t.Errorf("empty graph produced conventions: %+v", conv)
	}
}

func TestConventionsStoreRoundTrip(t *testing.T) {
	repoDir := t.TempDir()
	store := NewConventionsStore(repoDir)

	if got, err := store.Load(); err != nil || got != nil {
		t.Fatalf("missing conventions file must load as (nil, nil): %v, %v", got, err)
	}

	conv := LearnConventions(testGraph(t), nil, WithLearnedAt(time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)))
	if err := store.Save(conv); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.ServiceNamingPattern.Value != "*Service" {
		t.Errorf("round-trip lost service naming: %+v", got.ServiceNamingPattern)
	}
	if got.ADRDirectory.Value != "docs/adr" {
		t.Errorf("round-trip lost ADR directory: %+v", got.ADRDirectory)
	}
	if !got.LearnedAt.Equal(conv.LearnedAt) {
		t.Errorf("learned_at lost in round-trip")
	}
}

func TestConventionsStoreRejectsNil(t *testing.T) {
	store := NewConventionsStore(t.TempDir())
	if err := store.Save(nil); err == nil {
		t.Fatal("saving nil conventions must fail")
	}
}

func testBundle(reference string) evidence.Bundle {
	return evidence.NewBundle(evidence.EvidenceItem{
		Source:     evidence.SourceDocs,
		Reference:  reference,
		Confidence: 0.9,
	})
}
