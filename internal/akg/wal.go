package akg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

// WALStatus defines the lifecycle status of a Write-Ahead Log entry.
type WALStatus string

const (
	WALStatusStarted   WALStatus = "STARTED"
	WALStatusCommitted WALStatus = "COMMITTED"
	WALStatusAborted   WALStatus = "ABORTED"
)

// WALEntry represents a single transaction log record written to disk before mutating the AKG database.
type WALEntry struct {
	TxID          uint64               `json:"tx_id"`
	CommitHash    string               `json:"commit_hash"`
	Timestamp     time.Time            `json:"timestamp"`
	ModifiedFiles []string             `json:"modified_files"`
	Payload       *stage4.Stage4Output `json:"payload"`
	Status        WALStatus            `json:"status"`
}

// WriteAheadLog manages append-only disk logging for transaction durability and crash recovery.
type WriteAheadLog struct {
	mu          sync.Mutex
	LogFilePath string
}

// NewWriteAheadLog creates or initializes a WAL logger at the specified disk directory.
func NewWriteAheadLog(dirPath string) (*WriteAheadLog, error) {
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	filePath := filepath.Join(dirPath, "akg_transactions.wal")
	return &WriteAheadLog{
		LogFilePath: filePath,
	}, nil
}

// AppendEntry writes a new WAL log record to disk in append mode.
func (w *WriteAheadLog) AppendEntry(entry *WALEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	f, err := os.OpenFile(w.LogFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open WAL log file: %w", err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	if err := encoder.Encode(entry); err != nil {
		return fmt.Errorf("failed to encode WAL entry: %w", err)
	}

	return f.Sync()
}

// MarkCommitted appends a commit marker for the specified TxID.
func (w *WriteAheadLog) MarkCommitted(txID uint64) error {
	entry := &WALEntry{
		TxID:      txID,
		Timestamp: time.Now().UTC(),
		Status:    WALStatusCommitted,
	}
	return w.AppendEntry(entry)
}

// Checkpoint checks the WAL file size and rotates it if it exceeds 100MB.
// It keeps the last 2 WAL segments for crash recovery. Returns true if rotation occurred.
func (w *WriteAheadLog) Checkpoint() (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	info, err := os.Stat(w.LogFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	// 100MB limit
	if info.Size() > 100*1024*1024 {
		old1 := w.LogFilePath + ".1"
		old2 := w.LogFilePath + ".2"

		os.Remove(old2)
		os.Rename(old1, old2)
		os.Rename(w.LogFilePath, old1)
		return true, nil
	}
	return false, nil
}

// Truncate removes all WAL segments. It must be called only after a
// successful atomic TTL write: every committed transaction up to the graph's
// current Version is then captured in the TTL, so replaying the WAL would be
// redundant. Recovery remains correct after a crash between the atomic write
// and the truncation because replay is bounded by maxAppliedTx (the TTL
// metadata gm:version), not by WAL contents (AUDIT Issue 3 Phase 3B-7 /
// Issue 4 Phase 4B-8).
func (w *WriteAheadLog) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	segments := []string{w.LogFilePath + ".2", w.LogFilePath + ".1", w.LogFilePath}
	for _, seg := range segments {
		if err := os.Remove(seg); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to truncate WAL segment %s: %w", seg, err)
		}
	}
	return nil
}

// ForEachEntry streams every WAL entry from the segment chain (.2, .1,
// current) in chronological order, invoking fn for each entry. Streaming
// keeps recovery memory bounded by the in-flight transaction rather than by
// the whole log (AUDIT Issue 4 Phase 4B-5 — ReadAllEntries materialized every
// entry of every segment before replay). If fn returns an error, iteration
// stops and that error is returned.
func (w *WriteAheadLog) ForEachEntry(fn func(*WALEntry) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	filesToRead := []string{w.LogFilePath + ".2", w.LogFilePath + ".1", w.LogFilePath}
	for _, fp := range filesToRead {
		f, err := os.Open(fp)
		if err != nil {
			continue // Skip if missing
		}

		decoder := json.NewDecoder(f)
		for {
			var entry WALEntry
			if err := decoder.Decode(&entry); err != nil {
				break
			}
			e := entry
			if err := fn(&e); err != nil {
				f.Close()
				return err
			}
		}
		f.Close()
	}

	return nil
}

// ReadAllEntries reads all entries from the WAL file for crash recovery replay.
func (w *WriteAheadLog) ReadAllEntries() ([]*WALEntry, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := os.Stat(w.LogFilePath); os.IsNotExist(err) {
		return nil, nil
	}
	var entries []*WALEntry

	filesToRead := []string{w.LogFilePath + ".2", w.LogFilePath + ".1", w.LogFilePath}
	for _, fp := range filesToRead {
		f, err := os.Open(fp)
		if err != nil {
			continue // Skip if missing
		}

		decoder := json.NewDecoder(f)
		for {
			var entry WALEntry
			if err := decoder.Decode(&entry); err != nil {
				break
			}
			e := entry
			entries = append(entries, &e)
		}
		f.Close()
	}

	return entries, nil
}
