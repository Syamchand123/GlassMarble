package verifier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

func proseHasRule(vs []ProseViolation, rule string) bool {
	for _, v := range vs {
		if v.Rule == rule {
			return true
		}
	}
	return false
}

func proseCountRule(vs []ProseViolation, rule string) int {
	n := 0
	for _, v := range vs {
		if v.Rule == rule {
			n++
		}
	}
	return n
}

func proseCheck(t *testing.T, content string, style config.StyleSpec, strict bool) ProseReport {
	t.Helper()
	return CheckProse(content, style, strict)
}

// ── Rule 1: sentence length ──

func TestProseSentenceLengthFail(t *testing.T) {
	content := "one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen seventeen eighteen nineteen twenty twenty-one twenty-two twenty-three twenty-four twenty-five twenty-six twenty-seven twenty-eight twenty-nine thirty."
	report := proseCheck(t, content, config.StyleSpec{}, false)
	if !proseHasRule(report.Violations, "sentence-length") {
		t.Errorf("expected sentence-length violation, got %+v", report.Violations)
	}
}

func TestProseSentenceLengthPass(t *testing.T) {
	report := proseCheck(t, "Cats sit here.", config.StyleSpec{}, false)
	if proseHasRule(report.Violations, "sentence-length") {
		t.Errorf("unexpected sentence-length violation: %+v", report.Violations)
	}
}

func TestProseSentenceLengthBoundaries(t *testing.T) {
	// "v1.2.0" must not split sentences on internal dots.
	report := proseCheck(t, "Release v1.2.0 is out.", config.StyleSpec{}, false)
	if proseHasRule(report.Violations, "sentence-length") {
		t.Errorf("unexpected sentence-length violation: %+v", report.Violations)
	}
}

// ── Rule 2: passive voice (heuristic) ──

func TestProsePassiveVoiceFail(t *testing.T) {
	report := proseCheck(t, "The file was created by the tool.", config.StyleSpec{}, false)
	if !proseHasRule(report.Violations, "passive-voice") {
		t.Errorf("expected passive-voice violation, got %+v", report.Violations)
	}
	for _, v := range report.Violations {
		if v.Rule == "passive-voice" && v.Severity != "warn" {
			t.Errorf("passive-voice must be warn, got %q", v.Severity)
		}
	}
}

func TestProsePassiveVoicePass(t *testing.T) {
	report := proseCheck(t, "The tool creates the file.", config.StyleSpec{}, false)
	if proseHasRule(report.Violations, "passive-voice") {
		t.Errorf("unexpected passive-voice violation: %+v", report.Violations)
	}
}

// ── Rule 3: jargon blacklist ──

func TestProseJargonStrictError(t *testing.T) {
	style := config.StyleSpec{JargonBlacklist: []string{"leverage"}}
	report := proseCheck(t, "We leverage the cache.", style, true)
	if !proseHasRule(report.Violations, "jargon") {
		t.Fatalf("expected jargon violation, got %+v", report.Violations)
	}
	for _, v := range report.Violations {
		if v.Rule == "jargon" && v.Severity != "error" {
			t.Errorf("jargon in strict mode must be error, got %q", v.Severity)
		}
	}
}

func TestProseJargonNonStrictWarn(t *testing.T) {
	style := config.StyleSpec{JargonBlacklist: []string{"leverage"}}
	report := proseCheck(t, "We leverage the cache.", style, false)
	if !proseHasRule(report.Violations, "jargon") {
		t.Fatalf("expected jargon violation, got %+v", report.Violations)
	}
	for _, v := range report.Violations {
		if v.Rule == "jargon" && v.Severity != "warn" {
			t.Errorf("jargon in non-strict mode must be warn, got %q", v.Severity)
		}
	}
}

func TestProseJargonEmptyBlacklistDisabled(t *testing.T) {
	report := proseCheck(t, "We leverage and utilize the cache.", config.StyleSpec{}, true)
	if proseHasRule(report.Violations, "jargon") {
		t.Errorf("empty blacklist must disable jargon rule, got %+v", report.Violations)
	}
}

// ── Rule 4: typos + doubled words ──

func TestProseTypoFail(t *testing.T) {
	report := proseCheck(t, "Fix teh bug.", config.StyleSpec{}, false)
	if !proseHasRule(report.Violations, "typo") {
		t.Errorf("expected typo violation, got %+v", report.Violations)
	}
	for _, v := range report.Violations {
		if v.Rule == "typo" && v.Severity != "error" {
			t.Errorf("typo must be error, got %q", v.Severity)
		}
	}
}

func TestProseTypoPass(t *testing.T) {
	report := proseCheck(t, "Fix the bug.", config.StyleSpec{}, false)
	if proseHasRule(report.Violations, "typo") {
		t.Errorf("unexpected typo violation: %+v", report.Violations)
	}
}

