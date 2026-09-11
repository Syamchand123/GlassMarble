package devex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
)

const ragTestGoSource = `package auth

import (
	"errors"
	"fmt"
)

func Authenticate(user string) error {
	if user == "" {
		return errors.New("empty user")
	}
	return nil
}

func ValidateToken(tok string) bool {
	return tok != ""
}

type Session struct {
	User string
}

func (s *Session) Refresh() error {
	s.User = fmt.Sprint(s.User)
	return nil
}

func (s Session) Name() string {
	return s.User
}
`

func chunkSymbols(chunks []CodeChunk) [][]string {
	out := make([][]string, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, c.Symbols)
	}
	return out
}

func TestStableChunkIDDeterminism(t *testing.T) {
	a := StableChunkID("docs/auth.md", "intro", "hello `World`")
	b := StableChunkID("docs/auth.md", "intro", "hello `World`")
	if a != b {
		t.Fatalf("same input gave different IDs: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "c") || len(a) != 13 {
		t.Fatalf("ID shape: want c+12 hex, got %q", a)
	}
	if c := StableChunkID("docs/auth.md", "intro", "hello `Other`"); c == a {
		t.Errorf("different content must give different IDs")
	}
	if d := StableChunkID("docs/auth.md", "other", "hello `World`"); d == a {
		t.Errorf("different key must give different IDs")
	}
	// CRLF vs LF and trailing spaces must not churn IDs.
	crlf := StableChunkID("docs/auth.md", "intro", "line one  \r\nline two\t\r\n")
	lf := StableChunkID("docs/auth.md", "intro", "line one\nline two\n")
	if crlf != lf {
		t.Errorf("normalization failed: %q vs %q", crlf, lf)
	}
}

func TestStableChunkIDReorderedDocs(t *testing.T) {
	docs := []struct{ target, section, content string }{
		{"docs/a.md", "s1", "alpha body"},
		{"docs/b.md", "s2", "beta body"},
	}
	ids := map[string]string{}
	for _, d := range docs {
		ids[d.target] = StableChunkID(d.target, d.section, d.content)
	}
	// Recompute in reverse order: per-chunk IDs must be identical.
	for i := len(docs) - 1; i >= 0; i-- {
		d := docs[i]
		if got := StableChunkID(d.target, d.section, d.content); got != ids[d.target] {
			t.Errorf("reordered export changed ID for %s: %q vs %q", d.target, got, ids[d.target])
		}
	}
}

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens(""); got != 1 {
		t.Errorf("empty string: got %d, want 1", got)
	}
	if got, want := EstimateTokens("abcd"), 1; got != want {
		t.Errorf("4 bytes: got %d, want %d", got, want)
	}
	if got, want := EstimateTokens(strings.Repeat("x", 400)), 100; got != want {
		t.Errorf("400 bytes: got %d, want %d", got, want)
	}
}

func TestChunkGoSourceGrouping(t *testing.T) {
	chunks := ChunkGoSource("internal/auth/auth.go", []byte(ragTestGoSource), 1000)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (2 funcs + type/methods group), got %d: %v", len(chunks), chunkSymbols(chunks))
	}
	if got := strings.Join(chunks[0].Symbols, ","); got != "Authenticate" {
		t.Errorf("chunk 0 symbols: got %q", got)
	}
	if got := strings.Join(chunks[1].Symbols, ","); got != "ValidateToken" {
		t.Errorf("chunk 1 symbols: got %q", got)
	}
	wantGroup := "Session,Refresh,Name"
	if got := strings.Join(chunks[2].Symbols, ","); got != wantGroup {
		t.Errorf("chunk 2 symbols: got %q, want %q", got, wantGroup)
	}
	for _, c := range chunks {
		if c.Kind != "code" {
			t.Errorf("chunk %v: Kind = %q, want code", c.Symbols, c.Kind)
		}
		if c.ID == "" || !strings.HasPrefix(c.ID, "c") {
			t.Errorf("chunk %v: missing stable ID %q", c.Symbols, c.ID)
		}
		if c.Tokens != EstimateTokens(c.Content) {
			t.Errorf("chunk %v: Tokens %d != EstimateTokens(content) %d", c.Symbols, c.Tokens, EstimateTokens(c.Content))
		}
		if len(c.FileRefs) != len(c.Symbols) {
			t.Errorf("chunk %v: %d FileRefs for %d symbols", c.Symbols, len(c.FileRefs), len(c.Symbols))
		}
		for _, r := range c.FileRefs {
			if !strings.Contains(r, "@internal/auth/auth.go#") {
				t.Errorf("chunk %v: bad FileRef %q", c.Symbols, r)
			}
		}
	}
	// Never-mid-node: every chunk body (headers stripped) ends at a
	// declaration boundary.
	for _, c := range chunks {
		lines := strings.Split(c.Content, "\n")
		if len(lines) < 4 {
			t.Errorf("chunk %v: content too short:\n%s", c.Symbols, c.Content)
			continue
		}
		if last := strings.TrimSpace(lines[len(lines)-1]); last != "}" {
			t.Errorf("chunk %v: body does not end at node boundary, ends %q", c.Symbols, last)
		}
	}
}

