package product

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetrySpans(t *testing.T) {
	tmpDir := t.TempDir()

	done := StartSpan("parse")
	time.Sleep(10 * time.Millisecond)
	done()

	doneCommit := StartSpan("akg-commit")
	time.Sleep(5 * time.Millisecond)
	doneCommit()

	err := SaveTelemetry(tmpDir)
	require.NoError(t, err)

	spans, err := LoadTelemetry(tmpDir)
	require.NoError(t, err)
	assert.NotEmpty(t, spans)

	foundParse := false
	foundCommit := false
	for _, s := range spans {
		if s.Name == "parse" {
			foundParse = true
			assert.GreaterOrEqual(t, s.DurationMS, 5.0)
		}
		if s.Name == "akg-commit" {
			foundCommit = true
			assert.GreaterOrEqual(t, s.DurationMS, 1.0)
		}
	}
	assert.True(t, foundParse, "parse span recorded")
	assert.True(t, foundCommit, "akg-commit span recorded")
}
