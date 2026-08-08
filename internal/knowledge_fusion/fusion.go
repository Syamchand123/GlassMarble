package knowledge_fusion

import (
	"context"
	"fmt"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// FusionEngine coordinates multi-source knowledge extraction and linking.
type FusionEngine struct {
	PRAdapter    PRAdapter
	IssueAdapter IssueAdapter
}

func NewFusionEngine(pr PRAdapter, issue IssueAdapter) *FusionEngine {
	return &FusionEngine{
		PRAdapter:    pr,
		IssueAdapter: issue,
	}
}

// Fuse combines claims from ADRs, Readmes, PRs, and Issues, linking them to the AKG.
func (f *FusionEngine) Fuse(
	ctx context.Context,
	repoDir string,
	adrPaths []string,
	readmePaths []string,
	prRefs []string,
	issueRefs []string,
	graph *akg.CodePropertyGraph,
) ([]developer_memory.KnowledgeClaim, error) {
	var rawClaims []developer_memory.KnowledgeClaim

	// 1. Parse ADRs
	for _, path := range adrPaths {
		claim, err := ParseADR(path)
		if err == nil && claim != nil {
			rawClaims = append(rawClaims, *claim)
		}
	}

	// 2. Parse Readmes
	for _, path := range readmePaths {
		claims := ParseReadme(path)
		rawClaims = append(rawClaims, claims...)
	}

	// 3. Fetch PRs - logically grounding them to the files they changed
	if f.PRAdapter != nil {
		prs, _ := f.PRAdapter.FetchRelatedPRs(ctx, prRefs)
		for _, pr := range prs {
			for _, file := range pr.FilesChanged {
				claim := developer_memory.KnowledgeClaim{
					ID:        fmt.Sprintf("pr-%s-%s", pr.ID, file),
					Subject:   file, // Linked to nodes by EntityLinker
					Predicate: "was_modified_by_pr",
					Object:    pr.ID,
					State:     developer_memory.StateActive,
					ValidFrom: time.Now(),
				}
				b := evidence.Bundle{}
				b.Add(evidence.EvidenceItem{
					Source:     evidence.SourcePR,
					Reference:  pr.ID,
					Excerpt:    pr.Title + "\n" + pr.Description,
					Confidence: 0.90,
					Timestamp:  time.Now(),
				})
				claim.Evidence = b
				rawClaims = append(rawClaims, claim)
			}
		}
	}

	// 4. Fetch Issues - logically grounding them to the files they changed
	if f.IssueAdapter != nil {
		issues, _ := f.IssueAdapter.FetchRelatedIssues(ctx, issueRefs)
		for _, issue := range issues {
			for _, file := range issue.FilesChanged {
				claim := developer_memory.KnowledgeClaim{
					ID:        fmt.Sprintf("issue-%s-%s", issue.ID, file),
					Subject:   file, // Linked to nodes by EntityLinker
					Predicate: "fixes_issue",
					Object:    issue.ID,
					State:     developer_memory.StateActive,
					ValidFrom: time.Now(),
				}
				b := evidence.Bundle{}
				b.Add(evidence.EvidenceItem{
					Source:     evidence.SourceUser,
					Reference:  issue.ID,
					Excerpt:    issue.Title + "\n" + issue.Description,
					Confidence: 0.98,
					Timestamp:  time.Now(),
				})
				claim.Evidence = b
				rawClaims = append(rawClaims, claim)
			}
		}
	}

	// 5. Link Claims to AKG
	linkedClaims := LinkDocumentClaimsToAKG(rawClaims, graph)

	// 6. Resolve Conflicts
	resolvedMap := make(map[string]developer_memory.KnowledgeClaim)
	for _, claim := range linkedClaims {
		key := fmt.Sprintf("%s|%s|%s", claim.Subject, claim.Predicate, claim.Object)
		if existing, found := resolvedMap[key]; found {
			resolvedMap[key] = ResolveConflict(existing, claim)
		} else {
			resolvedMap[key] = claim
		}
	}

	var finalClaims []developer_memory.KnowledgeClaim
	for _, claim := range resolvedMap {
		finalClaims = append(finalClaims, claim)
	}

	return finalClaims, nil
}
