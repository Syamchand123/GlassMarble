package doc_engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

func TestDeriveDocID(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"docs/architecture.md", "architecture"},
		{"docs/api/v1/auth.md", "auth"},
		{"README.md", "readme"},
		{"docs/nested/user-guide.md", "user-guide"},
	}

	for _, tt := range tests {
		got := deriveDocID(tt.path)
		if got != tt.want {
			t.Errorf("deriveDocID(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// TestDeriveDocID_SanitizesInvalidCharacters guards against a real,
// serious bug found via live end-to-end testing: an ordinary filename like
// "adr_doc.md" used to derive the id "adr_doc" — accepted by
// updateDocsYAML's raw write (which never validates before writing) but
// rejected by LoadDocsConfig's own validation on the VERY NEXT `doc init`
// call. That next call's load-existing-then-append step failed, and
// (before the updateDocsYAML fix below) silently discarded every other
// already-configured document and started over with an empty
// docs.yaml — a real data-loss incident reproduced in a live scratch
// repo, not a hypothetical.
func TestDeriveDocID_SanitizesInvalidCharacters(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"docs/adr_doc.md", "adr-doc"},
		{"docs/My Report.md", "my-report"},
		{"docs/v1.2_release.md", "v1-2-release"},
		{"docs/___.md", "doc"},
	}
	for _, tt := range tests {
		got := deriveDocID(tt.path)
		if got != tt.want {
			t.Errorf("deriveDocID(%q) = %q, want %q", tt.path, got, tt.want)
		}
		if !config.IsValidDocOrSectionID(got) {
			t.Errorf("deriveDocID(%q) = %q is not a valid doc id", tt.path, got)
		}
	}
}

// TestInit_RejectsInvalidExplicitID guards the other half of the same
// bug: an explicit --id that isn't URL-safe must be rejected immediately
// with a clear error, not silently written (only to break the NEXT init
// call, exactly like the auto-derived case above).
func TestInit_RejectsInvalidExplicitID(t *testing.T) {
	tempDir := t.TempDir()
	opts := InitOptions{
		TargetPath:  "docs/thing.md",
		DocID:       "bad_id",
		Archetype:   "module",
		ScopePaths:  []string{"internal/**"},
		Interactive: false,
		Out:         os.Stdout,
	}
	if err := Init(tempDir, opts); err == nil {
		t.Fatal("expected Init to reject an invalid --id, got nil error")
	}
}

// TestInit_SecondCallNeverDiscardsFirstDocument guards against the actual
// data-loss incident directly: scaffolding a document whose derived id
// would have been invalid before the sanitizeDocID fix, immediately
// followed by a second, unrelated document, must leave BOTH documents in
// docs.yaml — not silently drop the first.
func TestInit_SecondCallNeverDiscardsFirstDocument(t *testing.T) {
	tempDir := t.TempDir()

	first := InitOptions{
		TargetPath:  "docs/adr_doc.md",
		Archetype:   "adr",
		ScopePaths:  []string{"internal/**"},
		Interactive: false,
		Out:         os.Stdout,
	}
	if err := Init(tempDir, first); err != nil {
		t.Fatalf("first Init failed: %v", err)
	}

	second := InitOptions{
		TargetPath:  "docs/security.md",
		Archetype:   "security",
		ScopePaths:  []string{"internal/**"},
		Interactive: false,
		Out:         os.Stdout,
	}
	if err := Init(tempDir, second); err != nil {
		t.Fatalf("second Init failed: %v", err)
	}

	cfg, err := config.LoadDocsConfig(tempDir)
	if err != nil {
		t.Fatalf("failed to load docs.yaml after both Init calls: %v", err)
	}
	if len(cfg.Documents) != 2 {
		ids := make([]string, len(cfg.Documents))
		for i, d := range cfg.Documents {
			ids[i] = d.ID
		}
		t.Fatalf("expected both documents to survive, got %d: %v", len(cfg.Documents), ids)
	}
}

func TestInit_NonInteractive_NewFile(t *testing.T) {
	tempDir := t.TempDir()

	opts := InitOptions{
		TargetPath:  "docs/auth.md",
		Archetype:   "architecture",
		ScopePaths:  []string{"internal/auth/**"},
		Title:       "Authentication Architecture",
		Purpose:     "Details authentication flows and token verification",
		Audience:    "Security engineers",
		Interactive: false,
		Out:         os.Stdout,
	}

	if err := Init(tempDir, opts); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// 1. Verify markdown was created
	targetFull := filepath.Join(tempDir, "docs", "auth.md")
	data, err := os.ReadFile(targetFull)
	if err != nil {
		t.Fatalf("target markdown file not created: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "title: Authentication Architecture") {
		t.Errorf("expected frontmatter title in generated markdown, got:\n%s", content)
	}
	if !strings.Contains(content, "<!-- gmb:begin:overview -->") {
		t.Errorf("expected overview anchor in generated markdown, got:\n%s", content)
	}
	if !strings.Contains(content, "<!-- gmb:end:overview -->") {
		t.Errorf("expected overview end anchor in generated markdown, got:\n%s", content)
	}

	// 2. Verify docs.yaml was created and contains the doc spec
	cfgPath := config.DocsConfigPath(tempDir)
	cfg, err := config.LoadDocsConfig(tempDir)
	if err != nil {
		t.Fatalf("failed to load generated docs.yaml at %s: %v", cfgPath, err)
	}

	if len(cfg.Documents) != 1 {
		t.Fatalf("expected 1 document in docs.yaml, got %d", len(cfg.Documents))
	}
	doc := cfg.Documents[0]
	if doc.ID != "auth" {
		t.Errorf("expected doc ID 'auth', got %q", doc.ID)
	}
	if doc.Archetype != "architecture" {
		t.Errorf("expected archetype 'architecture', got %q", doc.Archetype)
	}
	if len(doc.Sections) == 0 {
		t.Errorf("expected sections scaffolded from archetype, got 0")
	}
}

// TestInit_NonInteractive_EntryPointsWired guards against a regression
// where `gmb doc init` had NO way, interactive or not, to ever set
// Scope.EntryPoints — every document it scaffolded permanently rendered
// its archetype's callgraph/sequence diagrams as an empty "No call graph
// edges detected" placeholder, since nothing else populates entry_points
// short of hand-editing docs.yaml afterward.
func TestInit_NonInteractive_EntryPointsWired(t *testing.T) {
	tempDir := t.TempDir()

	opts := InitOptions{
		TargetPath:  "docs/auth.md",
		Archetype:   "module",
		ScopePaths:  []string{"internal/auth/**"},
		EntryPoints: []string{"internal/auth/service.go::Authenticate"},
		Title:       "Auth",
		Interactive: false,
		Out:         os.Stdout,
	}

	if err := Init(tempDir, opts); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	cfg, err := config.LoadDocsConfig(tempDir)
	if err != nil {
		t.Fatalf("failed to load generated docs.yaml: %v", err)
	}
	if len(cfg.Documents) != 1 {
		t.Fatalf("expected 1 document in docs.yaml, got %d", len(cfg.Documents))
	}
	got := cfg.Documents[0].Scope.EntryPoints
	if len(got) != 1 || got[0] != "internal/auth/service.go::Authenticate" {
		t.Errorf("EntryPoints = %v, want [\"internal/auth/service.go::Authenticate\"]", got)
	}
}

func TestInit_ExistingFile_PreservesHumanContent(t *testing.T) {
	tempDir := t.TempDir()

	// Pre-create existing markdown file with human prose
	docsDir := filepath.Join(tempDir, "docs")
	_ = os.MkdirAll(docsDir, 0755)
	existingContent := "# Custom Header\n\nThis is human authored prose that must survive.\n"
	targetFull := filepath.Join(docsDir, "guide.md")
	if err := os.WriteFile(targetFull, []byte(existingContent), 0644); err != nil {
		t.Fatalf("failed to write existing file: %v", err)
	}

	opts := InitOptions{
		TargetPath:  "docs/guide.md",
		Archetype:   "runbook",
		ScopePaths:  []string{"cmd/**"},
		Title:       "Operations Runbook",
		Interactive: false,
		Out:         os.Stdout,
	}

	if err := Init(tempDir, opts); err != nil {
		t.Fatalf("Init failed on existing file: %v", err)
	}

	data, err := os.ReadFile(targetFull)
	if err != nil {
		t.Fatalf("failed to read target file: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "This is human authored prose that must survive.") {
		t.Errorf("existing human content was overwritten or destroyed:\n%s", content)
	}
	if !strings.Contains(content, "<!-- gmb:begin:") {
		t.Errorf("expected managed zones appended to existing file, got:\n%s", content)
	}
}
