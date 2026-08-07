package cmd_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/cmd"
)

func writeMockState(t *testing.T, dir string) {
	stateContent := `{
  "schema_version": 3,
  "commit_hash": "fixture",
  "version": 0,
  "entrypoints": ["src/db.go::DBStore"],
  "nodes": [
    {
      "id": "src/db.go::DBStore",
      "kind": "TYPE_DECL",
      "name": "DBStore",
      "primitive": "DATABASE",
      "file_spec": { "path": "src/db.go", "line_start": 1, "line_end": 30 }
    }
  ],
  "edges": []
}
`

	gmDir := filepath.Join(dir, ".glassmarble")
	if err := os.MkdirAll(gmDir, 0755); err != nil {
		t.Fatalf("Failed to create .glassmarble directory: %v", err)
	}

	statePath := filepath.Join(gmDir, "akg.json")
	if err := os.WriteFile(statePath, []byte(stateContent), 0644); err != nil {
		t.Fatalf("Failed to write mock state file: %v", err)
	}
}

func TestVisualizeCommand_Class(t *testing.T) {
	cmd.ResetVisualizeFlags()
	tempDir, err := os.MkdirTemp("", "cmd_visualize_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	writeMockState(t, tempDir)

	// Execute command with custom dir path
	buf := new(bytes.Buffer)
	root := cmd.Execute

	// Create new test execution command runner
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"visualize", "class", "--dir", tempDir, "--unused", "--entry", "src/db.go::DBStore"})

	if err := command.Execute(); err != nil {
		t.Fatalf("visualize class command execution failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "classDiagram") {
		t.Errorf("Expected classDiagram output header, got:\n%s", output)
	}

	if !strings.Contains(output, "class src_db_go_DBStore") {
		t.Errorf("Expected class src_db_go_DBStore in output, got:\n%s", output)
	}

	if !strings.Contains(output, "DATABASE") {
		t.Errorf("Expected DATABASE primitive in output, got:\n%s", output)
	}
	_ = root
}

func TestVisualizeCommand_SaveFlag(t *testing.T) {
	cmd.ResetVisualizeFlags()
	tempDir, err := os.MkdirTemp("", "cmd_visualize_save_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	writeMockState(t, tempDir)

	buf := new(bytes.Buffer)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"visualize", "class", "--dir", tempDir, "--unused", "--entry", "src/db.go::DBStore", "--save", "my_marble"})

	if err := command.Execute(); err != nil {
		t.Fatalf("visualize class --save execution failed: %v", err)
	}

	// Verify standard console message prints
	consoleOutput := buf.String()
	if !strings.Contains(consoleOutput, "Marble saved successfully to") {
		t.Errorf("Expected success confirmation message, got: %q", consoleOutput)
	}

	// Verify file was written inside marbles subdirectory
	saveFilePath := filepath.Join(tempDir, ".glassmarble", "marbles", "my_marble.md")
	contentBytes, err := os.ReadFile(saveFilePath)
	if err != nil {
		t.Fatalf("Saved markdown marble file could not be read: %v", err)
	}

	fileContent := string(contentBytes)
	if !strings.HasPrefix(fileContent, "```mermaid\n") {
		t.Errorf("Expected markdown code block wrapper prefix in file, got:\n%s", fileContent)
	}
	if !strings.HasSuffix(fileContent, "```\n") {
		t.Errorf("Expected markdown code block wrapper suffix in file, got:\n%s", fileContent)
	}
	if !strings.Contains(fileContent, "class src_db_go_DBStore") {
		t.Errorf("Expected class content inside the markdown code block, got:\n%s", fileContent)
	}
}

func TestVisualizeCommand_SummaryFlag(t *testing.T) {
	cmd.ResetVisualizeFlags()
	tempDir, err := os.MkdirTemp("", "cmd_visualize_summary_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	writeMockState(t, tempDir)

	buf := new(bytes.Buffer)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"visualize", "class", "--dir", tempDir, "--unused", "--entry", "src/db.go::DBStore", "--summary"})

	if err := command.Execute(); err != nil {
		t.Fatalf("visualize class --summary execution failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "=== Graph Summary ===") {
		t.Errorf("Expected summary header in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Nodes:") {
		t.Errorf("Expected Nodes count in summary, got:\n%s", output)
	}
	if !strings.Contains(output, "classDiagram") {
		t.Errorf("Expected classDiagram output after summary, got:\n%s", output)
	}
}

func TestVisualizeCommand_OutputFlag(t *testing.T) {
	cmd.ResetVisualizeFlags()
	tempDir, err := os.MkdirTemp("", "cmd_visualize_output_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	writeMockState(t, tempDir)

	outputPath := filepath.Join(tempDir, "output.md")
	buf := new(bytes.Buffer)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"visualize", "class", "--dir", tempDir, "--unused", "--entry", "src/db.go::DBStore", "--output", outputPath})

	if err := command.Execute(); err != nil {
		t.Fatalf("visualize class --output execution failed: %v", err)
	}

	// Verify nothing was written to stdout
	if buf.String() != "" {
		t.Errorf("Expected empty stdout when --output is used, got: %q", buf.String())
	}

	// Verify file was written
	contentBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Output file could not be read: %v", err)
	}
	fileContent := string(contentBytes)
	if !strings.Contains(fileContent, "classDiagram") {
		t.Errorf("Expected classDiagram in output file, got:\n%s", fileContent)
	}
	if !strings.Contains(fileContent, "class src_db_go_DBStore") {
		t.Errorf("Expected class content in output file, got:\n%s", fileContent)
	}
}

