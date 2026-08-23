package aggregate

import (
	"log"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
)

// IndexedNode is used to stream nodes into the map lock-free
type IndexedNode struct {
	Key  string
	Node *normalize.GASTNode
}

// Aggregate executes Aggregation, processing a NormalizeOutput and emitting the AggregateOutput topology.
// It supports both cold full-repository runs and fast incremental Git delta runs.
// rootDir is the repository root used to discover workspace/monorepo config
// files (go.mod, tsconfig.json, go.work). Passing the actual analysis root —
// instead of a hardcoded "." — ensures module aliases resolve correctly when
// gmb runs from a different working directory (AUDIT Issue 2 Phase 2A-1).
func Aggregate(payload *normalize.NormalizeOutput, existingState *AggregateOutput, rootDir string) (*AggregateOutput, error) {
	output := existingState
	if output == nil {
		output = &AggregateOutput{
			RootNode:              NewDirectoryNode(".", ""),
			GlobalDefinitionIndex: make(map[string][]*normalize.GASTNode),
			GlobalCallQueue:       nil,
			LocalTables:           make(map[string]*normalize.FileSymbolTable),
			WorkspaceCtx:          NewWorkspaceContext(),
		}
	}

	if output.FileToSymbols == nil {
		output.FileToSymbols = make(map[string][]string)
	}
	if output.FileToMembers == nil {
		output.FileToMembers = make(map[string][]string)
	}
	if output.FileToCalls == nil {
		output.FileToCalls = make(map[string][]LinkedCallSite)
	}

	if payload == nil {
		return output, nil
	}

	output.CommitHash = payload.CommitHash

	if output.WorkspaceCtx == nil {
		output.WorkspaceCtx = NewWorkspaceContext()
	}
	if rootDir == "" {
		rootDir = "."
	}
	output.WorkspaceCtx.ScanWorkspace(rootDir)

	// Step 3.1 (Pruning): Remove deleted files and prune empty directories incrementally
	var pruneWg sync.WaitGroup
	var pruneMu sync.Mutex

	for _, deletedPath := range payload.DeletedPaths {
		pruneWg.Add(1)
		go func(dp string) {
			defer pruneWg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("aggregate: prune goroutine panicked: %v", r)
				}
			}()
			relPath := NormalizeRelativePath(dp)
			PruneFileNode(output.RootNode, relPath)

			pruneMu.Lock()
			// O(1) Global Definition Pruning
			if symbols, ok := output.FileToSymbols[relPath]; ok {
				for _, sym := range symbols {
					var updatedNodes []*normalize.GASTNode
					for _, node := range output.GlobalDefinitionIndex[sym] {
						if node.Properties["file_path"] != relPath {
							updatedNodes = append(updatedNodes, node)
						}
					}
					if len(updatedNodes) == 0 {
						delete(output.GlobalDefinitionIndex, sym)
					} else {
						output.GlobalDefinitionIndex[sym] = updatedNodes
					}
				}
			}
			delete(output.FileToSymbols, relPath)
			delete(output.FileToMembers, relPath)
			delete(output.FileToCalls, relPath)
			delete(output.LocalTables, relPath)
			pruneMu.Unlock()
		}(deletedPath)
	}
	pruneWg.Wait()
	pruneEmptyDirectories(output.RootNode)

	// Step 3.2 & 3.3 (Grafting & Visibility): Graft updated files incrementally
	var graftWg sync.WaitGroup
	type graftResult struct {
		relPath   string
		symTable  *normalize.FileSymbolTable
		localSyms []string
		nodes     []IndexedNode
	}
	resultCh := make(chan graftResult, len(payload.UpsertedTrees))

	for relPath, gastRoot := range payload.UpsertedTrees {
		graftWg.Add(1)
		go func(rp string, root *normalize.GASTNode) {
			defer graftWg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("aggregate: graft goroutine panicked: %v", r)
				}
			}()
			normRelPath := NormalizeRelativePath(rp)
			symTable := payload.LocalSymbolTables[rp]

			var imports []string
			var lang string
			if symTable != nil {
				imports = symTable.Imports
				lang = string(symTable.Language)
			}
			GraftFileNode(output.RootNode, normRelPath, root, imports, lang)

			// Stamp Visibility (Step 3.3) directly on nodes
			ComputeVisibilityEnclave(root, normRelPath, output.WorkspaceCtx)

			// Extract new symbols for O(1) Indexing
			var localSyms []string
			var nodes []IndexedNode
			collectExportedGASTNodes(root, normRelPath, &nodes, &localSyms)

			resultCh <- graftResult{
				relPath:   normRelPath,
				symTable:  symTable,
				localSyms: localSyms,
				nodes:     nodes,
			}
		}(relPath, gastRoot)
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("aggregate: result collector panicked: %v", r)
			}
		}()
		graftWg.Wait()
		close(resultCh)
	}()

	// Lock-free Map-Reduce Aggregation
	for res := range resultCh {
		if res.symTable != nil {
			output.LocalTables[res.relPath] = res.symTable
			output.FileToCalls[res.relPath] = extractCallsFromFile(res.relPath, res.symTable)
		}
		// FileToSymbols carries the per-file symbol list, extended with the
		// bare method names (simple_name) so symbol lookup finds them.
		syms := res.localSyms
		for _, idxNode := range res.nodes {
			if sn := idxNode.Node.Properties["simple_name"]; sn != "" {
				syms = append(syms, sn)
			}
		}
		output.FileToSymbols[res.relPath] = syms
		// v2 (W1-10, §5.3.4): per-file member list for real file→symbol
		// containment edges (A-12). Contains the same resolution keys as
		// the global index (canonical IDs when present, else FQNs) — every
		// member must be resolvable there.
		output.FileToMembers[res.relPath] = dedupeStrings(res.localSyms)
		for _, idxNode := range res.nodes {
			output.GlobalDefinitionIndex[idxNode.Key] = append(output.GlobalDefinitionIndex[idxNode.Key], idxNode.Node)
		}
	}

	// Determinize GlobalDefinitionIndex value slices (completion-order nondeterminism → canonical by file_path, then ID)
	for key, nodes := range output.GlobalDefinitionIndex {
		sort.Slice(nodes, func(i, j int) bool {
			fi := nodes[i].Properties["file_path"]
			fj := nodes[j].Properties["file_path"]
			if fi != fj {
				return fi < fj
			}
			return nodes[i].ID < nodes[j].ID
		})
		output.GlobalDefinitionIndex[key] = nodes
	}

	// Step 3.4: Rebuild GlobalCallQueue instantly from cached FileToCalls
	output.GlobalCallQueue = make([]LinkedCallSite, 0)
	for _, calls := range output.FileToCalls {
		output.GlobalCallQueue = append(output.GlobalCallQueue, calls...)
	}

	// Step 3.5: Cyclic Dependency & Architecture Validation
	DetectArchitecturalCycles(output.GlobalCallQueue)

	// Step 3.6: External Dependency Indexing
	IndexExternalDependencies(output)

	// Step 3.7: Generics Canonicalization
	IndexGenerics(output)

	// Step 3.8: Entrypoint & Root Execution Node Registry
	IndexEntrypoints(output)

	// Step 3.9: Behavioral Primitive Escalation (Zone Tainting)
	EscalatePrimitives(output.RootNode)

	return output, nil
}

