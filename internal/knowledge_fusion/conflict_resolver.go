package knowledge_fusion

import (
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// ResolveConflict merges two conflicting/overlapping claims into a single resolved claim.
// Rule: never delete either claim's evidence. Both provenance trails are preserved.
func ResolveConflict(a, b developer_memory.KnowledgeClaim) developer_memory.KnowledgeClaim {
	resolved := a

	// Merge evidence items
	for _, item := range b.Evidence.Items {
		resolved.Evidence.Add(item)
	}

	// Recompute aggregate confidence based on all merged evidence
	resolved.Evidence.Aggregate()

	// Update state based on evidence reliability.
	// We rely on the evidence bundle's PrimarySource to dictate the most reliable truth.
	// If one was explicitly deprecated by user or code, keep it if it's highest priority.
	if evidence.SourceReliability(b.Evidence.PrimarySource) > evidence.SourceReliability(a.Evidence.PrimarySource) {
		resolved.State = b.State
		resolved.Object = b.Object
		resolved.Predicate = b.Predicate
	}

	return resolved
}
