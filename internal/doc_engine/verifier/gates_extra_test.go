// Package verifier — gates_extra_test.go
//
// Whitebox coverage for the Quality Firewall: gate ordering, Gate 4
// terminal semantics (no retry, no semantic-noop bypass), Gate 5 advisory
// semantics, CheckProseGateWithVocab nil-vocab parity, RegenerateTOC
// idempotence, and the doc-lint EvaluateAsserts rule matrix.
package verifier

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/patcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ────────────────────────────────────────────────────────────────────────────
// Gate ordering: the first failing gate wins
// ────────────────────────────────────────────────────────────────────────────

func TestExtraGateOrdering(t *testing.T) {
	akg := &mockAKG{known: map[string]bool{}}
	for _, tc := range []struct {
		name string
		old  string
		new  string
		want int
	}{
		{
			name: "gate1 beats gate4",
			old:  "",
			new:  "```go\nfunc Foo() {}\nAPI key leaked: api_key = supersecretvalue123",
			want: 1,
		},
		{
			name: "gate2 beats gate3",
			old:  "",
			new:  "```mermaid\nbloblogram\n  A --> B\n```\nUse `Hallucinated` here.",
			want: 2,
		},
		{
			name: "gate3 beats gate4",
			old:  "",
			new:  "Use `Hallucinated` with api_key = supersecretvalue123",
			want: 3,
		},
		{
			name: "gate4 beats gate5 bypass",
			old:  "api_key = supersecretvalue123",
			new:  "api_key = supersecretvalue123",
			want: 4,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := RunGates(tc.old, tc.new, akg)
			assert.False(t, res.Pass)
			assert.Equal(t, tc.want, res.FailedGate)
			require.NotNil(t, res.Error)
			assert.Equal(t, tc.want, res.Error.Gate)
		})
	}
}

// TestExtraGate4SkipsNothing documents Gate 4's terminal semantics at the
// firewall level: secrets fail even when Gate 3 is skipped (nil index) and
// even when the change is churn-only (Gate 5 never waves a secret through).
// The no-retry half of the contract lives in the orchestrator (a Gate 4
// failure aborts the section instead of entering the repair ladder).
func TestExtraGate4Terminal(t *testing.T) {
	secret := "Deploy with api_key = supersecretvalue123 in env."
	// Nil AKG skips Gate 3 but Gate 4 still fires.
	res := RunGates("", secret, nil)
	assert.False(t, res.Pass)
	assert.Equal(t, 4, res.FailedGate)

	// Identical old/new (semantic no-op) with a secret still fails.
	res = RunGates(secret, secret, nil)
	assert.False(t, res.Pass, "Gate 4 must fire before the Gate 5 no-op check")
	assert.Equal(t, 4, res.FailedGate)
	assert.False(t, res.SemanticNoOp, "a failed run must not report SemanticNoOp")
}

// ────────────────────────────────────────────────────────────────────────────
// Gate 5 advisory semantics
// ────────────────────────────────────────────────────────────────────────────

func TestExtraGate5Advisory(t *testing.T) {
	akg := &mockAKG{known: map[string]bool{}}
	// Churn-only rewrite: Pass with SemanticNoOp set — the caller keeps
	// existing content (zero git churn) instead of writing THEIRS.
	res := RunGates("The function returns an error.", "The function return an error.", akg)
	assert.True(t, res.Pass)
	assert.True(t, res.SemanticNoOp)
	assert.Equal(t, 0, res.FailedGate)

	// Case/whitespace-only churn is also a no-op.
	res = RunGates("Hello   World", "hello world", akg)
	assert.True(t, res.Pass)
	assert.True(t, res.SemanticNoOp)

	// A factual change clears the flag: the candidate ships.
	res = RunGates(
		"The pool holds 10 connections.",
		"The pool holds 100 connections with backpressure handling and metrics.",
		akg)
	assert.True(t, res.Pass)
	assert.False(t, res.SemanticNoOp)

	// Empty old content is never a no-op: everything is new information.
	res = RunGates("", "Brand new section with fresh facts.", akg)
	assert.True(t, res.Pass)
	assert.False(t, res.SemanticNoOp)
}

// ────────────────────────────────────────────────────────────────────────────
// CheckProseGateWithVocab nil-vocab parity
// ────────────────────────────────────────────────────────────────────────────

func TestExtraProseVocabNilParity(t *testing.T) {
	style := config.StyleSpec{JargonBlacklist: []string{"simply"}}
	content := "Teh system simply returns teh value.\n\n# Title\n\nA short clean sentence here."
	for _, strict := range []bool{false, true} {
		nilPass, nilFails := CheckProseGateWithVocab(content, style, strict, nil)
		emptyPass, emptyFails := CheckProseGateWithVocab(content, style, strict, map[string]bool{})
		unrelatedPass, unrelatedFails := CheckProseGateWithVocab(content, style, strict,
			map[string]bool{"kubernetes": true})
		assert.Equal(t, nilPass, emptyPass, "strict=%v: nil vs empty vocab", strict)
		assert.Equal(t, nilFails, emptyFails, "strict=%v: nil vs empty vocab", strict)
		assert.Equal(t, nilPass, unrelatedPass, "strict=%v: unrelated vocab term", strict)
		assert.Equal(t, nilFails, unrelatedFails, "strict=%v: unrelated vocab term", strict)
	}
}

