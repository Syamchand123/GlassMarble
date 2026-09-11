// Package review implements the D6 human review queue: pending items (merge
// conflicts, Gate-5 discards, snippet fixes, ADR drafts, prose flags) with
// approve/reject, plus revert observations that feed prompt/style tuning.
//
// State machine (documented here as the contract):
//
//	pending → approved | rejected
//	observed (terminal, informational)
//
// "observed" records a human action already taken (a revert of machine
// text): there is nothing to approve, so Resolve rejects any transition
// FROM observed with a "terminal" error. approved and rejected are
// re-resolvable (a later reviewer may overturn); only observed is terminal.
//
// Persistence is a JSON array file at <repoRoot>/.glassmarble/review.json,
// written atomically (temp file + rename in the same directory). Stdlib only.
package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Review statuses.
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
	StatusObserved = "observed"
)

// ReviewItem is one human-review entry.
type ReviewItem struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	DocPath    string `json:"doc_path"`
	SectionID  string `json:"section_id"`
	Summary    string `json:"summary"`
	Detail     string `json:"detail"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	ResolvedAt string `json:"resolved_at,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// reviewFile returns the queue file path for a repo root.
func reviewFile(repoRoot string) string {
	return filepath.Join(repoRoot, ".glassmarble", "review.json")
}

// load reads the queue file. A missing file — or an empty/whitespace-only
// file — yields an empty queue with nil error. Malformed JSON is a hard
// error so corruption surfaces instead of silently dropping reviews.
func load(repoRoot string) ([]ReviewItem, error) {
	raw, err := os.ReadFile(reviewFile(repoRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("review: read queue: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	trimmed := raw
	for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\t' || trimmed[0] == '\n' || trimmed[0] == '\r') {
		trimmed = trimmed[1:]
	}
	if len(trimmed) == 0 {
		return nil, nil
	}
	var items []ReviewItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("review: corrupt queue file: %w", err)
	}
	return items, nil
}

// save persists the queue atomically: temp file in the same directory +
// rename, so a crash mid-write leaves either the old or the new bytes.
func save(repoRoot string, items []ReviewItem) error {
	dir := filepath.Join(repoRoot, ".glassmarble")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("review: create queue dir: %w", err)
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("review: encode queue: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "review-*.tmp")
	if err != nil {
		return fmt.Errorf("review: create temp queue: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("review: write queue: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("review: close queue: %w", err)
	}
	if err := os.Rename(tmpName, reviewFile(repoRoot)); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("review: commit queue: %w", err)
	}
	return nil
}

// validStatus reports whether s is a known review state.
func validStatus(s string) bool {
	switch s {
	case StatusPending, StatusApproved, StatusRejected, StatusObserved:
		return true
	}
	return false
}

// Queue appends item to the repo's review queue and returns its ID.
// The ID is "r" + 8 hex chars of SHA256(kind + doc + section + summary +
// timestamp-nanoseconds), so IDs are unique per enqueue and carry no run
// ordering. An empty Status defaults to pending; an unknown Status is an
// error. An empty CreatedAt defaults to now (UTC, RFC3339).
func Queue(repoRoot string, item ReviewItem) (string, error) {
	items, err := load(repoRoot)
	if err != nil {
		return "", err
	}
	if item.Status == "" {
		item.Status = StatusPending
	} else if !validStatus(item.Status) {
		return "", fmt.Errorf("review: unknown status %q", item.Status)
	}
	now := time.Now().UTC()
	nanos := time.Now().UnixNano()
	sum := sha256.Sum256([]byte(item.Kind + "\x00" + item.DocPath + "\x00" + item.SectionID + "\x00" + item.Summary + "\x00" + fmt.Sprintf("%d", nanos)))
	item.ID = "r" + hex.EncodeToString(sum[:])[:8]
	if item.CreatedAt == "" {
		item.CreatedAt = now.Format(time.RFC3339)
	}
	items = append(items, item)
	if err := save(repoRoot, items); err != nil {
		return "", err
	}
	return item.ID, nil
}

// ListPending returns all pending items in queue order. A missing queue
// file yields an empty slice and nil error.
func ListPending(repoRoot string) ([]ReviewItem, error) {
	items, err := load(repoRoot)
	if err != nil {
		return nil, err
	}
	var out []ReviewItem
	for _, it := range items {
		if it.Status == StatusPending {
			out = append(out, it)
		}
	}
	if out == nil {
		out = []ReviewItem{}
	}
	return out, nil
}

// Resolve marks item id approved (approve=true) or rejected, stamping
// ResolvedAt and Reason. Unknown IDs are an error. Transitions FROM the
// terminal observed state are rejected with an error; approved/rejected
// items may be re-resolved (reviewers can overturn).
func Resolve(repoRoot, id string, approve bool, reason string) error {
	items, err := load(repoRoot)
	if err != nil {
		return err
	}
	for i, it := range items {
		if it.ID != id {
			continue
		}
		if it.Status == StatusObserved {
			return fmt.Errorf("review: item %q is terminal (observed)", id)
		}
		if approve {
			items[i].Status = StatusApproved
		} else {
			items[i].Status = StatusRejected
		}
		items[i].ResolvedAt = time.Now().UTC().Format(time.RFC3339)
		items[i].Reason = reason
		return save(repoRoot, items)
	}
	return fmt.Errorf("review: unknown id %q", id)
}

// RecordRevert logs a human revert of machine text as a terminal
// informational observation: Kind "revert", Status "observed", ResolvedAt
// set at record time. It never appears in ListPending and Resolve refuses
// to transition it.
func RecordRevert(repoRoot, docPath, sectionID, reason string) error {
	summary := "Revert of " + docPath + "#" + sectionID
	items, err := load(repoRoot)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	nanos := time.Now().UnixNano()
	sum := sha256.Sum256([]byte("revert" + "\x00" + docPath + "\x00" + sectionID + "\x00" + reason + "\x00" + fmt.Sprintf("%d", nanos)))
	items = append(items, ReviewItem{
		ID:         "r" + hex.EncodeToString(sum[:])[:8],
		Kind:       "revert",
		DocPath:    docPath,
		SectionID:  sectionID,
		Summary:    summary,
		Detail:     reason,
		Status:     StatusObserved,
		CreatedAt:  now.Format(time.RFC3339),
		ResolvedAt: now.Format(time.RFC3339),
		Reason:     reason,
	})
	return save(repoRoot, items)
}

// Stats counts queue items by status. A missing queue yields all zeros.
func Stats(repoRoot string) (pending, approved, rejected, observed int, err error) {
	items, err := load(repoRoot)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	for _, it := range items {
		switch it.Status {
		case StatusPending:
			pending++
		case StatusApproved:
			approved++
		case StatusRejected:
			rejected++
		case StatusObserved:
			observed++
		}
	}
	return pending, approved, rejected, observed, nil
}
