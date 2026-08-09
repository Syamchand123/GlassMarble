package learning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// conventionsFile is the derived-aggregate file name inside the memory
// directory. Like memory.json, it is always recomputable from the graph and
// the memory WALs via LearnConventions — never a source of truth.
const conventionsFile = "conventions.json"

// ConventionsStore persists and reloads the learned project conventions
// (master plan §8.4). Written atomically (temp + rename) so a crash can
// never leave a truncated conventions file behind.
type ConventionsStore struct {
	dir string
	mu  sync.Mutex
}

// NewConventionsStore creates a store rooted at
// <repoDir>/.glassmarble/memory/conventions.json.
func NewConventionsStore(repoDir string) *ConventionsStore {
	return &ConventionsStore{dir: filepath.Join(repoDir, ".glassmarble", "memory")}
}

// Path returns the conventions file path.
func (s *ConventionsStore) Path() string {
	return filepath.Join(s.dir, conventionsFile)
}

// Save persists the conventions atomically.
func (s *ConventionsStore) Save(conv *ProjectConventions) error {
	if conv == nil {
		return fmt.Errorf("learning: refusing to save nil conventions")
	}
	data, err := json.MarshalIndent(conv, "", "  ")
	if err != nil {
		return fmt.Errorf("learning: marshal conventions: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("learning: create memory dir: %w", err)
	}
	path := s.Path()
	tmp, err := os.CreateTemp(s.dir, "."+conventionsFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("learning: create temp for conventions: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("learning: write conventions temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("learning: sync conventions temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("learning: close conventions temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("learning: rename conventions into place: %w", err)
	}
	return nil
}

// Load returns the persisted conventions, or (nil, nil) when none exist
// yet. A corrupt file is treated as absent (the conventions can always be
// re-learned) rather than as an error that hides the rest of the CLI.
func (s *ConventionsStore) Load() (*ProjectConventions, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("learning: read conventions: %w", err)
	}
	var conv ProjectConventions
	if err := json.Unmarshal(data, &conv); err != nil {
		return nil, nil // corrupt → re-learn; never fatal
	}
	return &conv, nil
}
