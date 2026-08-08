package developer_memory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// Storage layout (master plan §4.2). The JSONL files are append-only
// write-ahead logs and the source of truth; the JSON files are derived
// aggregates rebuilt from the logs (see Rebuild).
const (
	eventsFile   = "events.jsonl"  // append-only WAL of ArchEvents
	claimsFile   = "claims.jsonl"  // append-only WAL of KnowledgeClaims
	memoryFile   = "memory.json"   // derived aggregate (DeveloperMemory)
	timelineFile = "timeline.json" // derived timeline entries (fast path for `gmb timeline`)
	memoryDir    = ".glassmarble"  // workspace root inside the repo
)

// MemoryStore handles file persistence for developer memory.
//
// Concurrency: single-writer model (the memory builder is the only
// producer). All file access is guarded by an internal mutex, so concurrent
// readers are safe; concurrent writers are the caller's responsibility.
type MemoryStore struct {
	dir string
	mu  sync.RWMutex

	// logf receives non-fatal warnings (e.g. corrupt JSONL lines that were
	// skipped). Nil disables warnings.
	logf func(format string, args ...any)
}

// NewMemoryStore creates a MemoryStore rooted at dir. The directory is
// created on first use if missing. dir is the memory directory itself
// (e.g. <repo>/.glassmarble/memory) — see NewStoreForRepo for the
// repo-root convenience constructor.
func NewMemoryStore(dir string) *MemoryStore {
	return &MemoryStore{dir: dir, logf: func(string, ...any) {}}
}

// NewStoreForRepo creates a MemoryStore rooted at <repoDir>/.glassmarble/memory,
// matching the storage layout defined by the master plan §4.2.
func NewStoreForRepo(repoDir string) *MemoryStore {
	return NewMemoryStore(filepath.Join(repoDir, memoryDir, "memory"))
}

// Dir returns the memory directory.
func (s *MemoryStore) Dir() string {
	return s.dir
}

// WithLogger attaches a warning sink. Returns the store for chaining.
func (s *MemoryStore) WithLogger(logf func(format string, args ...any)) *MemoryStore {
	if logf != nil {
		s.logf = logf
	}
	return s
}

// warn reports a non-fatal condition through the logger if one is attached.
func (s *MemoryStore) warn(format string, args ...any) {
	if s.logf != nil {
		s.logf(format, args...)
	}
}

// ensureDir lazily creates the memory directory.
func (s *MemoryStore) ensureDir() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("developer_memory: create memory dir: %w", err)
	}
	return nil
}

// --- Write-ahead logs (append-only, source of truth) ---

// AppendEvent appends one ArchEvent to events.jsonl. Never overwrites.
func (s *MemoryStore) AppendEvent(event archmodel.ArchEvent) error {
	return s.appendJSONL(eventsFile, event)
}

// AppendClaim appends one KnowledgeClaim to claims.jsonl. Never overwrites.
func (s *MemoryStore) AppendClaim(claim KnowledgeClaim) error {
	return s.appendJSONL(claimsFile, claim)
}

// appendJSONL appends a single JSON-marshaled value as one line.
// The write is fsynced before returning so a crash cannot lose events.
func (s *MemoryStore) appendJSONL(filename string, v any) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, filename)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("developer_memory: open %s: %w", filename, err)
	}

	data, err := json.Marshal(v)
	if err != nil {
		f.Close()
		return fmt.Errorf("developer_memory: marshal %s: %w", filename, err)
	}
	data = append(data, '\n')

	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("developer_memory: write %s: %w", filename, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("developer_memory: sync %s: %w", filename, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("developer_memory: close %s: %w", filename, err)
	}
	return nil
}

// LoadEvents reads every event from the WAL in order.
func (s *MemoryStore) LoadEvents() ([]archmodel.ArchEvent, error) {
	var events []archmodel.ArchEvent
	_, err := s.scanJSONL(eventsFile, func(line []byte) error {
		var e archmodel.ArchEvent
		if err := json.Unmarshal(line, &e); err != nil {
			return err
		}
		events = append(events, e)
		return nil
	})
	return events, err
}

// LoadClaims reads every claim from the WAL in order.
func (s *MemoryStore) LoadClaims() ([]KnowledgeClaim, error) {
	var claims []KnowledgeClaim
	_, err := s.scanJSONL(claimsFile, func(line []byte) error {
		var c KnowledgeClaim
		if err := json.Unmarshal(line, &c); err != nil {
			return err
		}
		claims = append(claims, c)
		return nil
	})
	return claims, err
}

