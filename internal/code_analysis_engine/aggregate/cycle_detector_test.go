package aggregate

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectArchitecturalCyclesSimple(t *testing.T) {
	queue := []LinkedCallSite{
		{SourceFilePath: "svc/handler.go", SourceFolderPath: "svc", ReceiverName: "core.Handler", MethodName: "Handle"},
		{SourceFilePath: "core/client.go", SourceFolderPath: "core", ReceiverName: "svc.Client", MethodName: "Call"},
		{SourceFilePath: "web/routes.go", SourceFolderPath: "web", ReceiverName: "svc.API", MethodName: "Get"},
	}

	DetectArchitecturalCycles(queue)

	marked := 0
	markedIdx := -1
	for i := range queue {
		if queue[i].HasPrimitive {
			marked++
			markedIdx = i
		}
	}

	assert.Equal(t, 1, marked, "exactly one back-edge in the svc<->core cycle should be flagged")
	require.True(t, markedIdx == 0 || markedIdx == 1, "flagged entry must be one of the cycle edges, got index %d", markedIdx)

	flagged := queue[markedIdx]
	require.Contains(t, flagged.Primitives, normalize.PrimCycleViolation)

	assert.False(t, queue[2].HasPrimitive)
	assert.Empty(t, queue[2].Primitives)
}

func TestDetectArchitecturalCyclesNoCycle(t *testing.T) {
	queue := []LinkedCallSite{
		{SourceFolderPath: "a", ReceiverName: "b.Service"},
		{SourceFolderPath: "b", ReceiverName: "c.Repo"},
		{SourceFolderPath: "c", ReceiverName: "d.Handler"},
	}

	DetectArchitecturalCycles(queue)

	for i := range queue {
		assert.False(t, queue[i].HasPrimitive, "entry %d should not be flagged", i)
		assert.Empty(t, queue[i].Primitives)
	}
}

func TestDetectArchitecturalCyclesEmptyQueue(t *testing.T) {
	queue := []LinkedCallSite{}
	assert.NotPanics(t, func() {
		DetectArchitecturalCycles(queue)
	})
}

func TestDetectArchitecturalCyclesNoDotReceiver(t *testing.T) {
	queue := []LinkedCallSite{
		{SourceFolderPath: "svc", ReceiverName: "core.Handler"},
		{SourceFolderPath: "core", ReceiverName: "plainName"},
	}

	DetectArchitecturalCycles(queue)

	// The second call has no dot in ReceiverName, so no target folder is guessed
	// and it cannot participate in a cycle edge.
	assert.False(t, queue[1].HasPrimitive)
	assert.Empty(t, queue[1].Primitives)
}

func TestGuessTargetFolder(t *testing.T) {
	tests := []struct {
		receiver string
		want     string
	}{
		{"com.Foo.bar", "com"},
		{"database.DB", "database"},
		{"Foo", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := guessTargetFolder(LinkedCallSite{ReceiverName: tt.receiver})
		assert.Equal(t, tt.want, got, "guessTargetFolder(%q)", tt.receiver)
	}
}