// dedupeStrings returns xs with duplicates removed, preserving order.
func dedupeStrings(xs []string) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = appendUnique(out, x)
	}
	return out
}

// normalizeCallerID prefixes a bare caller symbol with its file path and// normalizes dotted method-style IDs ("Store.Save") into universal ID form
// ("Store::Save") so they match the graph nodes produced by the link
// builder (AUDIT Issue 1.6 — Go method caller IDs never matched).
//
// Only symbol-style callers undergo the dotted-method conversion. Path-like
// callers (file-root defaults on Windows, e.g. "cmd\diff.go") keep their
// separators and extension untouched — converting those would mangle the
// path ("cmd\diff.go" -> "cmd\diff::go") and the ".go" extension dot of the
// file-prefix must never be interpreted as a method separator.
func normalizeCallerID(rp, callerID string) string {
	if strings.Contains(callerID, "::") {
		return callerID
	}
	if NormalizeRelativePath(callerID) == NormalizeRelativePath(rp) {
		return "file:" + NormalizeRelativePath(rp)
	}
	if strings.Contains(callerID, ".") && !strings.ContainsAny(callerID, "/\\") {
		if dot := strings.LastIndex(callerID, "."); dot != -1 {
			callerID = callerID[:dot] + "::" + callerID[dot+1:]
		}
	}
	return NormalizeRelativePath(rp) + "::" + callerID
}

