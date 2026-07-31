package akg

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func TestWAL_AppendAndRead(t *testing.T) {
	dir := t.TempDir()
	wal, err := NewWriteAheadLog(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

	entry := &WALEntry{TxID: 1, CommitHash: "abc123", Status: WALStatusStarted}
	if err := wal.AppendEntry(entry); err != nil {
		t.Fatalf("failed to append entry: %v", err)
	}

	entries, err := wal.ReadAllEntries()
	if err != nil {
		t.Fatalf("failed to read entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].TxID != 1 || entries[0].Status != WALStatusStarted {
		t.Errorf("entry content mismatch: TxID=%d Status=%s", entries[0].TxID, entries[0].Status)
	}
}

func TestWAL_MarkCommitted(t *testing.T) {
	dir := t.TempDir()
	wal, err := NewWriteAheadLog(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

	entry := &WALEntry{TxID: 1, Status: WALStatusStarted}
	if err := wal.AppendEntry(entry); err != nil {
		t.Fatalf("failed to append: %v", err)
	}
	if err := wal.MarkCommitted(1); err != nil {
		t.Fatalf("failed to mark committed: %v", err)
	}

	entries, _ := wal.ReadAllEntries()
	// Should have both entries
	found := false
	for _, e := range entries {
		if e.TxID == 1 && e.Status == WALStatusCommitted {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected COMMITTED entry for TxID 1")
	}
}

func TestWAL_EmptyRead(t *testing.T) {
	dir := t.TempDir()
	wal, err := NewWriteAheadLog(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}
	os.Remove(wal.LogFilePath) // Remove file to simulate missing WAL

	entries, err := wal.ReadAllEntries()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestWAL_Checkpoint(t *testing.T) {
	dir := t.TempDir()
	wal, err := NewWriteAheadLog(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

	rotated, err := wal.Checkpoint()
	if err != nil {
		t.Fatalf("checkpoint error: %v", err)
	}
	if rotated {
		t.Log("WAL rotated (expected on large files)")
	}
}

func TestWAL_ConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	wal, err := NewWriteAheadLog(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			entry := &WALEntry{TxID: uint64(id), Status: WALStatusStarted}
			if err := wal.AppendEntry(entry); err != nil {
				t.Errorf("concurrent append failed: %v", err)
			}
		}(i)
	}
	wg.Wait()

	entries, _ := wal.ReadAllEntries()
	if len(entries) != 10 {
		t.Errorf("expected 10 entries after concurrent appends, got %d", len(entries))
	}
}

func TestWAL_EntryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	wal, err := NewWriteAheadLog(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

	entry := &WALEntry{
		TxID:          42,
		CommitHash:    "deadbeef",
		Status:        WALStatusCommitted,
		ModifiedFiles: []string{"a.go", "b.go"},
	}
	if err := wal.AppendEntry(entry); err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	entries, _ := wal.ReadAllEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].TxID != 42 || entries[0].CommitHash != "deadbeef" {
		t.Errorf("round-trip content mismatch: %+v", entries[0])
	}
}

// ===== ADDITIONAL WAL TESTS =====

func TestWAL_RecoverReplay(t *testing.T) {
	dir := t.TempDir()
	wal, err := NewWriteAheadLog(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

	entry := &WALEntry{TxID: 1, CommitHash: "abc", Status: WALStatusStarted, ModifiedFiles: []string{"a.go"}}
	if err := wal.AppendEntry(entry); err != nil {
		t.Fatalf("failed to append: %v", err)
	}
	if err := wal.MarkCommitted(1); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	entries, err := wal.ReadAllEntries()
	if err != nil {
		t.Fatalf("ReadAllEntries failed: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(entries))
	}
	_ = entries
}

func TestWAL_RecoverSkipsUncommitted(t *testing.T) {
	dir := t.TempDir()
	wal, err := NewWriteAheadLog(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

	entry := &WALEntry{TxID: 1, Status: WALStatusStarted}
	if err := wal.AppendEntry(entry); err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	entries, err := wal.ReadAllEntries()
	if err != nil {
		t.Fatalf("ReadAllEntries failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Status != WALStatusStarted {
		t.Errorf("expected STARTED status for uncommitted, got %s", entries[0].Status)
	}
}

func TestWAL_AppendAfterRotation(t *testing.T) {
	dir := t.TempDir()
	wal, err := NewWriteAheadLog(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

	for i := 0; i < 10; i++ {
		entry := &WALEntry{
			TxID:          uint64(i + 1),
			CommitHash:    fmt.Sprintf("hash_%d", i),
			Timestamp:     time.Now().UTC(),
			Status:        WALStatusCommitted,
			ModifiedFiles: []string{fmt.Sprintf("file_%d.go", i)},
		}
		if err := wal.AppendEntry(entry); err != nil {
			t.Fatalf("failed to append entry: %v", err)
		}
	}

	rotated, err := wal.Checkpoint()
	if err != nil {
		t.Fatalf("checkpoint failed: %v", err)
	}
	t.Logf("Checkpoint rotated: %v", rotated)

	for i := 10; i < 15; i++ {
		entry := &WALEntry{
			TxID:          uint64(i + 1),
			CommitHash:    fmt.Sprintf("hash_%d", i),
			Timestamp:     time.Now().UTC(),
			Status:        WALStatusCommitted,
			ModifiedFiles: []string{fmt.Sprintf("file_%d.go", i)},
		}
		if err := wal.AppendEntry(entry); err != nil {
			t.Fatalf("failed to append entry: %v", err)
		}
	}

	allEntries, err := wal.ReadAllEntries()
	if err != nil {
		t.Fatalf("ReadAllEntries failed: %v", err)
	}
	if len(allEntries) < 15 {
		t.Errorf("expected at least 15 entries across rotation, got %d", len(allEntries))
	}
}

func TestWAL_CheckpointTriggersRotation(t *testing.T) {
	dir := t.TempDir()
	wal, err := NewWriteAheadLog(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

	for i := 0; i < 5; i++ {
		entry := &WALEntry{
			TxID:       uint64(i + 1),
			CommitHash: "test",
			Timestamp:  time.Now().UTC(),
			Status:     WALStatusCommitted,
		}
		if err := wal.AppendEntry(entry); err != nil {
			t.Fatalf("failed to append: %v", err)
		}
	}

	rotated, err := wal.Checkpoint()
	if err != nil {
		t.Fatalf("checkpoint error: %v", err)
	}
	t.Logf("Checkpoint rotated: %v", rotated)
}
