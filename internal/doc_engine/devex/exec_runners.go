// Package devex — C3 executable snippets (rustdoc model, opt-in only).
//
// Tag semantics:
//
//	gmb:snippet:example  documentation-only, NEVER executed (existing behavior).
//	gmb:snippet:exec      compile + run the fenced block below the tag.
//	gmb:snippet:exec:no_run  compile only, never run.
//	gmb:snippet:exec:should_panic  run must EXIT NONZERO to pass (panic path);
//	    exit 0 is reported as `expected panic, exited 0`.
//	gmb:snippet:exec:no_run:should_panic  accepted but the panic expectation
//	    is unverifiable without execution, so only the compile gate applies.
//
// Only blocks carrying an exec tag are executed; plain example blocks are
// untouched, so enabling execution is non-breaking for existing docs.
//
// Hidden `# `-prefixed setup lines (rustdoc convention) are stripped ONLY
// for rust blocks before compilation. Rust documents the `#` convention
// natively (rustdoc hides `# `-prefixed lines from rendered docs while
// compiling them), so stripping matches what rust readers expect. Every
// other language receives its block VERBATIM: a leading `# ...` line is
// passed to the toolchain as-is (and will fail compilation for go/python/ts),
// because those ecosystems have no `#`-hides-line convention and silently
// dropping lines would mask real doc bugs.
//
// Go multi-block files share one merged compilation unit: all mergeable Go
// exec blocks of a single markdown file (statement/import-only, no `package`
// clause, no top-level `func` declarations, not no_run, not should_panic)
// are concatenated into one temp program — imports hoisted into a single
// deduplicated import block, each block body wrapped in uniquely-named funcs
// execBlock0..N called in order from main — and built+run once (semantically
// `go run`; implemented as build-then-exec so the timeout kills the snippet
// itself, mirroring runGo). Compiler/runtime diagnostics are mapped back to
// the owning block via a merged-line offset table (plus an execBlockN parse
// of runtime stacks). Blocks that cannot merge (full `package` programs,
// top-level func declarations, no_run, should_panic) fall back to per-block
// verification, as does any merged failure whose location is unmappable —
// precise per-block attribution is preferred over guessing.
//
// SuggestedFix semantics (--fix): exec failures carry a commented actionable
// marker (`// TODO(doc-fix): snippet failed (...); needs human repair`) so
// `ApplySnippetFixes` inserts a VISIBLE, greppable marker at the failing
// block instead of silently skipping the error (empty SuggestedFix means
// --fix ignores the error entirely and the doc ships broken with no trace).
// Marker-beats-skip: a marker is reviewable and searchable; a skip is not.
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
	TimedOut   bool   // run gate exceeded its timeout (a hang, not a panic)
}

// maxSnippetOutput caps captured output at 4KB.
const maxSnippetOutput = 4 * 1024

// outputTruncatedMarker is appended when output exceeds the cap.
const outputTruncatedMarker = "\n... [output truncated at 4KB]\n"

// defaultExecTimeout is the timeout used by ExecuteSnippetBlock.
const defaultExecTimeout = 30 * time.Second

// execBlockRe matches an exec-tagged fenced block. Group 1 is ":no_run" or
// empty, group 2 is ":should_panic" or empty, group 3 is the fence language
// tag, group 4 is the block body. Only the canonical suffix order
// (:no_run before :should_panic) is recognized.
var execBlockRe = regexp.MustCompile(
	"<!--\\s*gmb:snippet:exec(:no_run)?(:should_panic)?\\s*-->\\s*```(\\w+)[^\\S\\n]*\\n([\\s\\S]*?)```")

// execBlock holds one parsed exec-tagged snippet block.
type execBlock struct {
	noRun       bool
	shouldPanic bool
	lang        string
	code        string
	line        int // 1-based line number of the code start in the markdown
}

