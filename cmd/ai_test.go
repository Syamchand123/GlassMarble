package cmd_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Syamchand123/GlassMarble/cmd"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/aiconfig"
)

func fakeCompletionsServer(t *testing.T, responseBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, responseBody)
	}))
}

// stagedCompletionsServer replays a different response per request.
func stagedCompletionsServer(t *testing.T, responses []string) *httptest.Server {
	t.Helper()
	idx := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		if idx < len(responses) {
			fmt.Fprint(w, responses[idx])
			idx++
		} else {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"(fallback)"}}],"usage":{}}`)
		}
	}))
}

// sseCompletionsServer serves SSE bodies in request order when the request
// carries "stream": true, and a plain JSON response otherwise. Request bodies
// are recorded for wire-format assertions.
func sseCompletionsServer(t *testing.T, sseBodies []string) (*httptest.Server, *[]map[string]any, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var bodies []map[string]any
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		var parsed map[string]any
		_ = json.Unmarshal(raw, &parsed)
		bodies = append(bodies, parsed)
		i := idx
		idx++
		mu.Unlock()
		streaming, _ := parsed["stream"].(bool)
		if !streaming {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"choices":[{"message":{"content":"buffered answer"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if i < len(sseBodies) {
			fmt.Fprint(w, sseBodies[i])
		} else {
			fmt.Fprint(w, "data: [DONE]\n\n")
		}
	}))
	return srv, &bodies, &mu
}

func TestAIModelsCommand(t *testing.T) {
	cmd.ResetAIFlags()
	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "models"})

	if err := command.Execute(); err != nil {
		t.Fatalf("ai models failed: %v", err)
	}

	output := buf.String()
	for _, want := range []string{"openai", "anthropic", "gemini", "deepseek", "mistral", "glm", "nvidia", "openrouter", "groq", "ollama", "custom", "key env"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if !strings.Contains(output, "* = configured provider") {
		t.Errorf("missing footer marker")
	}
}

func TestAIConfigureProjectScope(t *testing.T) {
	cmd.ResetAIFlags()
	tempDir := t.TempDir()

	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "configure", "--scope", "project", "--dir", tempDir,
		"--provider", "deepseek", "--model", "deepseek-chat", "--key", "sk-secret-123"})

	if err := command.Execute(); err != nil {
		t.Fatalf("ai configure failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "AI configuration saved to") {
		t.Errorf("missing save confirmation: %q", output)
	}
	if !strings.Contains(output, "sk-s...-123") {
		t.Errorf("expected masked key in output: %q", output)
	}

	path := filepath.Join(tempDir, aiconfig.ProjectConfigPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	content := string(data)
	for _, want := range []string{"deepseek", "deepseek-chat", "sk-secret-123"} {
		if !strings.Contains(content, want) {
			t.Errorf("config missing %q:\n%s", want, content)
		}
	}

	cfg, err := aiconfig.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() failed: %v", err)
	}
	if cfg.Provider != "deepseek" || cfg.Model != "deepseek-chat" || cfg.APIKey != "sk-secret-123" {
		t.Errorf("loaded config = %+v", cfg)
	}
}

func TestAIConfigureInvalidScope(t *testing.T) {
	cmd.ResetAIFlags()
	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "configure", "--scope", "bogus"})

	if err := command.Execute(); err == nil {
		t.Fatal("expected error for invalid scope")
	}
}

func TestAIConfigureUnknownProvider(t *testing.T) {
	cmd.ResetAIFlags()
	tempDir := t.TempDir()
	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "configure", "--scope", "project", "--dir", tempDir,
		"--provider", "nope", "--model", "m"})

	if err := command.Execute(); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestAIAskCommand(t *testing.T) {
	cmd.ResetAIFlags()
	srv := fakeCompletionsServer(t, `{"choices":[{"message":{"content":"Hello from the fake model!"}}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`)
	defer srv.Close()

	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "--provider", "custom", "--base-url", srv.URL, "--model", "test-model", "hello"})

	if err := command.Execute(); err != nil {
		t.Fatalf("ai ask failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Hello from the fake model!") {
		t.Errorf("missing model response: %q", buf.String())
	}
}

func TestAIAskNoQuestion(t *testing.T) {
	cmd.ResetAIFlags()
	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai"})

	if err := command.Execute(); err == nil {
		t.Fatal("expected error when no question is given")
	}
}

func TestAIAskUnknownProvider(t *testing.T) {
	cmd.ResetAIFlags()
	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "--provider", "nope", "--model", "m", "hello"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unknown AI provider") {
		t.Errorf("error = %v", err)
	}
}

func TestAIDoctorCommand(t *testing.T) {
	cmd.ResetAIFlags()
	srv := fakeCompletionsServer(t, `{"choices":[{"message":{"content":"OK"}}],"usage":{}}`)
	defer srv.Close()

	tempDir := t.TempDir()
	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "doctor", "--root-dir", tempDir,
		"--provider", "custom", "--base-url", srv.URL, "--model", "test-model"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected doctor to report the missing AKG as a problem")
	}
	if !strings.Contains(err.Error(), "doctor found") {
		t.Errorf("error = %v", err)
	}

	output := buf.String()
	for _, want := range []string{"AI Engine Doctor", "custom", "test-model", "Ping", "run `gmb analyze` first"} {
		if !strings.Contains(output, want) {
			t.Errorf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func TestAIDoctorCommandAllGood(t *testing.T) {
	cmd.ResetAIFlags()
	srv := fakeCompletionsServer(t, `{"choices":[{"message":{"content":"OK"}}],"usage":{}}`)
	defer srv.Close()

	tempDir := t.TempDir()
	gmDir := filepath.Join(tempDir, ".glassmarble")
	if err := os.MkdirAll(gmDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gmDir, "akg_state.ttl"), []byte("@prefix gm: <http://glassmarble.org/schema#> .\n"), 0o644); err != nil {
		t.Fatalf("write ttl: %v", err)
	}

	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "doctor", "--root-dir", tempDir,
		"--provider", "custom", "--base-url", srv.URL, "--model", "test-model"})

	if err := command.Execute(); err != nil {
		t.Fatalf("ai doctor (all good) failed: %v", err)
	}
	if !strings.Contains(buf.String(), "All checks passed.") {
		t.Errorf("missing success message: %q", buf.String())
	}
}

func TestAIChatCommandExitImmediately(t *testing.T) {
	cmd.ResetAIFlags()
	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetIn(strings.NewReader("exit\n"))
	command.SetArgs([]string{"ai", "chat", "--provider", "custom", "--base-url", "http://localhost:1", "--model", "test-model"})

	if err := command.Execute(); err != nil {
		t.Fatalf("ai chat exit failed: %v", err)
	}
}

func TestAIAgentToolCallFlow(t *testing.T) {
	cmd.ResetAIFlags()
	srv := stagedCompletionsServer(t, []string{
		`{"choices":[{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"akg_status","arguments":"{}"}}]}}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`,
		`{"choices":[{"message":{"content":"The AKG database is not present."}}],"usage":{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13}}`,
	})
	defer srv.Close()

	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "--verbose", "--root-dir", t.TempDir(),
		"--provider", "custom", "--base-url", srv.URL, "--model", "test-model",
		"what is the state of the AKG?"})

	if err := command.Execute(); err != nil {
		t.Fatalf("ai agent flow failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "The AKG database is not present.") {
		t.Errorf("missing final answer: %q", output)
	}
	if !strings.Contains(output, "→ akg_status({})") {
		t.Errorf("missing tool-call event line: %q", output)
	}
	if !strings.Contains(output, "← akg_status: error") {
		t.Errorf("missing tool-result event line: %q", output)
	}
	if !strings.Contains(output, "tool-calls=1") {
		t.Errorf("missing verbose trace: %q", output)
	}
}

func TestAINoToolsFlag(t *testing.T) {
	cmd.ResetAIFlags()
	var requestBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		requestBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Plain opinion."}}],"usage":{}}`)
	}))
	defer srv.Close()

	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "--no-tools", "--root-dir", t.TempDir(),
		"--provider", "custom", "--base-url", srv.URL, "--model", "test-model", "opinion?"})

	if err := command.Execute(); err != nil {
		t.Fatalf("ai --no-tools failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Plain opinion.") {
		t.Errorf("missing answer: %q", buf.String())
	}
	if strings.Contains(requestBody, `"tools"`) {
		t.Error("--no-tools must not declare tools in the request")
	}
}

