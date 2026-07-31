package session_test

import (
	"os"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/session"
)

func msg(role provider.Role, content string) provider.Message {
	return provider.Message{Role: role, Content: content}
}

func TestSessionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := session.Create(dir, "openai", "gpt-4o")
	s.Messages = []provider.Message{
		msg(provider.RoleUser, "first question"),
		msg(provider.RoleAssistant, "first answer"),
	}
	s.Usage = provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
	s.CostUSD = 0.0012
	s.Turns = 1
	s.ToolCalls = 2
	s.Touch()

	if err := s.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(s.Path(dir)); err != nil {
		t.Fatalf("session file missing: %v", err)
	}

	loaded, err := session.Open(dir, s.ID)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if loaded.ID != s.ID || loaded.Provider != "openai" || loaded.Model != "gpt-4o" {
		t.Errorf("identity mismatch: %+v", loaded)
	}
	if len(loaded.Messages) != 2 || loaded.Messages[0].Content != "first question" {
		t.Errorf("messages = %+v", loaded.Messages)
	}
	if loaded.Usage.TotalTokens != 15 || loaded.CostUSD != 0.0012 {
		t.Errorf("usage/cost = %+v / %v", loaded.Usage, loaded.CostUSD)
	}
}

func TestSessionListAndLatest(t *testing.T) {
	dir := t.TempDir()
	s1 := session.Create(dir, "openai", "gpt-4o")
	s1.Touch()
	if err := s1.Save(dir); err != nil {
		t.Fatalf("Save s1: %v", err)
	}
	s2 := session.Create(dir, "gemini", "gemini-2.5-flash")
	s2.Touch()
	if err := s2.Save(dir); err != nil {
		t.Fatalf("Save s2: %v", err)
	}

	list, err := session.List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list = %d, want 2", len(list))
	}
	// Newest (s2) first.
	if list[0].ID != s2.ID || list[1].ID != s1.ID {
		t.Errorf("order = %s, %s", list[0].ID, list[1].ID)
	}

	latest, err := session.Latest(dir)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest.ID != s2.ID {
		t.Errorf("latest = %s, want %s", latest.ID, s2.ID)
	}
}

func TestSessionDelete(t *testing.T) {
	dir := t.TempDir()
	s := session.Create(dir, "openai", "gpt-4o")
	if err := s.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := session.Delete(dir, s.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := session.Delete(dir, s.ID); err == nil {
		t.Fatal("second delete should fail")
	}
	if err := session.Delete(dir, "..%2Fevil"); err == nil {
		t.Fatal("invalid id should be rejected")
	}
	if _, err := session.Open(dir, "../../secret"); err == nil {
		t.Fatal("traversal id should be rejected")
	}
}

func TestSessionTrim(t *testing.T) {
	s := session.Create(t.TempDir(), "openai", "gpt-4o")
	for i := 0; i < 10; i++ {
		s.Messages = append(s.Messages,
			msg(provider.RoleUser, "q"+string(rune('0'+i))),
			msg(provider.RoleAssistant, "a"+string(rune('0'+i))),
		)
	}
	s.Trim(6)
	if len(s.Messages) != 6 {
		t.Fatalf("after trim: %d messages, want 6", len(s.Messages))
	}
	// The trimmed transcript must start at a user turn: the first kept
	// message is the user question of turn 7 (index 12 of 20).
	if s.Messages[0].Role != provider.RoleUser || s.Messages[0].Content != "q7" {
		t.Errorf("first kept message = %+v, want user q7", s.Messages[0])
	}

	// No-op trims.
	s.Trim(100)
	if len(s.Messages) != 6 {
		t.Errorf("trim above size changed transcript: %d", len(s.Messages))
	}
	s.Trim(0)
	if len(s.Messages) != 6 {
		t.Errorf("trim 0 changed transcript: %d", len(s.Messages))
	}
}

func TestSessionTrimKeepsToolRoundIntact(t *testing.T) {
	s := session.Create(t.TempDir(), "openai", "gpt-4o")
	s.Messages = append(s.Messages,
		msg(provider.RoleUser, "old question"),
		msg(provider.RoleAssistant, "old answer"),
		msg(provider.RoleUser, "new question"),
		provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "akg_status", Arguments: "{}"}}},
		provider.Message{Role: provider.RoleTool, ToolResults: []provider.ToolResult{{ID: "c1", Name: "akg_status", Content: `{"ok":true}`}}},
		msg(provider.RoleAssistant, "final answer"),
	)
	s.Trim(2)
	// Only the last two messages survive: tool round + final answer would be
	// a split, so the retained window starts at the user question instead.
	if len(s.Messages) != 4 {
		t.Fatalf("messages = %d, want 4 (user turn kept whole)", len(s.Messages))
	}
	if s.Messages[0].Role != provider.RoleUser || s.Messages[0].Content != "new question" {
		t.Errorf("first kept = %+v", s.Messages[0])
	}
}

func TestSessionIDsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := session.NewID(tm(2026, 7, 31, 12, 0, 0))
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func tm(y int, mo int, d int, h int, mi int, s int) (t time.Time) {
	t = time.Date(y, time.Month(mo), d, h, mi, s, 0, time.UTC)
	return
}
