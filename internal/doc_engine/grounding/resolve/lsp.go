package resolve

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf16"

	"github.com/Syamchand123/GlassMarble/internal/akg"
)

// LSP scope (plan B1, pragmatic): a minimal JSON-RPC 2.0 client speaking
// LSP over gopls stdio (`gopls serve`). Per ResolveBatch call it sends
// initialize → initialized, then for each unresolved symbol a
// textDocument/didOpen (file content read from disk, once per file) plus a
// textDocument/definition at candidate code positions (identifier
// occurrences found by scanning the file, skipping comments and string
// literals — doc comments name the symbol they document, so the raw first
// textual match is usually a comment; the first candidate that resolves
// wins), and finally shutdown → exit.
//
// Guarantees: gopls is optional — when the binary is absent or any step
// fails, this stage resolves nothing and never returns an error. The total
// budget is 10s per ResolveBatch call and at most 50 symbols are
// attempted; the remainder fall through to AST.
const (
	lspTimeout    = 10 * time.Second
	maxLSPSymbols = 50
	// maxDefinitionCandidates bounds definition attempts per symbol.
	maxDefinitionCandidates = 5
)

// lspAvailable reports LSP usability per contract: a gopls binary on PATH.
func lspAvailable() bool {
	_, err := exec.LookPath("gopls")
	return err == nil
}

// lspResolve attempts definition lookup via gopls over LSP stdio. It never
// returns an error: any failure yields a partial (possibly empty) result
// map and the caller falls through to AST.
func lspResolve(ctx context.Context, repoRoot string, fqns []string, graph *akg.CodePropertyGraph) map[string]Resolution {
	out := make(map[string]Resolution)
	if len(fqns) == 0 || repoRoot == "" || !lspAvailable() {
		return out
	}
	if len(fqns) > maxLSPSymbols {
		fqns = fqns[:maxLSPSymbols]
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, lspTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gopls", "serve")
	cmd.Dir = absRoot
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return out
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return out
	}
	// Server logging is discarded; diagnostics travel over LSP.
	// (nil Stderr connects the descriptor to the null device.)
	if err := cmd.Start(); err != nil {
		return out
	}
	// Reap the server on every path; the process is also tied to ctx via
	// CommandContext, so a wedged server cannot outlive the timeout.
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}()

	conn := &lspConn{r: bufio.NewReader(stdout), w: bufio.NewWriter(stdin)}
	rootURI := fileURI(absRoot)
	initParams := map[string]any{
		"processId":    os.Getpid(),
		"rootUri":      rootURI,
		"capabilities": map[string]any{},
		"workspaceFolders": []any{
			map[string]any{"uri": rootURI, "name": filepath.Base(absRoot)},
		},
	}
	if _, err := conn.request(ctx, "initialize", initParams); err != nil {
		return out
	}
	_ = conn.notify("initialized", map[string]any{})

	opened := make(map[string]bool)
	for _, fqn := range fqns {
		if ctx.Err() != nil {
			break
		}
		if r, ok := lspDefinition(ctx, conn, absRoot, opened, fqn, graph); ok {
			out[fqn] = r
		}
	}
	_, _ = conn.request(ctx, "shutdown", nil)
	_ = conn.notify("exit", nil)
	return out
}

