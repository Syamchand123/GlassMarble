package ai_engine

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

// EvidenceContext is the structured, grounded context passed to the LLM.
type EvidenceContext struct {
	Question        string
	AKGNodes        []*stage4.ResolvedNode
	MemoryClaims    []developer_memory.KnowledgeClaim
	TimelineEntries []archmodel.TimelineEntry
	Patterns        []archmodel.DetectedPattern
	Smells          []archmodel.ArchSmell
	MetricSummary   string
	TokenCount      int
}

// BuildPrompt constructs the final grounded LLM prompt.
func (c *EvidenceContext) BuildPrompt() string {
	var builder strings.Builder

	builder.WriteString("=== ARCHITECTURE KNOWLEDGE ===\n")
	if len(c.AKGNodes) > 0 {
		for _, node := range c.AKGNodes {
			builder.WriteString(fmt.Sprintf("- Node %s: %s\n", node.Kind, node.Name))
		}
	} else {
		builder.WriteString("No direct AKG nodes found.\n")
	}

	builder.WriteString("\n=== HISTORY & MEMORY ===\n")
	if len(c.TimelineEntries) > 0 || len(c.MemoryClaims) > 0 {
		for _, entry := range c.TimelineEntries {
			builder.WriteString(fmt.Sprintf("- [%s] %s: %s\n", entry.Timestamp.Format("2006-01-02"), entry.EventKind, entry.Description))
		}
		for _, claim := range c.MemoryClaims {
			builder.WriteString(fmt.Sprintf("- Fact: %s %s %s (Confidence: %.2f)\n", claim.Subject, claim.Predicate, claim.Object, claim.FreshnessScore))
		}
	} else {
		builder.WriteString("No historical facts found.\n")
	}

	builder.WriteString("\n=== PATTERNS DETECTED ===\n")
	if len(c.Patterns) > 0 {
		for _, pat := range c.Patterns {
			builder.WriteString(fmt.Sprintf("- Pattern: %s (Components: %v)\n", pat.Name, pat.Components))
		}
	} else {
		builder.WriteString("No specific patterns found.\n")
	}

	builder.WriteString("\n=== METRICS ===\n")
	if c.MetricSummary != "" {
		builder.WriteString(c.MetricSummary + "\n")
	}

	builder.WriteString("\n=== QUESTION ===\n")
	builder.WriteString(c.Question + "\n")
	builder.WriteString("\nINSTRUCTIONS: Answer using ONLY the evidence provided above.\n")
	builder.WriteString("If the evidence does not support a definitive answer, say so.\n")
	builder.WriteString("Cite specific commits, PR numbers, or component names when available.\n")

	return builder.String()
}