func TestChunkGoSourceEnrichment(t *testing.T) {
	chunks := ChunkGoSource("internal/auth/auth.go", []byte(ragTestGoSource), 1000)
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}
	for _, c := range chunks {
		if !strings.HasPrefix(c.Content, "// file: internal/auth/auth.go\n") {
			t.Errorf("chunk %v: missing file header:\n%s", c.Symbols, c.Content)
		}
		if !strings.Contains(c.Content, "// imports: errors,fmt\n") {
			t.Errorf("chunk %v: missing imports header:\n%s", c.Symbols, c.Content)
		}
	}
	// Method group parent is the receiver type; func chunk parent is package.
	if !strings.Contains(chunks[0].Content, "// parent: auth\n") {
		t.Errorf("func chunk parent: want package auth:\n%s", chunks[0].Content)
	}
	if !strings.Contains(chunks[2].Content, "// parent: Session\n") {
		t.Errorf("method group parent: want Session:\n%s", chunks[2].Content)
	}
}

func TestChunkGoSourceOversizedSplit(t *testing.T) {
	// Tiny budget forces the type and each method onto separate chunks —
	// split on method boundaries, each still a whole node.
	chunks := ChunkGoSource("internal/auth/auth.go", []byte(ragTestGoSource), 5)
	if len(chunks) != 5 {
		t.Fatalf("expected 5 single-decl chunks under tiny budget, got %d: %v", len(chunks), chunkSymbols(chunks))
	}
	for _, c := range chunks {
		if len(c.Symbols) != 1 {
			t.Errorf("oversized split must not merge decls: %v", c.Symbols)
		}
		lines := strings.Split(c.Content, "\n")
		if last := strings.TrimSpace(lines[len(lines)-1]); last != "}" {
			t.Errorf("chunk %v: not node-atomic, ends %q", c.Symbols, last)
		}
	}
	// Method chunks still carry the receiver as parent even when split.
	found := false
	for _, c := range chunks {
		if len(c.Symbols) == 1 && c.Symbols[0] == "Refresh" {
			found = true
			if !strings.Contains(c.Content, "// parent: Session\n") {
				t.Errorf("split method lost receiver parent:\n%s", c.Content)
			}
		}
	}
	if !found {
		t.Error("Refresh chunk missing")
	}
}

func TestChunkGoSourcePartialOnSyntaxError(t *testing.T) {
	src := "package broken\n\nfunc Good() int { return 1 }\n\nfunc Bad( {\n"
	chunks := ChunkGoSource("broken.go", []byte(src), 1000)
	found := false
	for _, c := range chunks {
		for _, s := range c.Symbols {
			if s == "Good" {
				found = true
			}
			if s == "Bad" {
				t.Errorf("error-containing node must be skipped, got chunk %v", c.Symbols)
			}
		}
	}
	if !found {
		t.Errorf("valid declaration beside a syntax error must still chunk, got %v", chunkSymbols(chunks))
	}
}

