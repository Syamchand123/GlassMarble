package devex

import (
	"crypto/sha256"
	"encoding/hex"
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
	"strconv"
	"strings"
	"time"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"

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
	// ChunkID is the content-hash-stable identifier (see StableChunkID).
	// Always populated; the legacy ID field is kept for filenames.
	ChunkID string `json:"chunk_id"`
	// ParentID links a section chunk to its virtual document chunk, or a
	// code chunk to the section chunk whose FileRefs referenced its file
	// (AutoMergingRetriever-style hierarchy).
	ParentID string `json:"parent_id,omitempty"`
	// Kind is "section" for doc section chunks, "code" for AST code chunks.
	Kind string `json:"kind,omitempty"`
	// Tokens is the EstimateTokens heuristic over Content.
	Tokens int `json:"tokens,omitempty"`
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
					ChunkID:     StableChunkID(doc.TargetPath, z.SectionID, z.Content),
					ParentID:    StableChunkID(doc.TargetPath, documentChunkKey, ""),
					Kind:        chunkKindSection,
					Tokens:      EstimateTokens(z.Content),
				})
			}
		}
	}

	// AST-aware code chunks (D3): for every Go source file referenced by a
	// section chunk's FileRefs that exists on disk, emit one chunk per
	// top-level declaration. Best-effort and capped; never fails the export.
	chunks = append(chunks, collectCodeChunks(repoRoot, chunks)...)

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

// ────────────────────────────────────────────────────────────────────────────
// D3: RAG export 2.0 — stable IDs, token heuristic, AST-aware code chunks
// ────────────────────────────────────────────────────────────────────────────

const (
	// chunkKindSection marks doc section chunks; chunkKindCode marks AST
	// code chunks. Document chunks are virtual (never emitted): a section
	// chunk's ParentID is the StableChunkID of its document node.
	chunkKindSection = "section"
	chunkKindCode    = "code"
	// documentChunkKey is the key component of a virtual document node's
	// stable ID: StableChunkID(docTarget, documentChunkKey, "").
	documentChunkKey = "document"
	// defaultCodeChunkBudgetTokens caps a grouped code chunk. It mirrors the
	// grounding context default scale (~1k tokens of context per section).
	defaultCodeChunkBudgetTokens = 1000
	// maxCodeChunksPerExport bounds vector-store churn per export run.
	maxCodeChunksPerExport = 200
	// maxCodeFileBytes skips oversized sources (generated files, bundles).
	maxCodeFileBytes = 256 * 1024
)

// StableChunkID returns a content-hash-stable chunk identifier: "c" + the
// first 12 hex chars of SHA256(scope + "\x00" + key + "\x00" + normalized
// content), where scope is the doc target path (sections) or the repo-
// relative source path (code), key is the section ID (sections), the
// comma-joined declared symbols (code), or "document" (virtual parents),
// and normalization is LF line endings with trailing spaces/tabs trimmed
// per line.
//
// Stability contract: the ID is a pure function of (scope, key, content) —
// no indexes, timestamps, or run counters feed the hash. The same content
// therefore yields the same ID across runs (re-exports don't churn vector
// stores) and reordered documents keep their per-chunk IDs. Any content
// change yields a new ID, which is exactly the invalidation signal an
// embedding cache needs.
func StableChunkID(scope, key, content string) string {
	sum := sha256.Sum256([]byte(scope + "\x00" + key + "\x00" + normalizeChunkContent(content)))
	return "c" + hex.EncodeToString(sum[:])[:12]
}

// normalizeChunkContent applies the canonicalization covered by the
// StableChunkID contract: CRLF/CR → LF, trailing spaces/tabs trimmed per
// line. Internal blank lines and leading indentation are significant and
// preserved.
func normalizeChunkContent(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.Join(lines, "\n")
}