// parseExecBlocks extracts exec-tagged fenced blocks from markdown.
// The fence language tag is required; blocks without one are not matched.
func parseExecBlocks(markdown string) []execBlock {
	markdown = normalizeSnippetNewlines(markdown)
	var out []execBlock
	matches := execBlockRe.FindAllStringSubmatchIndex(markdown, -1)
	for _, m := range matches {
		if len(m) < 10 {
			continue
		}
		noRun := m[2] != m[3]       // group 1 present means ":no_run"
		shouldPanic := m[4] != m[5] // group 2 present means ":should_panic"
		lang := markdown[m[6]:m[7]]
		code := markdown[m[8]:m[9]]
		line := strings.Count(markdown[:m[8]], "\n") + 1
		out = append(out, execBlock{noRun: noRun, shouldPanic: shouldPanic, lang: lang, code: code, line: line})
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

// execFixExcerptMax caps the compiler/runtime excerpt embedded in a --fix marker.
const execFixExcerptMax = 120

// execSuggestedFix builds the --fix marker for an exec failure: a commented,
// actionable, greppable hint so `ApplySnippetFixes` inserts a visible TODO
// instead of silently skipping the error. excerpt is already trimmed to
// execFixExcerptMax chars by execFixExcerpt.
func execSuggestedFix(excerpt string) string {
	if excerpt == "" {
		excerpt = "exited 0"
	}
	return fmt.Sprintf("// TODO(doc-fix): snippet failed (%s); needs human repair", excerpt)
}

// execFixExcerpt collapses an error message to a single line capped at
// execFixExcerptMax bytes.
func execFixExcerpt(errMsg string) string {
	excerpt := strings.Join(strings.Fields(strings.TrimSpace(errMsg)), " ")
	if len(excerpt) > execFixExcerptMax {
		excerpt = excerpt[:execFixExcerptMax]
	}
	return excerpt
}

// verifyExecSnippets compiles (and runs, unless no_run) every exec-tagged
// block in markdown. Skipped blocks are ignored. Mergeable Go blocks share
// one merged compilation unit (see verifyMergedGoUnits); everything else is
// verified per block. Compile failures, runtime failures, and unfulfilled
// should_panic expectations each produce one SnippetError carrying a --fix
// marker SuggestedFix. Results are sorted by line for determinism.
func verifyExecSnippets(markdown string) []SnippetError {
	blocks := parseExecBlocks(markdown)
	var solo []execBlock
	var mergeable []execBlock
	for _, b := range blocks {
		if goMergeable(b) {
			mergeable = append(mergeable, b)
			continue
		}
		solo = append(solo, b)
	}
	var errs []SnippetError
	for _, b := range solo {
		errs = append(errs, verifyOneExecBlock(b)...)
	}
	switch len(mergeable) {
	case 0:
		// nothing merged
	case 1:
		errs = append(errs, verifyOneExecBlock(mergeable[0])...)
	default:
		errs = append(errs, verifyMergedGoUnits(mergeable)...)
	}
	sortExecErrors(errs)
	return errs
}

// sortExecErrors orders errors by line (stable) so merged-unit attribution
// cannot reshuffle multi-language diagnostics.
func sortExecErrors(errs []SnippetError) {
	for i := 1; i < len(errs); i++ {
		for j := i; j > 0 && errs[j].LineNumber < errs[j-1].LineNumber; j-- {
			errs[j], errs[j-1] = errs[j-1], errs[j]
		}
	}
}

// verifyOneExecBlock verifies a single exec-tagged block: compile gate (or
// compile+run), should_panic expectation, and --fix marker on failure.
func verifyOneExecBlock(b execBlock) []SnippetError {
	var res ExecResult
	if b.noRun {
		res = CompileSnippet(b.lang, b.code)
	} else {
		res = RunSnippet(b.lang, b.code, defaultExecTimeout)
	}
	if res.Skipped {
		return nil
	}
	if !res.Compiled {
		return []SnippetError{{
			LineNumber:   b.line,
			Symbol:       res.Language,
			ErrorMessage: fmt.Sprintf("executable snippet (%s) failed to compile: %s", res.Language, res.ErrMsg),
			SuggestedFix: execSuggestedFix(execFixExcerpt(res.ErrMsg)),
		}}
	}
	if b.shouldPanic {
		// Panic expectation needs execution: no_run blocks already passed
		// the only gate available, so there is nothing more to check.
		if b.noRun {
			return nil
		}
		// A timeout is a hang, not a panic: report it as a runtime error.
		if res.TimedOut {
			return []SnippetError{{
				LineNumber:   b.line,
				Symbol:       res.Language,
				ErrorMessage: fmt.Sprintf("executable snippet (%s) failed at runtime: %s", res.Language, res.ErrMsg),
				SuggestedFix: execSuggestedFix(execFixExcerpt(res.ErrMsg)),
			}}
		}
		if res.ErrMsg == "" {
			return []SnippetError{{
				LineNumber:   b.line,
				Symbol:       res.Language,
				ErrorMessage: fmt.Sprintf("executable snippet (%s) expected panic, exited 0", res.Language),
				SuggestedFix: execSuggestedFix("expected panic, exited 0"),
			}}
		}
		return nil // exited nonzero: panic path confirmed
	}
	if res.TimedOut || res.ErrMsg != "" {
		return []SnippetError{{
			LineNumber:   b.line,
			Symbol:       res.Language,
			ErrorMessage: fmt.Sprintf("executable snippet (%s) failed at runtime: %s", res.Language, res.ErrMsg),
			SuggestedFix: execSuggestedFix(execFixExcerpt(res.ErrMsg)),
		}}
	}
	return nil
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
// Rust is compile-only and never runs: Ran stays false and the compile
// outcome is returned. TypeScript runs via deno/node when available and
// degrades to compile-only (Skipped run) otherwise.
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
	case "typescript":
		return runTypeScript(code, timeout)
	case "rust":
		// Compile gate only; the run gate is not implemented for rust
		// (rustc --crate-type=lib). Ran=false.
		return CompileSnippet(canonical, code)
	default:
		return ExecResult{Language: canonical, Skipped: true, SkipReason: fmt.Sprintf("unsupported snippet language %q", lang)}
	}
}

// ─── merged Go compilation units ───

// execBlockFrameRe spots the owning merged block in a runtime stack trace
// (`main.execBlock2`). Heuristic: a snippet that literally names execBlockN
// could misattribute — accepted, the per-block fallback covers ambiguity.
var execBlockFrameRe = regexp.MustCompile(`execBlock(\d+)`)

// goMergedLineRe matches `main.go:LINE:` diagnostics from the merged build.
var goMergedLineRe = regexp.MustCompile(`(?m)^main\.go:(\d+):`)

// goMergeable reports whether a block can join the merged Go unit of its
// markdown file: Go, executed (not no_run), panic-free (a should_panic
// block's nonzero exit would fail the whole unit), without a `package`
// clause (full programs cannot concatenate), and without top-level `func`
// declarations (illegal nested inside the execBlockN wrapper).
func goMergeable(b execBlock) bool {
	if normalizeSnippetLanguage(b.lang) != "go" {
		return false
	}
	if b.noRun || b.shouldPanic {
		return false
	}
	if strings.Contains(b.code, "package ") {
		return false
	}
	_, rest, ok := splitGoImports(b.code)
	if !ok {
		return false
	}
	for _, line := range strings.Split(rest, "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "func ") || trimmed == "func" {
			return false
		}
	}
	return true
}

