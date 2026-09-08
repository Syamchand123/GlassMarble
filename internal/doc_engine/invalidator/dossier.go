// Package invalidator — dossier.go
// Assembles the GlobalCommitDossier once per commit from the AKG diff,
// commit metadata, and commit reasoning intent.
package invalidator

import (
	"context"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/commit_reasoning"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/git"
)

// BuildDossier constructs the GlobalCommitDossier for a given commit.
// If baseGraph and headGraph are available, it extracts exact added, modified,
// and removed symbols from the structural AKG diff.
func BuildDossier(repoDir string, commitHash string, baseGraph, headGraph *akg.CodePropertyGraph) (*config.GlobalCommitDossier, error) {
	dossier := &config.GlobalCommitDossier{
		CommitHash:   commitHash,
		CommitIntent: string(commit_reasoning.IntentUnknown),
	}

	// 1. Read git commit metadata if available
	if repoDir != "" && commitHash != "" {
		meta, err := git.ReadCommit(repoDir, commitHash)
		if err == nil && meta != nil {
			dossier.CommitReason = meta.Subject
			dossier.PRDescription = meta.Body
			dossier.IssueRefs = meta.RelatedIssues

			// Classify commit intent deterministically via commit_reasoning
			extractor := commit_reasoning.NewIntentExtractor()
			res := extractor.Extract(context.Background(), meta, meta.Body)
			dossier.CommitIntent = string(res.Intent)
		}
	}

	// 2. Compute AKG GraphDiff if both graphs are provided
	if headGraph != nil {
		diff := akg.DiffGraphs(baseGraph, headGraph)
		if diff != nil {
			// Added symbols
			for _, node := range diff.NodesAdded {
				fact := config.SymbolFact{
					FQN:  node.ID,
					Kind: node.Kind,
					File: node.File,
				}
				// Enrich with head node signature and doc comments if available
				if headGraph.Nodes != nil {
					if resolved, ok := headGraph.Nodes.Get(node.ID); ok && resolved != nil {
						populateSymbolFact(&fact, resolved)
					}
				}
				dossier.AddedSymbols = append(dossier.AddedSymbols, fact)

				// Detect config variables and sentinels
				if isConfigVar(fact.FQN) {
					dossier.AddedConfigVars = append(dossier.AddedConfigVars, config.ConfigVarFact{
						Name: fact.FQN,
						File: fact.File,
						Line: fact.Line,
					})
				}
				if isSentinelError(fact.FQN) {
					dossier.AddedSentinels = append(dossier.AddedSentinels, config.SentinelFact{
						FQN:  fact.FQN,
						Doc:  fact.Doc,
						File: fact.File,
						Line: fact.Line,
					})
				}
			}

			// Removed symbols
			for _, node := range diff.NodesRemoved {
				dossier.RemovedSymbols = append(dossier.RemovedSymbols, node.ID)
				if isConfigVar(node.ID) {
					dossier.RemovedConfigVars = append(dossier.RemovedConfigVars, node.ID)
				}
			}

			// Modified symbols: compare nodes present in both graphs
			if baseGraph != nil && baseGraph.Nodes != nil && headGraph.Nodes != nil {
				headGraph.Nodes.Iterate(func(id string, headNode *link.ResolvedNode) {
					baseNode, ok := baseGraph.Nodes.Get(id)
					if !ok || baseNode == nil || headNode == nil {
						return
					}

					baseSig := extractSignature(baseNode)
					headSig := extractSignature(headNode)
					baseDoc := extractDoc(baseNode)
					headDoc := extractDoc(headNode)

					if baseSig != headSig || baseDoc != headDoc {
						dossier.ModifiedSymbols = append(dossier.ModifiedSymbols, config.SymbolDelta{
							FQN:       id,
							Before:    baseSig,
							After:     headSig,
							DocBefore: baseDoc,
							DocAfter:  headDoc,
						})
					}
				})
			}
		}
	}

	return dossier, nil
}

// populateSymbolFact extracts location and documentation fields from a ResolvedNode.
func populateSymbolFact(fact *config.SymbolFact, n *link.ResolvedNode) {
	if n == nil {
		return
	}
	fact.Line = n.FileSpec.LineStart
	fact.Signature = extractSignature(n)
	fact.Doc = extractDoc(n)
	if fact.File == "" {
		fact.File = n.FileSpec.Path
	}
	if fact.Kind == "" {
		fact.Kind = string(n.Kind)
	}
}

func extractSignature(n *link.ResolvedNode) string {
	if n == nil {
		return ""
	}
	if sig, ok := n.Properties["signature"]; ok && sig != "" {
		return sig
	}
	return n.Name
}

func extractDoc(n *link.ResolvedNode) string {
	if n == nil {
		return ""
	}
	if doc, ok := n.Properties["doc_comment"]; ok {
		return doc
	}
	if doc, ok := n.Properties["doc"]; ok {
		return doc
	}
	return ""
}

func isConfigVar(fqn string) bool {
	lower := strings.ToLower(fqn)
	return strings.Contains(lower, "getenv") || strings.Contains(lower, "config")
}

func isSentinelError(fqn string) bool {
	parts := strings.Split(fqn, "::")
	name := fqn
	if len(parts) > 1 {
		name = parts[len(parts)-1]
	}
	return strings.HasPrefix(name, "Err")
}
