package e2e_test

// AI and developer-memory flows against a seeded memory store and a mock
// LLM: no real analysis runs here, so the sandbox is cheap. Everything runs
// IN PROCESS (no t.Parallel anywhere).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/cmd"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// runGmbWithStdin runs one in-process command with piped stdin (the harness
// runner leaves stdin untouched, which would block chat commands). stdout and
// stderr are captured to a temp file exactly like harness.RunGmb.
func runGmbWithStdin(t *testing.T, sb *harness.Sandbox, stdin string, args ...string) string {
	t.Helper()
	harness.ResetFlags()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(sb.Root); err != nil {
		t.Fatalf("chdir %s: %v", sb.Root, err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	oldStdout, oldStderr := os.Stdout, os.Stderr
	tmp, err := os.CreateTemp("", "gmb-e2e-*.log")
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	os.Stdout, os.Stderr = tmp, tmp

	command := cmd.RootCmdForTesting()
	command.SetOut(tmp)
	command.SetErr(tmp)
	command.SetIn(strings.NewReader(stdin))
	command.SetArgs(args)
	runErr := command.Execute()

	os.Stdout, os.Stderr = oldStdout, oldStderr
	_ = tmp.Close()
	data, _ := os.ReadFile(tmpName)
	if runErr != nil {
		t.Fatalf("gmb %v failed: %v\n--- output ---\n%s", args, runErr, data)
	}
	return string(data)
}

// TestAIMemoryQueries drives memory retrieval, the learning loop (correct ->
// corrections -> re-ask) and the AI architect against a seeded memory store.
func TestAIMemoryQueries(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.SampleProject()
	sb.GitInit()

	// Seed a deterministic memory so retrieval assertions are exact.
	sb.SeedMemory("proj_e2e_ai")

	// --- memory overview ---------------------------------------------------
	gmbWant(t, sb, []string{"Developer memory", "1 event(s)", "2 component(s)"}, "memory")

	// --- memory --ask -------------------------------------------------------
	gmbWant(t, sb, []string{
		"ranked from developer memory",
		"Components:",
		"cache",
	}, "memory", "--ask", "how is the cache used")

	// --- memory --component --------------------------------------------------
	gmbWant(t, sb, []string{"Component cache", "state=", "add cache layer"}, "memory", "--component", "cache")

	// --- learning loop: correct -> audit -> re-ask ----------------------------
	// A component-name correction is recorded to the audit log even when it
	// cannot be applied to the query results (the log is append-only).
	gmbWant(t, sb, []string{"Recorded correction", "INTENT", `"cache"`, "->"},
		"memory", "--correct", "cache", "--kind", "INTENT", "--value", "keep hot entries", "--reason", "audited by e2e")
	if !sb.Exists(".glassmarble/memory/corrections.jsonl") {
		t.Errorf("corrections.jsonl was not written")
	}

	// An event-ID correction with LABEL kind is applied to query results
	// (convention learning overlay), visible in the next --ask.
	gmbWant(t, sb, []string{"Recorded correction", "LABEL", "evt_fixture_0001", "add cache layer", "->", "cache layer re-labeled"},
		"memory", "--correct", "evt_fixture_0001", "--kind", "LABEL", "--value", "cache layer re-labeled", "--reason", "renamed in review")

	gmbWant(t, sb, []string{"2 correction(s) in the audit log", "INTENT", "keep hot entries", "LABEL"}, "memory", "--corrections")

	gmbWant(t, sb, []string{"cache layer re-labeled"}, "memory", "--ask", "how is the cache used")

	// Invalid correction kind is rejected.
	out, err := gmbErr(t, sb, "memory", "--correct", "cache", "--kind", "NOPE", "--value", "x")
	if err == nil {
		t.Errorf("unknown correction kind should fail:\n%s", out)
	}

	// --- AI ask with the mock LLM ----------------------------------------------
	mock := harness.NewMockLLM(t)
	defer mock.Close()
	url := mock.Start()
	mock.DefaultText("The service layer calls the repository, which uses the cache.")

	gmbWant(t, sb, []string{"The service layer calls the repository, which uses the cache."},
		"ai", "explain the architecture",
		"--provider", "custom", "--base-url", url, "--model", "gmb-e2e", "--root-dir", sb.Root)

	// --- AI ask with tool calling (scripted tool round + final answer) ----------
	mock.Script(
		harness.MockResponse{
			Text: "",
			ToolCalls: []harness.MockToolCall{
				{ID: "call_1", Name: "akg_search", Arguments: `{"query":"cache"}`},
			},
		},
		harness.MockResponse{Text: "Found the cache layer in internal/cache."},
	)
	out = gmbWant(t, sb, []string{"Found the cache layer in internal/cache."},
		"ai", "where is the cache",
		"--provider", "custom", "--base-url", url, "--model", "gmb-e2e", "--root-dir", sb.Root)
	if !strings.Contains(out, "akg_search") && !strings.Contains(out, "tool") {
		t.Logf("tool-call trace not visible in non-verbose output (informational)")
	}

	// The mock received both rounds.
	mock.WaitFor(t, 2)
	if mock.Count() < 2 {
		t.Errorf("expected >= 2 LLM requests (tool round + answer), got %d", mock.Count())
	}

	// --- ai chat: piped stdin, session saved and listed -------------------------
	mock.Script(harness.MockResponse{Text: "Chat answer: repo is the data layer."})

	chatOut := runGmbWithStdin(t, sb, "what is the repo for?\nexit\n",
		"ai", "chat", "--provider", "custom", "--base-url", url,
		"--model", "gmb-e2e", "--root-dir", sb.Root)
	if !strings.Contains(chatOut, "Chat answer: repo is the data layer.") {
		t.Errorf("chat answer missing:\n%s", chatOut)
	}
	if !strings.Contains(chatOut, "Session ") {
		t.Errorf("chat session header missing:\n%s", chatOut)
	}

	sessDir := filepath.Join(sb.Root, ".glassmarble", "ai", "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one saved session, got %d (%v)", len(entries), err)
	}
	sessID := strings.TrimSuffix(entries[0].Name(), ".json")

	gmbWant(t, sb, []string{"1 session(s)", sessID}, "ai", "sessions", "--root-dir", sb.Root)

	// --- ai chat --session resumes the saved transcript --------------------------
	mock.Script(harness.MockResponse{Text: "Following up on the repo."})
	resumeOut := runGmbWithStdin(t, sb, "anything else?\nexit\n",
		"ai", "chat", "--session", sessID, "--provider", "custom",
		"--base-url", url, "--model", "gmb-e2e", "--root-dir", sb.Root)
	if !strings.Contains(resumeOut, "Following up on the repo.") {
		t.Errorf("resumed chat answer missing:\n%s", resumeOut)
	}

	gmbWant(t, sb, []string{"Deleted session " + sessID},
		"ai", "sessions", "--delete", sessID, "--root-dir", sb.Root)
	if _, err := os.Stat(filepath.Join(sessDir, entries[0].Name())); !os.IsNotExist(err) {
		t.Errorf("session file still present after delete")
	}
}
