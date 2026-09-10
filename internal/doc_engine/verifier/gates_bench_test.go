package verifier

import (
	"strings"
	"testing"
)

// BenchmarkRunGates tracks the plan Section 13.4 <10ms verification budget.
func BenchmarkRunGates(b *testing.B) {
	oldContent := "### Exported Interface\n\n| Name | Signature |\n| --- | --- |\n| `Foo` | `func Foo() error` |\n"
	var sb strings.Builder
	sb.WriteString("### Exported Interface\n\nUpdated prose for `Foo`.\n\n```mermaid\nflowchart TD\n  A-->B\n```\n")
	newContent := sb.String()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RunGates(oldContent, newContent, nil)
	}
}
