package devex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
}

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

	var chunks []RAGChunk

	for _, doc := range cfg.Documents {
		absPath := filepath.Join(repoRoot, doc.TargetPath)
		contentBytes, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}

		freshness := 100
		if state != nil && state.Documents != nil {
			if ds, ok := state.Documents[doc.TargetPath]; ok && ds.FreshnessScore > 0 {
				freshness = ds.FreshnessScore
			}
		}

		parsed := patcher.ParseMarkdown(string(contentBytes))
		for _, z := range parsed.Zones {
			if z.Kind == patcher.ZoneManaged || z.Kind == patcher.ZoneFrozen {
				chunkID := fmt.Sprintf("%s_%s", doc.ID, z.SectionID)

				var diagrams []string
				for _, d := range doc.Diagrams {
					diagrams = append(diagrams, d.Type)
				}

				chunks = append(chunks, RAGChunk{
					ID:         chunkID,
					DocID:      doc.ID,
					TargetPath: doc.TargetPath,
					SectionID:  z.SectionID,
					Title:      fmt.Sprintf("%s - %s", doc.Title, z.SectionID),
					Content:    z.Content,
					Symbols:    doc.Scope.EntryPoints,
					Files:      doc.Scope.Paths,
					Diagrams:   diagrams,
					Freshness:  freshness,
					Metadata: map[string]string{
						"archetype": doc.Archetype,
						"audience":  doc.Audience,
					},
					GeneratedAt: time.Now().UTC().Format(time.RFC3339),
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
