package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AnthropicVersion is the Messages API version pinned by this adapter.
const AnthropicVersion = "2023-06-01"

// AnthropicProvider talks to the native Anthropic Messages API.
type AnthropicProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewAnthropicProvider creates an Anthropic provider adapter.
func NewAnthropicProvider(apiKey, baseURL string, timeout time.Duration) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: DurationFor(timeout)},
	}
}

// Name returns the adapter name.
func (p *AnthropicProvider) Name() string { return string(AdapterAnthropic) }

// Complete performs a single chat completion against the Messages API. When
// req.OnStream is set the request streams: text deltas are forwarded and
// tool-use arguments are reassembled from input_json_delta fragments. Bodies
// with no SSE events fall back to a one-shot JSON parse.
func (p *AnthropicProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	if p.baseURL == "" {
		return nil, fmt.Errorf("provider base URL is not configured")
	}

	system, messages := buildAnthropicMessages(req.System, req.Messages)

	payload := anthropicRequest{
		Model:     req.Model,
		MaxTokens: maxTokensOrDefault(req.MaxOutputTokens),
		System:    system,
		Messages:  messages,
	}
	if temp := effectiveTemperature(req.Temperature); temp != nil {
		payload.Temperature = temp
	}
	if len(req.Tools) > 0 {
		payload.Tools = buildAnthropicTools(req.Tools)
	}
	if req.OnStream != nil {
		payload.Stream = true
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	resp, err := p.post(ctx, "/messages", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if req.OnStream != nil {
		return p.completeStream(resp.Body, req)
	}

	var parsed anthropicResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("failed to decode provider response: %w", err)
	}

	out := &Response{}
	for _, block := range parsed.Content {
		switch block.Type {
		case "text":
			out.Text += block.Text
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: string(block.Input),
			})
		}
	}
	out.Usage = Usage{
		PromptTokens:     parsed.Usage.InputTokens,
		CompletionTokens: parsed.Usage.OutputTokens,
		TotalTokens:      parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
	}
	return out, nil
}

// completeStream parses the Anthropic SSE event stream incrementally from
// resp.Body: message_start carries the input token count, content_block_delta
// carries text or partial JSON for tool_use blocks, and message_delta carries
// the output token count. Capped at 8 MiB via LimitReader.
func (p *AnthropicProvider) completeStream(body io.Reader, req Request) (*Response, error) {
	limited := io.LimitReader(body, 8<<20)
	var capture bytes.Buffer
	tee := io.TeeReader(limited, &capture)

	out := &Response{}
	type block struct {
		id, name, args string
	}
	blocks := make(map[int]*block)
	var blockOrder []int
	sawEvent := false

	scanSSE(tee, func(event, data string) {
		if data == "" {
			return
		}
		var ev anthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return
		}
		sawEvent = true
		switch ev.Type {
		case "message_start":
			out.Usage.PromptTokens = ev.Message.Usage.InputTokens
		case "content_block_start":
			index := 0
			if ev.Index != nil {
				index = *ev.Index
			}
			if _, ok := blocks[index]; !ok {
				blockOrder = append(blockOrder, index)
			}
			blocks[index] = &block{id: ev.ContentBlock.ID, name: ev.ContentBlock.Name}
		case "content_block_delta":
			index := 0
			if ev.Index != nil {
				index = *ev.Index
			}
			b, ok := blocks[index]
			if !ok {
				b = &block{}
				blocks[index] = b
				blockOrder = append(blockOrder, index)
			}
			switch ev.Delta.Type {
			case "text_delta":
				out.Text += ev.Delta.Text
				req.OnStream(ev.Delta.Text)
			case "input_json_delta":
				b.args += ev.Delta.PartialJSON
			}
		case "message_delta":
			out.Usage.CompletionTokens = ev.Usage.OutputTokens
		}
	})

	if !sawEvent {
		var parsed anthropicResponse
		if err := json.Unmarshal(capture.Bytes(), &parsed); err != nil {
			return nil, fmt.Errorf("failed to decode provider response: %w", err)
		}
		for _, block := range parsed.Content {
			switch block.Type {
			case "text":
				out.Text += block.Text
			case "tool_use":
				out.ToolCalls = append(out.ToolCalls, ToolCall{ID: block.ID, Name: block.Name, Arguments: string(block.Input)})
			}
		}
		out.Usage = Usage{
			PromptTokens:     parsed.Usage.InputTokens,
			CompletionTokens: parsed.Usage.OutputTokens,
			TotalTokens:      parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
		}
		if out.Text != "" {
			req.OnStream(out.Text)
		}
		return out, nil
	}

	out.Usage.TotalTokens = out.Usage.PromptTokens + out.Usage.CompletionTokens
	for _, index := range blockOrder {
		b := blocks[index]
		if b.name != "" {
			out.ToolCalls = append(out.ToolCalls, ToolCall{ID: b.id, Name: b.name, Arguments: b.args})
		}
	}
	return out, nil
}

