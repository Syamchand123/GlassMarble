package akg

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommitBudgetGate verifies that committing a transaction takes ≤ 8s (W2-09 / §6.3).
func TestCommitBudgetGate(t *testing.T) {
	tmpDir := t.TempDir()
	tm, err := NewAKGTransactionManager(tmpDir)
	require.NoError(t, err)

	payload := &stage4.Stage4Output{
		GraphNodes: map[string]*stage4.ResolvedNode{
			"pkg/a.go::A": {Kind: "STRUCT", Name: "A", FileSpec: stage4.LocationMeta{Path: "pkg/a.go", LineStart: 1}},
			"pkg/b.go::B": {Kind: "STRUCT", Name: "B", FileSpec: stage4.LocationMeta{Path: "pkg/b.go", LineStart: 10}},
		},
		OutboundEdges: map[string][]stage4.ResolvedEdge{
			"pkg/a.go::A": {{Type: "calls", TargetID: "pkg/b.go::B", LineNumber: 5}},
		},
	}

	start := time.Now()
	err = tm.ExecuteDeltaTransaction(payload, []string{"pkg/a.go", "pkg/b.go"})
	require.NoError(t, err)
	duration := time.Since(start)

	assert.LessOrEqual(t, duration.Seconds(), 8.0, "Transaction commit must complete within 8 seconds")

	// Check output file: the canonical state is akg.json; the legacy TTL
	// mirror is no longer written since Phase C.
	jsonPath := filepath.Join(tmpDir, "akg.json")
	_, err = os.Stat(jsonPath)
	assert.NoError(t, err, "akg.json must exist after transaction commit")

	StatePath := filepath.Join(tmpDir, "akg_state.ttl")
	_, err = os.Stat(StatePath)
	assert.True(t, os.IsNotExist(err), "akg_state.ttl must not be written after transaction commit")
}
