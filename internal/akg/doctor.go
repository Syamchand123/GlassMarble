package akg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// DoctorReport is the health dashboard for the persisted AKG artifact
// (AUDIT Issue 4 Phase 4C-11 / Issue 5 Phase 5B-4).
type DoctorReport struct {
	Initialized     bool
	StorageDir      string
	StatePath       string
	StateBytes      int64
	StateModTime    time.Time
	SchemaVersion   int
	GraphVersion    uint64
	CommitHash      string
	LoadOK          bool
	LoadError       string
	NodeCount       int
	EdgeCount       int
	Dangling        int
	DuplicateIDs    []string
	Verified        bool
	VerificationMsg string
}

// RunDoctor inspects the AKG state in storageDir without mutating anything:
// akg.json parse-back, duplicate node-ID detection, and a dangling reference
// audit. It never creates files and never takes the database lock.
func RunDoctor(storageDir string) (*DoctorReport, error) {
	rep := &DoctorReport{
		StorageDir: storageDir,
		StatePath:  filepath.Join(storageDir, jsonStateFile),
	}

	st, err := os.Stat(rep.StatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return rep, nil // Initialized stays false
		}
		return nil, fmt.Errorf("failed to stat AKG state: %w", err)
	}
	rep.Initialized = true
	rep.StateBytes = st.Size()
	rep.StateModTime = st.ModTime()

	// Metadata (cheap, does not trust a full load).
	rep.CommitHash, rep.SchemaVersion, rep.GraphVersion, err = StateMetadata(storageDir)
	if err != nil {
		rep.LoadOK = false
		rep.LoadError = fmt.Sprintf("metadata scan failed: %v", err)
	}

	data, err := os.ReadFile(rep.StatePath)
	if err != nil {
		rep.LoadOK = false
		rep.LoadError = fmt.Sprintf("failed to read state file: %v", err)
		return rep, nil
	}

	// Duplicate node IDs at the document level.
	var doc GraphJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		rep.LoadOK = false
		rep.LoadError = fmt.Sprintf("state file did not parse back cleanly: %v", err)
		return rep, nil
	}
	idCount := make(map[string]int)
	for _, n := range doc.Nodes {
		if n.ID == "" {
			continue
		}
		idCount[n.ID]++
	}
	for id, n := range idCount {
		if n > 1 {
			rep.DuplicateIDs = append(rep.DuplicateIDs, id)
		}
	}
	sort.Strings(rep.DuplicateIDs)

	// Full parse-back: corruption surfaces as a hard, actionable error
	// instead of a silent empty graph (Issue 5 finding 6).
	graph, err := ImportGraphJSON(bytes.NewReader(data))
	if err != nil {
		rep.LoadOK = false
		rep.LoadError = fmt.Sprintf("parse-back failed: %v", err)
		return rep, nil
	}
	rep.LoadOK = true
	rep.NodeCount = graph.Nodes.Len()
	rep.EdgeCount = 0
	graph.OutboundEdges.Iterate(func(_ string, edges []link.ResolvedEdge) {
		rep.EdgeCount += len(edges)
	})
	rep.Dangling = 0
	graph.OutboundEdges.Iterate(func(_ string, edges []link.ResolvedEdge) {
		for _, e := range edges {
			if _, ok := graph.Nodes.Get(e.TargetID); !ok {
				rep.Dangling++
			}
		}
	})
	rep.Verified = graph.Verified
	rep.VerificationMsg = graph.VerificationMsg

	return rep, nil
}
