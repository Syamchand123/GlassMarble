package akg

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

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
	SourceID   string  `json:"source_id"`
	TargetID   string  `json:"target_id"`
	Type       string  `json:"type"`
	LineNumber int     `json:"line_number,omitempty"`
	Confidence float32 `json:"confidence,omitempty"`
	IsCycle    bool    `json:"is_cycle,omitempty"`
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
			doc.Nodes = append(doc.Nodes, GraphNodeJSON{
				ID:              node.ID,
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
				key := e.SourceID + "\x00" + e.TargetID + "\x00" + string(e.Type) + "\x00" + fmt.Sprint(e.LineNumber)
				if seen[key] {
					continue
				}
				seen[key] = true
				doc.Edges = append(doc.Edges, GraphEdgeJSON{
					SourceID:   e.SourceID,
					TargetID:   e.TargetID,
					Type:       string(e.Type),
					LineNumber: e.LineNumber,
					Confidence: e.Confidence,
					IsCycle:    e.IsCycle,
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

// ImportGraphJSON parses a GraphJSON document and reconstructs an in-memory
// CodePropertyGraph. The returned graph is fully populated (nodes, edges,
// entrypoints, folder zones, metadata) but is not yet persisted.
func ImportGraphJSON(r io.Reader) (*CodePropertyGraph, error) {
	var doc GraphJSON
	dec := json.NewDecoder(r)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed to parse graph JSON: %w", err)
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
			Properties: n.Properties,
		})
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
		}
		out, _ := graph.OutboundEdges.Get(e.SourceID)
		graph.OutboundEdges = graph.OutboundEdges.Set(e.SourceID, append(out, edge))
		in, _ := graph.InboundEdges.Get(e.TargetID)
		graph.InboundEdges = graph.InboundEdges.Set(e.TargetID, append(in, edge))
	}

	for k, v := range doc.FolderZones {
		graph.FolderZones = graph.FolderZones.Set(k, v)
	}

	return graph, nil
}
