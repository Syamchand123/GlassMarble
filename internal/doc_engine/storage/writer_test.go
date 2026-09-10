package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteDoc_AtomicWrite verifies the full tmp→rename→verify sequence.
func TestWriteDoc_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir)

	targetPath := filepath.Join(dir, "docs", "arch.md")
	content := []byte("# Architecture\n\nGenerated content.\n")

	result, err := WriteDoc(sm, targetPath, "docs/arch.md", content, "doc-arch", "overview", "abc123", "deterministic")
	if err != nil {
		t.Fatalf("WriteDoc failed: %v", err)
	}
	if !result.Changed {
		t.Error("expected Changed=true on first write")
	}
	if result.FileHash == "" {
		t.Error("FileHash must be set")
	}

	// Verify file on disk.
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

// TestWriteDoc_NoOpOnIdentical verifies zero-write on byte-identical content.
func TestWriteDoc_NoOpOnIdentical(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir)
	targetPath := filepath.Join(dir, "docs", "same.md")
	content := []byte("# Same\n")

	if _, err := WriteDoc(sm, targetPath, "docs/same.md", content, "d", "s", "h1", "deterministic"); err != nil {
		t.Fatalf("first write failed: %v", err)
	}

	// Second write with identical content.
	result, err := WriteDoc(sm, targetPath, "docs/same.md", content, "d", "s", "h1", "deterministic")
	if err != nil {
		t.Fatalf("second write failed: %v", err)
	}
	if result.Changed {
		t.Error("expected Changed=false on byte-identical re-write")
	}
}

// TestWriteDoc_BackupCreated verifies .gmb.bak is created on first write.
func TestWriteDoc_BackupCreated(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir)
	targetPath := filepath.Join(dir, "docs", "bak.md")

	// Write initial content.
	initial := []byte("# Initial\n")
	if _, err := WriteDoc(sm, targetPath, "docs/bak.md", initial, "d", "s", "h1", "deterministic"); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	// Write updated content — should create .gmb.bak.
	updated := []byte("# Updated\n")
	if _, err := WriteDoc(sm, targetPath, "docs/bak.md", updated, "d", "s", "h2", "deterministic"); err != nil {
		t.Fatalf("update write: %v", err)
	}

	if _, err := os.Stat(targetPath + ".gmb.bak"); os.IsNotExist(err) {
		t.Error(".gmb.bak not created")
	}
}

// TestWriteSectionHash updates only the state without touching any file.
func TestWriteSectionHash(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir)

	err := WriteSectionHash(sm, "docs/arch.md", "overview", "sha256:abc", "llm", "commit1", 42, 300)
	if err != nil {
		t.Fatalf("WriteSectionHash: %v", err)
	}

	state, err := sm.Load()
	if err != nil {
		t.Fatalf("loading state: %v", err)
	}
	ds, ok := state.Documents["docs/arch.md"]
	if !ok {
		t.Fatal("document state not found")
	}
	ss, ok := ds.Sections["overview"]
	if !ok {
		t.Fatal("section state not found")
	}
	if ss.ASTSubgraphHash != "sha256:abc" {
		t.Errorf("unexpected hash: %q", ss.ASTSubgraphHash)
	}
	if ss.LastTokenCost != 42 {
		t.Errorf("unexpected token cost: %d", ss.LastTokenCost)
	}
}
