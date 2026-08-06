package stage2

import (
	"strings"
	"testing"
)

// TestAllNodesHaveFilePath is the W1-17 path coverage gate (A-14): every
// GAST node in the payload carries a non-empty file_path property.
func TestAllNodesHaveFilePath(t *testing.T) {
	files := make(map[string]string)
	loadRealFile(t, files, "internal/tui/programs/analyze/program.go", "internal/tui/programs/analyze/program.go")
	loadRealFile(t, files, "internal/ai_engine/agent/dispatcher.go", "internal/ai_engine/agent/dispatcher.go")

	payload := RunStage2(t, files)

	nodeCount := 0
	for rel, root := range payload.UpsertedTrees {
		var nodes []*GASTNode
		collectNodes(root, &nodes)
		for _, n := range nodes {
			nodeCount++
			path := n.Properties["file_path"]
			if path == "" {
				t.Errorf("node %q (%s) missing file_path", n.Name, n.Type)
				continue
			}
			if strings.ToLower(path) != strings.ToLower(rel) {
				t.Errorf("node %q file_path = %q, want tree key %q", n.Name, path, rel)
			}
		}
	}
	if nodeCount == 0 {
		t.Fatal("no nodes collected from fixtures")
	}
	t.Logf("verified file_path on %d GAST nodes", nodeCount)
}

// TestEnsureFilePathsSweep verifies the defensive sweep repairs nodes the
// pipeline would otherwise leave path-less (nil Properties, missing key).
func TestEnsureFilePathsSweep(t *testing.T) {
	root := &GASTNode{
		Type: GASTFileRoot,
		Name: "orphan.go",
		Children: []*GASTNode{
			{Type: GASTFunction, Name: "f", Properties: map[string]string{"other": "x"}},
			{Type: GASTTypeDeclaration, Name: "T", Properties: nil},
		},
	}
	payload := &Stage2Payload{UpsertedTrees: map[string]*GASTNode{"src/orphan.go": root}}

	ensureFilePaths(payload)

	if got := root.Properties["file_path"]; got != "src/orphan.go" {
		t.Errorf("root file_path = %q, want src/orphan.go", got)
	}
	if got := root.Children[0].Properties["file_path"]; got != "src/orphan.go" {
		t.Errorf("child 0 file_path = %q, want src/orphan.go", got)
	}
	if got := root.Children[1].Properties["file_path"]; got != "src/orphan.go" {
		t.Errorf("child 1 file_path = %q, want src/orphan.go (nil Properties repaired)", got)
	}
}
