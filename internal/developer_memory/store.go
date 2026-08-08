package developer_memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// MemoryStore handles file persistence for developer memory.
type MemoryStore struct {
	dir string
	mu  sync.RWMutex
}

// NewMemoryStore creates a MemoryStore.
func NewMemoryStore(dir string) *MemoryStore {
	os.MkdirAll(dir, 0755)
	return &MemoryStore{dir: dir}
}

// appendJSONL appends a single JSON-marshaled struct to a file.
func (s *MemoryStore) appendJSONL(filename string, v interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, filename)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

// AppendEvent appends an ArchEvent to events.jsonl.
func (s *MemoryStore) AppendEvent(event archmodel.ArchEvent) error {
	return s.appendJSONL("events.jsonl", event)
}

// AppendClaim appends a KnowledgeClaim to claims.jsonl.
func (s *MemoryStore) AppendClaim(claim KnowledgeClaim) error {
	return s.appendJSONL("claims.jsonl", claim)
}

// AppendTimelineEntry appends a TimelineEntry to timeline.json (actually a jsonl for appends).
func (s *MemoryStore) AppendTimelineEntry(entry archmodel.TimelineEntry) error {
	return s.appendJSONL("timeline.jsonl", entry)
}

// LoadMemory reads the monolithic DeveloperMemory from memory.json.
func (s *MemoryStore) LoadMemory() (*DeveloperMemory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dir, "memory.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &DeveloperMemory{
				ComponentMemory: make(map[string]ComponentHistory),
			}, nil
		}
		return nil, err
	}

	var mem DeveloperMemory
	if err := json.Unmarshal(data, &mem); err != nil {
		return nil, err
	}
	if mem.ComponentMemory == nil {
		mem.ComponentMemory = make(map[string]ComponentHistory)
	}
	return &mem, nil
}

// SaveMemory writes DeveloperMemory to memory.json.
func (s *MemoryStore) SaveMemory(mem *DeveloperMemory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, "memory.json")
	data, err := json.MarshalIndent(mem, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// GetComponentHistory loads a component's history from the master memory file.
func (s *MemoryStore) GetComponentHistory(name string) ComponentHistory {
	mem, err := s.LoadMemory()
	if err != nil || mem == nil {
		return ComponentHistory{Name: name, State: StateUnknown}
	}
	if ch, ok := mem.ComponentMemory[name]; ok {
		return ch
	}
	return ComponentHistory{Name: name, State: StateUnknown}
}

// SaveComponentHistory updates a component's history in the master memory file.
func (s *MemoryStore) SaveComponentHistory(name string, history ComponentHistory) {
	mem, err := s.LoadMemory()
	if err != nil || mem == nil {
		mem = &DeveloperMemory{ComponentMemory: make(map[string]ComponentHistory)}
	}
	mem.ComponentMemory[name] = history
	s.SaveMemory(mem)
}
