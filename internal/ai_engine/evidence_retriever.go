package ai_engine

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// EvidenceRetriever finds the most relevant evidence for a user question.
// This is called BEFORE the LLM — it assembles the context.
type EvidenceRetriever struct {
	graph  *akg.CodePropertyGraph
	memory *developer_memory.DeveloperMemory
}

func NewEvidenceRetriever(graph *akg.CodePropertyGraph, memory *developer_memory.DeveloperMemory) *EvidenceRetriever {
	return &EvidenceRetriever{
		graph:  graph,
		memory: memory,
	}
}

// RetrieveForQuestion finds the top-k most relevant pieces of evidence.
func (r *EvidenceRetriever) RetrieveForQuestion(question string, topK int) *EvidenceContext {
	ctx := &EvidenceContext{Question: question}

	// 1: Extract entity names from the question (naive tokenization for now)
	entities := extractEntities(question)

	// 2: Find matching nodes in the AKG
	if r.graph != nil && r.graph.Nodes != nil {
		for _, entity := range entities {
			entLower := strings.ToLower(entity)
			r.graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
				if strings.Contains(strings.ToLower(node.Name), entLower) {
					ctx.AKGNodes = append(ctx.AKGNodes, node)
				}
			})
		}
	}

	// 3: Find matching memories (naive search over global memory)
	if r.memory != nil {
		for _, entity := range entities {
			entLower := strings.ToLower(entity)
			for _, claim := range r.memory.GlobalMemory {
				if strings.Contains(strings.ToLower(claim.Subject), entLower) || strings.Contains(strings.ToLower(claim.Object), entLower) {
					ctx.MemoryClaims = append(ctx.MemoryClaims, claim)
				}
			}
			
			// Timeline entries
			for _, entry := range r.memory.Timeline {
				match := false
				for _, c := range entry.Components {
					if strings.Contains(strings.ToLower(c), entLower) {
						match = true
						break
					}
				}
				if match || strings.Contains(strings.ToLower(entry.Description), entLower) {
					ctx.TimelineEntries = append(ctx.TimelineEntries, entry)
				}
			}
		}
	}

	// 4: Trim to topK limits (naive limit)
	if len(ctx.MemoryClaims) > topK {
		ctx.MemoryClaims = ctx.MemoryClaims[:topK]
	}
	if len(ctx.TimelineEntries) > topK {
		ctx.TimelineEntries = ctx.TimelineEntries[:topK]
	}
	if len(ctx.AKGNodes) > topK {
		ctx.AKGNodes = ctx.AKGNodes[:topK]
	}

	return ctx
}

func extractEntities(q string) []string {
	// Simple naive extraction: return words longer than 3 chars
	words := strings.Fields(q)
	var out []string
	for _, w := range words {
		clean := strings.Trim(w, "?.,!\"'")
		if len(clean) > 3 {
			out = append(out, clean)
		}
	}
	if len(out) == 0 {
		out = append(out, q)
	}
	return out
}
