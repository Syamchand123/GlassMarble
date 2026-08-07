package stage4

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// fileEntry holds a file's relative path and its declared imports.
type fileEntry struct {
	relPath string
	imports []string
}

// LinkFileDependencies creates EdgeDependsOn edges between files based on their declared import lists.
// It maps import tokens (e.g. "b", "database/sql") to known file paths in the CPG.
func LinkFileDependencies(stage3Out *stage3.Stage3Output, cpg *Stage4Output) {
	if stage3Out == nil || stage3Out.RootNode == nil || cpg == nil {
		return
	}

	var files []fileEntry
	collectFileImports(stage3Out.RootNode, &files, cpg)

	// Build a reverse index: module/package name -> file rel path
	// e.g. "b" -> "src/b.go", "store" -> "src/store.go"
	moduleToFile := make(map[string]string, len(files))
	for _, fe := range files {
		// Extract module name from path (last segment without extension)
		mod := moduleFromRelPath(fe.relPath)
		if mod != "" {
			moduleToFile[mod] = fe.relPath
		}
		// Also index by full normalized path
		moduleToFile[fe.relPath] = fe.relPath
	}

	// v3 (incremental): unmodified files are invisible to the delta's own
	// import index; backfill it from the persisted graph so DEPENDS_ON edges
	// across the delta boundary survive (e.g. a delta of service.go must
	// still depend on store.go). A full rescan links against an empty base,
	// so this is a no-op there.
	if cpg.db != nil {
		for _, fn := range cpg.db.GetNodesByKind("FILE") {
			if fn == nil || fn.FileSpec.Path == "" {
				continue
			}
			mod := moduleFromRelPath(fn.FileSpec.Path)
			if mod != "" {
				if _, exists := moduleToFile[mod]; !exists {
					moduleToFile[mod] = fn.FileSpec.Path
				}
			}
			if _, exists := moduleToFile[fn.FileSpec.Path]; !exists {
				moduleToFile[fn.FileSpec.Path] = fn.FileSpec.Path
			}
		}
	}

	// Create EdgeDependsOn for each file that imports another known file
	for _, fe := range files {
		// File nodes are registered under the "file:" prefix by the builder.
		srcID := "file:" + fe.relPath
		for _, imp := range fe.imports {
			imp = strings.TrimSpace(imp)
			if imp == "" {
				continue
			}

			// Try to match import to a known file
			var targetPath string

			// Direct path/module match
			if p, ok := moduleToFile[imp]; ok {
				targetPath = p
			} else {
				// Try last segment of import path (e.g. "database/sql" -> "sql")
				parts := strings.Split(imp, "/")
				lastSeg := parts[len(parts)-1]
				if p, ok := moduleToFile[lastSeg]; ok {
					targetPath = p
				}
			}

			if targetPath != "" && targetPath != fe.relPath {
				cpg.AddEdge(srcID, "file:"+targetPath, EdgeDependsOn, 0)
				continue
			}

			// v2 (W1-14, §5.3.3/A-11): external imports get file → ext
			// DEPENDS_ON edges (replacing the removed ext CONTAINS spine),
			// when the index knows the dependency.
			if stage3Out != nil && !stage3.IsStdlibImport(imp, fe.relPath) {
				if extNode, ok := stage3Out.ExternalDependencies[stage3.ResolveExternalKey(imp)]; ok {
					extID := stage3.ResolveExternalKey(imp)
					cpg.AddEdgeProperties(srcID, extID, EdgeDependsOn, 0, 1.0,
						map[string]string{ont.PredProvenance: "ast"})
					if extNode != nil && extNode.Properties["alias"] != "" {
						cpg.GraphNodes[extID] = &ResolvedNode{
							ID:         extID,
							Kind:       "EXTERNAL_SDK",
							Name:       imp,
							Properties: extNode.Properties,
						}
					}
				}
			}
		}
	}
}

// collectFileImports recursively collects all files and their imports from the directory tree.
func collectFileImports(dir *stage3.DirectoryNode, out *[]fileEntry, cpg *Stage4Output) {
	if dir == nil {
		return
	}

	for _, file := range dir.Files {
		if file == nil {
			continue
		}
		if len(cpg.ModifiedFiles) > 0 && !cpg.ModifiedFiles[stage3.NormalizeRelativePath(file.RelativePath)] {
			continue
		}
		*out = append(*out, fileEntry{
			relPath: stage3.NormalizeRelativePath(file.RelativePath),
			imports: file.LocalImports,
		})
	}

	for _, sub := range dir.SubFolders {
		collectFileImports(sub, out, cpg)
	}
}

// moduleFromRelPath extracts a module/package name from a relative file path.
// e.g. "src/b.go" -> "b", "internal/store/postgres.go" -> "postgres"
func moduleFromRelPath(relPath string) string {
	// Get last path segment
	lastSlash := strings.LastIndex(relPath, "/")
	base := relPath
	if lastSlash >= 0 {
		base = relPath[lastSlash+1:]
	}
	// Strip extension
	dot := strings.LastIndex(base, ".")
	if dot >= 0 {
		return base[:dot]
	}
	return base
}

// detectCyclicDependencies runs Tarjan's Strongly Connected Components algorithm
// over the merged EdgeDependsOn graph to tag EdgeCyclic.
func detectCyclicDependencies(cpg *Stage4Output) {
	index := 0
	stack := []string{}
	indices := make(map[string]int)
	lowlink := make(map[string]int)
	onStack := make(map[string]bool)

	var strongconnect func(v string)
	strongconnect = func(v string) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		edges := cpg.OutboundEdges[v]
		if len(edges) == 0 && cpg.db != nil {
			edges = cpg.db.GetOutboundEdges(v)
		}

		for _, e := range edges {
			if e.Type == EdgeDependsOn {
				w := e.TargetID
				if _, ok := indices[w]; !ok {
					strongconnect(w)
					if lowlink[w] < lowlink[v] {
						lowlink[v] = lowlink[w]
					}
				} else if onStack[w] {
					if indices[w] < lowlink[v] {
						lowlink[v] = indices[w]
					}
				}
			}
		}

		if lowlink[v] == indices[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			if len(scc) > 1 {
				// Mark the SCC as cyclic, but only along dependency pairs that
				// already exist — never the O(k²) Cartesian product of the
				// component (AUDIT Issue 1.5 / Phase 1A-4).
				sccSet := make(map[string]bool, len(scc))
				for _, n := range scc {
					sccSet[n] = true
				}
				for _, src := range scc {
					edges := cpg.OutboundEdges[src]
					if len(edges) == 0 && cpg.db != nil {
						edges = cpg.db.GetOutboundEdges(src)
					}
					for _, e := range edges {
						if e.Type == EdgeDependsOn && sccSet[e.TargetID] {
							cpg.AddEdge(src, e.TargetID, EdgeCyclic, 0)
						}
					}
				}
			}
		}
	}

	for v := range cpg.GraphNodes {
		if _, ok := indices[v]; !ok {
			// Only run on modules/files
			node, exists := cpg.GetNode(v)
			if exists && (node.Kind == "MODULE" || node.Kind == "FILE") {
				strongconnect(v)
			}
		}
	}
}
