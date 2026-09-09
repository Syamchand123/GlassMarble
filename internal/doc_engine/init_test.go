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
