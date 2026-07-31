// Package session implements persistent multi-turn chat memory for the
// GlassMarble AI engine. Each session is a JSON file under
// .glassmarble/ai/sessions/<id>.json carrying the full conversation
// transcript, accumulated token usage, and estimated cost. Sessions make
// "gmb ai chat" resumable across process runs.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
)

// Session is one saved conversation.
type Session struct {
	ID        string             `json:"id"`
	Created   time.Time          `json:"created"`
	Updated   time.Time          `json:"updated"`
	Provider  string             `json:"provider"`
	Model     string             `json:"model"`
	Messages  []provider.Message `json:"messages"`
	Usage     provider.Usage     `json:"usage"`
	CostUSD   float64            `json:"cost_usd"`
	Turns     int                `json:"turns"`
	ToolCalls int                `json:"tool_calls"`
}

// Summary is the lightweight list view of a session.
type Summary struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
	Messages  int       `json:"messages"`
	Tokens    int       `json:"tokens"`
	Turns     int       `json:"turns"`
	ToolCalls int       `json:"tool_calls"`
	CostUSD   float64   `json:"cost_usd"`
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Dir returns the sessions directory for a repository root.
func Dir(rootDir string) string {
	return filepath.Join(rootDir, ".glassmarble", "ai", "sessions")
}

// NewID mints a collision-resistant session identifier.
func NewID(t time.Time) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return t.Format("20060102T150405") + "-" + hex.EncodeToString(b[:])
}

// Create returns a fresh in-memory session. Nothing is written until Save.
func Create(dir, providerName, model string) *Session {
	now := time.Now()
	return &Session{
		ID:       NewID(now),
		Created:  now,
		Updated:  now,
		Provider: providerName,
		Model:    model,
	}
}

// Path returns the storage path for the session.
func (s *Session) Path(dir string) string {
	return filepath.Join(dir, s.ID+".json")
}

// Save writes the session to disk (0600, transcript may contain source
// excerpts), creating the directory as needed.
func (s *Session) Save(dir string) error {
	if !idPattern.MatchString(s.ID) {
		return fmt.Errorf("invalid session id %q", s.ID)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create sessions directory: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode session: %w", err)
	}
	if err := os.WriteFile(s.Path(dir), data, 0o600); err != nil {
		return fmt.Errorf("failed to write session: %w", err)
	}
	return nil
}

// Open loads a session by id. A missing session returns an error wrapping
// os.ErrNotExist.
func Open(dir, id string) (*Session, error) {
	if !idPattern.MatchString(id) {
		return nil, fmt.Errorf("invalid session id %q", id)
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, fmt.Errorf("session %q: %w", id, err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("corrupt session %q: %w", id, err)
	}
	return &s, nil
}

// Latest returns the most recently updated session, or an error wrapping
// os.ErrNotExist when there are none.
func Latest(dir string) (*Session, error) {
	all, err := List(dir)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no sessions found: %w", os.ErrNotExist)
	}
	return Open(dir, all[0].ID)
}

// List returns all sessions, newest first.
func List(dir string) ([]Summary, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Summary{}, nil
		}
		return nil, fmt.Errorf("cannot list sessions: %w", err)
	}
	var out []Summary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		s, err := Open(dir, strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		out = append(out, s.summary())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

// Delete removes a session file. A missing session returns an error wrapping
// os.ErrNotExist.
func Delete(dir, id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid session id %q", id)
	}
	if err := os.Remove(filepath.Join(dir, id+".json")); err != nil {
		return fmt.Errorf("session %q: %w", id, err)
	}
	return nil
}

// Trim bounds the transcript to at most maxMessages entries (somewhat over
// on turn boundaries), aligned so the retained history starts at a user turn:
// tool rounds are never split and the trailing answer is never dropped.
func (s *Session) Trim(maxMessages int) {
	if maxMessages <= 0 || len(s.Messages) <= maxMessages {
		return
	}
	cut := len(s.Messages) - maxMessages
	for cut < len(s.Messages) && s.Messages[cut].Role != provider.RoleUser {
		cut++
	}
	if cut >= len(s.Messages) {
		// No user turn inside the window: fall back to the last complete
		// user turn in the transcript so the model keeps the question and
		// its answer.
		cut = 0
		for i, m := range s.Messages {
			if m.Role == provider.RoleUser {
				cut = i
			}
		}
	}
	s.Messages = s.Messages[cut:]
}

// Touch updates the Updated timestamp (call before Save after a turn).
func (s *Session) Touch() {
	s.Updated = time.Now()
}

func (s *Session) summary() Summary {
	return Summary{
		ID:        s.ID,
		Provider:  s.Provider,
		Model:     s.Model,
		Created:   s.Created,
		Updated:   s.Updated,
		Messages:  len(s.Messages),
		Tokens:    s.Usage.TotalTokens,
		Turns:     s.Turns,
		ToolCalls: s.ToolCalls,
		CostUSD:   s.CostUSD,
	}
}
