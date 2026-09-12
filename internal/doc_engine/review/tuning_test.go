package review

import (
	"os"
	"strings"
	"testing"
)

func TestSuggestTuningEmpty(t *testing.T) {
	got, err := SuggestTuning(t.TempDir())
	if err != nil {
		t.Fatalf("SuggestTuning on missing queue: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("missing queue should yield empty non-nil slice, got %v", got)
	}
}

func TestSuggestTuningAggregates(t *testing.T) {
	root := t.TempDir()
	id1, err := Queue(root, ReviewItem{Kind: "conflict", DocPath: "docs/a.md", SectionID: "s", Summary: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := Queue(root, ReviewItem{Kind: "conflict", DocPath: "docs/a.md", SectionID: "s", Summary: "c2"})
	if err != nil {
		t.Fatal(err)
	}
	id3, err := Queue(root, ReviewItem{Kind: "snippet-fix", DocPath: "docs/b.md", SectionID: "s", Summary: "f"})
	if err != nil {
		t.Fatal(err)
	}
	// Pending items must not contribute.
	if _, err := Queue(root, ReviewItem{Kind: "conflict", DocPath: "docs/z.md", SectionID: "s", Summary: "pending"}); err != nil {
		t.Fatal(err)
	}
	if err := Resolve(root, id1, false, "wrong callers"); err != nil {
		t.Fatal(err)
	}
	if err := Resolve(root, id2, false, "wrong callers"); err != nil {
		t.Fatal(err)
	}
	if err := Resolve(root, id3, false, "bad arity"); err != nil {
		t.Fatal(err)
	}
	if err := RecordRevert(root, "docs/c.md", "s", "human rewrote table"); err != nil {
		t.Fatal(err)
	}

	got, err := SuggestTuning(root)
	if err != nil {
		t.Fatalf("SuggestTuning failed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 suggestion groups, got %v", got)
	}
	// Sorted by count desc: the 2x conflict group first.
	if !strings.Contains(got[0].Evidence, "2x") || !strings.Contains(got[0].Evidence, "conflict") {
		t.Errorf("first suggestion should be the 2x conflict group, got %+v", got[0])
	}
	if got[0].Area != "merge-conflict prompts" {
		t.Errorf("conflict area = %q, want merge-conflict prompts", got[0].Area)
	}
	foundSnippet, foundRevert := false, false
	for _, s := range got[1:] {
		if s.Area == "snippet grounding" {
			foundSnippet = true
		}
		if s.Area == "doc corrections" {
			foundRevert = true
		}
	}
	if !foundSnippet {
		t.Errorf("missing snippet grounding suggestion: %+v", got)
	}
	if !foundRevert {
		t.Errorf("missing observed-revert doc corrections suggestion: %+v", got)
	}
}

func TestSuggestTuningMapping(t *testing.T) {
	root := t.TempDir()
	adrID, _ := Queue(root, ReviewItem{Kind: "adr-draft", Summary: "draft"})
	revID, _ := Queue(root, ReviewItem{Kind: "revert", Summary: "r"})
	if err := Resolve(root, adrID, false, "premature"); err != nil {
		t.Fatal(err)
	}
	if err := Resolve(root, revID, true, "style drift"); err != nil {
		t.Fatal(err)
	}
	got, err := SuggestTuning(root)
	if err != nil {
		t.Fatal(err)
	}
	byArea := map[string]bool{}
	for _, s := range got {
		byArea[s.Area] = true
		if strings.TrimSpace(s.Action) == "" || strings.TrimSpace(s.Evidence) == "" {
			t.Errorf("suggestion must carry evidence and action: %+v", s)
		}
	}
	if !byArea["adr triggers"] {
		t.Errorf("adr-draft rejection should suggest trigger tuning, got %v", got)
	}
	if !byArea["style/prompts"] {
		t.Errorf("resolved revert with reason should suggest style/prompt changes, got %v", got)
	}
}

func TestSuggestTuningCap(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 15; i++ {
		id, err := Queue(root, ReviewItem{Kind: "prose", Summary: "drift"})
		if err != nil {
			t.Fatal(err)
		}
		// Distinct reasons force distinct groups.
		if err := Resolve(root, id, false, strings.Repeat("r", i+1)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := SuggestTuning(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 {
		t.Errorf("suggestions must be capped at 10, got %d", len(got))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// TuningApply safe auto-fix tests (APPEND ONLY — above untouched)
// ────────────────────────────────────────────────────────────────────────────

func writeTuningDocsYAML(t *testing.T, root, body string) {
	t.Helper()
	gmDir := root + "/.glassmarble"
	if err := os.MkdirAll(gmDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gmDir+"/docs.yaml", []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestTuningApplyStyleJargon(t *testing.T) {
	root := t.TempDir()
	writeTuningDocsYAML(t, root, "version: 1\ndocs_dir: docs\n")
	s := TuningSuggestion{Area: "style/prompts", Evidence: `1x kind="revert" reason="overuses \"synergize\" in intro"`}
	if err := TuningApply(root, s); err != nil {
		t.Fatalf("TuningApply failed: %v", err)
	}
	data, _ := os.ReadFile(root + "/.glassmarble/docs.yaml")
	if !strings.Contains(string(data), "synergize") || !strings.Contains(string(data), "jargon_blacklist:") {
		t.Errorf("jargon term must be appended to style.jargon_blacklist:\n%s", data)
	}
	// Re-apply is a no-op success.
	if err := TuningApply(root, s); err != nil {
		t.Fatalf("TuningApply re-apply failed: %v", err)
	}
	data2, _ := os.ReadFile(root + "/.glassmarble/docs.yaml")
	if got := strings.Count(string(data2), "synergize"); got != 1 {
		t.Errorf("re-apply must be idempotent, got %d occurrences:\n%s", got, data2)
	}
}

func TestTuningApplyManualRequired(t *testing.T) {
	root := t.TempDir()
	writeTuningDocsYAML(t, root, "version: 1\ndocs_dir: docs\n")
	// Non-style area → manual.
	if err := TuningApply(root, TuningSuggestion{Area: "merge-conflict prompts", Evidence: `2x kind="conflict" reason="wrong callers"`}); err == nil || !strings.Contains(err.Error(), "manual apply required") {
		t.Errorf("non-style area must return manual-apply error, got %v", err)
	}
	// Style area without a quoted term → manual.
	if err := TuningApply(root, TuningSuggestion{Area: "style/prompts", Evidence: `1x kind="revert" reason="tone drift"`}); err == nil || !strings.Contains(err.Error(), "manual apply required") {
		t.Errorf("style suggestion without jargon term must return manual-apply error, got %v", err)
	}
	// Style suggestion with term but no docs.yaml → manual.
	empty := t.TempDir()
	if err := TuningApply(empty, TuningSuggestion{Area: "style/prompts", Evidence: `1x kind="revert" reason="overuses \"synergize\""`}); err == nil || !strings.Contains(err.Error(), "manual apply required") {
		t.Errorf("missing docs.yaml must return manual-apply error, got %v", err)
	}
}
