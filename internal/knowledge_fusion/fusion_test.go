package knowledge_fusion

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// stage4Node builds a minimal resolved node for graph construction.
func stage4Node(name, file string) *stage4.ResolvedNode {
	return &stage4.ResolvedNode{
		ID:       name,
		Kind:     "MODULE",
		Name:     name,
		FileSpec: stage4.LocationMeta{Path: file},
	}
}

// stubPRAdapter is a scripted PR source for engine-level tests.
type stubPRAdapter struct {
	prs []PullRequest
}

func (s stubPRAdapter) Name() string { return "stub" }

func (s stubPRAdapter) FetchRelatedPRs(ctx context.Context, refs []string) ([]PullRequest, error) {
	return s.prs, nil
}

// stubIssueAdapter is a scripted issue source for engine-level tests.
type stubIssueAdapter struct {
	issues []Issue
}

func (s stubIssueAdapter) Name() string { return "stub" }

func (s stubIssueAdapter) FetchRelatedIssues(ctx context.Context, refs []string) ([]Issue, error) {
	return s.issues, nil
}

// testConfig returns the default fusion config with git sources disabled so
// tests run hermetically unless they opt in explicitly.
func testConfig() *config.FusionConfig {
	gitOff := false
	cfg := config.DefaultFusionConfig()
	cfg.IncludeGitSources = &gitOff
	return cfg
}