// lspDefinition resolves one FQN via textDocument/definition. The boolean
// result is false when the symbol cannot be attempted (unknown file,
// unreadable file, identifier absent) or the lookup fails.
func lspDefinition(ctx context.Context, conn *lspConn, absRoot string, opened map[string]bool, fqn string, graph *akg.CodePropertyGraph) (Resolution, bool) {
	var zero Resolution
	fileRel, sym := lspTarget(fqn, graph)
	if fileRel == "" || sym == "" || !strings.HasSuffix(strings.ToLower(fileRel), ".go") {
		return zero, false
	}
	absFile := filepath.Join(absRoot, filepath.FromSlash(fileRel))
	content, err := os.ReadFile(absFile)
	if err != nil {
		return zero, false
	}
	uri := fileURI(absFile)
	if !opened[uri] {
		err := conn.notify("textDocument/didOpen", map[string]any{
			"textDocument": map[string]any{
				"uri": uri, "languageId": "go", "version": 1, "text": string(content),
			},
		})
		if err != nil {
			return zero, false
		}
		opened[uri] = true
	}
	cands := codeOccurrences(string(content), sym, maxDefinitionCandidates)
	if len(cands) == 0 {
		return zero, false
	}
	for _, cand := range cands {
		if ctx.Err() != nil {
			return zero, false
		}
		raw, err := conn.request(ctx, "textDocument/definition", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": cand[0], "character": cand[1]},
		})
		if err != nil {
			return zero, false
		}
		file, start, end, ok := parseDefinition(raw, absRoot)
		if !ok {
			continue // no definition here (e.g. a leftover reference); try next
		}
		return Resolution{FQN: fqn, File: file, Line: start, EndLine: end, Provenance: ProvenanceLSP}, true
	}
	return zero, false
}

// lspTarget derives the repo-relative file and identifier for an FQN:
// "path/to/file.go::Type.Method" → ("path/to/file.go", "Method"). FQNs
// without a "::" file prefix fall back to the AKG node's recorded path.
func lspTarget(fqn string, graph *akg.CodePropertyGraph) (fileRel, sym string) {
	rest := fqn
	if i := strings.Index(fqn, "::"); i >= 0 {
		fileRel, rest = fqn[:i], fqn[i+2:]
	} else if graph != nil && graph.Nodes != nil {
		if n, ok := graph.Nodes.Get(fqn); ok && n != nil {
			fileRel = n.FileSpec.Path
		} else {
			return "", ""
		}
	} else {
		return "", ""
	}
	if i := strings.LastIndex(rest, "."); i >= 0 {
		rest = rest[i+1:]
	}
	return fileRel, rest
}

// codeOccurrences returns up to max 0-based (line, character) positions of
// sym used as an identifier in code. Occurrences inside comments and
// string/character literals are skipped — doc comments name the symbol
// they document, so the raw first textual match is usually a comment.
// Character offsets are LSP (UTF-16) units.
func codeOccurrences(content, sym string, max int) [][2]int {
	var out [][2]int
	if sym == "" || max <= 0 {
		return nil
	}
	want := []rune(sym)
	inBlock, inBacktick := false, false
	for i, raw := range strings.Split(content, "\n") {
		line := []rune(strings.TrimSuffix(raw, "\r"))
		skip := maskSpans(line, &inBlock, &inBacktick)
		for from := 0; from+len(want) <= len(line); {
			j := indexRunes(line[from:], want)
			if j < 0 {
				break
			}
			j += from
			if !inSpans(skip, j, j+len(want)) && isIdentBoundaryRunes(line, j, j+len(want)) {
				out = append(out, [2]int{i, utf16Length(line[:j])})
				if len(out) >= max {
					return out
				}
			}
			from = j + 1
		}
	}
	return out
}

