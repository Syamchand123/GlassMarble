package akg

import (
	"fmt"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// MetadataV2 represents the Metadata v2 block stored in the AKG TTL database (W2-03 / §6.2).
type MetadataV2 struct {
	CommitHash      string    `json:"commit_hash"`
	SchemaVersion   int       `json:"schema_version"`
	Version         uint64    `json:"version"`
	AnalyzerVersion string    `json:"analyzer_version"`
	GeneratedAt     time.Time `json:"generated_at"`
	Views           string    `json:"views"`
	LinkLevel       string    `json:"link_level"`
	Name            string    `json:"name"`
}

// DefaultMetadataV2 initializes a MetadataV2 struct with default values.
func DefaultMetadataV2(commitHash string, version uint64) *MetadataV2 {
	return &MetadataV2{
		CommitHash:      commitHash,
		SchemaVersion:   CurrentSchemaVersion,
		Version:         version,
		AnalyzerVersion: "1.0.0-overhaul",
		GeneratedAt:     time.Now().UTC(),
		Views:           "structural",
		LinkLevel:       "architecture",
		Name:            "GlassMarble Project MetaData",
	}
}

// FormatMetadataV2 renders a MetadataV2 struct as RDF Turtle markup.
func FormatMetadataV2(m *MetadataV2) string {
	if m == nil {
		m = DefaultMetadataV2("", 1)
	}
	var sb strings.Builder
	sb.WriteString("<http://glassmarble.org/node/metadata> a gm:MetaData ;\n")
	sb.WriteString(fmt.Sprintf("    %s %q ;\n", ont.PredCommitHash, m.CommitHash))
	sb.WriteString(fmt.Sprintf("    %s %d ;\n", ont.PredSchemaVersion, m.SchemaVersion))
	sb.WriteString(fmt.Sprintf("    %s %d ;\n", ont.PredVersion, m.Version))
	sb.WriteString(fmt.Sprintf("    %s %q ;\n", ont.PredAnalyzerVersion, m.AnalyzerVersion))
	genAt := m.GeneratedAt.UTC().Format(time.RFC3339)
	if m.GeneratedAt.IsZero() {
		genAt = time.Now().UTC().Format(time.RFC3339)
	}
	sb.WriteString(fmt.Sprintf("    %s %q ;\n", ont.PredGeneratedAt, genAt))
	sb.WriteString(fmt.Sprintf("    %s %q ;\n", ont.PredViews, m.Views))
	sb.WriteString(fmt.Sprintf("    %s %q ;\n", ont.PredLinkLevel, m.LinkLevel))
	sb.WriteString(fmt.Sprintf("    %s %q .\n\n", ont.PredName, m.Name))
	return sb.String()
}