func TestProseDoubledWordFail(t *testing.T) {
	report := proseCheck(t, "Fix the the bug.", config.StyleSpec{}, false)
	if !proseHasRule(report.Violations, "doubled-word") {
		t.Errorf("expected doubled-word violation, got %+v", report.Violations)
	}
}

func TestProseDoubledWordCaseInsensitive(t *testing.T) {
	report := proseCheck(t, "Fix The the bug.", config.StyleSpec{}, false)
	if !proseHasRule(report.Violations, "doubled-word") {
		t.Errorf("expected case-insensitive doubled-word violation, got %+v", report.Violations)
	}
}

// ── Rule 5: structural ──

func TestProseHeadingSkipFail(t *testing.T) {
	report := proseCheck(t, "# Title\n\n### Deep\n", config.StyleSpec{}, false)
	if !proseHasRule(report.Violations, "heading-skip") {
		t.Errorf("expected heading-skip violation, got %+v", report.Violations)
	}
	for _, v := range report.Violations {
		if v.Rule == "heading-skip" && v.Severity != "error" {
			t.Errorf("heading-skip must be error, got %q", v.Severity)
		}
	}
}

func TestProseHeadingSkipPass(t *testing.T) {
	report := proseCheck(t, "# Title\n\n## Section\n\n### Deep\n", config.StyleSpec{}, false)
	if proseHasRule(report.Violations, "heading-skip") {
		t.Errorf("unexpected heading-skip violation: %+v", report.Violations)
	}
}

func TestProseMultipleH1Fail(t *testing.T) {
	report := proseCheck(t, "# One\n\n# Two\n", config.StyleSpec{}, false)
	if !proseHasRule(report.Violations, "multiple-h1") {
		t.Errorf("expected multiple-h1 violation, got %+v", report.Violations)
	}
}

func TestProseTrailingWhitespaceFail(t *testing.T) {
	report := proseCheck(t, "hello   \nworld\n", config.StyleSpec{}, false)
	if !proseHasRule(report.Violations, "trailing-whitespace") {
		t.Errorf("expected trailing-whitespace violation, got %+v", report.Violations)
	}
}

func TestProseTrailingWhitespacePass(t *testing.T) {
	report := proseCheck(t, "hello\nworld\n", config.StyleSpec{}, false)
	if proseHasRule(report.Violations, "trailing-whitespace") {
		t.Errorf("unexpected trailing-whitespace violation: %+v", report.Violations)
	}
}

func TestProseFenceLanguageFail(t *testing.T) {
	report := proseCheck(t, "```\ncode\n```\n", config.StyleSpec{}, false)
	if !proseHasRule(report.Violations, "fence-language") {
		t.Errorf("expected fence-language violation, got %+v", report.Violations)
	}
}

func TestProseFenceLanguagePass(t *testing.T) {
	report := proseCheck(t, "```go\ncode\n```\n", config.StyleSpec{}, false)
	if proseHasRule(report.Violations, "fence-language") {
		t.Errorf("unexpected fence-language violation: %+v", report.Violations)
	}
}

func TestProseListMarkerFail(t *testing.T) {
	report := proseCheck(t, "- a\n- b\n* c\n", config.StyleSpec{}, false)
	if !proseHasRule(report.Violations, "list-marker") {
		t.Errorf("expected list-marker violation, got %+v", report.Violations)
	}
}

func TestProseListMarkerPass(t *testing.T) {
	report := proseCheck(t, "- a\n- b\n- c\n", config.StyleSpec{}, false)
	if proseHasRule(report.Violations, "list-marker") {
		t.Errorf("unexpected list-marker violation: %+v", report.Violations)
	}
}

func TestProseTabIndentationFail(t *testing.T) {
	report := proseCheck(t, "\tindented line\n", config.StyleSpec{}, false)
	if !proseHasRule(report.Violations, "tab-indentation") {
		t.Errorf("expected tab-indentation violation, got %+v", report.Violations)
	}
	for _, v := range report.Violations {
		if v.Rule == "tab-indentation" && v.Severity != "error" {
			t.Errorf("tab-indentation must be error, got %q", v.Severity)
		}
	}
}

func TestProseTabIndentationPass(t *testing.T) {
	report := proseCheck(t, "  indented line\n", config.StyleSpec{}, false)
	if proseHasRule(report.Violations, "tab-indentation") {
		t.Errorf("unexpected tab-indentation violation: %+v", report.Violations)
	}
}

// ── Exclusions: code spans, fences, URLs, comments ──

