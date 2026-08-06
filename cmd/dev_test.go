package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDevRebaseGoldensCmd(t *testing.T) {
	tempDir := t.TempDir()
	goldenDir := filepath.Join(tempDir, "golden")

	cmd := rebaseGoldensCmd
	if err := cmd.Flags().Set("dir", "."); err != nil {
		t.Fatalf("failed to set dir flag: %v", err)
	}
	if err := cmd.Flags().Set("golden-dir", goldenDir); err != nil {
		t.Fatalf("failed to set golden-dir flag: %v", err)
	}

	err := cmd.RunE(cmd, []string{})
	if err != nil {
		t.Fatalf("dev rebase-goldens failed: %v", err)
	}

	if _, err := os.Stat(goldenDir); os.IsNotExist(err) {
		t.Fatalf("expected golden dir to be created at %s", goldenDir)
	}
}
