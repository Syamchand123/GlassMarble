package akg

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMetadataV2_Format(t *testing.T) {
	meta := &MetadataV2{
		CommitHash:      "abcdef123456",
		SchemaVersion:   3,
		Version:         10,
		AnalyzerVersion: "1.0.0-overhaul",
		GeneratedAt:     time.Now().UTC(),
		Views:           "structural",
		LinkLevel:       "architecture",
		Name:            "GlassMarble Project MetaData",
	}

	ttl := FormatMetadataV2(meta)
	assert.Contains(t, ttl, "abcdef123456")
	assert.Contains(t, ttl, "gm:schemaVersion 3")
	assert.Contains(t, ttl, "gm:version 10")
	assert.Contains(t, ttl, "gm:MetaData")
}
