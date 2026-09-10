package patcher

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEvaluateAsserts_NoTodo(t *testing.T) {
	md := "<!-- gmb:begin:sec -->\ncontent\n<!-- gmb:assert: no-todo -->\n<!-- gmb:end:sec -->\n"
	assert.Empty(t, EvaluateAsserts(md, nil))

	withTodo := md + "<!-- gmb:todo: fill this in -->\n"
	failures := EvaluateAsserts(withTodo, nil)
	assert.Len(t, failures, 1)
	assert.Contains(t, failures[0], "no-todo")
}

func TestEvaluateAsserts_SymbolsCovered(t *testing.T) {
	md := "<!-- gmb:assert: symbols-covered:Foo, Bar -->\n"
	exists := func(s string) bool { return s == "Foo" }
	failures := EvaluateAsserts(md, exists)
	assert.Len(t, failures, 1)
	assert.Contains(t, failures[0], `"Bar"`)

	assert.Empty(t, EvaluateAsserts(md, func(s string) bool { return true }))
}

func TestEvaluateAsserts_FreshnessSkipped(t *testing.T) {
	md := "<!-- gmb:assert: freshness>=90 -->\n"
	assert.Empty(t, EvaluateAsserts(md, nil))
	assert.Empty(t, EvaluateAsserts("no asserts here", nil))
}

func TestPopulateTodos(t *testing.T) {
	out := PopulateTodos("body\n", []string{"Zebra", "apple", "Apple", "apple"})
	assert.Contains(t, out, "> TODO addressed from AKG: 3 exported symbols in scope:")
	assert.Contains(t, out, "`Apple`")
	// Idempotent: second call is a no-op.
	assert.Equal(t, out, PopulateTodos(out, []string{"Other"}))
	// Empty scope still drafts a line.
	assert.True(t, strings.Contains(PopulateTodos("x", nil), "0 exported symbols"))
}
