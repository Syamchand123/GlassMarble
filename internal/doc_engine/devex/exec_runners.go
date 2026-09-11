// Package devex — C3 executable snippets (rustdoc model, opt-in only).
//
// Tag semantics:
//
//	gmb:snippet:example  documentation-only, NEVER executed (existing behavior).
//	gmb:snippet:exec      compile + run the fenced block below the tag.
//	gmb:snippet:exec:no_run  compile only, never run.
//
// Only blocks carrying an exec tag are executed; plain example blocks are
// untouched, so enabling execution is non-breaking for existing docs.
//
// Hidden `# `-prefixed setup lines (rustdoc convention) are kept VERBATIM
// in every runner: nothing is stripped. For go/python/ts this means a
// leading `# ...` line is passed to the toolchain as-is (and will fail
// compilation for those languages); only rust documents the `#` convention,
// and even there the line is preserved.
//
// TRUST MODEL: snippets run as child processes with the invoker's user
// privileges, no sandboxing, no network restrictions. Only execute
// CI-trusted content (same trust as the repo's own test suite).
package devex

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// ExecResult is the outcome of compiling and/or running one snippet block.
type ExecResult struct {
	Language   string // canonical language ("go", "python", "typescript", "rust", ...)
	Compiled   bool   // compile gate passed
	Ran        bool   // run gate executed (false for compile-only languages and no_run)
	Skipped    bool   // toolchain absent or language unsupported — never an error
	SkipReason string // why Skipped
	Output     string // captured stdout (+stderr), capped at 4KB
	ErrMsg     string // compiler/runtime failure text (empty on success or skip)
}

// maxSnippetOutput caps captured output at 4KB.
const maxSnippetOutput = 4 * 1024

// outputTruncatedMarker is appended when output exceeds the cap.
const outputTruncatedMarker = "\n... [output truncated at 4KB]\n"

// defaultExecTimeout is the timeout used by ExecuteSnippetBlock.
const defaultExecTimeout = 30 * time.Second

// execBlockRe matches an exec-tagged fenced block. Group 1 is ":no_run" or
// empty, group 2 is the fence language tag, group 3 is the block body.
var execBlockRe = regexp.MustCompile(
	"<!--\\s*gmb:snippet:exec(:no_run)?\\s*-->\\s*```(\\w+)[^\\S\\n]*\\n([\\s\\S]*?)```")

// execBlock holds one parsed exec-tagged snippet block.
type execBlock struct {
	noRun bool
	lang  string
	code  string
	line  int // 1-based line number of the code start in the markdown
}

// parseExecBlocks extracts exec-tagged fenced blocks from markdown.
// The fence language tag is required; blocks without one are not matched.
func parseExecBlocks(markdown string) []execBlock {
	markdown = normalizeSnippetNewlines(markdown)
	var out []execBlock
	matches := execBlockRe.FindAllStringSubmatchIndex(markdown, -1)
	for _, m := range matches {
		if len(m) < 8 {
			continue
		}
		noRun := m[2] != m[3] // group 1 present means ":no_run"
		lang := markdown[m[4]:m[5]]
		code := markdown[m[6]:m[7]]
		line := strings.Count(markdown[:m[6]], "\n") + 1
		out = append(out, execBlock{noRun: noRun, lang: lang, code: code, line: line})
	}
	return out
}

// normalizeSnippetLanguage maps a fence info/ language tag to its canonical
// runner name. It returns "" for empty or unsupported languages.
// The info string may carry extra fence metadata ("go linenums"); only the
// first whitespace-separated field is considered.
func normalizeSnippetLanguage(info string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(info)))
	if len(fields) == 0 {
		return ""
	}
	switch fields[0] {
	case "go", "golang":
		return "go"
	case "py", "python", "python3":
		return "python"
	case "ts", "typescript":
		return "typescript"
	case "rs", "rust":
		return "rust"
	default:
		return ""
	}
}