// splitGoImports hoists leading import declarations out of a bare-statement
// snippet (same grammar as wrapGoMain) and returns (importLines, rest, ok).
// ok is false on unclosed import blocks or bare `import` lines — callers
// must fall back to per-block verification.
func splitGoImports(code string) (importLines []string, rest string, ok bool) {
	var imp []string
	var restLines []string
	inImportBlock := false
	for _, line := range strings.Split(code, "\n") {
		trimmed := strings.TrimSpace(line)
		if inImportBlock {
			imp = append(imp, line)
			if strings.Contains(trimmed, ")") {
				inImportBlock = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "import (") {
			inImportBlock = !strings.Contains(trimmed, ")")
			imp = append(imp, line)
			continue
		}
		if strings.HasPrefix(trimmed, "import \"") || strings.HasPrefix(trimmed, "import '") {
			imp = append(imp, line)
			continue
		}
		if trimmed == "import" {
			return nil, "", false
		}
		restLines = append(restLines, line)
	}
	if inImportBlock {
		return nil, "", false
	}
	return imp, strings.Join(restLines, "\n"), true
}

// mergeGoImportSpecs flattens per-block import lines into one deduplicated
// spec list for a single `import (...)` block. ok is false on lines that
// are not recognizable import declarations.
func mergeGoImportSpecs(groups [][]string) (specs []string, ok bool) {
	seen := make(map[string]bool)
	for _, lines := range groups {
		inBlock := false
		for _, line := range lines {
			t := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(t, "import (") && strings.Contains(t, ")"):
				// Single-line block: import ( "fmt"; "os" ).
				inner := t[strings.Index(t, "(")+1 : strings.LastIndex(t, ")")]
				for _, part := range strings.Split(inner, ";") {
					if s := strings.TrimSpace(part); s != "" && !strings.HasPrefix(s, "//") {
						if !seen[s] {
							seen[s] = true
							specs = append(specs, s)
						}
					}
				}
			case strings.HasPrefix(t, "import ("):
				inBlock = true
			case inBlock && strings.Contains(t, ")"):
				inBlock = false
			case inBlock:
				if t == "" || strings.HasPrefix(t, "//") {
					continue
				}
				if !seen[t] {
					seen[t] = true
					specs = append(specs, t)
				}
			case strings.HasPrefix(t, "import "):
				if s := strings.TrimSpace(strings.TrimPrefix(t, "import ")); s != "" {
					if !seen[s] {
						seen[s] = true
						specs = append(specs, s)
					}
				} else {
					return nil, false
				}
			default:
				return nil, false
			}
		}
		if inBlock {
			return nil, false
		}
	}
	return specs, true
}

