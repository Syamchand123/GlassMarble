package akg

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// DoctorReport is the health dashboard for the persisted AKG artifact
// (AUDIT Issue 4 Phase 4C-11 / Issue 5 Phase 5B-4).
type DoctorReport struct {
	Initialized     bool
	StorageDir      string
	TTLPath         string
	WALPath         string
	TTLBytes        int64
	WALBytes        int64
	TTLModTime      time.Time
	WALModTime      time.Time
	SchemaVersion   int
	GraphVersion    uint64
	CommitHash      string
	LoadOK          bool
	LoadError       string
	NodeCount       int
	EdgeCount       int
	Dangling        int
	DuplicateIDs    []string
	UnknownTerms    []string
	WALTxCount      int
	WALCommitted    int
	WALPending      int
	StaleWAL        bool
	Verified        bool
	VerificationMsg string
}

var (
	nodeBlockHeader = regexp.MustCompile(`(?m)^<http://glassmarble\.org/node/([^>]+)> a ` + ont.PrefixGM + `[A-Za-z0-9_]+ ?[;.]`)
	gmTerm          = regexp.MustCompile(`\b` + ont.PrefixGM + `([A-Za-z0-9_]+)`)
	// ttlStringLiteral matches a quoted TTL literal, honoring backslash
	// escapes, so that gm: terms inside source-code content strings are not
	// mistaken for live vocabulary (Issue 3 Phase 3A-2 conformance scan).
	ttlStringLiteral = regexp.MustCompile(`"[^"\\]*(?:\\.[^"\\]*)*"`)
)

// RunDoctor inspects the AKG state in storageDir without mutating anything:
// TTL parse-back, ontology conformance of the file's gm: terms, dangling
// reference audit, duplicate node-ID detection, WAL state, and freshness.
// It never creates files and never takes the database lock.
func RunDoctor(storageDir string) (*DoctorReport, error) {
	rep := &DoctorReport{
		StorageDir: storageDir,
		TTLPath:    filepath.Join(storageDir, "akg_state.ttl"),
		WALPath:    filepath.Join(storageDir, "wal"),
	}

	ttlStat, err := os.Stat(rep.TTLPath)
	if err != nil {
		if os.IsNotExist(err) {
			return rep, nil // Initialized stays false
		}
		return nil, fmt.Errorf("failed to stat TTL: %w", err)
	}
	rep.Initialized = true
	rep.TTLBytes = ttlStat.Size()
	rep.TTLModTime = ttlStat.ModTime()

	// Metadata (cheap, does not trust a full load).
	rep.CommitHash, rep.SchemaVersion, rep.GraphVersion, err = scanTTLMetadata(rep.TTLPath)
	if err != nil {
		rep.LoadOK = false
		rep.LoadError = fmt.Sprintf("metadata scan failed: %v", err)
	}

	// Full parse-back: corruption surfaces as a hard, actionable error
	// instead of a silent empty graph (Issue 5 finding 6).
	graph, err := reconstructFromTTLFile(rep.TTLPath)
	if err != nil {
		rep.LoadOK = false
		rep.LoadError = fmt.Sprintf("TTL parse-back failed: %v", err)
		return rep, nil
	}
	rep.LoadOK = true
	rep.NodeCount = graph.Nodes.Len()
	rep.EdgeCount = 0
	graph.OutboundEdges.Iterate(func(_ string, edges []stage4.ResolvedEdge) {
		rep.EdgeCount += len(edges)
	})
	rep.Dangling = 0
	graph.OutboundEdges.Iterate(func(_ string, edges []stage4.ResolvedEdge) {
		for _, e := range edges {
			if _, ok := graph.Nodes.Get(e.TargetID); !ok {
				rep.Dangling++
			}
		}
	})

	ttlContent := readFile(rep.TTLPath)

	// Duplicate node IDs: an ID declared in more than one node block.
	idCount := make(map[string]int)
	for _, m := range nodeBlockHeader.FindAllStringSubmatch(ttlContent, -1) {
		idCount[m[1]]++
	}
	for id, n := range idCount {
		if n > 1 {
			rep.DuplicateIDs = append(rep.DuplicateIDs, id)
		}
	}
	sort.Strings(rep.DuplicateIDs)

	// Ontology conformance of every gm: term present in the file. Quoted
	// literals are stripped first: source-content strings routinely mention
	// gm: vocabulary and must not be counted as live terms.
	terms := make(map[string]bool)
	ttlVocabulary := ttlStringLiteral.ReplaceAllString(ttlContent, `""`)
	for _, m := range gmTerm.FindAllStringSubmatch(ttlVocabulary, -1) {
		terms[m[1]] = true
	}
	for term := range terms {
		if !isOntologyTermDeclared(ont.PrefixGM + term) {
			rep.UnknownTerms = append(rep.UnknownTerms, term)
		}
	}
	sort.Strings(rep.UnknownTerms)

	// WAL state + freshness (a WAL newer than the TTL with entries means a
	// write may not have been fully persisted).
	if walEntries, err := walSnapshot(rep.WALPath); err == nil {
		rep.WALTxCount = len(walEntries)
		for _, e := range walEntries {
			if e.Status == WALStatusCommitted {
				rep.WALCommitted++
			} else {
				rep.WALPending++
			}
		}
		if segments, err := os.ReadDir(rep.WALPath); err == nil {
			latest := rep.TTLModTime
			for _, seg := range segments {
				if seg.IsDir() {
					continue
				}
				info, err := seg.Info()
				if err != nil {
					continue
				}
				rep.WALBytes += info.Size()
				if info.ModTime().After(latest) {
					latest = info.ModTime()
				}
			}
			rep.WALModTime = latest
			rep.StaleWAL = latest.After(ttlStat.ModTime()) && rep.WALTxCount > 0
		}
	}

	return rep, nil
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// walSnapshot reads every WAL segment's entries.
func walSnapshot(walDir string) ([]*WALEntry, error) {
	files, err := os.ReadDir(walDir)
	if err != nil {
		return nil, err
	}
	var entries []*WALEntry
	for _, f := range files {
		if f.IsDir() || !strings.Contains(f.Name(), ".wal") {
			continue
		}
		wal, err := NewWriteAheadLog(walDir)
		if err != nil {
			return nil, err
		}
		wal.LogFilePath = filepath.Join(walDir, f.Name())
		fileEntries, err := wal.ReadAllEntries()
		if err != nil {
			continue
		}
		entries = append(entries, fileEntries...)
	}
	return entries, nil
}

// TTLMetadata returns the persisted metadata of the active state file:
// commit hash, schema version, and graph version (Issue 5 Phase 5B-5).
func TTLMetadata(storageDir string) (commitHash string, schemaVersion int, version uint64, err error) {
	return scanTTLMetadata(filepath.Join(storageDir, "akg_state.ttl"))
}
