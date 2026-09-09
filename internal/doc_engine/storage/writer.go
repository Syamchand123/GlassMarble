// Package storage — writer.go
// Implements Stage 8 atomic MVCC writer for .md files.
// This is a thin orchestrator over storage.AtomicWriteFile that adds
// the docs_state.json update step and wires together the patcher.
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// WriterResult is returned by WriteDoc.
type WriterResult struct {
	// Changed is true when bytes on disk actually changed (non-identical content written).
	Changed bool
	// FileHash is the SHA256 hex of the written file (or current file if unchanged).
	FileHash string
	// BackupCreated is true when a .gmb.bak was created.
	BackupCreated bool
}

// WriteDoc atomically writes content to targetPath following the MVCC contract:
//
//	write .tmp → fsync → rename → verify SHA256
//
// If content is byte-identical to the existing file, no write is performed.
// On the first write to a file, a .gmb.bak backup is created.
// The state manager (sm) is updated atomically after a successful write.
//
// This function is non-fatal-safe: callers may treat errors as warnings per P5.
func WriteDoc(sm *StateManager, targetPath string, content []byte,
	docID, sectionID, commitHash, renderMode string) (WriterResult, error) {

	// Use the existing AtomicWriteFile from state.go.
	changed, err := AtomicWriteFile(targetPath, content)
	if err != nil {
		return WriterResult{}, fmt.Errorf("doc_engine/writer: %w", err)
	}

	hash := sha256hexBytes(content)
	backupCreated := !changed && backupExists(targetPath)

	if !changed {
		return WriterResult{Changed: false, FileHash: hash, BackupCreated: backupCreated}, nil
	}

	// Update docs_state.json.
	state, err := sm.Load()
	if err != nil {
		return WriterResult{Changed: true, FileHash: hash}, fmt.Errorf("doc_engine/writer: loading state: %w", err)
	}

	ds := GetOrCreateDocState(state, targetPath)
	ds.FileHash = "sha256:" + hash
	ds.LastUpdatedCommit = commitHash
	ds.LastUpdatedAt = time.Now().UTC()

	if sectionID != "" {
		ss := GetOrCreateSectionState(ds, sectionID)
		ss.RenderMode = renderMode
		ss.LastUpdatedAt = time.Now().UTC()
	}

	if err := sm.Save(state); err != nil {
		// State update failure is not fatal — the file is already written.
		return WriterResult{Changed: true, FileHash: hash}, fmt.Errorf("doc_engine/writer: saving state: %w", err)
	}

	return WriterResult{Changed: true, FileHash: hash, BackupCreated: true}, nil
}

// WriteSectionHash updates only the AST subgraph hash for a specific section
// in docs_state.json, without touching any file on disk.
// Used by the invalidator after computing section hashes.
func WriteSectionHash(sm *StateManager, targetPath, sectionID, astHash, renderMode, commitHash string, tokenCost int, renderMs int64) error {
	state, err := sm.Load()
	if err != nil {
		return fmt.Errorf("doc_engine/writer: loading state: %w", err)
	}

	ds := GetOrCreateDocState(state, targetPath)
	ds.LastUpdatedCommit = commitHash
	ds.LastUpdatedAt = time.Now().UTC()

	ss := GetOrCreateSectionState(ds, sectionID)
	ss.ASTSubgraphHash = astHash
	ss.RenderMode = renderMode
	ss.LastTokenCost = tokenCost
	ss.LastRenderMs = renderMs
	ss.LastUpdatedAt = time.Now().UTC()

	return sm.Save(state)
}

// CopyToStdout writes the content of targetPath to w (e.g., os.Stdout).
// Used by `gmb doc view`.
func CopyToStdout(w io.Writer, targetPath string) error {
	f, err := os.Open(filepath.Clean(targetPath))
	if err != nil {
		return fmt.Errorf("doc_engine/writer: opening %s: %w", targetPath, err)
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ────────────────────────────────────────────────────────────────────────────

func sha256hexBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func backupExists(targetPath string) bool {
	_, err := os.Stat(targetPath + ".gmb.bak")
	return err == nil
}