// mergedGoUnit is one temp program covering several markdown blocks.
type mergedGoUnit struct {
	src       string
	blocks    []execBlock
	starts    []int // 1-based merged-file line where each block body starts
	bodyLines []int // body line count per block (ownership window)
}

// buildMergedGoUnit concatenates mergeable blocks into a single program:
// one deduplicated import block, each body wrapped in execBlockN called in
// order from main. ok is false when imports are unrecognizable (per-block
// fallback).
func buildMergedGoUnit(blocks []execBlock) (*mergedGoUnit, bool) {
	groups := make([][]string, len(blocks))
	rests := make([][]string, len(blocks))
	for i, b := range blocks {
		imp, rest, ok := splitGoImports(b.code)
		if !ok {
			return nil, false
		}
		groups[i] = imp
		rests[i] = strings.Split(rest, "\n")
	}
	specs, ok := mergeGoImportSpecs(groups)
	if !ok {
		return nil, false
	}
	var sb strings.Builder
	sb.WriteString("package main\n")
	if len(specs) > 0 {
		sb.WriteString("import (\n")
		for _, s := range specs {
			sb.WriteString(s + "\n")
		}
		sb.WriteString(")\n")
	}
	unit := &mergedGoUnit{blocks: blocks}
	for i := range blocks {
		fmt.Fprintf(&sb, "func execBlock%d() {\n", i)
		unit.starts = append(unit.starts, strings.Count(sb.String(), "\n")+1)
		for _, l := range rests[i] {
			sb.WriteString(l + "\n")
		}
		unit.bodyLines = append(unit.bodyLines, len(rests[i]))
		sb.WriteString("}\n")
	}
	sb.WriteString("func main() {\n")
	for i := range blocks {
		fmt.Fprintf(&sb, "execBlock%d()\n", i)
	}
	sb.WriteString("}\n")
	unit.src = sb.String()
	return unit, true
}

// mergedBlockOwner maps a 1-based merged-file line to its block index via
// the offset table. ok is false for header/dispatch lines (unmappable).
func mergedBlockOwner(unit *mergedGoUnit, mergedLine int) (int, bool) {
	for i := range unit.blocks {
		if mergedLine >= unit.starts[i] && mergedLine < unit.starts[i]+unit.bodyLines[i] {
			return i, true
		}
	}
	return 0, false
}