// maskSpans returns rune spans covering comments and string/character
// literals on one line, threading block-comment (/* */) and raw-string
// (backtick) state across lines via inBlock/inBacktick.
func maskSpans(line []rune, inBlock, inBacktick *bool) [][2]int {
	var spans [][2]int
	segStart := -1
	openSpan := func(at int) {
		if segStart < 0 {
			segStart = at
		}
	}
	closeSpan := func(end int) {
		if segStart >= 0 && end > segStart {
			spans = append(spans, [2]int{segStart, end})
		}
		segStart = -1
	}
	if *inBlock || *inBacktick {
		segStart = 0
	}
	i := 0
	for i < len(line) {
		c := line[i]
		switch {
		case *inBlock:
			if c == '*' && i+1 < len(line) && line[i+1] == '/' {
				i += 2
				*inBlock = false
				closeSpan(i)
			} else {
				i++
			}
		case *inBacktick:
			if c == '`' {
				i++
				*inBacktick = false
				closeSpan(i)
			} else {
				i++
			}
		case c == '/' && i+1 < len(line) && line[i+1] == '/':
			openSpan(i)
			closeSpan(len(line))
			i = len(line)
		case c == '/' && i+1 < len(line) && line[i+1] == '*':
			openSpan(i)
			*inBlock = true
			i += 2
		case c == '"':
			openSpan(i)
			i++
			for i < len(line) {
				if line[i] == '\\' {
					i += 2
					continue
				}
				if line[i] == '"' {
					i++
					break
				}
				i++
			}
			closeSpan(i)
		case c == '\'':
			openSpan(i)
			i++
			for i < len(line) {
				if line[i] == '\\' {
					i += 2
					continue
				}
				if line[i] == '\'' {
					i++
					break
				}
				i++
			}
			closeSpan(i)
		case c == '`':
			openSpan(i)
			*inBacktick = true
			i++
		default:
			i++
		}
	}
	closeSpan(len(line)) // trailing unterminated block comment / raw string
	return spans
}

// indexRunes reports the first rune index of needle in hay, or -1.
func indexRunes(hay, needle []rune) int {
	if len(needle) == 0 {
		return -1
	}
outer:
	for i := 0; i+len(needle) <= len(hay); i++ {
		for j, r := range needle {
			if hay[i+j] != r {
				continue outer
			}
		}
		return i
	}
	return -1
}

// inSpans reports whether [start, end) overlaps any masked span.
func inSpans(spans [][2]int, start, end int) bool {
	for _, s := range spans {
		if start < s[1] && end > s[0] {
			return true
		}
	}
	return false
}

func isIdentBoundaryRunes(line []rune, start, end int) bool {
	if start > 0 && isIdentRune(line[start-1]) {
		return false
	}
	if end < len(line) && isIdentRune(line[end]) {
		return false
	}
	return true
}

func isIdentRune(r rune) bool {
	return r == '_' || '0' <= r && r <= '9' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || r > 0x7F
}

// utf16Length reports the length of s in UTF-16 code units, matching LSP
// character offsets.
func utf16Length(s []rune) int {
	return len(utf16.Encode(s))
}

type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}

// parseDefinition extracts the first definition location from a
// textDocument/definition result (a Location, LocationLink, or array of
// either) and rewrites its file URI to a slash-style repo-relative path
// with 1-based lines.
func parseDefinition(raw json.RawMessage, absRoot string) (file string, line, endLine int, ok bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", 0, 0, false
	}
	first := json.RawMessage(trimmed)
	if strings.HasPrefix(trimmed, "[") {
		var arr []json.RawMessage
		if err := json.Unmarshal([]byte(trimmed), &arr); err != nil || len(arr) == 0 {
			return "", 0, 0, false
		}
		first = arr[0]
	}
	var loc struct {
		URI         string   `json:"uri"`
		TargetURI   string   `json:"targetUri"`
		Range       lspRange `json:"range"`
		TargetRange lspRange `json:"targetRange"`
	}
	if err := json.Unmarshal(first, &loc); err != nil {
		return "", 0, 0, false
	}
	uri, rng := loc.URI, loc.Range
	if uri == "" {
		uri, rng = loc.TargetURI, loc.TargetRange
	}
	if uri == "" {
		return "", 0, 0, false
	}
	abs, err := filePathFromURI(uri)
	if err != nil {
		return "", 0, 0, false
	}
	rel, err := filepath.Rel(absRoot, abs)
	if err != nil {
		return "", 0, 0, false
	}
	return filepath.ToSlash(rel), rng.Start.Line + 1, rng.End.Line + 1, true
}

