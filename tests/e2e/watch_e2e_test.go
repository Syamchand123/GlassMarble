package e2e_test

// True end-to-end watch test: the real compiled gmb binary runs `gmb watch`
// against the sandbox as a separate process, performs the initial analysis,
// then rebuilds when a tracked source file changes. The process is killed
// after the assertions. This is the only test that needs the real binary.

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestWatchLiveAnalysis asserts the watcher performs an initial analysis and
// rebuilds on a tracked file change, then kills it.
func TestWatchLiveAnalysis(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.SampleProject()
	sb.GitInit()

	bin := harness.BuildBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "watch", "--dir", sb.Root)
	cmd.Dir = sb.Root
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start watch: %v", err)
	}

	log, kill := captureAndKill(t, stdout, cmd, cancel)

	// 1. The watcher prints its banner and performs the initial analysis.
	waitForOutput(t, log, "Analyzed", 60*time.Second)
	if got := log.Get(); !strings.Contains(got, "GlassMarble Watcher active") {
		t.Errorf("missing watcher banner:\n%s", got)
	}
	// Wait until the initial analysis has fully finished (the watcher is only
	// registered afterwards); modifying the tree earlier would race the
	// watcher setup and the change would be absorbed into the baseline
	// fingerprint.
	waitForQuiet(t, log, 2*time.Second, 30*time.Second)

	// 2. A change to a tracked source file triggers a live rebuild: the
	// watcher prints a second analysis summary.
	sb.WriteFile("internal/cache/cache.go", `package cache

import "sync"

type Cache struct {
	mu   sync.Mutex
	keys map[string]string
}

func New() *Cache {
	return &Cache{keys: make(map[string]string)}
}

func (c *Cache) Get(k string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.keys[k]
}

func (c *Cache) Set(k, v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys[k] = v
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = make(map[string]string)
}
`)

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(log.Get(), "Analyzed") >= 2 {
			break
		}
		select {
		case <-log.ch:
		case <-time.After(500 * time.Millisecond):
		}
	}
	if count := strings.Count(log.Get(), "Analyzed"); count < 2 {
		t.Errorf("expected >= 2 analyses (initial + rebuild), saw %d:\n%s", count, log.Get())
	}

	kill()
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		t.Errorf("watch exited unexpectedly: %v\nstderr: %s", err, stderr.String())
	}
}

// outputLog is a goroutine-safe capture of the watcher's stdout.
type outputLog struct {
	mu   sync.Mutex
	ch   chan string
	buf  []byte
}

func newOutputLog() *outputLog { return &outputLog{ch: make(chan string, 64)} }

func (l *outputLog) append(s string) {
	l.mu.Lock()
	l.buf = append(l.buf, s...)
	l.mu.Unlock()
	select {
	case l.ch <- s:
	default:
	}
}

func (l *outputLog) Get() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.buf)
}

// captureAndKill starts a reader goroutine that drains the watcher's stdout
// into an outputLog. Returns the log and a kill function that terminates the
// process and stops the reader.
func captureAndKill(t *testing.T, stdout interface{ Read([]byte) (int, error) }, cmd *exec.Cmd, cancel context.CancelFunc) (*outputLog, func()) {
	t.Helper()
	log := newOutputLog()
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				log.append(string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()
	kill := func() {
		cancel() // CommandContext kills the child process.
		<-readerDone
	}
	return log, kill
}

// tail returns the last 4000 bytes of a long string for error messages.
func tail(s string) string {
	if len(s) > 4000 {
		return s[len(s)-4000:]
	}
	return s
}

// waitForOutput polls the captured output until fragment appears or the
// deadline passes, failing the test on timeout.
func waitForOutput(t *testing.T, log *outputLog, fragment string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(log.Get(), fragment) {
			return
		}
		select {
		case <-log.ch:
		case <-time.After(500 * time.Millisecond):
		}
	}
	t.Fatalf("timed out waiting for %q in watcher output:\n%s", fragment, log.Get())
}

// waitForQuiet returns once no new output has arrived for quietFor (meaning
// the analysis pipeline has drained), or fails after overallTimeout.
func waitForQuiet(t *testing.T, log *outputLog, quietFor, overallTimeout time.Duration) {
	t.Helper()
	overall := time.Now().Add(overallTimeout)
	lastSeen := time.Now()
	for {
		select {
		case <-log.ch:
			lastSeen = time.Now()
			if time.Since(lastSeen) > quietFor {
				return
			}
		case <-time.After(200 * time.Millisecond):
			if time.Since(lastSeen) >= quietFor {
				return
			}
		}
		if time.Now().After(overall) {
			t.Fatalf("watcher output never went quiet:\n%s", tail(log.Get()))
		}
	}
}