// EstimateTokens returns the deterministic token heuristic for s: byte
// length divided by 4 (integer division), minimum 1. This matches the
// grounding context package convention (doc_engine/grounding/context), so
// chunk budgets and context budgets are denominated in the same unit.
func EstimateTokens(s string) int {
	if n := len(s) / 4; n > 1 {
		return n
	}
	return 1
}

// CodeChunk is a single AST-aware source chunk: one top-level declaration,
// or one type declaration grouped with its methods while the token budget
// allows. Chunks always align to AST node boundaries — never mid-node —
// which is why an oversized single declaration is emitted whole (the budget
// is a soft cap; atomicity wins).
type CodeChunk struct {
	ID       string   `json:"id"`
	ParentID string   `json:"parent_id,omitempty"`
	Kind     string   `json:"kind"`
	Symbols  []string `json:"symbols"`
	FileRefs []string `json:"file_refs,omitempty"`
	Content  string   `json:"content"`
	Tokens   int      `json:"tokens,omitempty"`
}

// ChunkGoSource splits Go source into AST-aware CodeChunks: every top-level
// func, type, and method is one chunk; methods accumulate under their type's
// chunk while the group fits maxTokens (<=0 selects the default budget) and
// split onto their own chunks when over budget. Every chunk's Content is
// prepended with enrichment headers:
//
//	// file: <repo-relative path>
//	// parent: <receiver type, or package name>
//	// imports: <comma-separated import paths>
//
// Primary path is tree-sitter-go (fault-tolerant: valid declarations around
// a syntax error are still chunked; error-containing nodes are skipped). When
// tree-sitter fails to parse outright, or parses but yields no declarations,
// it falls back to the pure go/parser path (chunkGoSourceFallback), which is
// also directly unit-testable. Total failure (both parsers reject the input)
// returns nil — never an error, so export wiring stays best-effort.
func ChunkGoSource(path string, content []byte, maxTokens int) []CodeChunk {
	if units, pkg, imports, ok := parseGoUnits(path, content); ok {
		if chunks := buildCodeChunks(path, pkg, imports, groupCodeUnits(units, maxTokens)); len(chunks) > 0 {
			return chunks
		}
		// Parsed but declaration-free (grammar gap, or only a package
		// clause): give the stdlib parser a chance before giving up.
		if fb := chunkGoSourceFallback(path, content, maxTokens); len(fb) > 0 {
			return fb
		}
		return nil
	}
	return chunkGoSourceFallback(path, content, maxTokens)
}

// codeUnit is one top-level declaration slice before budget grouping.
type codeUnit struct {
	symbols  []string // declared names, source order
	receiver string   // method receiver base type, "" otherwise
	isType   bool     // type declaration (method-grouping anchor)
	text     string   // exact source slice (node boundaries)
	line     int      // 1-based declaration start line
}

// groupCodeUnits packs units into node-atomic groups: a type declaration
// opens a group that following same-receiver methods join while the group
// fits maxTokens; anything else (funcs, const/var blocks, budget-overflow
// methods, a new type) flushes the pending group and starts fresh. A single
// unit over budget still gets its own whole chunk — never split mid-node.
func groupCodeUnits(units []codeUnit, maxTokens int) [][]codeUnit {
	if maxTokens <= 0 {
		maxTokens = defaultCodeChunkBudgetTokens
	}
	var groups [][]codeUnit
	var pending []codeUnit
	pendingKeys := make(map[string]bool)
	pendingTokens := 0
	flush := func() {
		if len(pending) > 0 {
			groups = append(groups, pending)
			pending = nil
			pendingKeys = make(map[string]bool)
			pendingTokens = 0
		}
	}
	for _, u := range units {
		ut := EstimateTokens(u.text)
		switch {
		case u.isType:
			flush()
			pending = []codeUnit{u}
			for _, s := range u.symbols {
				pendingKeys[s] = true
			}
			pendingTokens = ut
		case u.receiver != "":
			if pendingKeys[u.receiver] && pendingTokens+ut <= maxTokens {
				pending = append(pending, u)
				pendingTokens += ut
			} else {
				flush()
				pending = []codeUnit{u}
				pendingKeys[u.receiver] = true
				pendingTokens = ut
			}
		default:
			flush()
			groups = append(groups, []codeUnit{u})
		}
	}
	flush()
	return groups
}