// fileURI renders an absolute path as a file:// URI.
func fileURI(abs string) string {
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// filePathFromURI parses a file:// URI back to an OS path.
func filePathFromURI(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	p := u.Path
	if p == "" {
		return "", fmt.Errorf("resolve: empty path in URI %q", uri)
	}
	// url.Parse keeps the leading slash on "file:///G:/..."; strip it to
	// recover the drive-letter path on Windows.
	if runtime.GOOS == "windows" && strings.HasPrefix(p, "/") && len(p) > 2 && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p), nil
}

// lspConn is a minimal JSON-RPC 2.0 client over LSP Content-Length
// framing. It is used single-threaded from the session loop, so no mutex
// guards the buffered writer.
type lspConn struct {
	r      *bufio.Reader
	w      *bufio.Writer
	nextID int64
}

func (c *lspConn) send(method string, params any, id int64, hasID bool) error {
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if hasID {
		msg["id"] = id
	}
	if params != nil {
		msg["params"] = params
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	if _, err := c.w.Write(body); err != nil {
		return err
	}
	return c.w.Flush()
}

func (c *lspConn) notify(method string, params any) error {
	return c.send(method, params, 0, false)
}

// request sends a JSON-RPC request and waits for its response, answering
// any interleaved server→client requests with null results and ignoring
// notifications so a chatty server cannot deadlock the session.
func (c *lspConn) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := atomic.AddInt64(&c.nextID, 1)
	if err := c.send(method, params, id, true); err != nil {
		return nil, err
	}
	for {
		msg, err := c.read(ctx)
		if err != nil {
			return nil, err
		}
		rawID, hasID := msg["id"]
		if _, isCall := msg["method"]; isCall && hasID {
			// Server→client request (e.g. window/workDoneProgress/create,
			// workspace/configuration): answer null and keep waiting.
			reply := []byte(`{"jsonrpc":"2.0","id":` + string(rawID) + `,"result":null}`)
			_, _ = fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(reply))
			_, _ = c.w.Write(reply)
			_ = c.w.Flush()
			continue
		}
		if !hasID {
			continue // notification; not ours
		}
		if !idMatches(rawID, id) {
			continue
		}
		if rawErr, bad := msg["error"]; bad && strings.TrimSpace(string(rawErr)) != "null" {
			return nil, fmt.Errorf("resolve: lsp %s: %s", method, string(rawErr))
		}
		rawRes, ok := msg["result"]
		if !ok {
			return nil, fmt.Errorf("resolve: lsp %s: missing result", method)
		}
		return rawRes, nil
	}
}

// read waits for one LSP message, bounded by ctx. The helper goroutine is
// transient: on timeout the server process is killed via CommandContext,
// its pipes close, and the blocked read fails out.
func (c *lspConn) read(ctx context.Context) (map[string]json.RawMessage, error) {
	type outcome struct {
		msg map[string]json.RawMessage
		err error
	}
	ch := make(chan outcome, 1)
	go func() {
		msg, err := readLSPFrame(c.r)
		ch <- outcome{msg, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case o := <-ch:
		return o.msg, o.err
	}
}

// readLSPFrame reads one Content-Length-framed LSP message.
func readLSPFrame(r *bufio.Reader) (map[string]json.RawMessage, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if i := strings.Index(line, ":"); i >= 0 {
			if strings.EqualFold(strings.TrimSpace(line[:i]), "Content-Length") {
				n, err := strconv.Atoi(strings.TrimSpace(line[i+1:]))
				if err != nil {
					return nil, fmt.Errorf("resolve: bad Content-Length %q", line)
				}
				length = n
			}
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("resolve: missing Content-Length header")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// idMatches compares a response id (JSON number or string) to ours.
func idMatches(raw json.RawMessage, want int64) bool {
	var num float64
	if err := json.Unmarshal(raw, &num); err == nil {
		return int64(num) == want
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str == strconv.FormatInt(want, 10)
	}
	return false
}