func TestAIAgentToolsFlag(t *testing.T) {
	cmd.ResetAIFlags()
	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "--tools", "bogus", "--root-dir", t.TempDir(),
		"--provider", "custom", "--base-url", "http://localhost:1", "--model", "test-model", "x"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected error for unknown tool name")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("err = %v", err)
	}
}

func TestAIAskStreaming(t *testing.T) {
	cmd.ResetAIFlags()
	sseBody := "data: {\"choices\":[{\"delta\":{\"content\":\"The AKG is \"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"missing.\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":4,\"total_tokens\":13}}\n\n" +
		"data: [DONE]\n\n"
	srv, bodies, mu := sseCompletionsServer(t, []string{sseBody})
	defer srv.Close()

	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "--root-dir", t.TempDir(),
		"--provider", "custom", "--base-url", srv.URL, "--model", "test-model",
		"state of the AKG?"})

	if err := command.Execute(); err != nil {
		t.Fatalf("ai streaming failed: %v", err)
	}
	if !strings.Contains(buf.String(), "The AKG is missing.") {
		t.Errorf("missing streamed answer: %q", buf.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(*bodies) != 1 {
		t.Fatalf("requests = %d, want 1", len(*bodies))
	}
	if stream, _ := (*bodies)[0]["stream"].(bool); !stream {
		t.Error("request must carry stream: true")
	}
}

func TestAIAskNoStreamFlag(t *testing.T) {
	cmd.ResetAIFlags()
	srv, bodies, mu := sseCompletionsServer(t, nil)
	defer srv.Close()

	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "--no-stream", "--root-dir", t.TempDir(),
		"--provider", "custom", "--base-url", srv.URL, "--model", "test-model", "hello"})

	if err := command.Execute(); err != nil {
		t.Fatalf("ai --no-stream failed: %v", err)
	}
	if !strings.Contains(buf.String(), "buffered answer") {
		t.Errorf("missing buffered answer: %q", buf.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(*bodies) != 1 {
		t.Fatalf("requests = %d, want 1", len(*bodies))
	}
	if stream, _ := (*bodies)[0]["stream"].(bool); stream {
		t.Error("--no-stream must not set stream: true")
	}
}

func TestAIAskCostBudgetNote(t *testing.T) {
	cmd.ResetAIFlags()
	sseBody := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":500000,\"completion_tokens\":0,\"total_tokens\":500000}}\n\n" +
		"data: [DONE]\n\n"
	srv, _, _ := sseCompletionsServer(t, []string{sseBody})
	defer srv.Close()

	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "--root-dir", t.TempDir(), "--max-cost", "0.5",
		"--provider", "openai", "--key", "sk-test", "--base-url", srv.URL, "--model", "gpt-4o",
		"hello"})

	if err := command.Execute(); err != nil {
		t.Fatalf("ai cost guardrail failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "cost budget exceeded") {
		t.Errorf("missing cost-budget note: %q", output)
	}
	if !strings.Contains(output, "hi") {
		t.Errorf("streamed answer missing: %q", output)
	}
}