// extractCallsFromFile parses LocalCalls into LinkedCallSites.
func extractCallsFromFile(rp string, st *normalize.FileSymbolTable) []LinkedCallSite {
	folderPath := NormalizeRelativePath(filepath.Dir(rp))
	var localQueue []LinkedCallSite
	for _, call := range st.LocalCalls {
		callerID := normalizeCallerID(rp, call.CallerNodeID)
		localQueue = append(localQueue, LinkedCallSite{
			SourceFileNodeID: callerID,
			SourceFilePath:   NormalizeRelativePath(rp),
			SourceFolderPath: folderPath,
			ReceiverName:     call.ReceiverName,
			MethodName:       call.MethodName,
			LineNumber:       call.LineNumber,
			HasPrimitive:     call.HasPrimitive,
			Primitives:       call.Primitives,
			LocalImports:     st.Imports,
		})
	}
	return localQueue
}

// SynthesizeGlobalDefinitionIndex traverses the DirectoryNode hierarchy concurrently and collects all exported symbols lock-free.
func SynthesizeGlobalDefinitionIndex(root *DirectoryNode) map[string][]*normalize.GASTNode {
	index := make(map[string][]*normalize.GASTNode)
	var wg sync.WaitGroup
	resultCh := make(chan []IndexedNode, 1000)

	traverseAndIndex(root, resultCh, &wg)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("aggregate: global index collector panicked: %v", r)
			}
		}()
		wg.Wait()
		close(resultCh)
	}()

	for nodes := range resultCh {
		for _, idxNode := range nodes {
			index[idxNode.Key] = append(index[idxNode.Key], idxNode.Node)
		}
	}
	for key, nodes := range index {
		sort.Slice(nodes, func(i, j int) bool {
			fi := nodes[i].Properties["file_path"]
			fj := nodes[j].Properties["file_path"]
			if fi != fj {
				return fi < fj
			}
			return nodes[i].ID < nodes[j].ID
		})
		index[key] = nodes
	}
	return index
}

func traverseAndIndex(dir *DirectoryNode, resultCh chan<- []IndexedNode, wg *sync.WaitGroup) {
	if dir == nil {
		return
	}

	dir.mu.RLock()
	files := make([]*FileBoundaryNode, 0, len(dir.Files))
	for _, file := range dir.Files {
		files = append(files, file)
	}
	subFolders := make([]*DirectoryNode, 0, len(dir.SubFolders))
	for _, sub := range dir.SubFolders {
		subFolders = append(subFolders, sub)
	}
	dir.mu.RUnlock()

	for _, file := range files {
		if file == nil || file.GASTRoot == nil {
			continue
		}
		wg.Add(1)
		go func(f *FileBoundaryNode) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("aggregate: index goroutine panicked: %v", r)
				}
			}()
			var nodes []IndexedNode
			collectExportedGASTNodes(f.GASTRoot, f.RelativePath, &nodes, nil)
			resultCh <- nodes
		}(file)
	}

	for _, subDir := range subFolders {
		traverseAndIndex(subDir, resultCh, wg)
	}
}