// verifyExecSnippets compiles (and runs, unless no_run) every exec-tagged
// block in markdown. Skipped blocks are ignored; compile failures and
// runtime failures each produce one SnippetError with empty SuggestedFix.
func verifyExecSnippets(markdown string) []SnippetError {
	var errs []SnippetError
	for _, b := range parseExecBlocks(markdown) {
		var res ExecResult
		if b.noRun {
			res = CompileSnippet(b.lang, b.code)
		} else {
			res = RunSnippet(b.lang, b.code, defaultExecTimeout)
		}
		if res.Skipped {
			continue
		}
		if !res.Compiled {
			errs = append(errs, SnippetError{
				LineNumber:   b.line,
				Symbol:       res.Language,
				ErrorMessage: fmt.Sprintf("executable snippet (%s) failed to compile: %s", res.Language, res.ErrMsg),
			})
			continue
		}
		if res.ErrMsg != "" {
			errs = append(errs, SnippetError{
				LineNumber:   b.line,
				Symbol:       res.Language,
				ErrorMessage: fmt.Sprintf("executable snippet (%s) failed at runtime: %s", res.Language, res.ErrMsg),
			})
		}
	}
	return errs
}

// CompileSnippet runs only the compile gate for lang.
// Unknown languages and absent toolchains yield Skipped (never an error).
func CompileSnippet(lang, code string) ExecResult {
	canonical := normalizeSnippetLanguage(lang)
	if canonical == "" {
		return ExecResult{Language: lang, Skipped: true, SkipReason: fmt.Sprintf("unsupported snippet language %q", lang)}
	}
	switch canonical {
	case "go":
		return compileGo(code)
	case "python":
		return compilePython(code)
	case "typescript":
		return compileTypeScript(code)
	case "rust":
		return compileRust(code)
	default:
		return ExecResult{Language: canonical, Skipped: true, SkipReason: fmt.Sprintf("unsupported snippet language %q", lang)}
	}
}

// RunSnippet compiles and runs the snippet, killing the process on timeout.
// Compile-only languages (typescript, rust) never run: Ran stays false and
// the compile outcome is returned.
func RunSnippet(lang, code string, timeout time.Duration) ExecResult {
	return runWithTimeout(lang, code, timeout)
}

// ExecuteSnippetBlock compiles and runs the snippet with the 30s default timeout.
func ExecuteSnippetBlock(lang, code string) ExecResult {
	return RunSnippet(lang, code, defaultExecTimeout)
}

// runWithTimeout is the unexported core behind RunSnippet, kept separate so
// tests can exercise short timeouts without waiting out the 30s default.
func runWithTimeout(lang, code string, timeout time.Duration) ExecResult {
	canonical := normalizeSnippetLanguage(lang)
	if canonical == "" {
		return ExecResult{Language: lang, Skipped: true, SkipReason: fmt.Sprintf("unsupported snippet language %q", lang)}
	}
	if timeout <= 0 {
		timeout = defaultExecTimeout
	}
	switch canonical {
	case "go":
		return runGo(code, timeout)
	case "python":
		return runPython(code, timeout)
	case "typescript", "rust":
		// Compile gate only; the run gate is not implemented for these
		// languages (tsc --noEmit / rustc --crate-type=lib). Ran=false.
		return CompileSnippet(canonical, code)
	default:
		return ExecResult{Language: canonical, Skipped: true, SkipReason: fmt.Sprintf("unsupported snippet language %q", lang)}
	}
}

// ─── helpers ───

// firstPresent returns the first name found on PATH.
func firstPresent(names ...string) (string, bool) {
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p, true
		}
	}
	return "", false
}

// nullDevice is the compile-only output sink (go build / rustc -o).
func nullDevice() string {
	if runtime.GOOS == "windows" {
		return "NUL"
	}
	return "/dev/null"
}

// capOutput truncates s to 4KB with a marker.
func capOutput(s string) string {
	if len(s) > maxSnippetOutput {
		return s[:maxSnippetOutput] + outputTruncatedMarker
	}
	return s
}

// runCmd runs name with args in dir, capped by timeout, returning combined
// stdout (+stderr section) already output-capped.
func runCmd(timeout time.Duration, dir, name string, args ...string) (output string, err error, timedOut bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	out := stdout.String()
	if stderr.Len() > 0 {
		out += "\n[stderr]\n" + stderr.String()
	}
	out = capOutput(out)
	if ctx.Err() == context.DeadlineExceeded {
		return out, ctx.Err(), true
	}
	return out, err, false
}