func TestExtraProseVocabExemptsTyposOnly(t *testing.T) {
	style := config.StyleSpec{}
	// A learned project term never flags the typo rule.
	pass, _ := CheckProseGateWithVocab("Teh deploy finished.", style, true, map[string]bool{"teh": true})
	assert.True(t, pass, "vocab term must exempt the typo rule")
	failPass, _ := CheckProseGateWithVocab("Teh deploy finished.", style, true, nil)
	assert.False(t, failPass, "nil vocab must flag the typo in strict mode")

	// Doubled-word detection is unaffected by vocabulary: a repeated
	// project term is still a doubled word.
	_, failures := CheckProseGateWithVocab("the the deploy", style, false, map[string]bool{"the": true})
	found := false
	for _, f := range failures {
		if strings.Contains(f, "doubled-word") {
			found = true
		}
	}
	assert.True(t, found, "doubled words must flag despite vocab, got %v", failures)
}

// ────────────────────────────────────────────────────────────────────────────
// RegenerateTOC idempotence
// ────────────────────────────────────────────────────────────────────────────

func TestExtraRegenerateTOCIdempotent(t *testing.T) {
	commentForm := `# Guide

<!-- toc -->

- [Stale Entry](#gone)
- [Old](#old)

# Install

## Usage

Content here.
`
	contentsForm := `# Guide

## Contents

- [Stale](#gone)

# Install

## Usage

Content here.
`
	for _, md := range []string{commentForm, contentsForm} {
		once := RegenerateTOC(md)
		assert.NotEqual(t, md, once, "stale TOC must be rewritten")
		assert.Contains(t, once, "- [Install](#install)")
		assert.Contains(t, once, "- [Usage](#usage)")
		assert.NotContains(t, once, "#gone")
		twice := RegenerateTOC(once)
		assert.Equal(t, once, twice, "second regeneration must be a no-op")
	}

	// No marker → passthrough, byte-identical.
	plain := "# Guide\n\nNo toc here.\n"
	assert.Equal(t, plain, RegenerateTOC(plain))

	// Headings inside fences are excluded from the TOC.
	fenced := "# Guide\n\n<!-- toc -->\n\n```md\n# Not A Heading\n```\n\n# Real\n"
	once := RegenerateTOC(fenced)
	assert.Contains(t, once, "- [Real](#real)")
	// The fenced body text stays (only TOC entries are rewritten), but no
	// TOC entry may point at the fenced pseudo-heading.
	assert.NotContains(t, once, "- [Not A Heading]")
	assert.Equal(t, once, RegenerateTOC(once))
}

// ────────────────────────────────────────────────────────────────────────────
// EvaluateAsserts rule matrix (doc-lint directives via patcher)
// ────────────────────────────────────────────────────────────────────────────

func TestExtraEvaluateAssertsMatrix(t *testing.T) {
	always := func(string) bool { return true }
	never := func(string) bool { return false }
	for _, tc := range []struct {
		name   string
		md     string
		exists func(string) bool
		fails  int // expected failure count
		want   []string
	}{
		{
			name:   "no-todo passes clean",
			md:     "<!-- gmb:assert: no-todo -->\nClean body.\n",
			exists: nil, fails: 0,
		},
		{
			name:   "no-todo fails on marker",
			md:     "<!-- gmb:assert: no-todo -->\n<!-- gmb:todo: fill me -->\n",
			exists: nil, fails: 1, want: []string{"no-todo"},
		},
		{
			name:   "symbols-covered all known",
			md:     "<!-- gmb:assert: symbols-covered: Foo, Bar -->\n",
			exists: always, fails: 0,
		},
		{
			name:   "symbols-covered nil fails every symbol",
			md:     "<!-- gmb:assert: symbols-covered: Foo, Bar -->\n",
			exists: nil, fails: 2, want: []string{`"Foo"`, `"Bar"`},
		},
		{
			name: "symbols-covered partial",
			md:   "<!-- gmb:assert: symbols-covered: Foo, Gone -->\n",
			exists: func(s string) bool {
				return s == "Foo"
			},
			fails: 1, want: []string{`"Gone"`},
		},
		{
			name:   "freshness skipped silently",
			md:     "<!-- gmb:assert: freshness>=80 -->\n",
			exists: never, fails: 0,
		},
		{
			name:   "unknown rule ignored",
			md:     "<!-- gmb:assert: quantum-entangled -->\n",
			exists: never, fails: 0,
		},
		{
			name:   "empty rule ignored",
			md:     "<!-- gmb:assert:  -->\n",
			exists: never, fails: 0,
		},
		{
			name:   "non-assert comments ignored",
			md:     "<!-- gmb:begin:sec -->\nBody.\n",
			exists: never, fails: 0,
		},
		{
			name:   "no asserts at all",
			md:     "Just prose.\n",
			exists: never, fails: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := patcher.EvaluateAsserts(tc.md, tc.exists)
			assert.Len(t, got, tc.fails)
			for _, w := range tc.want {
				assert.Contains(t, strings.Join(got, "\n"), w)
			}
		})
	}
}