// scanJSONL streams one JSONL file, invoking decode for each non-empty line.
// Corrupt lines are skipped and reported through the warning sink — a single
// bad line must never make the whole memory unreadable ("never destroy
// historical knowledge" applies to resilience as much as to semantics).
func (s *MemoryStore) scanJSONL(filename string, decode func(line []byte) error) (skipped int, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	f, err := os.Open(filepath.Join(s.dir, filename))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("developer_memory: open %s: %w", filename, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if err := decode(line); err != nil {
			skipped++
			s.warn("developer_memory: skipping corrupt line in %s: %v", filename, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return skipped, fmt.Errorf("developer_memory: read %s: %w", filename, err)
	}
	return skipped, nil
}

// --- Derived aggregates (rebuildable from the WALs) ---

// Rebuild derives the complete DeveloperMemory aggregate from the WALs.
// It is the single place where events become memory: component histories,
// knowledge claims, timeline entries and counters are all recomputed from
// the logs, so the aggregate can never drift from the source of truth.
//
// Rebuild is idempotent: processing the same event twice (by ID) has the
// same effect as processing it once.
func (s *MemoryStore) Rebuild() (*DeveloperMemory, error) {
	events, err := s.LoadEvents()
	if err != nil {
		return nil, err
	}
	claims, err := s.LoadClaims()
	if err != nil {
		return nil, err
	}

	mem := &DeveloperMemory{
		ComponentMemory: make(map[string]ComponentHistory),
	}
	seenClaims := make(map[string]bool)
	for _, c := range claims {
		if seenClaims[c.ID] {
			continue
		}
		seenClaims[c.ID] = true
		mem.GlobalMemory = append(mem.GlobalMemory, c)
	}

	seenEvents := make(map[string]bool)
	for _, ev := range events {
		if seenEvents[ev.ID] {
			continue
		}
		seenEvents[ev.ID] = true
		mem.Events = append(mem.Events, ev)
		applyEvent(mem, ev)
	}
	sortTimeline(mem.Timeline)
	return mem, nil
}

// SaveMemory atomically persists the DeveloperMemory aggregate to
// memory.json. Writes go to a temp file and are renamed into place, so a
// crash mid-write can never leave a truncated aggregate behind.
func (s *MemoryStore) SaveMemory(mem *DeveloperMemory) error {
	data, err := json.MarshalIndent(mem, "", "  ")
	if err != nil {
		return fmt.Errorf("developer_memory: marshal memory: %w", err)
	}
	return s.writeFileAtomic(memoryFile, data)
}

// SaveTimeline atomically persists the derived timeline entries to
// timeline.json (fast path for `gmb timeline`; master plan §5.5).
func (s *MemoryStore) SaveTimeline(entries []archmodel.TimelineEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("developer_memory: marshal timeline: %w", err)
	}
	return s.writeFileAtomic(timelineFile, data)
}

// SaveMemoryAndTimeline persists both derived aggregates in one call.
func (s *MemoryStore) SaveMemoryAndTimeline(mem *DeveloperMemory) error {
	if err := s.SaveMemory(mem); err != nil {
		return err
	}
	return s.SaveTimeline(mem.Timeline)
}

// LoadMemory returns the current memory aggregate. It prefers the cached
// memory.json but self-heals: if the aggregate is missing or corrupt it is
// rebuilt from the WALs (which are authoritative). A nil result is never
// returned for a valid store.
func (s *MemoryStore) LoadMemory() (*DeveloperMemory, error) {
	s.mu.RLock()
	data, err := os.ReadFile(filepath.Join(s.dir, memoryFile))
	s.mu.RUnlock()
	if err == nil {
		var mem DeveloperMemory
		if err := json.Unmarshal(data, &mem); err == nil {
			if mem.ComponentMemory == nil {
				mem.ComponentMemory = make(map[string]ComponentHistory)
			}
			return &mem, nil
		}
		s.warn("developer_memory: memory.json is corrupt (%v); rebuilding from WAL", err)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("developer_memory: read memory.json: %w", err)
	}

	mem, err := s.Rebuild()
	if err != nil {
		return nil, err
	}
	return mem, nil
}

// writeFileAtomic writes data to path via a temp file + rename so the
// destination is never observed in a partially-written state.
func (s *MemoryStore) writeFileAtomic(filename string, data []byte) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, filename)
	tmp, err := os.CreateTemp(s.dir, "."+filename+".tmp-*")
	if err != nil {
		return fmt.Errorf("developer_memory: create temp for %s: %w", filename, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("developer_memory: write temp %s: %w", filename, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("developer_memory: sync temp %s: %w", filename, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("developer_memory: close temp %s: %w", filename, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("developer_memory: rename temp %s: %w", filename, err)
	}
	return nil
}