// buildCodeChunks enriches each group (headers + stable ID + token count).
// ParentID is left empty here; the export wiring sets it to the referencing
// section chunk's ID.
func buildCodeChunks(path, pkg string, imports []string, groups [][]codeUnit) []CodeChunk {
	out := make([]CodeChunk, 0, len(groups))
	for _, g := range groups {
		parent := pkg
		for _, u := range g {
			if u.receiver != "" {
				parent = u.receiver
				break
			}
		}
		texts := make([]string, 0, len(g))
		var symbols []string
		var fileRefs []string
		seenSym := make(map[string]bool)
		for _, u := range g {
			texts = append(texts, u.text)
			for _, s := range u.symbols {
				if s == "" || seenSym[s] {
					continue
				}
				seenSym[s] = true
				symbols = append(symbols, s)
				fileRefs = append(fileRefs, fmt.Sprintf("%s@%s#%d", s, path, u.line))
			}
		}
		body := strings.Join(texts, "\n")
		content := "// file: " + path + "\n// parent: " + parent + "\n// imports: " + strings.Join(imports, ",") + "\n" + body
		out = append(out, CodeChunk{
			ID:       StableChunkID(path, strings.Join(symbols, ","), content),
			Kind:     chunkKindCode,
			Symbols:  symbols,
			FileRefs: fileRefs,
			Content:  content,
			Tokens:   EstimateTokens(content),
		})
	}
	return out
}

