package devex

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestExecuteGoPass(t *testing.T) {
	code := "import \"fmt\"\nfmt.Println(\"hello-exec\")"
	res := ExecuteSnippetBlock("go", code)
	if res.Skipped {
		t.Fatalf("go runner skipped: %s", res.SkipReason)
	}
	if !res.Compiled {
		t.Fatalf("expected compiled, err: %s", res.ErrMsg)
	}
	if !res.Ran {
		t.Fatalf("expected ran")
	}
	if res.ErrMsg != "" {
		t.Fatalf("unexpected runtime error: %s", res.ErrMsg)
	}
	if !strings.Contains(res.Output, "hello-exec") {
		t.Errorf("expected output to contain hello-exec, got %q", res.Output)
	}
}

func TestExecuteGoBareStatements(t *testing.T) {
	// Bare statements without package/func/import wrappers.
	res := ExecuteSnippetBlock("go", "println(\"bare-ok\")")
	if res.Skipped {
		t.Fatalf("go runner skipped: %s", res.SkipReason)
	}
	if !res.Compiled || !res.Ran || res.ErrMsg != "" {
		t.Fatalf("bare-statement run failed: %+v", res)
	}
	if !strings.Contains(res.Output, "bare-ok") {
		t.Errorf("expected bare-ok in output, got %q", res.Output)
	}
}

func TestCompileGoFail(t *testing.T) {
	res := CompileSnippet("go", "func broken( { this is not go }")
	if res.Skipped {
		t.Fatalf("go runner skipped: %s", res.SkipReason)
	}
	if res.Compiled {
		t.Errorf("expected Compiled=false for invalid Go")
	}
	if res.ErrMsg == "" {
		t.Errorf("expected ErrMsg with compiler output")
	}
	if res.Ran {
		t.Errorf("compile-only must not run")
	}
}

func TestCompileGoNoRun(t *testing.T) {
	res := CompileSnippet("go", "import \"fmt\"\nfmt.Println(\"hi\")")
	if res.Skipped {
		t.Fatalf("go runner skipped: %s", res.SkipReason)
	}
	if !res.Compiled {
		t.Fatalf("expected compiled, err: %s", res.ErrMsg)
	}
	if res.Ran {
		t.Errorf("CompileSnippet must leave Ran=false")
	}
}

func TestUnknownLanguageSkipped(t *testing.T) {
	for _, lang := range []string{"cobol", "", "brainfuck"} {
		res := ExecuteSnippetBlock(lang, "whatever")
		if !res.Skipped {
			t.Errorf("lang %q: expected Skipped, got %+v", lang, res)
		}
		if res.SkipReason == "" {
			t.Errorf("lang %q: expected SkipReason", lang)
		}
		if res.Compiled || res.Ran || res.ErrMsg != "" {
			t.Errorf("lang %q: skipped result must not compile/run/error: %+v", lang, res)
		}
	}
}

func TestRunTimeoutHonored(t *testing.T) {
	code := "package main\nimport \"time\"\nfunc main() { time.Sleep(30 * time.Second) }"
	start := time.Now()
	res := runWithTimeout("go", code, time.Second)
	elapsed := time.Since(start)
	if res.Skipped {
		t.Fatalf("go runner skipped: %s", res.SkipReason)
	}
	if !res.Compiled || !res.Ran {
		t.Fatalf("expected compiled+ran attempt, got %+v", res)
	}
	if !strings.Contains(res.ErrMsg, "timed out") {
		t.Errorf("expected timeout ErrMsg, got %q", res.ErrMsg)
	}
	if elapsed > 20*time.Second {
		t.Errorf("timeout not honored: took %s", elapsed)
	}
}

