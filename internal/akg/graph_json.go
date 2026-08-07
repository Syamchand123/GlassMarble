package akg

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

// GraphJSON is the portable, language-neutral interchange format for AKG
// snapshots (`gmb export` / `gmb import`). Unlike Turtle, it is lossless for
// edge confidence, self-loops and duplicate parallel edges, and it is trivially
// diffable / reviewable by humans and CI tooling.
type GraphJSON struct {
	SchemaVersion   int                      `json:"schema_version"`
	CommitHash      string                   `json:"commit_hash"`
	Version         uint64                   `json:"version"`
	Entrypoints     []string                 `json:"entrypoints,omitempty"`
	FolderZones     map[string]string        `json:"folder_zones,omitempty"`
	Nodes           []GraphNodeJSON          `json:"nodes"`
	Edges           []GraphEdgeJSON          `json:"edges"`
	Summary         *ArchitecturalSummary    `json:"summary,omitempty"`
	Errors          []DanglingReferenceError `json:"errors,omitempty"`
	Verified        bool                     `json:"verified,omitempty"`
	VerificationMsg string                   `json:"verification_msg,omitempty"`
}

// GraphNodeJSON is the portable form of a ResolvedNode.
type GraphNodeJSON struct {
	ID              string             `json:"id"`
	Kind            string             `json:"kind"`
	Name            string             `json:"name"`
	Primitive       string             `json:"primitive,omitempty"`
	PrimitiveScores map[string]float64 `json:"primitive_scores,omitempty"`
	FileSpec        LocationMetaJSON   `json:"file_spec"`
	Properties      map[string]string  `json:"properties,omitempty"`
}

// LocationMetaJSON mirrors stage4.LocationMeta.
type LocationMetaJSON struct {
	Path      string `json:"path"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
}

// GraphEdgeJSON is the portable form of a ResolvedEdge.
type GraphEdgeJSON struct {
	SourceID   string            `json:"source_id"`
	TargetID   string            `json:"target_id"`
	Type       string            `json:"type"`
	LineNumber int               `json:"line_number,omitempty"`
	Confidence float32           `json:"confidence,omitempty"`
	IsCycle    bool              `json:"is_cycle,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// ExportGraphJSON serializes an AKG snapshot into the portable GraphJSON
// interchange format. Nodes and edges are sorted so output is deterministic
// (stable across runs and diff-friendly for CI).
func ExportGraphJSON(graph *CodePropertyGraph, w io.Writer) error {
	if graph == nil {
		return fmt.Errorf("cannot export nil graph")
	}

	doc := GraphJSON{
		SchemaVersion:   graph.SchemaVersion,
		CommitHash:      graph.CommitHash,
		Version:         graph.Version,
		Summary:         graph.Summary,
		Errors:          graph.Errors,
		Verified:        graph.Verified,
		VerificationMsg: graph.VerificationMsg,
	}

	if graph.Entrypoints != nil {
		doc.Entrypoints = append([]string(nil), graph.Entrypoints...)
	}

	if graph.FolderZones != nil {
		doc.FolderZones = make(map[string]string)
		graph.FolderZones.Iterate(func(k, v string) {
			doc.FolderZones[k] = v
		})
	}

	if graph.Nodes != nil {
		graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
			if node == nil {
				return
			}
			// node.ID is the canonical identifier; fall back to the map key
			// when it is unset so export stays lossless even for graphs
			// built without explicit node IDs.
			outID := node.ID
			if outID == "" {
				outID = id
			}
			doc.Nodes = append(doc.Nodes, GraphNodeJSON{
				ID:              outID,
				Kind:            node.Kind,
				Name:            node.Name,
				Primitive:       node.Primitive,
				PrimitiveScores: node.PrimitiveScores,
				FileSpec: LocationMetaJSON{
					Path:      node.FileSpec.Path,
					LineStart: node.FileSpec.LineStart,
					LineEnd:   node.FileSpec.LineEnd,
				},
				Properties: node.Properties,
			})
		})
	}

	if graph.OutboundEdges != nil {
		seen := make(map[string]bool)
		graph.OutboundEdges.Iterate(func(srcID string, edges []stage4.ResolvedEdge) {
			for _, e := range edges {
				// Mirror the node fallback: the OutboundEdges map key is the
				// canonical source ID when the edge does not carry one.
				outSrc := e.SourceID
				if outSrc == "" {
					outSrc = srcID
				}
				key := outSrc + "\x00" + e.TargetID + "\x00" + string(e.Type) + "\x00" + fmt.Sprint(e.LineNumber)
				if len(e.Properties) > 0 {
					key += "\x00" + sortedPropertiesKey(e.Properties)
				}
				if seen[key] {
					continue
				}
				seen[key] = true
				doc.Edges = append(doc.Edges, GraphEdgeJSON{
					SourceID:   outSrc,
					TargetID:   e.TargetID,
					Type:       string(e.Type),
					LineNumber: e.LineNumber,
					Confidence: e.Confidence,
					IsCycle:    e.IsCycle,
					Properties: e.Properties,
				})
			}
		})
	}

	sort.Slice(doc.Nodes, func(i, j int) bool { return doc.Nodes[i].ID < doc.Nodes[j].ID })
	sort.Slice(doc.Edges, func(i, j int) bool {
		if doc.Edges[i].SourceID != doc.Edges[j].SourceID {
			return doc.Edges[i].SourceID < doc.Edges[j].SourceID
		}
		if doc.Edges[i].TargetID != doc.Edges[j].TargetID {
			return doc.Edges[i].TargetID < doc.Edges[j].TargetID
		}
		if doc.Edges[i].Type != doc.Edges[j].Type {
			return doc.Edges[i].Type < doc.Edges[j].Type
		}
		return doc.Edges[i].LineNumber < doc.Edges[j].LineNumber
	})

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// sortedPropertiesKey builds a deterministic serialization of a property map
// so edge deduplication does not collapse parallel edges that differ only in
// their properties (e.g. gm:embedding / gm:provenance facts).
func sortedPropertiesKey(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(m[k])
		b.WriteByte('\x01')
	}
	return b.String()
}