// parseGoUnits extracts top-level declaration units via tree-sitter-go.
// ok=false only when the parse itself fails (nil tree); a clean parse with
// no declarations returns ok=true and empty units. Nodes containing syntax
// errors are skipped so surrounding valid declarations still chunk.
func parseGoUnits(path string, content []byte) (units []codeUnit, pkg string, imports []string, ok bool) {
	p := sitter.NewParser()
	defer p.Close()
	lang := sitter.NewLanguage(tree_sitter_go.Language())
	if lang == nil {
		return nil, "", nil, false
	}
	if err := p.SetLanguage(lang); err != nil {
		return nil, "", nil, false
	}
	// Avoid ParseCtx: go-tree-sitter@v0.25.0 has a known race in its context
	// cancellation path (same reason the ingest parser avoids it).
	tree := p.Parse(content, nil)
	if tree == nil {
		return nil, "", nil, false
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil {
		return nil, "", nil, false
	}
	count := root.NamedChildCount()
	for i := uint(0); i < count; i++ {
		child := root.NamedChild(i)
		if child == nil || child.HasError() {
			continue
		}
		switch child.Kind() {
		case "package_clause":
			if n := child.NamedChildCount(); n > 0 {
				if name := child.NamedChild(0); name != nil {
					pkg = name.Utf8Text(content)
				}
			}
		case "import_declaration":
			for j, m := uint(0), child.NamedChildCount(); j < m; j++ {
				collectImportPaths(child.NamedChild(j), content, &imports)
			}
		case "comment":
			continue // doc comments ride along inside their declaration's byte range
		case "function_declaration":
			name := fieldText(child, "name", content)
			text, line, good := nodeSlice(child, content)
			if !good {
				continue
			}
			units = append(units, codeUnit{symbols: []string{name}, text: text, line: line})
		case "method_declaration":
			name := fieldText(child, "name", content)
			recv := ""
			if rl := child.ChildByFieldName("receiver"); rl != nil {
				recv = receiverBaseFromList(rl, content)
			}
			text, line, good := nodeSlice(child, content)
			if !good {
				continue
			}
			units = append(units, codeUnit{symbols: []string{name}, receiver: recv, text: text, line: line})
		case "type_declaration":
			var names []string
			for j, m := uint(0), child.NamedChildCount(); j < m; j++ {
				spec := child.NamedChild(j)
				if spec == nil {
					continue
				}
				if n := fieldText(spec, "name", content); n != "" {
					names = append(names, n)
				}
			}
			text, line, good := nodeSlice(child, content)
			if !good {
				continue
			}
			units = append(units, codeUnit{symbols: names, isType: true, text: text, line: line})
		default:
			// Any other top-level declaration (const/var blocks, …): own
			// chunk, best-effort name, so no source is silently dropped.
			name := fieldText(child, "name", content)
			text, line, good := nodeSlice(child, content)
			if !good {
				continue
			}
			var syms []string
			if name != "" {
				syms = []string{name}
			}
			units = append(units, codeUnit{symbols: syms, text: text, line: line})
		}
	}
	return units, pkg, imports, true
}

// collectImportPaths appends the unquoted path of an import_spec node; an
// import_spec_list (parenthesized import block) recurses into its specs.
func collectImportPaths(n *sitter.Node, content []byte, imports *[]string) {
	if n == nil {
		return
	}
	switch n.Kind() {
	case "import_spec":
		if pth := n.ChildByFieldName("path"); pth != nil {
			if unq, err := strconv.Unquote(pth.Utf8Text(content)); err == nil {
				*imports = append(*imports, unq)
			}
		}
	case "import_spec_list":
		for i, m := uint(0), n.NamedChildCount(); i < m; i++ {
			collectImportPaths(n.NamedChild(i), content, imports)
		}
	}
}

// fieldText returns the source text of a node's field, or "" when absent.
func fieldText(n *sitter.Node, field string, content []byte) string {
	if n == nil {
		return ""
	}
	if f := n.ChildByFieldName(field); f != nil {
		return f.Utf8Text(content)
	}
	return ""
}

// nodeSlice returns the exact source slice for a node plus its 1-based start
// line. good=false when byte offsets fall outside content (defensive; the
// parser should never produce those).
func nodeSlice(n *sitter.Node, content []byte) (text string, line int, good bool) {
	start, end := int(n.StartByte()), int(n.EndByte())
	if start < 0 || end < start || end > len(content) {
		return "", 0, false
	}
	return string(content[start:end]), int(n.StartPosition().Row) + 1, true
}

// receiverBaseFromList reduces a method receiver parameter_list — e.g.
// "(s *Server)" or "(r Repo[T])" — to its base type name ("Server", "Repo").
func receiverBaseFromList(list *sitter.Node, content []byte) string {
	if list == nil {
		return ""
	}
	for i, m := uint(0), list.NamedChildCount(); i < m; i++ {
		param := list.NamedChild(i)
		if param == nil {
			continue
		}
		t := fieldText(param, "type", content)
		if t == "" {
			t = param.Utf8Text(content)
		}
		if base := baseTypeName(t); base != "" {
			return base
		}
	}
	return ""
}

// baseTypeName strips pointers, whitespace, and generic arguments:
// "*Server" → "Server", "Repo[T]" → "Repo".
func baseTypeName(t string) string {
	t = strings.TrimSpace(strings.TrimLeft(t, "* \t"))
	if idx := strings.Index(t, "["); idx >= 0 {
		t = t[:idx]
	}
	t = strings.TrimSpace(t)
	if idx := strings.LastIndex(t, "."); idx >= 0 {
		t = t[idx+1:]
	}
	return t
}

// chunkGoSourceFallback is the pure-stdlib (go/parser) chunking path, used
// when tree-sitter fails outright and directly unit-testable as the
// no-tree-sitter fallback. Same grouping/enrichment semantics as the primary
// path: func/type/method units, receiver-grouped while under budget, header
// enrichment, stable IDs. Returns nil when go/parser also rejects the input.
func chunkGoSourceFallback(path string, content []byte, maxTokens int) []CodeChunk {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, content, 0)
	if err != nil {
		return nil
	}
	pkg := f.Name.Name
	var imports []string
	for _, imp := range f.Imports {
		if unq, err := strconv.Unquote(imp.Path.Value); err == nil {
			imports = append(imports, unq)
		}
	}
	var units []codeUnit
	for _, decl := range f.Decls {
		start, end := fset.Position(decl.Pos()), fset.Position(decl.End())
		if !start.IsValid() || !end.IsValid() || start.Offset < 0 || end.Offset < start.Offset || end.Offset > len(content) {
			continue
		}
		text := string(content[start.Offset:end.Offset])
		switch d := decl.(type) {
		case *ast.FuncDecl:
			recv := ""
			if d.Recv != nil && len(d.Recv.List) > 0 {
				recv = recvTypeName(d.Recv.List[0].Type)
			}
			units = append(units, codeUnit{symbols: []string{d.Name.Name}, receiver: recv, text: text, line: start.Line})
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue // already captured in the imports header
			}
			if d.Tok == token.TYPE {
				var names []string
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						names = append(names, ts.Name.Name)
					}
				}
				units = append(units, codeUnit{symbols: names, isType: true, text: text, line: start.Line})
			} else {
				units = append(units, codeUnit{text: text, line: start.Line})
			}
		}
	}
	if len(units) == 0 {
		return nil
	}
	return buildCodeChunks(path, pkg, imports, groupCodeUnits(units, maxTokens))
}