func TestChunkGoSourceFallbackDirect(t *testing.T) {
	// Pure-stdlib path, callable without tree-sitter: same grouping contract.
	chunks := chunkGoSourceFallback("internal/auth/auth.go", []byte(ragTestGoSource), 1000)
	if len(chunks) != 3 {
		t.Fatalf("fallback: expected 3 chunks, got %d: %v", len(chunks), chunkSymbols(chunks))
	}
	if got := strings.Join(chunks[2].Symbols, ","); got != "Session,Refresh,Name" {
		t.Errorf("fallback method grouping: got %q", got)
	}
	if !strings.Contains(chunks[0].Content, "// file: internal/auth/auth.go\n") {
		t.Errorf("fallback missing file header:\n%s", chunks[0].Content)
	}
	if !strings.Contains(chunks[0].Content, "// imports: errors,fmt\n") {
		t.Errorf("fallback missing imports header:\n%s", chunks[0].Content)
	}
	if got := chunkGoSourceFallback("bad.go", []byte("package bad\nfunc ( {"), 1000); len(got) != 0 {
		t.Errorf("fallback on unparseable input must return nil, got %v", chunkSymbols(got))
	}
	// Parity: fallback and tree-sitter agree on symbols for valid input.
	primary := ChunkGoSource("internal/auth/auth.go", []byte(ragTestGoSource), 1000)
	if strings.Join(flattenSyms(primary), ",") != strings.Join(flattenSyms(chunks), ",") {
		t.Errorf("fallback/primary symbol parity: %v vs %v", chunkSymbols(primary), chunkSymbols(chunks))
	}
}

func flattenSyms(chunks []CodeChunk) []string {
	var out []string
	for _, c := range chunks {
		out = append(out, c.Symbols...)
	}
	return out
}

