package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// WriteAKGState writes a raw GraphJSON v3 state file directly into the
// sandbox (no transaction manager involved) — used to seed commands like
// status/doctor/diff/visualize that only need a graph.
func (s *Sandbox) WriteAKGState(g *akg.GraphJSON) {
	s.T.Helper()
	if err := os.MkdirAll(s.GmDir, 0o755); err != nil {
		s.T.Fatalf("harness: mkdir .glassmarble: %v", err)
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		s.T.Fatalf("harness: marshal akg state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.GmDir, "akg.json"), data, 0o644); err != nil {
		s.T.Fatalf("harness: write akg state: %v", err)
	}
}

// WriteEmptyAKGState seeds a minimal empty v3 store (what `gmb init`
// produces) without running init.
func (s *Sandbox) WriteEmptyAKGState() {
	s.T.Helper()
	if err := os.MkdirAll(s.GmDir, 0o755); err != nil {
		s.T.Fatalf("harness: mkdir .glassmarble: %v", err)
	}
	if err := akg.WriteEmptyJSONState(filepath.Join(s.GmDir, "akg.json")); err != nil {
		s.T.Fatalf("harness: write empty akg state: %v", err)
	}
}

// TinyGraph returns a two-node, one-edge GraphJSON fixture with deterministic
// IDs — the same shape used by the cmd package's own CLI tests.
func TinyGraph() *akg.GraphJSON {
	return &akg.GraphJSON{
		SchemaVersion: akg.CurrentSchemaVersion,
		Version:       7,
		CommitHash:    "abcdef1234567890",
		Nodes: []akg.GraphNodeJSON{
			{ID: "cmd/app/main.go::Main", Kind: "EXECUTABLE", Name: "Main", FileSpec: akg.LocationMetaJSON{Path: "cmd/app/main.go", LineStart: 1, LineEnd: 5}},
			{ID: "internal/db/db.go::Connect", Kind: "EXECUTABLE", Name: "Connect", FileSpec: akg.LocationMetaJSON{Path: "internal/db/db.go", LineStart: 1, LineEnd: 3}},
		},
		Edges: []akg.GraphEdgeJSON{
			{SourceID: "cmd/app/main.go::Main", TargetID: "internal/db/db.go::Connect", Type: "CALLS", LineNumber: 3},
		},
	}
}

// BigGraph builds a synthetic graph of n nodes + (n-1) call edges in a chain
// with deterministic IDs, for scale/performance tests.
func BigGraph(n int) *akg.GraphJSON {
	g := &akg.GraphJSON{
		SchemaVersion: akg.CurrentSchemaVersion,
		Version:       1,
		CommitHash:    fmt.Sprintf("%064x", 1),
		Nodes:         make([]akg.GraphNodeJSON, 0, n),
		Edges:         make([]akg.GraphEdgeJSON, 0, n-1),
	}
	for i := 0; i < n; i++ {
		path := fmt.Sprintf("pkg/mod%d/mod.go", i%50)
		g.Nodes = append(g.Nodes, akg.GraphNodeJSON{
			ID:       fmt.Sprintf("%s::Func%d", path, i),
			Kind:     "EXECUTABLE",
			Name:     fmt.Sprintf("Func%d", i),
			FileSpec: akg.LocationMetaJSON{Path: path, LineStart: 1, LineEnd: 10},
		})
		if i > 0 {
			g.Edges = append(g.Edges, akg.GraphEdgeJSON{
				SourceID: fmt.Sprintf("pkg/mod%d/mod.go::Func%d", (i-1)%50, i-1),
				TargetID: fmt.Sprintf("pkg/mod%d/mod.go::Func%d", i%50, i),
				Type:     "CALLS",
				LineNumber: 5,
			})
		}
	}
	return g
}

// --- Sample project fixtures ---

const sampleGoMod = `module example.com/shop

go 1.21
`

// SampleProject writes a small but architecturally interesting Go project:
// an api entrypoint calling a service that calls a repository that calls a
// cache. Also includes: a Python helper, a JS file, a vendored copy (should
// be skipped), a generated .pb.go (should be skipped), a hidden directory,
// and a deliberately oversized file (skipped with warning).
func (s *Sandbox) SampleProject() {
	s.T.Helper()
	files := map[string]string{
		"go.mod": sampleGoMod,

		"cmd/api/main.go": `package main

import (
	"fmt"
	"net/http"

	"example.com/shop/internal/service"
)

func main() {
	s := service.New()
	http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s", s.Greet(r.URL.Query().Get("name")))
	})
	http.ListenAndServe(":8080", nil)
}
`,

		"internal/service/service.go": `package service

import "example.com/shop/internal/repo"

type Service struct {
	repo *repo.Repo
}

func New() *Service {
	return &Service{repo: repo.New()}
}

func (s *Service) Greet(name string) string {
	return s.repo.Fetch(name)
}
`,

		"internal/repo/repo.go": `package repo

import (
	"database/sql"

	"example.com/shop/internal/cache"
)

type Repo struct {
	db    *sql.DB
	cache *cache.Cache
}

func New() *Repo {
	return &Repo{cache: cache.New()}
}

func (r *Repo) Fetch(key string) string {
	if v := r.cache.Get(key); v != "" {
		return v
	}
	return r.query(key)
}

func (r *Repo) query(key string) string {
	var v string
	_ = r.db.QueryRow("SELECT value FROM kv WHERE k = ?", key).Scan(&v)
	return v
}
`,

		"internal/cache/cache.go": `package cache

import "sync"

type Cache struct {
	mu   sync.Mutex
	keys map[string]string
}

func New() *Cache {
	return &Cache{keys: make(map[string]string)}
}

func (c *Cache) Get(k string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.keys[k]
}

func (c *Cache) Set(k, v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys[k] = v
}
`,

		"scripts/ingest.py": `#!/usr/bin/env python3
import json


def load(path):
    with open(path, encoding="utf-8") as fh:
        return json.load(fh)


if __name__ == "__main__":
    data = load("data.json")
    print(len(data))
`,

		"web/app.js": `export function fetchGreeting(name) {
  return fetch("/hello?name=" + encodeURIComponent(name)).then((r) => r.text());
}

const btn = document.getElementById("greet");
btn.addEventListener("click", () => {
  fetchGreeting("world").then((t) => (btn.textContent = t));
});
`,

		// vendored third-party code must never enter the graph
		"vendor/example.com/lib/lib.go": `package lib

func Helper() string { return "vendored" }
`,

		// generated protobuf code must be skipped
		"api/gen.pb.go": `// Code generated by protoc-gen-go. DO NOT EDIT.
package api

func init() { registerGenerated() }
`,

		// hidden files excluded unless include_hidden is set
		".secrets/keys.go": `package secrets

var APIKey = "should-not-be-indexed"
`,

		// oversized file: skipped with a recorded warning (default 10MiB;
		// tests can lower the limit via config to make this cheap)
		"docs/huge.md": string(make([]byte, 2<<20)) + "\n# generated\n",
	}
	for rel, content := range files {
		s.WriteFile(rel, content)
	}
}

// --- Developer memory fixtures ---

// SeedMemory writes a raw developer-memory aggregate with a few claims about
// the sample project, bypassing the pipeline so retrieval/query tests can
// control exact contents. Uses the JSON shape written by MemoryStore.Rebuild.
func (s *Sandbox) SeedMemory(projectID string) {
	s.T.Helper()
	dir := filepath.Join(s.GmDir, "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.T.Fatalf("harness: mkdir memory: %v", err)
	}
	now := time.Now()
	mem := &developer_memory.DeveloperMemory{
		ProjectID:   projectID,
		LastUpdated: now,
		TotalEvents: 1,
		Events: []archmodel.ArchEvent{
			{
				ID:           "evt_fixture_0001",
				Kind:         archmodel.EventCachingAdded,
				CommitHash:   "abcdef1234567890",
				Timestamp:    now.Add(-48 * time.Hour),
				Title:        "add cache layer",
				Components:   []string{"cache"},
				Evidence:     evidence.Bundle{},
				Intent:       "add cache layer",
				ValidFrom:    now.Add(-48 * time.Hour),
			},
		},
		GlobalMemory: []developer_memory.KnowledgeClaim{
			{
				ID:            "claim_fixture_0001",
				Subject:       "cache",
				Predicate:     "serves",
				Object:        "greeting lookups",
				ClaimKind:     developer_memory.ClaimExplicitReason,
				State:         developer_memory.StateActive,
				FreshnessScore: 0.9,
				ValidFrom:     now.Add(-48 * time.Hour),
				Evidence:      evidence.Bundle{},
			},
			{
				ID:             "claim_fixture_0002",
				Subject:        "repo",
				Predicate:      "queries",
				Object:         "postgres",
				ClaimKind:      developer_memory.ClaimFact,
				State:          developer_memory.StateActive,
				FreshnessScore: 1.0,
				ValidFrom:      now.Add(-24 * time.Hour),
				Evidence:       evidence.Bundle{},
			},
		},
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"cache": {
				Name:      "cache",
				FirstSeen: now.Add(-48 * time.Hour),
				LastSeen:  now.Add(-48 * time.Hour),
				State:     developer_memory.StateActive,
				Events:    []string{"evt_fixture_0001"},
			},
			"repo": {
				Name:      "repo",
				FirstSeen: now.Add(-24 * time.Hour),
				LastSeen:  now.Add(-24 * time.Hour),
				State:     developer_memory.StateActive,
			},
		},
	}
	data, err := json.MarshalIndent(mem, "", "  ")
	if err != nil {
		s.T.Fatalf("harness: marshal memory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memory.json"), data, 0o644); err != nil {
		s.T.Fatalf("harness: write memory.json: %v", err)
	}
	// timeline.json derived artifact (queries may read it directly)
	tl, _ := json.MarshalIndent([]archmodel.TimelineEntry{{
		Timestamp:  now.Add(-48 * time.Hour),
		CommitHash: "abcdef1234567890",
		Title:      "add cache layer",
		EventKind:  "COMMIT",
		Components: []string{"cache"},
	}}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "timeline.json"), tl, 0o644); err != nil {
		s.T.Fatalf("harness: write timeline.json: %v", err)
	}
}