func collectExportedGASTNodes(node *normalize.GASTNode, fileRelPath string, out *[]IndexedNode, localSyms *[]string) {
	if node == nil {
		return
	}

	if node.Type == normalize.GASTTypeDeclaration || node.Type == normalize.GASTFunction || node.Type == normalize.GASTVariable {
		// Always ensure file_path is set so link resolvers can build UniversalIDs
		if node.Properties == nil {
			node.Properties = make(map[string]string)
		}
		if node.Properties["file_path"] == "" {
			node.Properties["file_path"] = fileRelPath
		}

		// v2 (§5.3.1): canonical IDs are the primary index key when present
		// (Phase 0 ids package); legacy dotted FQNs remain secondary keys
		// so existing resolvers keep working.
		if cid := node.Properties["canonical_id"]; cid != "" {
			if out != nil {
				*out = append(*out, IndexedNode{Key: cid, Node: node})
			}
			if localSyms != nil {
				*localSyms = append(*localSyms, cid)
			}
		}

		if fqn, exists := node.Properties["fully_qualified_name"]; exists && fqn != "" {
			if out != nil {
				*out = append(*out, IndexedNode{Key: fqn, Node: node})
			}
			if localSyms != nil {
				*localSyms = append(*localSyms, fqn)
			}
		}
		// Index by plain Name for same-package resolution
		if node.Name != "" {
			if out != nil {
				*out = append(*out, IndexedNode{Key: node.Name, Node: node})
			}
			if localSyms != nil {
				*localSyms = append(*localSyms, node.Name)
			}
		}
		// Fallback: path-based FQN
		dir := strings.ReplaceAll(path.Dir(fileRelPath), "/", ".")
		if dir != "." && dir != "" {
			pathSym := dir + "." + node.Name
			if out != nil {
				*out = append(*out, IndexedNode{Key: pathSym, Node: node})
			}
			if localSyms != nil {
				*localSyms = append(*localSyms, pathSym)
			}
		}
	}

	for _, child := range node.Children {
		collectExportedGASTNodes(child, fileRelPath, out, localSyms)
	}
}

// SynthesizeGlobalCallQueue aggregates unresolved local calls concurrently into a unified project-wide queue lock-free.
func SynthesizeGlobalCallQueue(localTables map[string]*normalize.FileSymbolTable) []LinkedCallSite {
	var queue []LinkedCallSite
	var wg sync.WaitGroup
	resultCh := make(chan []LinkedCallSite, len(localTables))

	for relPath, symTable := range localTables {
		if symTable == nil {
			continue
		}

		wg.Add(1)
		go func(rp string, st *normalize.FileSymbolTable) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("aggregate: call queue goroutine panicked: %v", r)
				}
			}()
			folderPath := NormalizeRelativePath(filepath.Dir(rp))
			var localQueue []LinkedCallSite

			for _, call := range st.LocalCalls {
				callerID := normalizeCallerID(rp, call.CallerNodeID)
				localQueue = append(localQueue, LinkedCallSite{
					SourceFileNodeID: callerID,
					SourceFilePath:   NormalizeRelativePath(rp),
					SourceFolderPath: folderPath,
					ReceiverName:     call.ReceiverName,
					MethodName:       call.MethodName,
					LineNumber:       call.LineNumber,
					HasPrimitive:     call.HasPrimitive,
					Primitives:       call.Primitives,
					LocalImports:     st.Imports,
				})
			}

			resultCh <- localQueue
		}(relPath, symTable)
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("aggregate: call queue collector panicked: %v", r)
			}
		}()
		wg.Wait()
		close(resultCh)
	}()

	for q := range resultCh {
		queue = append(queue, q...)
	}
	return queue
}
