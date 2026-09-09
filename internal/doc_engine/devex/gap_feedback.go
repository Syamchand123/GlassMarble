package devex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
)

// QueryGap represents a recorded user query that lacked managed documentation coverage.
type QueryGap struct {
	Query       string    `json:"query"`
	Topic       string    `json:"topic"`
	Count       int       `json:"count"`
	LastQueried time.Time `json:"last_queried"`
	MissingFrom string    `json:"missing_from"`
}

var gapMu sync.Mutex

// RecordQueryGap logs an ungrounded or high-frequency query into .glassmarble/docs_gaps.json.
func RecordQueryGap(repoRoot, query, topic, targetDoc string) error {
	gapMu.Lock()
	defer gapMu.Unlock()

	gapsPath := filepath.Join(repoRoot, ".glassmarble", "docs_gaps.json")
	var gaps []QueryGap

	if data, err := os.ReadFile(gapsPath); err == nil {
		_ = json.Unmarshal(data, &gaps)
	}

	found := false
	for i, g := range gaps {
		if g.Query == query || (g.Topic != "" && g.Topic == topic) {
			gaps[i].Count++
			gaps[i].LastQueried = time.Now().UTC()
			if targetDoc != "" {
				gaps[i].MissingFrom = targetDoc
			}
			found = true
			break
		}
	}

	if !found {
		gaps = append(gaps, QueryGap{
			Query:       query,
			Topic:       topic,
			Count:       1,
			LastQueried: time.Now().UTC(),
			MissingFrom: targetDoc,
		})
	}

	data, err := json.MarshalIndent(gaps, "", "  ")
	if err != nil {
		return err
	}

	_, err = storage.AtomicWriteFile(gapsPath, data)
	return err
}

// LoadQueryGaps loads the recorded gaps from .glassmarble/docs_gaps.json.
func LoadQueryGaps(repoRoot string) ([]QueryGap, error) {
	gapsPath := filepath.Join(repoRoot, ".glassmarble", "docs_gaps.json")
	data, err := os.ReadFile(gapsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var gaps []QueryGap
	if err := json.Unmarshal(data, &gaps); err != nil {
		return nil, err
	}
	return gaps, nil
}