// copyStringMap returns a shallow copy of m (nil for nil) so imported graphs
// never alias decoded document memory.
func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// WriteEmptyJSONState writes a valid empty GraphJSON state document (used by
// `gmb init` so a fresh repository parses cleanly on first load).
func WriteEmptyJSONState(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", path, err)
	}
	g := NewCodePropertyGraph("initial")
	g.Version = 0
	if err := ExportGraphJSON(g, f); err != nil {
		f.Close()
		return fmt.Errorf("failed to serialize empty state: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to finalize %s: %w", path, err)
	}
	return nil
}

// StateMetadata reads commit hash, schema version and graph version from the
// canonical akg.json state file without restoring the graph.
func StateMetadata(storageDir string) (commitHash string, schemaVersion int, version uint64, err error) {
	data, err := os.ReadFile(jsonStatePath(storageDir))
	if err != nil {
		return "", 0, 0, err
	}
	var doc GraphJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", 0, 0, fmt.Errorf("failed to parse state metadata: %w", err)
	}
	return doc.CommitHash, doc.SchemaVersion, doc.Version, nil
}

// ImportGraphJSON parses a GraphJSON document and reconstructs an in-memory
// CodePropertyGraph. The returned graph is fully populated (nodes, edges,
// entrypoints, folder zones, metadata) but is not yet persisted.
func ImportGraphJSON(r io.Reader) (*CodePropertyGraph, error) {
	var doc GraphJSON
	dec := json.NewDecoder(r)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed to parse graph JSON: %w", err)
	}

	if doc.SchemaVersion < 2 && doc.SchemaVersion != 0 {
		return nil, fmt.Errorf("cannot import schema v1 graph document (schema v1 predates overhaul; minimum supported schema is v2)")
	}
	if doc.SchemaVersion > CurrentSchemaVersion {
		return nil, fmt.Errorf("cannot import schema v%d graph document (exceeds maximum supported schema version v%d)", doc.SchemaVersion, CurrentSchemaVersion)
	}

	graph := NewCodePropertyGraph(doc.CommitHash)
	graph.SchemaVersion = doc.SchemaVersion
	graph.Version = doc.Version
	graph.Summary = doc.Summary
	graph.Errors = doc.Errors
	graph.Verified = doc.Verified
	graph.VerificationMsg = doc.VerificationMsg
	graph.Entrypoints = append([]string(nil), doc.Entrypoints...)

	for _, n := range doc.Nodes {
		if n.ID == "" {
			continue
		}
		graph.Nodes = graph.Nodes.Set(n.ID, &stage4.ResolvedNode{
			ID:              n.ID,
			Kind:            n.Kind,
			Name:            n.Name,
			Primitive:       n.Primitive,
			PrimitiveScores: n.PrimitiveScores,
			FileSpec: stage4.LocationMeta{
				Path:      n.FileSpec.Path,
				LineStart: n.FileSpec.LineStart,
				LineEnd:   n.FileSpec.LineEnd,
			},
			Properties: copyStringMap(n.Properties),
		})
	}

	// Rebuild derived indexes (KindIndex, HashIndex, FileNodeIndex) exactly as
	// reconstructFromTTLFileEx does, so an imported graph is index-complete.
	kindIndex := make(map[string]map[string]bool)
	hashIndex := make(map[string][]string)
	fileNodeIndex := make(map[string]map[string]bool)
	graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		if node == nil {
			return
		}
		if kindIndex[node.Kind] == nil {
			kindIndex[node.Kind] = make(map[string]bool)
		}
		kindIndex[node.Kind][id] = true
		if h, ok := node.Properties["hash"]; ok && h != "" {
			hashIndex[h] = append(hashIndex[h], id)
		}
		normPath := normalizePath(node.FileSpec.Path)
		if normPath != "" {
			if fileNodeIndex[normPath] == nil {
				fileNodeIndex[normPath] = make(map[string]bool)
			}
			fileNodeIndex[normPath][id] = true
		}
	})
	for k, v := range kindIndex {
		graph.KindIndex = graph.KindIndex.Set(k, v)
	}
	for k, v := range hashIndex {
		graph.HashIndex = graph.HashIndex.Set(k, v)
	}
	for k, v := range fileNodeIndex {
		graph.FileNodeIndex = graph.FileNodeIndex.Set(k, v)
	}

	for _, e := range doc.Edges {
		if e.SourceID == "" || e.TargetID == "" {
			continue
		}
		edge := stage4.ResolvedEdge{
			SourceID:   e.SourceID,
			TargetID:   e.TargetID,
			Type:       stage4.RelationshipType(e.Type),
			LineNumber: e.LineNumber,
			Confidence: e.Confidence,
			IsCycle:    e.IsCycle,
			Properties: copyStringMap(e.Properties),
		}
		out, _ := graph.OutboundEdges.Get(e.SourceID)
		graph.OutboundEdges = graph.OutboundEdges.Set(e.SourceID, append(out, edge))
		in, _ := graph.InboundEdges.Get(e.TargetID)
		graph.InboundEdges = graph.InboundEdges.Set(e.TargetID, append(in, edge))
	}

	for k, v := range doc.FolderZones {
		graph.FolderZones = graph.FolderZones.Set(k, v)
	}

	if graph.SchemaVersion < CurrentSchemaVersion {
		_ = MigrateToSchemaV3(graph)
	}

	return graph, nil
}
