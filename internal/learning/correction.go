package learning

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// CorrectionKind classifies what a user correction changes about a memory
// item. The values are persisted to corrections.jsonl and must never change
// after first release (master plan §8.2).
type CorrectionKind string

const (
	// CorrectionKindIntent overrides the WHY explanation (an event's
	// displayed intent).
	CorrectionKindIntent CorrectionKind = "INTENT"

	// CorrectionKindLabel overrides a displayed name (an event title, a
	// claim subject, a component name).
	CorrectionKindLabel CorrectionKind = "LABEL"

	// CorrectionKindState overrides a knowledge state (e.g. mark something
	// DEPRECATED / HISTORICAL). The corrected value must be a valid
	// developer_memory.KnowledgeState.
	CorrectionKindState CorrectionKind = "STATE"

	// CorrectionKindConfidence overrides a displayed confidence score. The
	// corrected value must parse as a float in [0, 1].
	CorrectionKindConfidence CorrectionKind = "CONFIDENCE"

	// CorrectionKindReject rejects an inference: the item stays visible but
	// is flagged as rejected by the developer. It never rewrites the
	// temporal state — rejection is an overlay, not a fact.
	CorrectionKindReject CorrectionKind = "REJECT"

	// CorrectionKindAccept accepts/confirms an inference: the item stays
	// visible and is flagged as confirmed. Feeds preferred-pattern
	// learning. (convention learning's inputs list "accepted/rejected inferences";
	// REJECT alone could not express confirmation.)
	CorrectionKindAccept CorrectionKind = "ACCEPT"
)

// Correction is one user-provided correction or annotation, appended
// forever to .glassmarble/memory/corrections.jsonl. The log is append-only:
// nothing in it is ever rewritten, so the learning history is fully
// auditable and every change is reversible via a compensating correction.
type Correction struct {
	// ID is deterministic: corr_ + sha256(kind, target, corrected value,
	// timestamp). Appending the same correction twice yields the same ID,
	// and loads deduplicate by ID — re-running a CLI command never
	// duplicates the log.
	ID string `json:"id"`

	// Timestamp is when the correction was recorded (now when zero).
	Timestamp time.Time `json:"timestamp"`

	// Kind is what the correction changes (INTENT/LABEL/STATE/CONFIDENCE/
	// REJECT/ACCEPT).
	Kind CorrectionKind `json:"kind"`

	// TargetID is the memory item being corrected: a claim ID, event ID,
	// or component name.
	TargetID string `json:"target_id"`

	// OriginalValue is what was displayed before the correction, captured
	// at record time for the audit trail (and to build compensating
	// corrections). Optional but strongly encouraged.
	OriginalValue string `json:"original_value"`

	// CorrectedValue is the new value. Required for INTENT/LABEL/STATE/
	// CONFIDENCE; ignored for REJECT/ACCEPT.
	CorrectedValue string `json:"corrected_value"`

	// Reason is the developer's free-text justification (optional).
	Reason string `json:"reason"`

	// Author is the developer who made the correction (optional).
	Author string `json:"author,omitempty"`
}

// KnownKinds reports whether a CorrectionKind is a valid, supported kind.
func (k CorrectionKind) KnownKinds() bool {
	switch k {
	case CorrectionKindIntent, CorrectionKindLabel, CorrectionKindState,
		CorrectionKindConfidence, CorrectionKindReject, CorrectionKindAccept:
		return true
	}
	return false
}

// NeedsValue reports whether the kind requires a non-empty CorrectedValue.
func (k CorrectionKind) NeedsValue() bool {
	switch k {
	case CorrectionKindReject, CorrectionKindAccept:
		return false
	}
	return true
}

// Validate checks a correction before it is persisted. It enforces:
//
//   - a known kind and a non-empty target,
//   - a non-empty corrected value for the value-changing kinds,
//   - STATE values that are real KnowledgeStates,
//   - CONFIDENCE values that parse as floats in [0, 1].
//
// The original value is never validated — it is a historical record of what
// the memory displayed, whatever that happened to be.
func (c Correction) Validate() error {
	if !c.Kind.KnownKinds() {
		return fmt.Errorf("learning: unknown correction kind %q", c.Kind)
	}
	if c.TargetID == "" {
		return fmt.Errorf("learning: correction %q has an empty target_id", c.Kind)
	}
	if c.Kind.NeedsValue() && c.CorrectedValue == "" {
		return fmt.Errorf("learning: %s correction %q needs a non-empty corrected value", c.Kind, c.TargetID)
	}
	switch c.Kind {
	case CorrectionKindState:
		if !validKnowledgeState(c.CorrectedValue) {
			return fmt.Errorf("learning: %q is not a valid knowledge state (want one of CURRENT, DEPRECATED, REMOVED, HISTORICAL, EXPERIMENTAL, UNKNOWN)", c.CorrectedValue)
		}
	case CorrectionKindConfidence:
		v, err := strconv.ParseFloat(c.CorrectedValue, 64)
		if err != nil || v < 0 || v > 1 {
			return fmt.Errorf("learning: %q is not a confidence in [0,1]", c.CorrectedValue)
		}
	}
	return nil
}

// validKnowledgeState reports whether s names one of the persisted
// developer_memory knowledge states. The strings are the storage contract
// (master plan §1.5) and are matched against the constants' values.
func validKnowledgeState(s string) bool {
	switch developer_memory.KnowledgeState(s) {
	case developer_memory.StateActive,
		developer_memory.StateDeprecated,
		developer_memory.StateRemoved,
		developer_memory.StateHistorical,
		developer_memory.StateExperimental,
		developer_memory.StateUnknown:
		return true
	}
	return false
}

