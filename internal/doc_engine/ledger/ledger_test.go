package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testRecords() []RunRecord {
	mk := func(min int, commit string, docs, sections, tokens int, dur int64, tracks map[string]int, repairs, fallbacks, fresh int) RunRecord {
		return RunRecord{
			Timestamp:         time.Date(2026, time.January, 2, 3, 4, min, 0, time.UTC),
			Commit:            commit,
			BranchPolicy:      "main-only",
			DocsUpdated:       docs,
			SectionsUpdated:   sections,
			SectionsProcessed: sections + 2,
			TokensUsed:        tokens,
			DurationMs:        dur,
			TracksUsed:        tracks,
			Repairs:           repairs,
			Fallbacks:         fallbacks,
			GlobalFreshness:   fresh,
		}
	}
	return []RunRecord{
		mk(5, "aaa", 2, 3, 1000, 2000, map[string]int{"llm": 1, "deterministic": 2}, 1, 0, 80),
		mk(6, "bbb", 0, 5, 2000, 4000, map[string]int{"llm": 2}, 0, 1, 90),
		mk(7, "ccc", 4, 1, 3000, 6000, map[string]int{"llm": 3, "review": 1}, 2, 0, 100),
	}
}

func appendAll(t *testing.T, root string, recs []RunRecord) {
	t.Helper()
	for _, r := range recs {
		if err := Append(root, r); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
}

func TestAppendSummarizeExactMath(t *testing.T) {
	root := t.TempDir()
	recs := testRecords()
	appendAll(t, root, recs)

	// One JSON object per line.
	data, err := os.ReadFile(filepath.Join(root, ".glassmarble", "runs", "runs.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if n := len(strings.Split(strings.TrimSpace(string(data)), "\n")); n != 3 {
		t.Fatalf("ledger lines = %d, want 3", n)
	}

	sum, err := Summarize(root, 0)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.Runs != 3 {
		t.Fatalf("Runs = %d, want 3", sum.Runs)
	}
	if sum.TotalTokens != 6000 {
		t.Fatalf("TotalTokens = %d, want 6000", sum.TotalTokens)
	}
	if sum.AvgDurationMs != 4000 {
		t.Fatalf("AvgDurationMs = %v, want 4000", sum.AvgDurationMs)
	}
	if sum.TotalDocsUpdated != 6 {
		t.Fatalf("TotalDocsUpdated = %d, want 6", sum.TotalDocsUpdated)
	}
	if sum.TotalSectionsUpdated != 9 {
		t.Fatalf("TotalSectionsUpdated = %d, want 9", sum.TotalSectionsUpdated)
	}
	if sum.TotalRepairs != 3 {
		t.Fatalf("TotalRepairs = %d, want 3", sum.TotalRepairs)
	}
	if sum.TotalFallbacks != 1 {
		t.Fatalf("TotalFallbacks = %d, want 1", sum.TotalFallbacks)
	}
	wantTracks := map[string]int{"llm": 6, "deterministic": 2, "review": 1}
	if len(sum.TracksUsed) != len(wantTracks) {
		t.Fatalf("TracksUsed = %v, want %v", sum.TracksUsed, wantTracks)
	}
	for k, v := range wantTracks {
		if sum.TracksUsed[k] != v {
			t.Fatalf("TracksUsed = %v, want %v", sum.TracksUsed, wantTracks)
		}
	}
	if sum.AvgFreshness != 90 {
		t.Fatalf("AvgFreshness = %v, want 90", sum.AvgFreshness)
	}
	if sum.FirstRun != "2026-01-02T03:04:05Z" {
		t.Fatalf("FirstRun = %q", sum.FirstRun)
	}
	if sum.LastRun != "2026-01-02T03:04:07Z" {
		t.Fatalf("LastRun = %q", sum.LastRun)
	}
}

func TestSummarizeLastN(t *testing.T) {
	root := t.TempDir()
	appendAll(t, root, testRecords())

	sum, err := Summarize(root, 2)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.Runs != 2 {
		t.Fatalf("Runs = %d, want 2", sum.Runs)
	}
	if sum.TotalTokens != 5000 {
		t.Fatalf("TotalTokens = %d, want 5000", sum.TotalTokens)
	}
	if sum.FirstRun != "2026-01-02T03:04:06Z" || sum.LastRun != "2026-01-02T03:04:07Z" {
		t.Fatalf("window = %q..%q", sum.FirstRun, sum.LastRun)
	}

	if sum, err := Summarize(root, -1); err != nil || sum.Runs != 3 {
		t.Fatalf("lastN<=0 = all: runs=%d err=%v", sum.Runs, err)
	}
}

func TestMalformedLineTolerated(t *testing.T) {
	root := t.TempDir()
	appendAll(t, root, testRecords())

	f, err := os.OpenFile(filepath.Join(root, ".glassmarble", "runs", "runs.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("this is not json\n{\"tokens_used\": oops}\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	sum, err := Summarize(root, 0)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.Runs != 3 || sum.TotalTokens != 6000 {
		t.Fatalf("malformed lines must be skipped: %+v", sum)
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	root := t.TempDir()
	recs := testRecords()
	extra := []RunRecord{
		{Timestamp: time.Date(2026, time.January, 2, 3, 4, 8, 0, time.UTC), Commit: "ddd", TokensUsed: 4000, DurationMs: 8000},
		{Timestamp: time.Date(2026, time.January, 2, 3, 4, 9, 0, time.UTC), Commit: "eee", TokensUsed: 5000, DurationMs: 10000},
	}
	appendAll(t, root, append(recs, extra...))

	if err := Prune(root, 2); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	sum, err := Summarize(root, 0)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.Runs != 2 {
		t.Fatalf("Runs = %d, want 2", sum.Runs)
	}
	if sum.TotalTokens != 9000 {
		t.Fatalf("TotalTokens = %d, want 9000 (newest two)", sum.TotalTokens)
	}
	if sum.FirstRun != "2026-01-02T03:04:08Z" || sum.LastRun != "2026-01-02T03:04:09Z" {
		t.Fatalf("window = %q..%q, want newest two", sum.FirstRun, sum.LastRun)
	}

	if err := Prune(root, 0); err != nil {
		t.Fatalf("Prune keepN<=0 must be no-op nil, got %v", err)
	}
	if sum2, _ := Summarize(root, 0); sum2.Runs != 2 {
		t.Fatalf("Prune(0) changed ledger: %+v", sum2)
	}
	if err := Prune(filepath.Join(root, "missing"), 5); err != nil {
		t.Fatalf("Prune missing ledger must be nil, got %v", err)
	}
}

func TestSummarizeMissingDir(t *testing.T) {
	sum, err := Summarize(filepath.Join(t.TempDir(), "does-not-exist"), 0)
	if err != nil {
		t.Fatalf("Summarize missing dir: %v", err)
	}
	if sum.Runs != 0 || sum.TotalTokens != 0 || sum.AvgDurationMs != 0 ||
		sum.AvgFreshness != 0 || sum.FirstRun != "" || sum.LastRun != "" {
		t.Fatalf("want empty Summary, got %+v", sum)
	}
	if sum.TracksUsed == nil {
		t.Fatal("TracksUsed must be non-nil")
	}
}

func TestAppendDefaultsTimestamp(t *testing.T) {
	root := t.TempDir()
	if err := Append(root, RunRecord{Commit: "zzz"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	sum, err := Summarize(root, 0)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.Runs != 1 || sum.LastRun == "" {
		t.Fatalf("zero timestamp must default: %+v", sum)
	}
}

func TestEvalRecordKindBackCompat(t *testing.T) {
	root := t.TempDir()
	// Pre-D1 lines carry no kind ("" = run); eval lines carry Kind:"eval".
	if err := Append(root, RunRecord{Commit: "run1", TokensUsed: 100}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := Append(root, RunRecord{Commit: "", Kind: "eval", EvalScore: 0.85, EvalSamples: 4}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".glassmarble", "runs", "runs.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("ledger lines = %d, want 2", len(lines))
	}
	// Old readers ignore the additive fields; new readers see them.
	var evalRec RunRecord
	if err := json.Unmarshal([]byte(lines[1]), &evalRec); err != nil {
		t.Fatalf("unmarshal eval record: %v", err)
	}
	if evalRec.Kind != "eval" || evalRec.EvalScore != 0.85 || evalRec.EvalSamples != 4 {
		t.Fatalf("eval fields lost: %+v", evalRec)
	}
	var runRec RunRecord
	if err := json.Unmarshal([]byte(lines[0]), &runRec); err != nil {
		t.Fatalf("unmarshal run record: %v", err)
	}
	if runRec.Kind != "" {
		t.Fatalf("run Kind must default to %q, got %q", "", runRec.Kind)
	}
	// Summaries roll both kinds up without breaking existing math.
	sum, err := Summarize(root, 0)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.Runs != 2 || sum.TotalTokens != 100 {
		t.Fatalf("summary must include both records: %+v", sum)
	}
}

func TestCheckAlerts(t *testing.T) {
	sum := Summary{TotalTokens: 5000, AvgFreshness: 72.5, TotalFallbacks: 3}

	// Zero thresholds = all disabled → all clear.
	if got := CheckAlerts(sum, AlertThresholds{}); len(got) != 0 {
		t.Fatalf("zero thresholds must be all clear, got %v", got)
	}
	// Generous thresholds → all clear.
	if got := CheckAlerts(sum, AlertThresholds{MaxTokensPerRun: 9000, MinFreshness: 50, MaxFallbacks: 5}); len(got) != 0 {
		t.Fatalf("generous thresholds must be all clear, got %v", got)
	}
	// Every breach fires with a human string naming the dimension.
	got := CheckAlerts(sum, AlertThresholds{MaxTokensPerRun: 1000, MinFreshness: 90, MaxFallbacks: 1})
	if len(got) != 3 {
		t.Fatalf("want 3 alerts, got %v", got)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"tokens 5000", "freshness 72.5", "fallbacks 3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("alert strings must name values, missing %q in %v", want, got)
		}
	}
	// Boundary values do not fire (strict inequalities).
	if got := CheckAlerts(sum, AlertThresholds{MaxTokensPerRun: 5000, MaxFallbacks: 3}); len(got) != 0 {
		t.Fatalf("boundary values must not fire, got %v", got)
	}
}