func TestAIAskSaveStreamingDiagram(t *testing.T) {
	cmd.ResetAIFlags()
	diagram := "```mermaid\ngraph TB\n  A --> B\n```\n"
	sseBody := "data: {\"choices\":[{\"delta\":{\"content\":\"```mermaid\\n\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"graph TB\\n  A --> B\\n\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"```\\n\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":4,\"total_tokens\":9}}\n\n" +
		"data: [DONE]\n\n"
	srv, _, _ := sseCompletionsServer(t, []string{sseBody})
	defer srv.Close()

	rootDir := t.TempDir()
	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "--root-dir", rootDir, "--save", "c4.md",
		"--provider", "custom", "--base-url", srv.URL, "--model", "test-model",
		"generate a C4 container diagram"})

	if err := command.Execute(); err != nil {
		t.Fatalf("ai --save failed: %v", err)
	}
	if strings.Contains(buf.String(), "mermaid") {
		t.Errorf("diagram markup must not be echoed to stdout: %q", buf.String())
	}
	path := filepath.Join(rootDir, ".glassmarble", "marbles", "c4.md")
	if !strings.Contains(buf.String(), "Artifact saved to "+path) {
		t.Errorf("missing path receipt: %q", buf.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("saved file missing: %v", err)
	}
	if string(data) != diagram {
		t.Errorf("saved content = %q, want %q", string(data), diagram)
	}
}

func TestAIAskSaveBufferedProse(t *testing.T) {
	cmd.ResetAIFlags()
	srv, _, _ := sseCompletionsServer(t, nil)
	defer srv.Close()

	rootDir := t.TempDir()
	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "--no-stream", "--root-dir", rootDir, "--save", "notes.md",
		"--provider", "custom", "--base-url", srv.URL, "--model", "test-model",
		"write architecture notes"})

	if err := command.Execute(); err != nil {
		t.Fatalf("ai --no-stream --save failed: %v", err)
	}
	path := filepath.Join(rootDir, ".glassmarble", "ai", "notes.md")
	if !strings.Contains(buf.String(), "Artifact saved to "+path) {
		t.Errorf("missing path receipt: %q", buf.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("saved file missing: %v", err)
	}
	if string(data) != "buffered answer" {
		t.Errorf("saved content = %q, want %q", string(data), "buffered answer")
	}
}