// correctionID derives the deterministic ID for a correction. Same content,
// same timestamp → same ID, which is what makes repeated appends
// idempotent.
// correctionID derives a correction's identity from its CONTENT only.
//
// The timestamp is deliberately excluded. Append sets it to time.Now() when
// zero, so folding it in meant two identical `gmb memory --correct`
// invocations produced different IDs and both survived LoadAll's dedup -
// directly contradicting the documented contract that re-running a CLI
// command never duplicates the log. Recording the same correction twice is
// now genuinely idempotent; the timestamp is still stored for audit.
//
// Fields are length-prefixed rather than joined, because skipping empty parts
// let (kind, target, "") and (kind, "", target) hash identically.
func correctionID(kind CorrectionKind, targetID, correctedValue string, _ time.Time) string {
	h := sha256.New()
	for _, part := range []string{"corr", string(kind), targetID, correctedValue} {
		fmt.Fprintf(h, "%d:%s\x00", len(part), part)
	}
	return "corr_" + hex.EncodeToString(h.Sum(nil)[:16])
}

// stringsJoinNonEmpty joins parts with sep, skipping empty parts so the
// hash is stable when optional fields are absent.
func stringsJoinNonEmpty(sep string, parts ...string) string {
	var out []byte
	first := true
	for _, p := range parts {
		if p == "" {
			continue
		}
		if !first {
			out = append(out, sep...)
		}
		first = false
		out = append(out, p...)
	}
	return string(out)
}

// Store persists corrections to .glassmarble/memory/corrections.jsonl.
//
// Discipline (mirrors developer_memory.MemoryStore):
//
//   - append-only: lines are written once and never rewritten,
//   - every write is fsynced before returning,
//   - all access is guarded by a mutex so concurrent appends (watch mode +
//     CLI) are safe,
//   - corrupt lines are skipped with a warning, never fatal — one bad line
//     must never hide the rest of the learning history.
type Store struct {
	filePath string
	mu       sync.Mutex
	logf     func(format string, args ...any)
}

// NewStore creates a correction store rooted at <repoDir>/.glassmarble/
// memory/corrections.jsonl (master plan §8.2).
func NewStore(repoDir string) *Store {
	return NewStoreAtPath(filepath.Join(repoDir, ".glassmarble", "memory", "corrections.jsonl"))
}

// NewStoreAtPath creates a correction store for an explicit file path.
// Used by tests and by consumers that resolve the storage root themselves.
func NewStoreAtPath(path string) *Store {
	return &Store{filePath: path, logf: func(string, ...any) {}}
}

// WithLogger attaches a warning sink. Returns the store for chaining.
func (s *Store) WithLogger(logf func(format string, args ...any)) *Store {
	if logf != nil {
		s.logf = logf
	}
	return s
}

// Path returns the corrections file path.
func (s *Store) Path() string {
	return s.filePath
}

// warn reports a non-fatal condition through the logger if one is attached.
func (s *Store) warn(format string, args ...any) {
	if s.logf != nil {
		s.logf(format, args...)
	}
}

// Append validates and persists one correction. The timestamp defaults to
// now, the ID is derived deterministically, and the line is fsynced before
// returning. Duplicate appends (same content, same timestamp) are harmless:
// LoadAll deduplicates by ID.
func (s *Store) Append(c Correction) (Correction, error) {
	if c.Timestamp.IsZero() {
		c.Timestamp = time.Now()
	}
	c.ID = correctionID(c.Kind, c.TargetID, c.CorrectedValue, c.Timestamp)
	if err := c.Validate(); err != nil {
		return Correction{}, err
	}

	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o755); err != nil {
		return Correction{}, fmt.Errorf("learning: create corrections dir: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(c)
	if err != nil {
		return Correction{}, fmt.Errorf("learning: marshal correction: %w", err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(s.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return Correction{}, fmt.Errorf("learning: open corrections log: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return Correction{}, fmt.Errorf("learning: write correction: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return Correction{}, fmt.Errorf("learning: sync corrections log: %w", err)
	}
	if err := f.Close(); err != nil {
		return Correction{}, fmt.Errorf("learning: close corrections log: %w", err)
	}
	return c, nil
}

// LoadAll reads every correction in log order. Duplicate IDs (repeated
// appends of the same correction) collapse to the first occurrence, and
// corrupt lines are skipped with a warning. A missing log is not an error —
// it means nothing has been learned yet.
func (s *Store) LoadAll() ([]Correction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("learning: open corrections log: %w", err)
	}
	defer f.Close()

	var corrections []Correction
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var c Correction
		if err := json.Unmarshal(line, &c); err != nil {
			s.warn("learning: skipping corrupt correction line: %v", err)
			continue
		}
		if c.ID == "" {
			s.warn("learning: skipping correction with empty ID: %s", string(line))
			continue
		}
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		corrections = append(corrections, c)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("learning: read corrections log: %w", err)
	}
	return corrections, nil
}

// LoadForTargets returns the corrections that target any of the given
// target IDs/names, in log order. Used by the overlay engine to apply
// corrections only where they matter.
func (s *Store) LoadForTargets(targets ...string) ([]Correction, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	all, err := s.LoadAll()
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(targets))
	for _, t := range targets {
		want[t] = true
	}
	var out []Correction
	for _, c := range all {
		if want[c.TargetID] {
			out = append(out, c)
		}
	}
	return out, nil
}

// SortedCorrections orders corrections so later timestamps come later in the
// slice — the order in which the overlay applies them. Corrections with
// identical timestamps keep their log order (stable sort).
func SortedCorrections(corrections []Correction) []Correction {
	out := make([]Correction, len(corrections))
	copy(out, corrections)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}
