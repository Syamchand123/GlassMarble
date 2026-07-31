package ai_engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/aiconfig"
)

func TestNewUnknownProvider(t *testing.T) {
	_, err := New(&aiconfig.Config{Provider: "nope", Model: "m"}, ".")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unknown AI provider") {
		t.Errorf("error = %v", err)
	}
}

func TestNewMissingKey(t *testing.T) {
	t.Setenv("GLASSMARBLE_OPENAI_API_KEY", "")
	t.Setenv("GLASSMARBLE_AI_API_KEY", "")
	_, err := New(&aiconfig.Config{Provider: "openai", Model: "gpt-4o"}, ".")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !strings.Contains(err.Error(), "no API key") {
		t.Errorf("error = %v", err)
	}
}

func TestNewMissingModel(t *testing.T) {
	t.Setenv("GLASSMARBLE_OPENAI_API_KEY", "sk-test")
	_, err := New(&aiconfig.Config{Provider: "openai"}, ".")
	if err == nil {
		t.Fatal("expected error for missing model")
	}
	if !strings.Contains(err.Error(), "no model configured") {
		t.Errorf("error = %v", err)
	}
}

func TestNewLocalProviderWithoutKey(t *testing.T) {
	e, err := New(&aiconfig.Config{Provider: "ollama", Model: "llama3.3"}, ".")
	if err != nil {
		t.Fatalf("New(ollama) failed: %v", err)
	}
	if e.Provider == nil {
		t.Fatal("provider is nil")
	}
	if !strings.Contains(e.Provider.Name(), "openai_compat") {
		t.Errorf("adapter = %q", e.Provider.Name())
	}
}

func TestNewCustomWithBaseURL(t *testing.T) {
	e, err := New(&aiconfig.Config{Provider: "custom", Model: "my-model", BaseURL: "http://localhost:1/v1"}, ".")
	if err != nil {
		t.Fatalf("New(custom) failed: %v", err)
	}
	if e.Config.Model != "my-model" {
		t.Errorf("model = %q", e.Config.Model)
	}
}

func fakeOpenAICompletionsServer(t *testing.T, responseBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, responseBody)
	}))
}