// verifyExecBlocksSolo runs per-block verification over a block list.
func verifyExecBlocksSolo(blocks []execBlock) []SnippetError {
	var errs []SnippetError
	for _, b := range blocks {
		errs = append(errs, verifyOneExecBlock(b)...)
	}
	return errs
}

// verifyMergedGoUnits builds and runs all mergeable Go blocks of one file
// once. Success verifies every block; a mappable compile/runtime failure is
// attributed to its owning block; anything unmappable (header errors, blank
// stacks, timeouts, unbuildable merges) falls back to per-block runs for
// precise attribution.
func verifyMergedGoUnits(blocks []execBlock) []SnippetError {
	if _, ok := firstPresent("go"); !ok {
		return nil // every block would Skip individually
	}
	unit, ok := buildMergedGoUnit(blocks)
	if !ok {
		return verifyExecBlocksSolo(blocks)
	}
	dir, err := writeTempFile("gmbsnip-gomerge-", "main.go", unit.src)
	if err != nil {
		return verifyExecBlocksSolo(blocks)
	}
	defer os.RemoveAll(dir)
	exe := filepath.Join(dir, "snippet")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	out, runErr, _ := runCmd(defaultExecTimeout, dir, "go", "build", "-o", exe, "main.go")
	if runErr != nil {
		trimmed := strings.TrimSpace(out)
		if m := goMergedLineRe.FindStringSubmatch(trimmed); m != nil {
			var mergedLine int
			fmt.Sscanf(m[1], "%d", &mergedLine)
			if idx, ok := mergedBlockOwner(unit, mergedLine); ok {
				b := unit.blocks[idx]
				return []SnippetError{{
					LineNumber:   b.line + (mergedLine - unit.starts[idx]),
					Symbol:       "go",
					ErrorMessage: fmt.Sprintf("executable snippet (go) failed to compile: %s", trimmed),
					SuggestedFix: execSuggestedFix(execFixExcerpt(trimmed)),
				}}
			}
		}
		return verifyExecBlocksSolo(blocks)
	}
	out, runErr, timedOut := runCmd(defaultExecTimeout, dir, exe)
	if timedOut {
		// Which block hung is unknowable from a killed process: per-block
		// runs attribute the hang precisely (same cost as no merging).
		return verifyExecBlocksSolo(blocks)
	}
	if runErr != nil {
		trimmed := strings.TrimSpace(out)
		if m := execBlockFrameRe.FindStringSubmatch(trimmed); m != nil {
			var idx int
			fmt.Sscanf(m[1], "%d", &idx)
			if idx >= 0 && idx < len(unit.blocks) {
				b := unit.blocks[idx]
				return []SnippetError{{
					LineNumber:   b.line,
					Symbol:       "go",
					ErrorMessage: fmt.Sprintf("executable snippet (go) failed at runtime: %s", trimmed),
					SuggestedFix: execSuggestedFix(execFixExcerpt(trimmed)),
				}}
			}
		}
		return verifyExecBlocksSolo(blocks)
	}
	return nil
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
		return ExecResult{Language: "go", Compiled: true, Ran: true, TimedOut: true, Output: out, ErrMsg: fmt.Sprintf("timed out after %s", timeout)}
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
		return ExecResult{Language: "python", Compiled: true, Ran: true, TimedOut: true, Output: out, ErrMsg: fmt.Sprintf("timed out after %s", timeout)}
	}
	if runErr != nil {
		return ExecResult{Language: "python", Compiled: true, Ran: true, Output: out, ErrMsg: strings.TrimSpace(out)}
	}
	return ExecResult{Language: "python", Compiled: true, Ran: true, Output: out}
}

// ─── typescript ───

// compileTypeScript runs the tsc --noEmit compile gate. Ran is always false;
// execution happens only in runTypeScript after this gate passes.
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