func TestExportKnowledgeBaseCodeChunks(t *testing.T) {
	tempDir := t.TempDir()

	cfgPath := config.DocsConfigPath(tempDir)
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0755)
	data := "version: 1\ndocs_dir: docs\ndocuments:\n  - id: auth\n    target: docs/auth.md\n    title: Auth Guide\n    scope:\n      paths: [\"internal/auth/**\"]\n    sections:\n      - id: intro\n        title: Intro\n        managed: true\n"
	_, _ = storage.AtomicWriteFile(cfgPath, []byte(data))

	targetFull := filepath.Join(tempDir, "docs", "auth.md")
	_ = os.MkdirAll(filepath.Dir(targetFull), 0755)
	md := "# Auth Guide\n\n<!-- gmb:begin:intro -->\nCall `Authenticate` to log in.\n<!-- gmb:end:intro -->\n"
	_, _ = storage.AtomicWriteFile(targetFull, []byte(md))

	authDir := filepath.Join(tempDir, "internal", "auth")
	_ = os.MkdirAll(authDir, 0755)
	_ = os.WriteFile(filepath.Join(authDir, "auth.go"), []byte(ragTestGoSource), 0644)

	summary, err := ExportKnowledgeBase(tempDir, "rag", ".glassmarble/rag")
	if err != nil {
		t.Fatalf("ExportKnowledgeBase failed: %v", err)
	}
	// 1 section + 4 code chunks (Authenticate, ValidateToken, Session-group… wait: grouped).
	if summary.TotalChunks != 4 {
		t.Fatalf("expected 4 chunks (1 section + 3 code), got %d", summary.TotalChunks)
	}

	// Section chunk keeps its legacy filename and gains stable fields.
	raw, err := os.ReadFile(filepath.Join(tempDir, ".glassmarble", "rag", "auth_intro.json"))
	if err != nil {
		t.Fatalf("reading section chunk: %v", err)
	}
	var section RAGChunk
	if err := json.Unmarshal(raw, &section); err != nil {
		t.Fatalf("parsing section chunk: %v", err)
	}
	if section.ChunkID == "" || !strings.HasPrefix(section.ChunkID, "c") {
		t.Errorf("section missing stable chunk_id: %+v", section)
	}
	if section.Kind != "section" {
		t.Errorf("section kind = %q, want section", section.Kind)
	}
	if section.Tokens == 0 {
		t.Error("section tokens not populated")
	}
	if section.ParentID == "" {
		t.Error("section missing parent document link")
	}
	wantParent := StableChunkID("docs/auth.md", "document", "")
	if section.ParentID != wantParent {
		t.Errorf("section parent = %q, want virtual document %q", section.ParentID, wantParent)
	}

	// Code chunk files are ID-stable: re-export must produce the same
	// filenames and chunk_ids (no vector-store churn). File bytes differ
	// only in the GeneratedAt timestamp, which is intentionally per-run.
	collectCodeIDs := func() map[string]string {
		ids := map[string]string{}
		entries, _ := os.ReadDir(filepath.Join(tempDir, ".glassmarble", "rag"))
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), "code_") {
				continue
			}
			b, _ := os.ReadFile(filepath.Join(tempDir, ".glassmarble", "rag", e.Name()))
			var cc RAGChunk
			if err := json.Unmarshal(b, &cc); err != nil {
				t.Fatalf("parsing code chunk %s: %v", e.Name(), err)
			}
			ids[e.Name()] = cc.ChunkID
		}
		return ids
	}
	before := collectCodeIDs()
	if len(before) != 3 {
		t.Fatalf("expected 3 code chunk files, got %d", len(before))
	}
	if _, err := ExportKnowledgeBase(tempDir, "rag", ".glassmarble/rag"); err != nil {
		t.Fatalf("re-export failed: %v", err)
	}
	after := collectCodeIDs()
	if len(after) != len(before) {
		t.Fatalf("re-export changed code chunk set: %v vs %v", before, after)
	}
	for name, id := range before {
		if after[name] != id {
			t.Errorf("re-export churned code chunk %s: %q vs %q", name, id, after[name])
		}
	}

	// One code chunk links back to the section chunk.
	foundLink := false
	for name := range before {
		b, _ := os.ReadFile(filepath.Join(tempDir, ".glassmarble", "rag", name))
		var cc RAGChunk
		if err := json.Unmarshal(b, &cc); err != nil {
			t.Fatalf("parsing code chunk %s: %v", name, err)
		}
		if cc.Kind != "code" || cc.ChunkID == "" {
			t.Errorf("code chunk %s missing kind/chunk_id: %+v", name, cc)
		}
		if cc.ParentID == section.ChunkID {
			foundLink = true
		}
		if !strings.Contains(cc.Content, "// file: internal/auth/auth.go\n") {
			t.Errorf("code chunk %s missing enrichment header", name)
		}
	}
	if !foundLink {
		t.Error("no code chunk links to the referencing section chunk")
	}
}

func TestExportKnowledgeBaseShapesBackwardCompatible(t *testing.T) {
	// Old-shape JSON (pre-D3, no new fields) must unmarshal cleanly.
	legacy := `{"id":"auth_intro","doc_id":"auth","target_path":"docs/auth.md","section_id":"intro","title":"Auth Guide - intro","content":"hi","symbols":["X"],"files":["internal/auth"],"diagrams":[],"freshness":0,"metadata":{"archetype":"guide"},"generated_at":"2026-01-01T00:00:00Z"}`
	var c RAGChunk
	if err := json.Unmarshal([]byte(legacy), &c); err != nil {
		t.Fatalf("legacy chunk JSON must still parse: %v", err)
	}
	if c.ChunkID != "" || c.ParentID != "" || c.Kind != "" || c.Tokens != 0 {
		t.Errorf("legacy chunk must yield zero new fields, got %+v", c)
	}

	// Manifest shape unchanged: exactly version/chunks_count/exported_at.
	tempDir := t.TempDir()
	cfgPath := config.DocsConfigPath(tempDir)
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0755)
	data := "version: 1\ndocs_dir: docs\ndocuments:\n  - id: a\n    target: docs/a.md\n    title: A\n    scope:\n      paths: [\"x/**\"]\n    sections:\n      - id: s\n        title: S\n        managed: true\n"
	_, _ = storage.AtomicWriteFile(cfgPath, []byte(data))
	_ = os.MkdirAll(filepath.Join(tempDir, "docs"), 0755)
	_, _ = storage.AtomicWriteFile(filepath.Join(tempDir, "docs", "a.md"), []byte("# A\n\n<!-- gmb:begin:s -->\nbody\n<!-- gmb:end:s -->\n"))
	if _, err := ExportKnowledgeBase(tempDir, "rag", ".glassmarble/rag"); err != nil {
		t.Fatalf("export failed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(tempDir, ".glassmarble", "rag", "manifest.json"))
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parsing manifest: %v", err)
	}
	for _, k := range []string{"version", "chunks_count", "exported_at"} {
		if _, ok := manifest[k]; !ok {
			t.Errorf("manifest missing key %q: %v", k, manifest)
		}
	}
	if len(manifest) != 3 {
		t.Errorf("manifest must stay additive-only, got keys %v", manifest)
	}
}