func TestProseInlineCodeSpanExcluded(t *testing.T) {
	withCode := proseCheck(t, "Use `teh` here.", config.StyleSpec{}, false)
	if proseHasRule(withCode.Violations, "typo") {
		t.Errorf("typo inside inline code must be ignored, got %+v", withCode.Violations)
	}
	withoutCode := proseCheck(t, "Use teh here.", config.StyleSpec{}, false)
	if !proseHasRule(withoutCode.Violations, "typo") {
		t.Errorf("typo in prose must fire, got %+v", withoutCode.Violations)
	}
}

func TestProseFencedBlockExcluded(t *testing.T) {
	report := proseCheck(t, "```go\nteh\n```\n\nClean text.\n", config.StyleSpec{}, false)
	if proseHasRule(report.Violations, "typo") {
		t.Errorf("typo inside fenced block must be ignored, got %+v", report.Violations)
	}
}

func TestProseURLExcluded(t *testing.T) {
	report := proseCheck(t, "See https://example.com/teh for info.", config.StyleSpec{}, false)
	if proseHasRule(report.Violations, "typo") {
		t.Errorf("typo inside URL must be ignored, got %+v", report.Violations)
	}
}

func TestProseHTMLCommentExcluded(t *testing.T) {
	report := proseCheck(t, "Hello <!-- teh --> world.", config.StyleSpec{}, false)
	if proseHasRule(report.Violations, "typo") {
		t.Errorf("typo inside HTML comment must be ignored, got %+v", report.Violations)
	}
}

// ── CRLF input ──

func TestProseCRLFInput(t *testing.T) {
	crlf := "Cats sit here.\r\nDogs run fast.\r\n"
	lf := "Cats sit here.\nDogs run fast.\n"
	rCRLF := proseCheck(t, crlf, config.StyleSpec{}, false)
	rLF := proseCheck(t, lf, config.StyleSpec{}, false)
	if len(rCRLF.Violations) != len(rLF.Violations) {
		t.Errorf("CRLF handling differs: CRLF=%+v LF=%+v", rCRLF.Violations, rLF.Violations)
	}
	report := proseCheck(t, "Fix teh bug.\r\n", config.StyleSpec{}, false)
	if !proseHasRule(report.Violations, "typo") {
		t.Errorf("typo must be found in CRLF input, got %+v", report.Violations)
	}
}

// ── Rule 6: readability is informational only ──

func TestProseReadabilityInformational(t *testing.T) {
	// Two 22-word sentences: average (22) over the threshold but no single
	// sentence over the 25-word limit, so readability must fold into Score
	// without emitting violations.
	s22 := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau upsilon phi chi"
	content := s22 + ". " + s22 + "."
	report := proseCheck(t, content, config.StyleSpec{}, false)
	if proseHasRule(report.Violations, "sentence-length") {
		t.Errorf("22-word sentences must not trip sentence-length: %+v", report.Violations)
	}
	if len(report.Violations) != 0 {
		t.Errorf("readability must emit no violations, got %+v", report.Violations)
	}
	if report.Score >= 100 {
		t.Errorf("readability penalty must lower Score, got %v", report.Score)
	}
}

// ── Score formula ──

func TestProseScoreFormula(t *testing.T) {
	// "teh" (typo, error = -5) + trailing whitespace (warn = -2),
	// single short word so readability penalties are zero.
	report := proseCheck(t, "teh   \n", config.StyleSpec{}, false)
	if got := report.Score; got != 93 {
		t.Errorf("expected Score 93 (100-5-2), got %v (violations %+v)", got, report.Violations)
	}
}

func TestProseScoreFloorZero(t *testing.T) {
	content := strings.Repeat("teh ", 40) + "\n"
	report := proseCheck(t, content, config.StyleSpec{}, false)
	if report.Score != 0 {
		t.Errorf("expected Score floor 0, got %v", report.Score)
	}
}

// ── CheckProseGate pass/fail contract ──

func TestProseGateNonStrictNeverFails(t *testing.T) {
	// Typo is error-severity, yet non-strict must still pass while reporting.
	pass, failures := CheckProseGate("Fix teh bug.", config.StyleSpec{}, false)
	if !pass {
		t.Errorf("non-strict must never fail, got failures %v", failures)
	}
	if len(failures) == 0 {
		t.Error("non-strict must still return failure strings for reporting")
	}
}

func TestProseGateStrictFailsOnError(t *testing.T) {
	pass, failures := CheckProseGate("Fix teh bug.", config.StyleSpec{}, true)
	if pass {
		t.Error("strict must fail on error-severity typo")
	}
	if len(failures) == 0 {
		t.Error("strict failure must return failure strings")
	}
}

func TestProseGateStrictPassesOnWarnOnly(t *testing.T) {
	// Passive voice is warn-only: strict still passes.
	pass, _ := CheckProseGate("The file was created by the tool.", config.StyleSpec{}, true)
	if !pass {
		t.Error("strict must pass when only warn-severity violations exist")
	}
}