// collectCodeChunks emits RAGChunks of Kind "code" for Go source files
// referenced by section chunks' FileRefs ("Sym@file#line") that exist on
// disk. Deterministic (sorted files, first-referencing section wins the
// ParentID), best-effort (missing/oversized/unparseable files are skipped
// silently), and capped at maxCodeChunksPerExport chunks per export.
func collectCodeChunks(repoRoot string, sectionChunks []RAGChunk) []RAGChunk {
	type ref struct {
		docID   string
		chunkID string
	}
	byFile := make(map[string]ref)
	for _, c := range sectionChunks {
		if c.Kind != chunkKindSection {
			continue
		}
		for _, f := range resolvedFiles(c.FileRefs) {
			if _, seen := byFile[f]; !seen {
				byFile[f] = ref{docID: c.DocID, chunkID: c.ChunkID}
			}
		}
	}
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)

	now := time.Now().UTC().Format(time.RFC3339)
	var out []RAGChunk
	for _, f := range files {
		if len(out) >= maxCodeChunksPerExport {
			break
		}
		if !strings.HasSuffix(f, ".go") {
			continue // ChunkGoSource is Go-only; other languages are skipped
		}
		abs := filepath.Join(repoRoot, filepath.FromSlash(f))
		st, err := os.Stat(abs)
		if err != nil || st.IsDir() || st.Size() > maxCodeFileBytes {
			continue
		}
		src, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		r := byFile[f]
		for _, cc := range ChunkGoSource(f, src, defaultCodeChunkBudgetTokens) {
			if len(out) >= maxCodeChunksPerExport {
				break
			}
			title := f
			if len(cc.Symbols) > 0 {
				title = fmt.Sprintf("%s - %s", f, strings.Join(cc.Symbols, ", "))
			}
			out = append(out, RAGChunk{
				ID:          "code_" + strings.TrimPrefix(cc.ID, "c"),
				DocID:       r.docID,
				TargetPath:  f,
				Title:       title,
				Content:     cc.Content,
				Symbols:     cc.Symbols,
				Files:       []string{f},
				GeneratedAt: now,
				FileRefs:    cc.FileRefs,
				Metadata:    map[string]string{"language": "go"},
				ChunkID:     cc.ID,
				ParentID:    r.chunkID,
				Kind:        chunkKindCode,
				Tokens:      cc.Tokens,
			})
		}
	}
	return out
}