func TestExportKnowledgeBaseCodeChunkCaps(t *testing.T) {
	scaffold := func(t *testing.T, docMention string) string {
		t.Helper()
		tempDir := t.TempDir()
		cfgPath := config.DocsConfigPath(tempDir)
		_ = os.MkdirAll(filepath.Dir(cfgPath), 0755)
		data := "version: 1\ndocs_dir: docs\ndocuments:\n  - id: big\n    target: docs/big.md\n    title: Big\n    scope:\n      paths: [\"pkg/**\"]\n    sections:\n      - id: s\n        title: S\n        managed: true\n"
		_, _ = storage.AtomicWriteFile(cfgPath, []byte(data))
		_ = os.MkdirAll(filepath.Join(tempDir, "docs"), 0755)
		_, _ = storage.AtomicWriteFile(filepath.Join(tempDir, "docs", "big.md"), []byte("# Big\n\n<!-- gmb:begin:s -->\nSee "+docMention+".\n<!-- gmb:end:s -->\n"))
		return tempDir
	}

	// Cap: 205 declarations in one referenced file → 200 code chunks max.
	capDir := scaffold(t, "`Func000`")
	pkgDir := filepath.Join(capDir, "pkg", "big")
	_ = os.MkdirAll(pkgDir, 0755)
	var sb strings.Builder
	sb.WriteString("package big\n\n")
	for i := 0; i < 205; i++ {
		fmt.Fprintf(&sb, "func Func%03d() {}\n\n", i)
	}
	_ = os.WriteFile(filepath.Join(pkgDir, "big.go"), []byte(sb.String()), 0644)
	summary, err := ExportKnowledgeBase(capDir, "rag", ".glassmarble/rag")
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if summary.TotalChunks != 201 {
		t.Errorf("expected 1 + 200 capped code chunks, got %d", summary.TotalChunks)
	}

	// Size skip: a >256KB referenced file contributes zero chunks.
	skipDir := scaffold(t, "`SmallFunc` and `HugeFunc`")
	skipPkg := filepath.Join(skipDir, "pkg", "big")
	_ = os.MkdirAll(skipPkg, 0755)
	_ = os.WriteFile(filepath.Join(skipPkg, "small.go"), []byte("package big\n\nfunc SmallFunc() {}\n"), 0644)
	huge := append([]byte("package big\n\n"), []byte(strings.Repeat("// pad line to inflate the file past the size cap\n", 6000))...)
	huge = append(huge, []byte("func HugeFunc() {}\n")...)
	if len(huge) <= 256*1024 {
		t.Fatalf("fixture too small to trip the cap: %d bytes", len(huge))
	}
	_ = os.WriteFile(filepath.Join(skipPkg, "huge.go"), huge, 0644)
	summary, err = ExportKnowledgeBase(skipDir, "rag", ".glassmarble/rag")
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	// 1 section + 1 code chunk (small.go); huge.go skipped by size.
	if summary.TotalChunks != 2 {
		t.Errorf("expected oversized file skipped (2 chunks), got %d", summary.TotalChunks)
	}
}