// SeedCorrections writes a corrections.jsonl with one learned correction:
// the developer says the cache layer must NOT be reported as a repository
// pattern (convention learning rejection).
func (s *Sandbox) SeedCorrections() {
	s.T.Helper()
	dir := filepath.Join(s.GmDir, "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.T.Fatalf("harness: mkdir memory: %v", err)
	}
	line := fmt.Sprintf(`{"id":"corr_fixture_0001","kind":"REJECT","target_type":"pattern","target":"cache","timestamp":%q,"note":"not a pattern"}`+"\n",
		time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(dir, "corrections.jsonl"), []byte(line), 0o644); err != nil {
		s.T.Fatalf("harness: write corrections.jsonl: %v", err)
	}
}

// SeedAIConfig writes a project-local .glassmarble/ai.yaml pointing at the
// given base URL (typically the mock LLM) with a deterministic provider.
func (s *Sandbox) SeedAIConfig(baseURL string) {
	s.T.Helper()
	content := fmt.Sprintf("provider: custom\nmodel: gmb-test-1\nbase_url: %s\napi_key: test-key\nstream: true\nmax_turns: 2\n", baseURL)
	s.WriteFile(".glassmarble/ai.yaml", content)
}

// SeedConfigYAML writes a custom .glassmarble/config.yaml (overrides the
// init default).
func (s *Sandbox) SeedConfigYAML(content string) {
	s.T.Helper()
	if err := os.MkdirAll(s.GmDir, 0o755); err != nil {
		s.T.Fatalf("harness: mkdir .glassmarble: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.GmDir, "config.yaml"), []byte(content), 0o644); err != nil {
		s.T.Fatalf("harness: write config.yaml: %v", err)
	}
}
