package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// Transport abstracts MCP message I/O (Master Plan §5.1).
type Transport interface {
	ReadRequest() (*Request, error)
	WriteResponse(resp *Response) error
	Close() error
}

// StdioTransport implements Transport over stdin/stdout using newline-delimited JSON.
// It is the primary transport for Claude Desktop, Cursor, Zed, etc. (Master Plan §5.1).
type StdioTransport struct {
	scanner *bufio.Scanner
	writer  *bufio.Writer
	mu      sync.Mutex
	closed  bool
}

// NewStdioTransport creates a stdio transport with a 4 MiB scanner buffer as specified
// in the master plan (Scanner buffer: 4MB max message size).
func NewStdioTransport() *StdioTransport {
	sc := bufio.NewScanner(os.Stdin)
	const maxMessageSize = 4 * 1024 * 1024
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, maxMessageSize)
	return &StdioTransport{
		scanner: sc,
		writer:  bufio.NewWriter(os.Stdout),
	}
}

// NewStdioTransportWithIO creates a stdio transport from explicit readers/writers (testable).
func NewStdioTransportWithIO(r io.Reader, w io.Writer) *StdioTransport {
	sc := bufio.NewScanner(r)
	const maxMessageSize = 4 * 1024 * 1024
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, maxMessageSize)
	bw, ok := w.(*bufio.Writer)
	if !ok {
		bw = bufio.NewWriter(w)
	}
	return &StdioTransport{
		scanner: sc,
		writer:  bw,
	}
}

// ReadRequest reads the next newline-delimited JSON-RPC request from stdin.
// Returns io.EOF when stdin is closed.
func (t *StdioTransport) ReadRequest() (*Request, error) {
	if !t.scanner.Scan() {
		if err := t.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	line := t.scanner.Bytes()
	// Skip empty lines (clients may send keepalives).
	if len(line) == 0 {
		return t.ReadRequest()
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, fmt.Errorf("invalid JSON-RPC request: %w", err)
	}
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}
	return &req, nil
}

// WriteResponse marshals resp as JSON + newline and flushes stdout.
// All logs must go to stderr to keep this stream pure JSON-RPC.
func (t *StdioTransport) WriteResponse(resp *Response) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return fmt.Errorf("transport closed")
	}
	if resp.JSONRPC == "" {
		resp.JSONRPC = "2.0"
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	data = append(data, '\n')
	if _, err := t.writer.Write(data); err != nil {
		return err
	}
	return t.writer.Flush()
}

// Close marks the transport as closed. It does not close os.Stdin/Stdout
// to avoid interfering with process lifecycle.
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return nil
}

// Ensure StdioTransport implements Transport at compile time.
var _ Transport = (*StdioTransport)(nil)