func TestVisualizeCommand_OutputIgnoredWhenSave(t *testing.T) {
	cmd.ResetVisualizeFlags()
	tempDir, err := os.MkdirTemp("", "cmd_visualize_output_save_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	writeMockState(t, tempDir)

	outputPath := filepath.Join(tempDir, "ignored_output.md")
	buf := new(bytes.Buffer)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"visualize", "class", "--dir", tempDir, "--unused", "--entry", "src/db.go::DBStore", "--save", "save_test", "--output", outputPath})

	if err := command.Execute(); err != nil {
		t.Fatalf("visualize class --save --output execution failed: %v", err)
	}

	// Save wins: stdout should have success msg, output file should NOT exist
	consoleOutput := buf.String()
	if !strings.Contains(consoleOutput, "Marble saved successfully to") {
		t.Errorf("Expected save success message, got: %q", consoleOutput)
	}

	// Verify the ignored output file does not exist
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Errorf("Expected output file %s to NOT exist when --save is used", outputPath)
	}
}

func TestVisualizeCommand_PageRankFlag(t *testing.T) {
	cmd.ResetVisualizeFlags()
	tempDir, err := os.MkdirTemp("", "cmd_visualize_pagerank_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	writeMockState(t, tempDir)

	buf := new(bytes.Buffer)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"visualize", "class", "--dir", tempDir, "--unused", "--entry", "src/db.go::DBStore", "--pagerank"})

	if err := command.Execute(); err != nil {
		t.Fatalf("visualize class --pagerank execution failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "classDiagram") {
		t.Errorf("Expected classDiagram output, got:\n%s", output)
	}
	if !strings.Contains(output, "class src_db_go_DBStore") {
		t.Errorf("Expected class content in output, got:\n%s", output)
	}
}

func TestVisualizeCommand_CommunityFlag(t *testing.T) {
	cmd.ResetVisualizeFlags()
	tempDir, err := os.MkdirTemp("", "cmd_visualize_community_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	writeMockState(t, tempDir)

	buf := new(bytes.Buffer)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"visualize", "class", "--dir", tempDir, "--unused", "--entry", "src/db.go::DBStore", "--community"})

	if err := command.Execute(); err != nil {
		t.Fatalf("visualize class --community execution failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "classDiagram") {
		t.Errorf("Expected classDiagram output, got:\n%s", output)
	}
}

func TestVisualizeCommand_SccFlag(t *testing.T) {
	cmd.ResetVisualizeFlags()
	tempDir, err := os.MkdirTemp("", "cmd_visualize_scc_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	writeMockState(t, tempDir)

	buf := new(bytes.Buffer)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"visualize", "class", "--dir", tempDir, "--unused", "--entry", "src/db.go::DBStore", "--scc"})

	if err := command.Execute(); err != nil {
		t.Fatalf("visualize class --scc execution failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "classDiagram") {
		t.Errorf("Expected classDiagram output, got:\n%s", output)
	}
}

func TestRenderMermaidToImage_ViaKroki(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cmd_render_kroki_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	var gotMarkup string
	var gotFormat string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFormat = r.URL.Path
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(r.Body)
		gotMarkup = body.String()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<svg>rendered</svg>"))
	}))
	defer srv.Close()

	outPath := filepath.Join(tempDir, "graph.svg")
	err = cmd.RenderMermaidToImageForTest("classDiagram\nclass Foo", outPath, "mermaid", srv.URL)
	if err != nil {
		t.Fatalf("RenderMermaidToImage failed: %v", err)
	}

	if gotFormat != "/mermaid/svg" {
		t.Errorf("Expected Kroki path /mermaid/svg, got %q", gotFormat)
	}
	if !strings.Contains(gotMarkup, "classDiagram") {
		t.Errorf("Expected markup sent to Kroki, got %q", gotMarkup)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("Rendered file could not be read: %v", err)
	}
	if string(content) != "<svg>rendered</svg>" {
		t.Errorf("Expected rendered SVG content, got %q", string(content))
	}
}

func TestRenderMermaidToImage_FallbackPersistsMarkup(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cmd_render_fallback_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Unreachable endpoint -> no renderer available -> markup persisted to <target>.txt
	outPath := filepath.Join(tempDir, "graph.png")
	err = cmd.RenderMermaidToImageForTest("graph TD\nA-->B", outPath, "mermaid", "http://127.0.0.1:1")
	if err == nil {
		t.Fatalf("Expected an error when no renderer is available, got nil")
	}
	if !strings.Contains(err.Error(), ".txt") {
		t.Errorf("Expected error to reference persisted markup file, got: %v", err)
	}

	if _, statErr := os.Stat(outPath + ".txt"); statErr != nil {
		t.Errorf("Expected markup persisted to %s.txt, got stat error: %v", outPath, statErr)
	}
}

func TestRenderMermaidToImage_UnsupportedFormat(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cmd_render_badfmt_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	outPath := filepath.Join(tempDir, "graph.jpg")
	err = cmd.RenderMermaidToImageForTest("graph TD\nA", outPath, "mermaid", "http://127.0.0.1:1")
	if err == nil {
		t.Fatalf("Expected an error for unsupported render format, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported render format") {
		t.Errorf("Expected unsupported render format error, got: %v", err)
	}
}