func TestAIAskSaveInvalidFilename(t *testing.T) {
	cmd.ResetAIFlags()
	srv, _, _ := sseCompletionsServer(t, nil)
	defer srv.Close()

	rootDir := t.TempDir()
	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "--no-stream", "--root-dir", rootDir, "--save", "../escape.md",
		"--provider", "custom", "--base-url", srv.URL, "--model", "test-model", "hi"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected error for path-traversal filename")
	}
	if !strings.Contains(err.Error(), "invalid artifact filename") {
		t.Errorf("err = %v", err)
	}
}

func TestAIChatSessionPersistence(t *testing.T) {
	cmd.ResetAIFlags()
	sseBody := "data: {\"choices\":[{\"delta\":{\"content\":\"Hi there!\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n\n" +
		"data: [DONE]\n\n"
	srv, _, _ := sseCompletionsServer(t, []string{sseBody, sseBody})
	defer srv.Close()

	rootDir := t.TempDir()
	buf := new(strings.Builder)
	command := cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetIn(strings.NewReader("hello\nexit\n"))
	command.SetArgs([]string{"ai", "chat", "--root-dir", rootDir,
		"--provider", "custom", "--base-url", srv.URL, "--model", "test-model"})

	if err := command.Execute(); err != nil {
		t.Fatalf("ai chat failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Hi there!") {
		t.Errorf("missing chat answer: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "Session ") {
		t.Errorf("missing session summary: %q", buf.String())
	}

	sessDir := filepath.Join(rootDir, ".glassmarble", "ai", "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		t.Fatalf("sessions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("sessions = %d, want 1", len(entries))
	}
	sessID := strings.TrimSuffix(entries[0].Name(), ".json")

	// gmb ai sessions lists the saved session.
	buf.Reset()
	command = cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "sessions", "--root-dir", rootDir})
	if err := command.Execute(); err != nil {
		t.Fatalf("ai sessions failed: %v", err)
	}
	if !strings.Contains(buf.String(), sessID) {
		t.Errorf("sessions output missing %q: %q", sessID, buf.String())
	}
	if !strings.Contains(buf.String(), "1 session(s)") {
		t.Errorf("missing session count: %q", buf.String())
	}

	// gmb ai sessions --delete removes it.
	buf.Reset()
	command = cmd.RootCmdForTesting()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"ai", "sessions", "--delete", sessID, "--root-dir", rootDir})
	if err := command.Execute(); err != nil {
		t.Fatalf("ai sessions --delete failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted session "+sessID) {
		t.Errorf("missing delete confirmation: %q", buf.String())
	}
	if _, err := os.Stat(filepath.Join(sessDir, entries[0].Name())); !os.IsNotExist(err) {
		t.Errorf("session file still present after delete: %v", err)
	}
}

func TestAIChatResumesHistory(t *testing.T) {
	cmd.ResetAIFlags()
	sseBody := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n\n" +
		"data: [DONE]\n\n"
	srv, bodies, mu := sseCompletionsServer(t, []string{sseBody, sseBody})
	defer srv.Close()

	rootDir := t.TempDir()

	run := func(input string) {
		buf := new(strings.Builder)
		command := cmd.RootCmdForTesting()
		command.SetOut(buf)
		command.SetErr(buf)
		command.SetIn(strings.NewReader(input))
		command.SetArgs([]string{"ai", "chat", "--root-dir", rootDir,
			"--provider", "custom", "--base-url", srv.URL, "--model", "test-model"})
		if err := command.Execute(); err != nil {
			t.Fatalf("ai chat failed: %v", err)
		}
	}

	run("first question\nexit\n")
	run("second question\nexit\n")

	mu.Lock()
	defer mu.Unlock()
	if len(*bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(*bodies))
	}
	second := (*bodies)[1]
	msgs, _ := second["messages"].([]any)
	foundPrior, foundCurrent := false, false
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "user" {
			if mm["content"] == "first question" {
				foundPrior = true
			}
			if mm["content"] == "second question" {
				foundCurrent = true
			}
		}
	}
	if !foundPrior {
		t.Error("second chat request missing the prior turn (session history not resumed)")
	}
	if !foundCurrent {
		t.Error("second chat request missing the current question")
	}
}
