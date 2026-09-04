package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// serveBytes returns a server that streams n bytes in small chunks, slowly
// enough that the progress writer's 100ms redraw throttle lets several frames
// through. declareLength controls whether Content-Length is sent.
func serveBytes(n int, declareLength bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if declareLength {
			w.Header().Set("Content-Length", fmt.Sprint(n))
		}
		chunk := bytes.Repeat([]byte("x"), 1024)
		for sent := 0; sent < n; sent += len(chunk) {
			w.Write(chunk)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(20 * time.Millisecond)
		}
	}))
}

func TestDownloadProgressReportsPercentage(t *testing.T) {
	const size = 12 * 1024
	srv := serveBytes(size, true)
	defer srv.Close()

	var out bytes.Buffer
	dest := filepath.Join(t.TempDir(), "archive.bin")
	prog := &downloadProgress{w: &out, label: "  downloading"}

	if err := downloadFile(srv.URL, dest, prog); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}

	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Size() != size {
		t.Errorf("downloaded %d bytes, want %d — progress must not consume the body", fi.Size(), size)
	}

	got := out.String()
	if !strings.Contains(got, "downloading") {
		t.Errorf("progress output missing label: %q", got)
	}
	if !strings.Contains(got, "%") {
		t.Errorf("Content-Length was declared, so a percentage was expected: %q", got)
	}
	// The final frame is drawn by done() regardless of the throttle.
	if !strings.Contains(got, "100%") {
		t.Errorf("progress never reached 100%%: %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("progress line must be terminated so later output starts fresh: %q", got)
	}
}

// TestDownloadProgressWithoutContentLength: a server that declares no size
// must not produce a percentage computed from a zero denominator.
func TestDownloadProgressWithoutContentLength(t *testing.T) {
	srv := serveBytes(4*1024, false)
	defer srv.Close()

	var out bytes.Buffer
	dest := filepath.Join(t.TempDir(), "archive.bin")
	prog := &downloadProgress{w: &out, label: "  downloading"}

	if err := downloadFile(srv.URL, dest, prog); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "%") {
		t.Errorf("no Content-Length, so no percentage should be claimed: %q", got)
	}
	if !strings.Contains(got, "KB") && !strings.Contains(got, "B") {
		t.Errorf("expected a byte count instead of a percentage: %q", got)
	}
}

// TestDownloadWithoutProgressIsSilent pins the opt-out path used by --json,
// --quiet and non-TTY runs: nil progress must download normally and write
// nothing.
func TestDownloadWithoutProgressIsSilent(t *testing.T) {
	const size = 3 * 1024
	srv := serveBytes(size, true)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "archive.bin")
	if err := downloadFile(srv.URL, dest, nil); err != nil {
		t.Fatalf("downloadFile with nil progress: %v", err)
	}
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Size() != size {
		t.Errorf("downloaded %d bytes, want %d", fi.Size(), size)
	}
}