func TestAsk(t *testing.T) {
	srv := fakeOpenAICompletionsServer(t, `{"choices":[{"message":{"content":"Hello from the fake model!"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`)
	defer srv.Close()

	e, err := New(&aiconfig.Config{Provider: "custom", Model: "test-model", BaseURL: srv.URL}, ".")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	resp, err := e.Ask(context.Background(), "hello", nil)
	if err != nil {
		t.Fatalf("Ask() failed: %v", err)
	}
	if resp.Text != "Hello from the fake model!" {
		t.Errorf("text = %q", resp.Text)
	}
	if resp.Usage.TotalTokens != 8 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestAskWithHistory(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`)
	}))
	defer srv.Close()

	e, err := New(&aiconfig.Config{Provider: "custom", Model: "test-model", BaseURL: srv.URL}, ".")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	resp, err := e.Ask(context.Background(), "follow-up", nil)
	if err != nil {
		t.Fatalf("Ask() failed: %v", err)
	}
	if resp.Text != "ok" {
		t.Errorf("text = %q", resp.Text)
	}
	if !strings.Contains(string(gotBody), "follow-up") {
		t.Errorf("query not sent: %s", gotBody)
	}
}

func TestAskEmptyQuery(t *testing.T) {
	e, err := New(&aiconfig.Config{Provider: "custom", Model: "m", BaseURL: "http://localhost:1"}, ".")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if _, err := e.Ask(context.Background(), "", nil); err == nil {
		t.Error("Ask() with empty query should fail")
	}
}

func TestDoctorAllGood(t *testing.T) {
	srv := fakeOpenAICompletionsServer(t, `{"choices":[{"message":{"content":"OK"}}],"usage":{}}`)
	defer srv.Close()

	rootDir := t.TempDir()
	ttlDir := filepath.Join(rootDir, ".glassmarble")
	if err := os.MkdirAll(ttlDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ttlDir, "akg_state.ttl"), []byte("@prefix gm: <http://glassmarble.org/schema#> .\n"), 0o644); err != nil {
		t.Fatalf("write ttl: %v", err)
	}

	rep := Doctor(context.Background(), &aiconfig.Config{
		Provider:   "custom",
		Model:      "test-model",
		BaseURL:    srv.URL,
		TimeoutSec: 10,
	}, rootDir)

	if len(rep.Problems) != 0 {
		t.Errorf("problems = %v", rep.Problems)
	}
	if rep.PingStatus != "ok" {
		t.Errorf("ping status = %q", rep.PingStatus)
	}
	if !rep.AKGExists {
		t.Error("AKG should exist")
	}
	if rep.KeyRequired {
		t.Error("custom provider should not require a key")
	}
}

func TestDoctorMissingAKG(t *testing.T) {
	srv := fakeOpenAICompletionsServer(t, `{"choices":[{"message":{"content":"OK"}}],"usage":{}}`)
	defer srv.Close()

	rep := Doctor(context.Background(), &aiconfig.Config{
		Provider:   "custom",
		Model:      "test-model",
		BaseURL:    srv.URL,
		TimeoutSec: 10,
	}, t.TempDir())

	// Ping must still run even without an AKG.
	if rep.PingStatus != "ok" {
		t.Errorf("ping status = %q, want ok", rep.PingStatus)
	}
	if rep.AKGExists {
		t.Error("AKG should not exist in empty temp dir")
	}
	if !strings.Contains(rep.AKGPath, "akg_state.ttl") {
		t.Errorf("AKG path = %q", rep.AKGPath)
	}
}

func TestDoctorUnknownProvider(t *testing.T) {
	rep := Doctor(context.Background(), &aiconfig.Config{Provider: "nope", Model: "m"}, ".")
	if len(rep.Problems) == 0 {
		t.Error("expected problems for unknown provider")
	}
	if rep.PingStatus != "skipped" {
		t.Errorf("ping status = %q, want skipped", rep.PingStatus)
	}
}

func TestDoctorMissingKey(t *testing.T) {
	t.Setenv("GLASSMARBLE_OPENAI_API_KEY", "")
	t.Setenv("GLASSMARBLE_AI_API_KEY", "")
	rep := Doctor(context.Background(), &aiconfig.Config{Provider: "openai", Model: "gpt-4o"}, ".")
	found := false
	for _, p := range rep.Problems {
		if strings.Contains(p, "no API key") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected key problem, got %v", rep.Problems)
	}
	if rep.KeySource != "" {
		t.Errorf("key source = %q, want empty", rep.KeySource)
	}
}

func TestDoctorKeySourceConfig(t *testing.T) {
	rep := Doctor(context.Background(), &aiconfig.Config{Provider: "openai", Model: "gpt-4o", APIKey: "sk-test"}, ".")
	if rep.KeySource != "config" {
		t.Errorf("key source = %q, want config", rep.KeySource)
	}
	if !rep.KeySet {
		t.Error("key should be set")
	}
}

func TestDoctorKeySourceEnv(t *testing.T) {
	t.Setenv("GLASSMARBLE_OPENAI_API_KEY", "sk-env")
	rep := Doctor(context.Background(), &aiconfig.Config{Provider: "openai", Model: "gpt-4o"}, ".")
	if rep.KeySource != "environment" {
		t.Errorf("key source = %q, want environment", rep.KeySource)
	}
}

func TestMaskAPIKey(t *testing.T) {
	if got := MaskAPIKey(""); got != "(not set)" {
		t.Errorf("empty = %q", got)
	}
	if got := MaskAPIKey("short"); got != "********" {
		t.Errorf("short = %q", got)
	}
	if got := MaskAPIKey("sk-abcdefghijklmnop"); got != "sk-a...mnop" {
		t.Errorf("masked = %q", got)
	}
}
