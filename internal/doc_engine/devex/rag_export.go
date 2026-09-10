package devex

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	docconfig "github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/patcher"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
)

// RAGChunk represents a semantic section-level documentation chunk tagged with AST groundings.
type RAGChunk struct {
	ID          string            `json:"id"`
	DocID       string            `json:"doc_id"`
	TargetPath  string            `json:"target_path"`
	SectionID   string            `json:"section_id"`
	Title       string            `json:"title"`
	Content     string            `json:"content"`
	Symbols     []string          `json:"symbols"`
	Files       []string          `json:"files"`
	Diagrams    []string          `json:"diagrams"`
	Freshness   int               `json:"freshness"`
	Metadata    map[string]string `json:"metadata"`
	GeneratedAt string            `json:"generated_at"`
	// FileRefs resolves each extracted symbol to its declaring "file#line"
	// (best-effort; symbols without a repo declaration are omitted).
	FileRefs []string `json:"file_refs,omitempty"`
}

// backtickRe extracts `BacktickedIdentifiers` from chunk content.
var backtickRe = regexp.MustCompile("`([^`\\n]+)`")

// RAGSummary records statistics about exported RAG chunks.
type RAGSummary struct {
	TotalChunks int      `json:"total_chunks"`
	OutputDir   string   `json:"output_dir"`
	Format      string   `json:"format"`
	Files       []string `json:"files"`
}

// ExportKnowledgeBase builds section-level AST-grounded RAG chunks for AI agents and vector embeddings.
func ExportKnowledgeBase(repoRoot, format, outDir string) (RAGSummary, error) {
	if outDir == "" {
		outDir = ".glassmarble/rag"
	}
	absOutDir := filepath.Join(repoRoot, outDir)
	if err := os.MkdirAll(absOutDir, 0755); err != nil {
		return RAGSummary{}, err
	}

	cfg, err := docconfig.LoadDocsConfig(repoRoot)
	if err != nil {
		return RAGSummary{}, err
	}

	sm := storage.NewStateManager(docconfig.StorageDirPath(repoRoot))
	state, _ := sm.Load()

	// Symbol resolver: scan repo Go AST once, mapping exported func/type/var
	// names → declaring "file#line". Built inside the exporter (no signature
	// change) so per-chunk symbols resolve without extra caller plumbing.
	resolver := buildSymbolResolver(repoRoot)

	var chunks []RAGChunk

	for _, doc := range cfg.Documents {
		absPath := filepath.Join(repoRoot, doc.TargetPath)
		contentBytes, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}

		parsed := patcher.ParseMarkdown(string(contentBytes))
		for _, z := range parsed.Zones {
			if z.Kind == patcher.ZoneManaged || z.Kind == patcher.ZoneFrozen {
				chunkID := fmt.Sprintf("%s_%s", doc.ID, z.SectionID)

				var diagrams []string
				for _, d := range doc.Diagrams {
					diagrams = append(diagrams, d.Type)
				}

				// Per-chunk freshness: doc-level score from docs_state when
				// available (best-effort); 0 when the doc has no state entry.
				freshness := 0
				if state != nil && state.Documents != nil {
					if ds, ok := state.Documents[doc.TargetPath]; ok && ds != nil {
						freshness = ds.FreshnessScore
					}
				}

				// Per-chunk symbols: backticked identifiers in the chunk
				// content (deduped, capped at 50), each resolved to file#line.
				symbols := extractChunkSymbols(z.Content)
				var fileRefs []string
				seenRef := make(map[string]bool)
				for _, sym := range symbols {
					if loc, ok := resolver[sym]; ok && !seenRef[loc] {
						seenRef[loc] = true
						fileRefs = append(fileRefs, sym+"@"+loc)
					}
				}
				sort.Strings(fileRefs)

				// Per-chunk files: resolved declaring files first, falling
				// back to the doc scope paths when nothing resolved.
				files := resolvedFiles(fileRefs)
				if len(files) == 0 {
					files = doc.Scope.Paths
				}

				chunks = append(chunks, RAGChunk{
					ID:         chunkID,
					DocID:      doc.ID,
					TargetPath: doc.TargetPath,
					SectionID:  z.SectionID,
					Title:      fmt.Sprintf("%s - %s", doc.Title, z.SectionID),
					Content:    z.Content,
					Symbols:    symbols,
					Files:      files,
					Diagrams:   diagrams,
					Freshness:  freshness,
					Metadata: map[string]string{
						"archetype": doc.Archetype,
						"audience":  doc.Audience,
					},
					GeneratedAt: time.Now().UTC().Format(time.RFC3339),
					FileRefs:    fileRefs,
				})
			}
		}
	}

	var writtenFiles []string

	if format == "jsonl" {
		outFile := filepath.Join(absOutDir, "knowledge_base.jsonl")
		var lines []string
		for _, c := range chunks {
			data, _ := json.Marshal(c)
			lines = append(lines, string(data))
		}
		if _, err := storage.AtomicWriteFile(outFile, []byte(strings.Join(lines, "\n")+"\n")); err != nil {
			return RAGSummary{}, err
		}
		writtenFiles = append(writtenFiles, outFile)
	} else {
		// Default "rag": individual structured JSON chunks + index manifest
		for _, c := range chunks {
			cFile := filepath.Join(absOutDir, fmt.Sprintf("%s.json", c.ID))
			data, _ := json.MarshalIndent(c, "", "  ")
			if _, err := storage.AtomicWriteFile(cFile, data); err == nil {
				writtenFiles = append(writtenFiles, cFile)
			}
		}

		// Manifest index
		manifestFile := filepath.Join(absOutDir, "manifest.json")
		mData, _ := json.MarshalIndent(map[string]interface{}{
			"version":      1,
			"chunks_count": len(chunks),
			"exported_at":  time.Now().UTC().Format(time.RFC3339),
		}, "", "  ")
		_, _ = storage.AtomicWriteFile(manifestFile, mData)
		writtenFiles = append(writtenFiles, manifestFile)
	}

	return RAGSummary{
		TotalChunks: len(chunks),
		OutputDir:   outDir,
		Format:      format,
		Files:       writtenFiles,
	}, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Per-chunk symbol extraction & resolution (Pillar 37)
