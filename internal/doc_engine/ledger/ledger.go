// Package ledger implements the structured JSONL run ledger for the
// documentation engine (improvement plan D5: "Observability and cost
// attribution").
//
// Every engine run appends one RunRecord line to
// .glassmarble/runs/runs.jsonl; Summarize rolls the ledger up for
// `doc ledger --summary`, and Prune bounds its growth.
//
// Schema notes:
//   - Field names follow OpenTelemetry attribute conventions (lowercase
//     snake_case: duration_ms, docs_updated, ...) in spirit only: there is
//     NO OpenTelemetry dependency. The ledger is plain stdlib JSONL so it
//     works offline and stays cheap.
//   - Timestamps are RFC 3339 UTC strings in summaries; records carry
//     time.Time (defaulted to time.Now().UTC() by Append when zero).
//   - TracksUsed maps track name ("deterministic", "llm", ...) to the
//     number of sections rendered on that track; summaries merge maps by
//     summation.
//   - Malformed lines are skipped silently by Summarize (a corrupt line
//     must never break observability); Prune operates on raw lines and
//     therefore preserves them.
package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ledgerDirName  = ".glassmarble"
	ledgerRunsDir  = "runs"
	ledgerFileName = "runs.jsonl"
)

// RunRecord is one structured engine-run observation.
type RunRecord struct {
	Timestamp         time.Time      `json:"timestamp"`
	Commit            string         `json:"commit"`
	BranchPolicy      string         `json:"branch_policy"`
	DocsUpdated       int            `json:"docs_updated"`
	SectionsUpdated   int            `json:"sections_updated"`
	SectionsProcessed int            `json:"sections_processed"`
	TokensUsed        int            `json:"tokens_used"`
	DurationMs        int64          `json:"duration_ms"`
	TracksUsed        map[string]int `json:"tracks_used"`
	Repairs           int            `json:"repairs"`
	Fallbacks         int            `json:"fallbacks"`
	GlobalFreshness   int            `json:"global_freshness"`
	Warnings          int            `json:"warnings"`
}

// Summary is the rollup over the (windowed) ledger.
type Summary struct {
	Runs                 int            `json:"runs"`
	TotalTokens          int            `json:"total_tokens"`
	AvgDurationMs        float64        `json:"avg_duration_ms"`
	TotalDocsUpdated     int            `json:"total_docs_updated"`
	TotalSectionsUpdated int            `json:"total_sections_updated"`
	TotalRepairs         int            `json:"total_repairs"`
	TotalFallbacks       int            `json:"total_fallbacks"`
	TracksUsed           map[string]int `json:"tracks_used"`
	AvgFreshness         float64        `json:"avg_freshness"`
	FirstRun             string         `json:"first_run"`
	LastRun              string         `json:"last_run"`
}

// ledgerPath returns <repoRoot>/.glassmarble/runs/runs.jsonl.
func ledgerPath(repoRoot string) string {
	return filepath.Join(repoRoot, ledgerDirName, ledgerRunsDir, ledgerFileName)
}

// Append appends one JSON line to .glassmarble/runs/runs.jsonl, creating
// parent directories as needed. A zero Timestamp defaults to
// time.Now().UTC(). The fsync is best-effort (a sync failure never fails
// the run); write/close errors are returned.
func Append(repoRoot string, rec RunRecord) error {
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, ledgerDirName, ledgerRunsDir), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(ledgerPath(repoRoot), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	encErr := json.NewEncoder(f).Encode(rec)
	_ = f.Sync()
	if closeErr := f.Close(); encErr == nil {
		return closeErr
	} else {
		return encErr
	}
}

// Summarize rolls up the ledger. lastN <= 0 means all runs; otherwise only
// the newest lastN valid records are included. Malformed lines are skipped
// silently. A missing ledger (or repo dir) yields an empty Summary and nil
// error. FirstRun/LastRun are RFC 3339 UTC timestamps of the windowed
// records (empty when there are no runs).
func Summarize(repoRoot string, lastN int) (Summary, error) {
	sum := Summary{TracksUsed: map[string]int{}}
	data, err := os.ReadFile(ledgerPath(repoRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return sum, nil
		}
		return sum, err
	}
	var recs []RunRecord
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec RunRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		recs = append(recs, rec)
	}
	if lastN > 0 && len(recs) > lastN {
		recs = recs[len(recs)-lastN:]
	}
	if len(recs) == 0 {
		return sum, nil
	}
	var durSum, freshSum int64
	for _, r := range recs {
		sum.TotalTokens += r.TokensUsed
		sum.TotalDocsUpdated += r.DocsUpdated
		sum.TotalSectionsUpdated += r.SectionsUpdated
		sum.TotalRepairs += r.Repairs
		sum.TotalFallbacks += r.Fallbacks
		durSum += r.DurationMs
		freshSum += int64(r.GlobalFreshness)
		for k, v := range r.TracksUsed {
			sum.TracksUsed[k] += v
		}
	}
	sum.Runs = len(recs)
	sum.AvgDurationMs = float64(durSum) / float64(len(recs))
	sum.AvgFreshness = float64(freshSum) / float64(len(recs))
	sum.FirstRun = recs[0].Timestamp.UTC().Format(time.RFC3339)
	sum.LastRun = recs[len(recs)-1].Timestamp.UTC().Format(time.RFC3339)
	return sum, nil
}

// Prune keeps only the newest keepN raw lines in the ledger, preventing
// unbounded growth. keepN <= 0 is a no-op; a missing ledger is a no-op;
// both return nil.
func Prune(repoRoot string, keepN int) error {
	if keepN <= 0 {
		return nil
	}
	path := ledgerPath(repoRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(data), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) <= keepN {
		return nil
	}
	lines = lines[len(lines)-keepN:]
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