func TestProseGateCleanPasses(t *testing.T) {
	pass, failures := CheckProseGate("Cats sit here.", config.StyleSpec{}, true)
	if !pass {
		t.Errorf("clean content must pass strict, got %v", failures)
	}
	if len(failures) != 0 {
		t.Errorf("clean content must report no failures, got %v", failures)
	}
}

// ── Gap C1: terminology-allowlist + learned vocabulary ──

func writeVocabRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"authz/tokenbucket.go": "package authz\n\nfunc TokenBucket() {}\n\ntype Ledger struct{}\n\nfunc Foo() {}\n",
		"ledger/store.go":      "package ledger\n\nfunc Reconcile() {}\n\nvar ExportedVar = 1\n",
		"README.md":            "# hi\n", // non-Go files never contribute idents
	}
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func TestLearnVocabulary_CollectsExportedIdents(t *testing.T) {
	vocab := LearnVocabulary(writeVocabRepo(t), 100)
	for _, want := range []string{"tokenbucket", "ledger", "reconcile", "exportedvar", "authz", "store"} {
		if !vocab[want] {
			t.Errorf("expected vocab to contain %q, got %v", want, vocab)
		}
	}
	// Min length 4: "Foo" must not be collected.
	if vocab["foo"] {
		t.Errorf("short ident %q must be excluded (min len 4)", "foo")
	}
	if len(vocab) == 0 {
		t.Error("vocab must not be empty for a repo with exported idents")
	}
}

func TestLearnVocabulary_DeterministicCap(t *testing.T) {
	root := writeVocabRepo(t)
	a := LearnVocabulary(root, 3)
	b := LearnVocabulary(root, 3)
	if len(a) != 3 {
		t.Fatalf("expected capped vocab of 3, got %d (%v)", len(a), a)
	}
	for term := range a {
		if !b[term] {
			t.Errorf("cap is nondeterministic: %q in first but not second", term)
		}
	}
	// Empty root → empty vocab, never nil-map panic.
	if got := LearnVocabulary("", 100); len(got) != 0 {
		t.Errorf("empty repoRoot must yield empty vocab, got %v", got)
	}
}

func TestProseGateWithVocab_SkipsVocabTypo(t *testing.T) {
	// "teh" is in the fixed typo list: strict fails without vocab...
	if pass, _ := CheckProseGateWithVocab("Fix teh bug.", config.StyleSpec{}, true, nil); pass {
		t.Error("nil vocab must behave like CheckProseGate (strict fails on typo)")
	}
	// ...but passes when the project learned it as a term.
	vocab := map[string]bool{"teh": true}
	if pass, failures := CheckProseGateWithVocab("Fix teh bug.", config.StyleSpec{}, true, vocab); !pass {
		t.Errorf("vocab term must skip the typo rule, got %v", failures)
	}
}

func TestProseGateWithVocab_StillFlagsRealTypo(t *testing.T) {
	vocab := map[string]bool{"teh": true}
	pass, failures := CheckProseGateWithVocab("Fix teh and recieve bugs.", config.StyleSpec{}, true, vocab)
	if pass {
		t.Error("vocab must not excuse real typos (recieve must still fail strict)")
	}
	if len(failures) == 0 {
		t.Error("expected failure strings for the unlisted typo")
	}
	// Non-strict still reports the real typo while passing.
	if pass, failures := CheckProseGateWithVocab("Fix teh and recieve bugs.", config.StyleSpec{}, false, vocab); !pass || len(failures) == 0 {
		t.Errorf("non-strict must pass yet report, got pass=%v failures=%v", pass, failures)
	}
}

func TestProseGateWithVocab_DoubledWordUnaffected(t *testing.T) {
	// Doubled-word detection ignores vocab: a repeated project term is
	// still a doubled word.
	vocab := map[string]bool{"ledger": true}
	if pass, _ := CheckProseGateWithVocab("Update ledger ledger now.", config.StyleSpec{}, true, vocab); pass {
		t.Error("doubled project term must still fail strict (doubled-word is vocab-exempt)")
	}
}

func TestProseGateWithVocab_MatchesGateOnNilVocab(t *testing.T) {
	contents := []string{"Cats sit here.", "Fix teh bug.", "The file was created by the tool."}
	for _, c := range contents {
		for _, strict := range []bool{false, true} {
			wantPass, wantFails := CheckProseGate(c, config.StyleSpec{}, strict)
			gotPass, gotFails := CheckProseGateWithVocab(c, config.StyleSpec{}, strict, nil)
			if wantPass != gotPass || len(wantFails) != len(gotFails) {
				t.Errorf("nil vocab diverges for %q strict=%v: gate=(%v,%d) vocab=(%v,%d)",
					c, strict, wantPass, len(wantFails), gotPass, len(gotFails))
			}
		}
	}
}
