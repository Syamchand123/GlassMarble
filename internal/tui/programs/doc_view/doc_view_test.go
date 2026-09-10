package doc_view

import (
	"strings"
	"testing"
)

func TestFuzzyScoreRanking(t *testing.T) {
	exact := fuzzyScore("auth", "Authentication module")
	scattered := fuzzyScore("auth", "a-x-u-t-h scattered")
	if exact <= 0 {
		t.Fatalf("expected positive score for subsequence match, got %v", exact)
	}
	if exact <= scattered {
		t.Errorf("expected exact-substring %v to outrank scattered %v", exact, scattered)
	}
	if s := fuzzyScore("auth", "xyz"); s != 0 {
		t.Errorf("expected 0 for non-subsequence, got %v", s)
	}
	if s := fuzzyScore("", "anything"); s != 0 {
		t.Errorf("expected 0 for empty query, got %v", s)
	}
	// Consecutive runs weight: "abc" in "abc..." beats "abc" in "a-b-c".
	contig := fuzzyScore("abc", "abcdef")
	gapped := fuzzyScore("abc", "a-b-c-d")
	if contig <= gapped {
		t.Errorf("expected contiguous %v to outrank gapped %v", contig, gapped)
	}
}

func TestRankMatchesTopMatch(t *testing.T) {
	symbols := []symbolLink{
		{Display: "ValidatePKCEToken", File: "internal/auth/pkce.go", Line: 47},
		{Display: "RenderDiagram", File: "internal/viz/diagram.go", Line: 10},
	}
	lines := []string{
		"# Auth Guide",
		"ValidatePKCEToken validates an OAuth2 PKCE exchange.",
		"Unrelated billing paragraph here.",
	}

	matches := rankMatches("pkce", symbols, lines)
	if len(matches) == 0 {
		t.Fatal("expected matches for pkce")
	}
	if matches[0].Display != "ValidatePKCEToken" {
		t.Errorf("expected TOP match ValidatePKCEToken, got %q", matches[0].Display)
	}
	// Symbols rank above plain content lines via the symbol bonus.
	if matches[0].Kind != "symbol" {
		t.Errorf("expected TOP match kind symbol, got %q", matches[0].Kind)
	}

	// `/` filter semantics: non-matching query yields no candidates.
	if got := rankMatches("zzz-no-such-thing", symbols, lines); len(got) != 0 {
		t.Errorf("expected 0 matches for nonsense query, got %d", len(got))
	}

	// Empty query returns the symbol list in order (TOP = first symbol).
	empty := rankMatches("", symbols, lines)
	if len(empty) != len(symbols) {
		t.Errorf("expected %d matches for empty query, got %d", len(symbols), len(empty))
	}
}

func TestRefilterSelectionResets(t *testing.T) {
	cfg := Config{Title: "T", TargetPath: "docs/t.md", Content: "line one\nline two auth\n"}
	m := newModel(cfg)
	m.refilter("auth")
	if len(m.matches) == 0 {
		t.Fatal("expected matches after refilter")
	}
	if m.selectedIdx != 0 {
		t.Errorf("expected selection reset to TOP match, got %d", m.selectedIdx)
	}
	m.selectedIdx = len(m.matches) - 1
	m.refilter("line")
	if m.selectedIdx != 0 {
		t.Errorf("expected selection reset on new query, got %d", m.selectedIdx)
	}
}

func TestRenderMatchesSelected(t *testing.T) {
	matches := []searchMatch{
		{Kind: "symbol", Display: "Alpha", Score: 2},
		{Kind: "line", Display: "Beta", Score: 1},
	}
	out := renderMatches(matches, 1)
	if !strings.Contains(out, "Beta") || !strings.Contains(out, "Alpha") {
		t.Errorf("expected both rows rendered, got %q", out)
	}
}
