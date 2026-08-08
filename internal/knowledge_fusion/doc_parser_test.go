package knowledge_fusion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// writeDoc writes a file into dir and returns its DocSource. The mtime is
// pinned so ValidFrom assertions are exact.
func writeDoc(t *testing.T, dir, rel, content string, mtime time.Time) DocSource {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", rel, err)
	}
	return DocSource{Path: path, Rel: filepath.ToSlash(rel), Kind: DocKindADR, ModTime: mtime.UTC()}
}

func TestParseADR_FullTemplate(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC)
	doc := writeDoc(t, dir, "0007-use-redis.md", `---
status: accepted
date: 2024-03-15
---
# Use Redis

## Status

Accepted

## Context

We need a fast cache for session data.

## Decision

We will use Redis for the session cache.

## Consequences

Faster reads.
`, ts)

	claims, err := ParseADR(doc, config.DefaultFusionConfig().Lexicon())
	if err != nil {
		t.Fatalf("ParseADR: %v", err)
	}
	if len(claims) != 2 {
		t.Fatalf("got %d claims, want 2 (decision + redis tech mention)", len(claims))
	}

	decision := claims[0]
	if decision.Subject != "Use Redis" {
		t.Errorf("subject = %q, want %q", decision.Subject, "Use Redis")
	}
	if decision.Predicate != "decided_to" {
		t.Errorf("predicate = %q, want decided_to", decision.Predicate)
	}
	if !strings.Contains(decision.Object, "Redis for the session cache") {
		t.Errorf("object = %q, want decision text", decision.Object)
	}
	if decision.ClaimKind != developer_memory.ClaimExplicitReason {
		t.Errorf("claim_kind = %s, want EXPLICIT_REASON", decision.ClaimKind)
	}
	if decision.State != developer_memory.StateActive {
		t.Errorf("state = %s, want CURRENT", decision.State)
	}
	if !decision.ValidFrom.Equal(ts) {
		t.Errorf("valid_from = %v, want doc mtime %v (never time.Now())", decision.ValidFrom, ts)
	}
	if decision.Evidence.IsEmpty() {
		t.Error("evidence bundle is empty")
	}
	if decision.Evidence.Items[0].Reference != doc.Rel {
		t.Errorf("evidence reference = %q, want %q", decision.Evidence.Items[0].Reference, doc.Rel)
	}

	tech := claims[1]
	if tech.Predicate != "decided_to_use" || tech.Object != "redis" {
		t.Errorf("tech claim = %s %s, want decided_to_use redis", tech.Predicate, tech.Object)
	}
	// Both claims share the source timestamp; IDs must differ.
	if claims[0].ID == claims[1].ID {
		t.Error("claims share an ID")
	}
}

func TestParseADR_StatusTransitions(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().UTC()
	tests := []struct {
		name   string
		status string
		want   developer_memory.KnowledgeState
	}{
		{"accepted", "Accepted", developer_memory.StateActive},
		{"deprecated", "Deprecated", developer_memory.StateDeprecated},
		{"superseded", "Superseded", developer_memory.StateDeprecated},
		{"obsolete", "obsolete", developer_memory.StateDeprecated},
		{"proposed", "Proposed", developer_memory.StateExperimental},
		{"experimental", "experimental", developer_memory.StateExperimental},
		{"draft", "Draft", developer_memory.StateExperimental},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := writeDoc(t, dir, tt.name+".md",
				"# Something\n\n## Status\n\n"+tt.status+"\n\n## Decision\n\nDo it.\n", ts)
			claims, err := ParseADR(doc, nil)
			if err != nil {
				t.Fatalf("ParseADR: %v", err)
			}
			if claims[0].State != tt.want {
				t.Errorf("state = %s, want %s", claims[0].State, tt.want)
			}
		})
	}
}

func TestParseADR_InlineHeadingAndTitleNumbering(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().UTC()
	doc := writeDoc(t, dir, "adr-0012.md", "# ADR-0012 Add JWT\n\n## Decision: Use JWT for API auth\n\nMore prose.\n", ts)
	claims, err := ParseADR(doc, nil)
	if err != nil {
		t.Fatalf("ParseADR: %v", err)
	}
	if claims[0].Subject != "Add JWT" {
		t.Errorf("subject = %q, want %q (ADR numbering stripped)", claims[0].Subject, "Add JWT")
	}
	if !strings.Contains(claims[0].Object, "Use JWT for API auth") {
		t.Errorf("object = %q, want inline decision text", claims[0].Object)
	}
}

func TestParseADR_Malformed(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().UTC()
	tests := []struct {
		name    string
		content string
	}{
		{"no decision", "# Title only\n\n## Context\nnothing decided\n"},
		{"no title", "## Decision\nDecide this.\n"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := writeDoc(t, dir, strings.ReplaceAll(tt.name, " ", "-")+".md", tt.content, ts)
			if _, err := ParseADR(doc, nil); err == nil {
				t.Error("expected error for malformed ADR, got nil")
			}
		})
	}
}