// Ping verifies connectivity and authentication with a minimal completion.
func (p *AnthropicProvider) Ping(ctx context.Context, model string) error {
	if p.baseURL == "" {
		return fmt.Errorf("provider base URL is not configured")
	}
	if model == "" {
		return fmt.Errorf("no model configured")
	}

	payload := anthropicRequest{
		Model:     model,
		MaxTokens: 5,
		Messages: []anthropicMessage{
			{Role: "user", Content: []anthropicContentBlock{{Type: "text", Text: "Reply with exactly: OK"}}},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	resp, err := p.post(ctx, "/messages", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return nil
}

func (p *AnthropicProvider) post(ctx context.Context, path string, body []byte) (*http.Response, error) {
	url := p.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", AnthropicVersion)
	if p.apiKey != "" {
		req.Header.Set("x-api-key", p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("provider returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	return resp, nil
}

// ---- wire types ----

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
}

// anthropicStreamEvent is one SSE event of a streamed Messages response.
type anthropicStreamEvent struct {
	Type    string `json:"type"`
	Index   *int   `json:"index"`
	Message struct {
		Usage struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	} `json:"message"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicContentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Input     any    `json:"input,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   any    `json:"content,omitempty"`
	IsError   *bool  `json:"is_error,omitempty"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// ---- message/tool conversion ----

func buildAnthropicMessages(system string, msgs []Message) (string, []anthropicMessage) {
	var systemParts []string
	if system != "" {
		systemParts = append(systemParts, system)
	}

	out := make([]anthropicMessage, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case RoleSystem:
			if m.Content != "" {
				systemParts = append(systemParts, m.Content)
			}
		case RoleTool:
			blocks := make([]anthropicContentBlock, 0, len(m.ToolResults))
			for _, tr := range m.ToolResults {
				block := anthropicContentBlock{
					Type:      "tool_result",
					ToolUseID: tr.ID,
					Content:   tr.Content,
				}
				if tr.IsError {
					b := true
					block.IsError = &b
				}
				blocks = append(blocks, block)
			}
			if len(blocks) > 0 {
				out = append(out, anthropicMessage{Role: "user", Content: blocks})
			}
		case RoleAssistant:
			blocks := make([]anthropicContentBlock, 0, 1+len(m.ToolCalls))
			if m.Content != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: parseArguments(tc.Arguments),
				})
			}
			if len(blocks) > 0 {
				out = append(out, anthropicMessage{Role: "assistant", Content: blocks})
			}
		default:
			out = append(out, anthropicMessage{
				Role:    "user",
				Content: []anthropicContentBlock{{Type: "text", Text: m.Content}},
			})
		}
	}
	return strings.Join(systemParts, "\n\n"), out
}

func buildAnthropicTools(tools []Tool) []anthropicTool {
	out := make([]anthropicTool, 0, len(tools))
	for _, t := range tools {
		params := t.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: params,
		})
	}
	return out
}

func parseArguments(raw string) any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err == nil {
		return obj
	}
	return map[string]any{"raw": trimmed}
}

func maxTokensOrDefault(v int) int {
	if v <= 0 {
		return 8192
	}
	return v
}