// ────────────────────────────────────────────────────────────────────────────

// extractChunkSymbols returns deduped backticked identifiers from chunk
// content (cap 50, deterministic order). Candidates are filtered to plausible
// Go identifiers (letters/digits/._-); single-word prose in backticks that
// fails the identifier check is skipped.
func extractChunkSymbols(content string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, m := range backtickRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 {
			continue
		}
		for _, cand := range strings.Fields(m[1]) {
			cand = strings.Trim(cand, ".,;:!?()[]\"'")
			if !isChunkIdent(cand) || seen[cand] {
				continue
			}
			seen[cand] = true
			// Store the short name (after last dot) AND the full dotted form
			// is unnecessary: resolver keys include both, short lookup covers
			// qualified references like pkg.Func.
			out = append(out, cand)
			if len(out) >= 50 {
				return out
			}
		}
	}
	return out
}

func isChunkIdent(s string) bool {
	if len(s) < 2 {
		return false
	}
	for _, r := range s {
		if r == '.' || r == '_' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// buildSymbolResolver scans repo Go files once and maps exported func, type,
// and var names → declaring "file#line" (repo-relative, slash-separated).
// Methods are indexed under both "Recv.Name" and bare "Name".
func buildSymbolResolver(repoRoot string) map[string]string {
	resolver := make(map[string]string)
	fset := token.NewFileSet()

	_ = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".glassmarble" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		node, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		rel = filepath.ToSlash(rel)
		record := func(name string, pos token.Pos) {
			if name == "" || !ast.IsExported(name) {
				return
			}
			if _, exists := resolver[name]; exists {
				return
			}
			resolver[name] = fmt.Sprintf("%s#%d", rel, fset.Position(pos).Line)
		}
		for _, decl := range node.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name == nil {
					continue
				}
				record(d.Name.Name, d.Pos())
				if d.Recv != nil && len(d.Recv.List) > 0 {
					recv := recvTypeName(d.Recv.List[0].Type)
					if recv != "" {
						record(recv+"."+d.Name.Name, d.Pos())
					}
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						record(s.Name.Name, s.Pos())
					case *ast.ValueSpec:
						for _, nm := range s.Names {
							record(nm.Name, nm.Pos())
						}
					}
				}
			}
		}
		return nil
	})

	return resolver
}

func recvTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvTypeName(t.X)
	default:
		return ""
	}
}

// resolvedFiles derives the sorted unique file list from "Sym@file#line" refs.
func resolvedFiles(fileRefs []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, ref := range fileRefs {
		at := strings.LastIndex(ref, "@")
		if at < 0 {
			continue
		}
		loc := ref[at+1:]
		file := loc
		if idx := strings.LastIndex(loc, "#"); idx >= 0 {
			file = loc[:idx]
		}
		if file == "" || seen[file] {
			continue
		}
		seen[file] = true
		out = append(out, file)
	}
	sort.Strings(out)
	return out
}