func TestFindDocs_GlobsAndSkips(t *testing.T) {
	dir := t.TempDir()
	writeRaw := func(rel, content string) {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeRaw("README.md", "hi")
	writeRaw("docs/adr/0001-x.md", "hi")
	writeRaw("docs/adr/nested/0002-y.md", "hi")
	writeRaw("docs/decisions/0003-z.md", "hi")
	writeRaw("docs/adr-004.md", "hi")            // matches **/adr-*.md
	writeRaw("docs/README.md", "hi")             // configured readme
	writeRaw("docs/other.md", "hi")              // not matched
	writeRaw("vendor/docs/adr/0005-v.md", "hi")  // vendored dir must be skipped
	writeRaw("node_modules/README.md", "hi")     // skipped
	writeRaw("docs/adr/0006-huge.md", strings.Repeat("x", 2048)) // oversized

	cfg := config.DefaultFusionConfig()
	cfg.DocMaxSizeBytes = 1024
	docs, err := FindDocs(dir, cfg)
	if err != nil {
		t.Fatalf("FindDocs: %v", err)
	}
	var rels []string
	for _, d := range docs {
		rels = append(rels, d.Rel)
	}
	want := []string{
		"README.md",
		"docs/README.md",
		"docs/adr-004.md",
		"docs/adr/0001-x.md",
		"docs/adr/nested/0002-y.md",
		"docs/decisions/0003-z.md",
	}
	if strings.Join(rels, ",") != strings.Join(want, ",") {
		t.Errorf("docs = %v, want %v", rels, want)
	}
	// Deterministic order.
	if !sortStrings(rels) {
		t.Error("docs not sorted by relative path")
	}
}

func TestParseReadme(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().UTC()
	content := strings.Join([]string{
		"# Project",
		"We use Redis for caching and PostgreSQL for storage.",
		"",
		"```yaml",
		"redis: config example not a claim",
		"rabbitmq: queues in config examples are not claims either",
		"```",
		"",
		"| Redis | PostgreSQL |", // table rows are not claims
		"|-------|-----------|",
		"| yes   | yes       |",
		"",
		"rediscount is not a match.",
		"REDIS in caps still matches.",
	}, "\n")
	doc := writeDoc(t, dir, "README.md", content, ts)
	doc.Kind = DocKindReadme

	claims := ParseReadme(doc, config.DefaultFusionConfig().Lexicon())
	objects := make([]string, 0, len(claims))
	for _, c := range claims {
		objects = append(objects, c.Object)
	}

	if countString(objects, "rediscount") != 0 {
		t.Errorf("word-boundary match leaked into 'rediscount': %v", objects)
	}

	// Redis appears in prose twice, but a claim is emitted once per file.
	if countString(objects, "redis") != 1 {
		t.Errorf("redis claims = %d, want 1 (deduped per file): %v", countString(objects, "redis"), objects)
	}
	if countString(objects, "postgresql") != 1 {
		t.Errorf("postgresql claims = %d, want 1: %v", countString(objects, "postgresql"), objects)
	}
	if countString(objects, "rabbitmq") != 0 {
		t.Errorf("code-fence content produced claims: %v", objects)
	}
	for _, c := range claims {
		if c.Subject != "architecture" {
			t.Errorf("subject = %q, want global 'architecture'", c.Subject)
		}
		if c.Predicate != "uses_technology" {
			t.Errorf("predicate = %q, want uses_technology", c.Predicate)
		}
		if c.ClaimKind != developer_memory.ClaimExplicitReason {
			t.Errorf("claim_kind = %s, want EXPLICIT_REASON", c.ClaimKind)
		}
		if !c.ValidFrom.Equal(ts.UTC()) {
			t.Errorf("valid_from = %v, want doc mtime", c.ValidFrom)
		}
		if c.Evidence.Items[0].Reference != "README.md" {
			t.Errorf("evidence reference = %q", c.Evidence.Items[0].Reference)
		}
	}
	// Claims are sorted by object (determinism).
	if !sortStrings(objects) {
		t.Errorf("claims not deterministically ordered: %v", objects)
	}
}

func TestParseReadme_CustomLexicon(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().UTC()
	doc := writeDoc(t, dir, "README.md", "The platform uses Snowflake for warehouse queries.\n", ts)
	doc.Kind = DocKindReadme

	cfg := config.DefaultFusionConfig()
	cfg.TechLexicon = []string{"Snowflake", "BigQuery"}

	claims := ParseReadme(doc, cfg.Lexicon())
	if len(claims) != 1 {
		t.Fatalf("got %d claims, want 1 (custom lexicon entry)", len(claims))
	}
	if claims[0].Object != "snowflake" {
		t.Errorf("object = %q, want snowflake", claims[0].Object)
	}

	// Without the custom lexicon entry, no claim.
	claims = ParseReadme(doc, config.DefaultFusionConfig().Lexicon())
	if len(claims) != 0 {
		t.Errorf("got %d claims with builtin lexicon, want 0", len(claims))
	}
}

func TestParseReadme_MissingFile(t *testing.T) {
	doc := DocSource{Path: filepath.Join(t.TempDir(), "nope.md"), Rel: "nope.md", Kind: DocKindReadme}
	if claims := ParseReadme(doc, nil); len(claims) != 0 {
		t.Errorf("got %d claims for missing file, want 0", len(claims))
	}
}

// --- helpers ---

func countString(v []string, s string) int {
	n := 0
	for _, x := range v {
		if x == s {
			n++
		}
	}
	return n
}

func sortStrings(v []string) bool {
	for i := 1; i < len(v); i++ {
		if v[i-1] > v[i] {
			return false
		}
	}
	return true
}