func TestExecTagSemanticsInRepo(t *testing.T) {
	repo := t.TempDir()

	// Plain example blocks never execute, even with broken runtime code.
	plain := "# G\n<!-- gmb:snippet:example -->\n```go\npanic(\"must-not-run\")\n```\n"
	errs, err := VerifySnippetsInRepo(repo, plain, nil)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	for _, e := range errs {
		if strings.Contains(e.ErrorMessage, "executable snippet") {
			t.Errorf("plain example block must never execute: %+v", e)
		}
	}

	// exec block with passing code → no errors.
	good := "# G\n<!-- gmb:snippet:exec -->\n```go\nprintln(\"exec-ok\")\n```\n"
	errs, err = VerifySnippetsInRepo(repo, good, nil)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("expected zero errors for passing exec block, got %+v", errs)
	}

	// exec block with compile failure → one error, empty SuggestedFix.
	bad := "# G\n<!-- gmb:snippet:exec -->\n```go\nfunc broken( {\n```\n"
	errs, err = VerifySnippetsInRepo(repo, bad, nil)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	found := false
	for _, e := range errs {
		if e.Symbol == "go" && strings.Contains(e.ErrorMessage, "failed to compile") {
			found = true
			if e.SuggestedFix != "" {
				t.Errorf("exec errors must carry empty SuggestedFix, got %q", e.SuggestedFix)
			}
			if e.LineNumber <= 0 {
				t.Errorf("expected positive LineNumber, got %d", e.LineNumber)
			}
		}
	}
	if !found {
		t.Errorf("expected compile-failure exec error, got %+v", errs)
	}

	// exec:runtime failure → runtime error entry.
	runtimeFail := "# G\n<!-- gmb:snippet:exec -->\n```go\npanic(\"boom\")\n```\n"
	errs, err = VerifySnippetsInRepo(repo, runtimeFail, nil)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	found = false
	for _, e := range errs {
		if e.Symbol == "go" && strings.Contains(e.ErrorMessage, "failed at runtime") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected runtime-failure exec error, got %+v", errs)
	}

	// no_run compiles only: broken-at-runtime code passes the gate.
	noRun := "# G\n<!-- gmb:snippet:exec:no_run -->\n```go\nprintln(\"no-run-ok\")\n```\n"
	errs, err = VerifySnippetsInRepo(repo, noRun, nil)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	for _, e := range errs {
		if strings.Contains(e.ErrorMessage, "executable snippet") {
			t.Errorf("no_run passing block must not error: %+v", e)
		}
	}

	// no_run with compile failure still errors.
	noRunBad := "# G\n<!-- gmb:snippet:exec:no_run -->\n```go\nfunc broken( {\n```\n"
	errs, err = VerifySnippetsInRepo(repo, noRunBad, nil)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	found = false
	for _, e := range errs {
		if strings.Contains(e.ErrorMessage, "failed to compile") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected no_run compile error, got %+v", errs)
	}
}

func TestNormalizeSnippetLanguage(t *testing.T) {
	cases := map[string]string{
		"go": "go", "golang": "go", "Go": "go",
		"py": "python", "python": "python", "python3": "python",
		"ts": "typescript", "TypeScript": "typescript",
		"rs": "rust", "rust": "rust",
		"go linenums": "go",
		"cobol":       "", "": "", "   ": "",
	}
	for in, want := range cases {
		if got := normalizeSnippetLanguage(in); got != want {
			t.Errorf("normalizeSnippetLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToolchainAbsentSkips(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		res := CompileSnippet("typescript", "const x: number = 1;")
		if !res.Skipped || res.Compiled {
			t.Errorf("absent tsc must skip, got %+v", res)
		}
	} else {
		t.Log("tsc present; compile-path covered by toolchain")
	}
	if _, err := exec.LookPath("rustc"); err != nil {
		res := CompileSnippet("rust", "fn main() {}")
		if !res.Skipped || res.Compiled {
			t.Errorf("absent rustc must skip, got %+v", res)
		}
	} else {
		t.Log("rustc present; compile-path covered by toolchain")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err2 := exec.LookPath("python"); err2 != nil {
			res := ExecuteSnippetBlock("python", "print('hi')")
			if !res.Skipped {
				t.Errorf("absent python must skip, got %+v", res)
			}
		}
	}
}

func TestPythonRoundTrip(t *testing.T) {
	if _, ok := pythonBin(); !ok {
		t.Skip("python toolchain absent")
	}
	res := ExecuteSnippetBlock("py", "print('py-ok')")
	if res.Skipped {
		t.Fatalf("unexpected skip: %s", res.SkipReason)
	}
	if !res.Compiled || !res.Ran || res.ErrMsg != "" {
		t.Fatalf("python run failed: %+v", res)
	}
	if !strings.Contains(res.Output, "py-ok") {
		t.Errorf("expected py-ok in output, got %q", res.Output)
	}
	bad := CompileSnippet("python", "def broken(:\n")
	if bad.Skipped {
		t.Skip("python went missing mid-test")
	}
	if bad.Compiled || bad.ErrMsg == "" {
		t.Errorf("expected python compile failure, got %+v", bad)
	}
}