func TestFusion_Run_DocsOnlyPipeline(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	write(t, repo, "docs/adr/0001-use-redis.md", `# Use Redis

## Status

Accepted

## Decision

Use Redis for sessions.
`)
	write(t, repo, "docs/adr/0002-adopt-kafka.md", `# Adopt Kafka

## Status

Proposed

## Decision

Adopt Kafka for event streaming.
`)
	write(t, repo, "README.md", "# Repo\n\nUses Redis and PostgreSQL.\n")

	store := developer_memory.NewMemoryStore(filepath.Join(dir, "memory"))
	engine := NewFusionEngine(testConfig(), store)

	res, err := engine.Run(context.Background(), repo, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.AdrFiles != 2 || res.ReadmeFiles != 1 {
		t.Errorf("adr/readme files = %d/%d, want 2/1", res.AdrFiles, res.ReadmeFiles)
	}
	// 2 decision claims + 2 tech claims (redis, kafka) + 2 readme mentions.
	if res.TotalClaims != 6 {
		t.Errorf("total claims = %d, want 6", res.TotalClaims)
	}
	if res.NewClaims != 6 {
		t.Errorf("new claims = %d, want 6 on first run", res.NewClaims)
	}
	if res.Sources != 3 {
		t.Errorf("sources = %d, want 3", res.Sources)
	}

	claims, err := store.LoadClaims()
	if err != nil {
		t.Fatalf("LoadClaims: %v", err)
	}
	if len(claims) != 6 {
		t.Fatalf("stored claims = %d, want 6", len(claims))
	}
	// The experimental ADR maps to the EXPERIMENTAL state, and everything is
	// queryable through the store.
	var sawKafka bool
	for _, c := range claims {
		if c.Subject == "Adopt Kafka" && c.State != developer_memory.StateExperimental {
			t.Errorf("kafka claim state = %s, want EXPERIMENTAL (status: Proposed)", c.State)
		}
		if c.Predicate == "decided_to_use" && c.Object == "kafka" {
			sawKafka = true
		}
	}
	if !sawKafka {
		t.Error("no decided_to_use kafka claim persisted")
	}

	// Idempotency: a second run appends nothing.
	res2, err := NewFusionEngine(testConfig(), store).Run(context.Background(), repo, nil)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res2.NewClaims != 0 {
		t.Errorf("new claims on rerun = %d, want 0 (idempotent)", res2.NewClaims)
	}
	if res2.TotalClaims != 6 {
		t.Errorf("total claims on rerun = %d, want 6 (stable)", res2.TotalClaims)
	}
}

func TestFusion_Run_GitSourcesWithAdapters(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	write(t, repo, "docs/adr/0001-use-redis.md", `# Use Redis

## Status

Accepted

## Decision

Use Redis for sessions.
`)
	write(t, repo, "services/user-service/main.go", "package main\n")

	prs := []PullRequest{
		{ID: "42", Title: "Add user session handling", Timestamp: parseTS(t, "2024-05-01T10:00:00Z"), FilesChanged: []string{"services/user-service/main.go"}},
	}
	issues := []Issue{
		{ID: "7", Title: "User cache is slow", Timestamp: parseTS(t, "2024-05-02T10:00:00Z"), FilesChanged: []string{"services/user-service/main.go"}},
	}

	cfg := testConfig()
	gitOn := true
	cfg.IncludeGitSources = &gitOn
	store := developer_memory.NewMemoryStore(filepath.Join(dir, "memory"))
	engine := NewFusionEngine(cfg, store, WithPRAdapter(stubPRAdapter{prs}), WithIssueAdapter(stubIssueAdapter{issues}))

	res, err := engine.Run(context.Background(), repo, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.PRs != 1 || res.Issues != 1 {
		t.Errorf("prs/issues = %d/%d, want 1/1", res.PRs, res.Issues)
	}
	// 1 ADR decision + 1 redis tech + 1 PR claim + 1 issue claim.
	if res.TotalClaims != 4 {
		t.Errorf("total claims = %d, want 4", res.TotalClaims)
	}

	claims, err := store.LoadClaims()
	if err != nil {
		t.Fatalf("LoadClaims: %v", err)
	}
	// The PR claim's ValidFrom is the PR's timestamp — never time.Now().
	prTimestamp := parseTS(t, "2024-05-01T10:00:00Z")
	var prClaimFound bool
	for _, c := range claims {
		if c.Predicate == "was_modified_by_pr" {
			prClaimFound = true
			if !c.ValidFrom.Equal(prTimestamp) {
				t.Errorf("PR claim valid_from = %v, want %v", c.ValidFrom, prTimestamp)
			}
			if len(c.Evidence.Items) == 0 || c.Evidence.Items[0].Reference != "PR 42" {
				t.Errorf("PR claim evidence reference = %v, want PR 42", c.Evidence.Items)
			}
		}
		if c.Predicate == "fixes_issue" {
			if !c.ValidFrom.Equal(parseTS(t, "2024-05-02T10:00:00Z")) {
				t.Errorf("issue claim valid_from = %v", c.ValidFrom)
			}
		}
	}
	if !prClaimFound {
		t.Error("no was_modified_by_pr claim persisted")
	}
}

func TestFusion_Run_GitSourcesDisabledByDefaultInTests(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	write(t, repo, "README.md", "# Repo\n\nUses Redis.\n")

	store := developer_memory.NewMemoryStore(filepath.Join(dir, "memory"))
	res, err := NewFusionEngine(testConfig(), store).Run(context.Background(), repo, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.PRs != 0 || res.Issues != 0 {
		t.Errorf("prs/issues = %d/%d, want 0 (git sources disabled)", res.PRs, res.Issues)
	}
	if res.TotalClaims != 1 {
		t.Errorf("total claims = %d, want 1 (readme redis)", res.TotalClaims)
	}
}

func TestFusion_Run_RequiresStore(t *testing.T) {
	engine := NewFusionEngine(testConfig(), nil)
	_, err := engine.Run(context.Background(), t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error when no memory store is configured")
	}
}

func TestFusion_Run_EntityLinkingExpandsPRClaims(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	write(t, repo, "services/user-service/main.go", "package main\n")

	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("mod::user-service", stage4Node("UserService", "services/user-service/main.go"))
	graph.Nodes = graph.Nodes.Set("mod::user-cache", stage4Node("UserCache", "services/user-service/main.go"))
	graph.FileNodeIndex = graph.FileNodeIndex.Set("services/user-service/main.go", map[string]bool{
		"mod::user-service": true,
		"mod::user-cache":   true,
	})

	prs := []PullRequest{
		{ID: "42", Title: "Add user session handling", Timestamp: parseTS(t, "2024-05-01T10:00:00Z"), FilesChanged: []string{"services/user-service/main.go"}},
	}
	cfg := testConfig()
	gitOn := true
	cfg.IncludeGitSources = &gitOn
	store := developer_memory.NewMemoryStore(filepath.Join(dir, "memory"))
	engine := NewFusionEngine(cfg, store, WithPRAdapter(stubPRAdapter{prs}))

	res, err := engine.Run(context.Background(), repo, graph)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The single PR claim on a known file expands into one claim per AKG
	// node defined in that file (the original is replaced by its
	// expansions — every expansion keeps the file path as subject text).
	if res.TotalClaims != 2 {
		t.Errorf("total claims = %d, want 2 (two node expansions)", res.TotalClaims)
	}

	claims, err := store.LoadClaims()
	if err != nil {
		t.Fatalf("LoadClaims: %v", err)
	}
	// Each expansion keeps its own SubjectID — the two node facts coexist
	// (the conflict resolver must not merge text-identical claims with
	// different resolved entities).
	var withSubjectID int
	for _, c := range claims {
		if c.Predicate == "was_modified_by_pr" && c.SubjectID != "" {
			withSubjectID++
		}
	}
	if withSubjectID != 2 {
		t.Errorf("expanded claims with subject_id = %d, want 2", withSubjectID)
	}
}

func TestFusion_Run_MalformedADRNotFatal(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	write(t, repo, "docs/adr/0001-broken.md", "# Title only\n\nNo decision section.\n")
	write(t, repo, "docs/adr/0002-ok.md", `# Fine ADR

## Status

Accepted

## Decision

Use Redis for sessions.
`)

	store := developer_memory.NewMemoryStore(filepath.Join(dir, "memory"))
	res, err := NewFusionEngine(testConfig(), store).Run(context.Background(), repo, nil)
	if err != nil {
		t.Fatalf("Run: %v (malformed ADR must be skipped, not fatal)", err)
	}
	// The broken ADR is skipped; the good one yields 2 claims.
	if res.AdrFiles != 2 || res.TotalClaims != 2 {
		t.Errorf("adr files/total = %d/%d, want 2/2", res.AdrFiles, res.TotalClaims)
	}
}

// --- helpers ---

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func parseTS(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts.UTC()
}
