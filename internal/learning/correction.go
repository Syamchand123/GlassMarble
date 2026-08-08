package learning

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

type CorrectionKind string

const (
	CorrectionKindIntent     CorrectionKind = "INTENT"
	CorrectionKindLabel      CorrectionKind = "LABEL"
	CorrectionKindState      CorrectionKind = "STATE"
	CorrectionKindConfidence CorrectionKind = "CONFIDENCE"
	CorrectionKindReject     CorrectionKind = "REJECT"
)

type Correction struct {
	ID             string         `json:"id"`
	Timestamp      time.Time      `json:"timestamp"`
	Kind           CorrectionKind `json:"kind"`
	TargetID       string         `json:"target_id"` // event ID, claim ID, or node ID
	OriginalValue  string         `json:"original_value"`
	CorrectedValue string         `json:"corrected_value"`
	Reason         string         `json:"reason"`
	Author         string         `json:"author,omitempty"`
}

type Store struct {
	filePath string
}

func NewStore(repoDir string) *Store {
	return &Store{
		filePath: filepath.Join(repoDir, ".glassmarble", "memory", "corrections.jsonl"),
	}
}

func (s *Store) Append(c Correction) error {
	if c.Timestamp.IsZero() {
		c.Timestamp = time.Now()
	}
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

func (s *Store) LoadAll() ([]Correction, error) {
	f, err := os.Open(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var corrections []Correction
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var c Correction
		if err := json.Unmarshal(line, &c); err == nil {
			corrections = append(corrections, c)
		}
	}
	return corrections, scanner.Err()
}

// MemoryQueryResult is a unified struct for serving memory data.
type MemoryQueryResult struct {
	Claims []developer_memory.KnowledgeClaim
	Events []archmodel.ArchEvent
}

// ApplyCorrections applies corrections as an overlay to the queried results.
// It never mutates the underlying source of truth database, only the returned projection.
func ApplyCorrections(result *MemoryQueryResult, corrections []Correction) *MemoryQueryResult {
	// Group corrections by target ID
	cmap := make(map[string][]Correction)
	for _, c := range corrections {
		cmap[c.TargetID] = append(cmap[c.TargetID], c)
	}

	// Clone arrays so we don't accidentally mutate passed pointers if they are referenced
	res := &MemoryQueryResult{
		Claims: make([]developer_memory.KnowledgeClaim, len(result.Claims)),
		Events: make([]archmodel.ArchEvent, len(result.Events)),
	}
	copy(res.Claims, result.Claims)
	copy(res.Events, result.Events)

	// Apply to claims
	for i, claim := range res.Claims {
		for _, c := range cmap[claim.ID] {
			switch c.Kind {
			case CorrectionKindReject:
				res.Claims[i].State = developer_memory.StateRemoved
			case CorrectionKindState:
				res.Claims[i].State = developer_memory.KnowledgeState(c.CorrectedValue)
			}
		}
	}

	// Apply to events
	for i, ev := range res.Events {
		for _, c := range cmap[ev.ID] {
			switch c.Kind {
			case CorrectionKindIntent:
				res.Events[i].Intent = c.CorrectedValue
				res.Events[i].Title += " (Corrected)"
			case CorrectionKindLabel:
				res.Events[i].Title = c.CorrectedValue
			}
		}
	}

	return res
}
