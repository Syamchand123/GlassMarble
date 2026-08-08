package commit_reasoning

import (
	"crypto/sha256"
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// ReasonCommit is the main entry point: analyzes one commit and generates events.
func ReasonCommit(
	repoDir string,
	commitHash string,
	baseSnap *archmodel.ArchSnapshot,
	headSnap *archmodel.ArchSnapshot,
	graphDiff *akg.GraphDiff,
) ([]archmodel.ArchEvent, error) {
	// 1. Read commit metadata
	meta, err := ReadCommit(repoDir, commitHash)
	if err != nil {
		return nil, err
	}

	// 2. Classify architectural changes
	changes := ClassifyChange(graphDiff, meta, baseSnap, headSnap)

	// 3. Extract intent (no PR description for now)
	intent, src, conf := ExtractIntent(meta, "")

	// 4. Build events
	var events []archmodel.ArchEvent
	for i, change := range changes {
		idRaw := fmt.Sprintf("%s-%s-%d", commitHash, change.Kind, i)
		h := sha256.Sum256([]byte(idRaw))
		id := fmt.Sprintf("%x", h[:8])

		b := evidence.Bundle{}
		b.Add(evidence.EvidenceItem{
			Source:     evidence.SourceGit,
			Reference:  meta.Hash,
			Excerpt:    meta.Subject + "\n" + meta.Body,
			Confidence: 1.0,
			Timestamp:  meta.Timestamp,
		})

		// If ExtractIntent returns confidence > 0, we can add it as another evidence item
		if conf > 0 {
			b.Add(evidence.EvidenceItem{
				Source:     src,
				Reference:  meta.Hash,
				Excerpt:    intent,
				Confidence: conf,
				Timestamp:  meta.Timestamp,
			})
		}

		event := archmodel.ArchEvent{
			ID:            id,
			Kind:          change.Kind,
			CommitHash:    meta.Hash,
			Timestamp:     meta.Timestamp,
			Title:         string(change.Kind) + " detected",
			Description:   "Event reasoned from commit " + meta.Hash,
			AffectedIDs:   change.AffectedIDs,
			Intent:        intent,
			IntentSrc:     src,
			RelatedPRs:    meta.RelatedPRs,
			RelatedIssues: meta.RelatedIssues,
			ValidFrom:     meta.Timestamp,
			Evidence:      b,
		}
		
		events = append(events, event)
	}

	return events, nil
}