// writeTempFile creates dir/file with content under a fresh temp dir.
func writeTempFile(pattern, file, content string) (dir string, err error) {
	dir, err = os.MkdirTemp("", pattern)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0644); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

// ─── go ───

// wrapGoMain wraps bare-statement snippets in package main / func main like
// the existing verifier. Code already containing "package " is used as-is.
// Leading import lines are hoisted above func main so snippets that declare
// imports without a package clause still compile.
func wrapGoMain(code string) string {
	if strings.Contains(code, "package ") {
		return code
	}
	var imports []string
	var rest []string
	inImportBlock := false
	for _, line := range strings.Split(code, "\n") {
		trimmed := strings.TrimSpace(line)
		if inImportBlock {
			imports = append(imports, line)
			if strings.Contains(trimmed, ")") {
				inImportBlock = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "import (") {
			inImportBlock = !strings.Contains(trimmed, ")")
			imports = append(imports, line)
			continue
		}
		if strings.HasPrefix(trimmed, "import \"") || strings.HasPrefix(trimmed, "import '") {
			imports = append(imports, line)
			continue
		}
		rest = append(rest, line)
	}
	return "package main\n" + strings.Join(imports, "\n") + "\nfunc main() {\n" + strings.Join(rest, "\n") + "\n}\n"
}

func compileGo(code string) ExecResult {
	if _, ok := firstPresent("go"); !ok {
		return ExecResult{Language: "go", Skipped: true, SkipReason: "go toolchain not found on PATH"}
	}
	dir, err := writeTempFile("gmbsnip-go-", "main.go", wrapGoMain(code))
	if err != nil {
		return ExecResult{Language: "go", ErrMsg: fmt.Sprintf("temp file: %v", err)}
	}
	defer os.RemoveAll(dir)
	out, runErr, _ := runCmd(defaultExecTimeout, dir, "go", "build", "-o", nullDevice(), "main.go")
	if runErr != nil {
		return ExecResult{Language: "go", Compiled: false, Output: out, ErrMsg: strings.TrimSpace(out)}
	}
	return ExecResult{Language: "go", Compiled: true}
}

func runGo(code string, timeout time.Duration) ExecResult {
	if _, ok := firstPresent("go"); !ok {
		return ExecResult{Language: "go", Skipped: true, SkipReason: "go toolchain not found on PATH"}
	}
	dir, err := writeTempFile("gmbsnip-go-", "main.go", wrapGoMain(code))
	if err != nil {
		return ExecResult{Language: "go", ErrMsg: fmt.Sprintf("temp file: %v", err)}
	}
	defer os.RemoveAll(dir)
	// Build to a real binary first: it doubles as the compile gate (syntax
	// errors report Compiled=false) and lets the timeout below apply purely
	// to the snippet — killing the binary kills the snippet itself, whereas
	// `go run` would leave the child behind the killed go driver process.
	exe := filepath.Join(dir, "snippet")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	out, runErr, _ := runCmd(defaultExecTimeout, dir, "go", "build", "-o", exe, "main.go")
	if runErr != nil {
		return ExecResult{Language: "go", Compiled: false, Output: out, ErrMsg: strings.TrimSpace(out)}
	}
	out, runErr, timedOut := runCmd(timeout, dir, exe)
	if timedOut {
		return ExecResult{Language: "go", Compiled: true, Ran: true, Output: out, ErrMsg: fmt.Sprintf("timed out after %s", timeout)}
	}
	if runErr != nil {
		return ExecResult{Language: "go", Compiled: true, Ran: true, Output: out, ErrMsg: strings.TrimSpace(out)}
	}
	return ExecResult{Language: "go", Compiled: true, Ran: true, Output: out}
}

// ─── python ───

func pythonBin() (string, bool) {
	// Try python3 first, then python. Each candidate is validated with
	// --version because Windows ships stub executables on PATH that only
	// print a "Python was not found" notice and exit non-zero.
	for _, n := range []string{"python3", "python"} {
		p, err := exec.LookPath(n)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		probe := exec.CommandContext(ctx, p, "--version")
		probeErr := probe.Run()
		cancel()
		if probeErr == nil {
			return p, true
		}
	}
	return "", false
}

func compilePython(code string) ExecResult {
	bin, ok := pythonBin()
	if !ok {
		return ExecResult{Language: "python", Skipped: true, SkipReason: "python toolchain not found on PATH (tried python3, python)"}
	}
	dir, err := writeTempFile("gmbsnip-py-", "snippet.py", code)
	if err != nil {
		return ExecResult{Language: "python", ErrMsg: fmt.Sprintf("temp file: %v", err)}
	}
	defer os.RemoveAll(dir)
	out, runErr, _ := runCmd(defaultExecTimeout, dir, bin, "-m", "py_compile", "snippet.py")
	if runErr != nil {
		return ExecResult{Language: "python", Compiled: false, Output: out, ErrMsg: strings.TrimSpace(out)}
	}
	return ExecResult{Language: "python", Compiled: true}
}

func runPython(code string, timeout time.Duration) ExecResult {
	bin, ok := pythonBin()
	if !ok {
		return ExecResult{Language: "python", Skipped: true, SkipReason: "python toolchain not found on PATH (tried python3, python)"}
	}
	dir, err := writeTempFile("gmbsnip-py-", "snippet.py", code)
	if err != nil {
		return ExecResult{Language: "python", ErrMsg: fmt.Sprintf("temp file: %v", err)}
	}
	defer os.RemoveAll(dir)
	// Compile gate first so syntax errors report as compile failures.
	if c := compilePython(code); !c.Compiled && !c.Skipped {
		return c
	}
	out, runErr, timedOut := runCmd(timeout, dir, bin, "snippet.py")
	if timedOut {
		return ExecResult{Language: "python", Compiled: true, Ran: true, Output: out, ErrMsg: fmt.Sprintf("timed out after %s", timeout)}
	}
	if runErr != nil {
		return ExecResult{Language: "python", Compiled: true, Ran: true, Output: out, ErrMsg: strings.TrimSpace(out)}
	}
	return ExecResult{Language: "python", Compiled: true, Ran: true, Output: out}
}

// ─── typescript ───

// compileTypeScript runs the tsc --noEmit compile gate. TypeScript is
// compile-only in this implementation: Ran is always false because no JS
// runtime gate is wired up yet (documented limitation).
func compileTypeScript(code string) ExecResult {
	if _, ok := firstPresent("tsc"); !ok {
		return ExecResult{Language: "typescript", Skipped: true, SkipReason: "tsc not found on PATH"}
	}
	dir, err := writeTempFile("gmbsnip-ts-", "snippet.ts", code)
	if err != nil {
		return ExecResult{Language: "typescript", ErrMsg: fmt.Sprintf("temp file: %v", err)}
	}
	defer os.RemoveAll(dir)
	out, runErr, _ := runCmd(defaultExecTimeout, dir, "tsc", "--noEmit", "snippet.ts")
	if runErr != nil {
		return ExecResult{Language: "typescript", Compiled: false, Ran: false, Output: out, ErrMsg: strings.TrimSpace(out)}
	}
	return ExecResult{Language: "typescript", Compiled: true, Ran: false}
}

// ─── rust ───

// compileRust runs the rustc --crate-type=lib compile gate. Rust is
// compile-only: Ran is always false (no test-harness execution yet).
func compileRust(code string) ExecResult {
	if _, ok := firstPresent("rustc"); !ok {
		return ExecResult{Language: "rust", Skipped: true, SkipReason: "rustc not found on PATH"}
	}
	dir, err := writeTempFile("gmbsnip-rs-", "snippet.rs", code)
	if err != nil {
		return ExecResult{Language: "rust", ErrMsg: fmt.Sprintf("temp file: %v", err)}
	}
	defer os.RemoveAll(dir)
	out, runErr, _ := runCmd(defaultExecTimeout, dir, "rustc", "--crate-type=lib", "snippet.rs", "-o", nullDevice())
	if runErr != nil {
		return ExecResult{Language: "rust", Compiled: false, Ran: false, Output: out, ErrMsg: strings.TrimSpace(out)}
	}
	return ExecResult{Language: "rust", Compiled: true, Ran: false}
}