// runTypeScript keeps the tsc compile gate first, then runs the emitted
// script: `deno run --allow-none` preferred, `node` on tsc-emitted JS as
// fallback, else Skipped (compile pass stands, Ran stays false — never an
// error). Deno executes TypeScript directly; node cannot, so the node path
// reuses tsc emit (CommonJS) before execution.
func runTypeScript(code string, timeout time.Duration) ExecResult {
	if c := CompileSnippet("typescript", code); !c.Compiled || c.Skipped {
		return c
	}
	if deno, ok := firstPresent("deno"); ok {
		return runTypeScriptWithDeno(deno, code, timeout)
	}
	if node, ok := firstPresent("node", "nodejs"); ok {
		return runTypeScriptWithNode(node, code, timeout)
	}
	return ExecResult{Language: "typescript", Compiled: true, Ran: false, Skipped: true, SkipReason: "no JS runtime on PATH (tried deno, node) — compile gate passed"}
}

// runTypeScriptWithDeno executes the snippet via `deno run --allow-none`.
func runTypeScriptWithDeno(deno, code string, timeout time.Duration) ExecResult {
	dir, err := writeTempFile("gmbsnip-ts-", "snippet.ts", code)
	if err != nil {
		return ExecResult{Language: "typescript", Compiled: true, ErrMsg: fmt.Sprintf("temp file: %v", err)}
	}
	defer os.RemoveAll(dir)
	out, runErr, timedOut := runCmd(timeout, dir, deno, "run", "--allow-none", "snippet.ts")
	if timedOut {
		return ExecResult{Language: "typescript", Compiled: true, Ran: true, TimedOut: true, Output: out, ErrMsg: fmt.Sprintf("timed out after %s", timeout)}
	}
	if runErr != nil {
		return ExecResult{Language: "typescript", Compiled: true, Ran: true, Output: out, ErrMsg: strings.TrimSpace(out)}
	}
	return ExecResult{Language: "typescript", Compiled: true, Ran: true, Output: out}
}

// runTypeScriptWithNode emits CommonJS via tsc, then executes it with node.
func runTypeScriptWithNode(node, code string, timeout time.Duration) ExecResult {
	dir, err := writeTempFile("gmbsnip-ts-", "snippet.ts", code)
	if err != nil {
		return ExecResult{Language: "typescript", Compiled: true, ErrMsg: fmt.Sprintf("temp file: %v", err)}
	}
	defer os.RemoveAll(dir)
	outDir := filepath.Join(dir, "out")
	if out, emitErr, _ := runCmd(defaultExecTimeout, dir, "tsc", "snippet.ts", "--target", "es2020", "--module", "commonjs", "--outDir", outDir); emitErr != nil {
		return ExecResult{Language: "typescript", Compiled: true, Ran: true, Output: out, ErrMsg: strings.TrimSpace(out)}
	}
	js := filepath.Join(outDir, "snippet.js")
	out, runErr, timedOut := runCmd(timeout, dir, node, js)
	if timedOut {
		return ExecResult{Language: "typescript", Compiled: true, Ran: true, TimedOut: true, Output: out, ErrMsg: fmt.Sprintf("timed out after %s", timeout)}
	}
	if runErr != nil {
		return ExecResult{Language: "typescript", Compiled: true, Ran: true, Output: out, ErrMsg: strings.TrimSpace(out)}
	}
	return ExecResult{Language: "typescript", Compiled: true, Ran: true, Output: out}
}

// ─── rust ───

// stripRustHiddenLines removes rustdoc hidden setup lines — lines starting
// with "# " (hash-space) — before compilation. This is a rust-only
// convention: rustdoc hides such lines from rendered docs while still
// compiling them, so stripping matches reader expectations. All other
// languages receive their blocks verbatim (no stripping anywhere else).
func stripRustHiddenLines(code string) string {
	lines := strings.Split(code, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// compileRust runs the rustc --crate-type=lib compile gate on the code with
// rustdoc hidden (`# `-prefixed) setup lines stripped. Rust is
// compile-only: Ran is always false (no test-harness execution yet).
func compileRust(code string) ExecResult {
	if _, ok := firstPresent("rustc"); !ok {
		return ExecResult{Language: "rust", Skipped: true, SkipReason: "rustc not found on PATH"}
	}
	dir, err := writeTempFile("gmbsnip-rs-", "snippet.rs", stripRustHiddenLines(code))
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
