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

// OpenAICompatProvider talks to any endpoint implementing the OpenAI
// chat-completions wire format: OpenAI, DeepSeek, Mistral, GLM, NVIDIA,
// OpenRouter, Groq, Ollama, and arbitrary custom endpoints.
type OpenAICompatProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewOpenAICompatProvider creates an OpenAI-compatible provider adapter.
// An empty baseURL is allowed at construction time but produces an error on
// use (reported by `gmb ai doctor`).
func NewOpenAICompatProvider(apiKey, baseURL string, timeout time.Duration) *OpenAICompatProvider {
	return &OpenAICompatProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: DurationFor(timeout)},
	}
}

// Name returns the adapter name.
func (p *OpenAICompatProvider) Name() string { return string(AdapterOpenAICompat) }

// Complete performs a single chat completion against the endpoint. When
// req.OnStream is set the request streams tokens and each text delta is
// delivered to the callback; the returned Response is fully populated either
// way. Endpoints that ignore "stream": true (and plain non-SSE test doubles)
// are handled by falling back to a one-shot JSON parse.
func (p *OpenAICompatProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	if p.baseURL == "" {
		return nil, fmt.Errorf("provider base URL is not configured")
	}

	payload := openAICompatRequest{
		Model:    req.Model,
		Messages: buildOpenAICompatMessages(req.System, req.Messages),
	}
	if temp := effectiveTemperature(req.Temperature); temp != nil {
		payload.Temperature = temp
	}
	if req.MaxOutputTokens > 0 {
		payload.MaxTokens = intPtr(req.MaxOutputTokens)
	}
	if len(req.Tools) > 0 {
		payload.Tools = buildOpenAICompatTools(req.Tools)
	}
	if req.OnStream != nil {
		payload.Stream = true
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	resp, err := p.post(ctx, "/chat/completions", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if req.OnStream != nil {
		return p.completeStream(resp.Body, req)
	}

	var parsed openAICompatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("failed to decode provider response: %w", err)
	}

	out := &Response{}
	if len(parsed.Choices) > 0 {
		msg := parsed.Choices[0].Message
		out.Text = msg.Content
		for _, tc := range msg.ToolCalls {
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
	}
	out.Usage = Usage{
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		TotalTokens:      parsed.Usage.TotalTokens,
	}
	return out, nil
}

// completeStream parses an SSE chat-completions stream incrementally from
// resp.Body. Text deltas are forwarded to req.OnStream as they arrive;
// tool-call fragments are reassembled by index and usage is taken from the
// final chunk. If the body contains no SSE events at all it is treated as a
// plain JSON response (non-streaming fallback). The body is capped at 8 MiB
// via LimitReader.
func (p *OpenAICompatProvider) completeStream(body io.Reader, req Request) (*Response, error) {
	limited := io.LimitReader(body, 8<<20)
	var capture bytes.Buffer
	tee := io.TeeReader(limited, &capture)

	out := &Response{}
	var toolCalls []openAICompatToolCall
	toolIdx := make(map[int]int)
	sawEvent := false

	scanSSE(tee, func(event, data string) {
		if data == "[DONE]" {
			return
		}
		var chunk openAICompatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return
		}
		sawEvent = true
		if chunk.Usage.TotalTokens > 0 {
			out.Usage = Usage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}
		if len(chunk.Choices) == 0 {
			return
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			out.Text += delta.Content
			req.OnStream(delta.Content)
		}
		for _, tc := range delta.ToolCalls {
			pos, ok := toolIdx[tc.Index]
			if !ok {
				pos = len(toolCalls)
				toolIdx[tc.Index] = pos
				toolCalls = append(toolCalls, openAICompatToolCall{ID: tc.ID, Type: "function"})
			}
			if tc.ID != "" {
				toolCalls[pos].ID = tc.ID
			}
			if tc.Function.Name != "" {
				toolCalls[pos].Function.Name = tc.Function.Name
			}
			toolCalls[pos].Function.Arguments += tc.Function.Arguments
		}
	})

	if !sawEvent {
		// Non-SSE body: one-shot JSON response (stream flag ignored by the
		// endpoint, or a non-streaming test double).
		var parsed openAICompatResponse
		if err := json.Unmarshal(capture.Bytes(), &parsed); err != nil {
			return nil, fmt.Errorf("failed to decode provider response: %w", err)
		}
		if len(parsed.Choices) > 0 {
			out.Text = parsed.Choices[0].Message.Content
			for _, tc := range parsed.Choices[0].Message.ToolCalls {
				out.ToolCalls = append(out.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
			}
		}
		out.Usage = Usage{
			PromptTokens:     parsed.Usage.PromptTokens,
			CompletionTokens: parsed.Usage.CompletionTokens,
			TotalTokens:      parsed.Usage.TotalTokens,
		}
		if out.Text != "" {
			req.OnStream(out.Text)
		}
		return out, nil
	}

	for _, tc := range toolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return out, nil
}

// Ping verifies connectivity and authentication with a minimal completion.
func (p *OpenAICompatProvider) Ping(ctx context.Context, model string) error {
	if p.baseURL == "" {
		return fmt.Errorf("provider base URL is not configured")
	}
	if model == "" {
		return fmt.Errorf("no model configured")
	}

	payload := openAICompatRequest{
		Model: model,
		Messages: []openAICompatMessage{
			{Role: "user", Content: "Reply with exactly: OK"},
		},
		MaxTokens: intPtr(5),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	resp, err := p.post(ctx, "/chat/completions", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return nil
}

func (p *OpenAICompatProvider) post(ctx context.Context, path string, body []byte) (*http.Response, error) {
	url := p.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
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

type openAICompatRequest struct {
	Model       string                `json:"model"`
	Messages    []openAICompatMessage `json:"messages"`
	Tools       []openAICompatTool    `json:"tools,omitempty"`
	Temperature *float64              `json:"temperature,omitempty"`
	MaxTokens   *int                  `json:"max_tokens,omitempty"`
	Stream      bool                  `json:"stream,omitempty"`
}

// openAICompatStreamChunk is one SSE payload of a streamed completion.
type openAICompatStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openAICompatMessage struct {
	Role       string                 `json:"role"`
	Content    any                    `json:"content,omitempty"`
	ToolCalls  []openAICompatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string                 `json:"tool_call_id,omitempty"`
}

type openAICompatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAICompatTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description,omitempty"`
		Parameters  map[string]any `json:"parameters,omitempty"`
	} `json:"function"`
}

type openAICompatResponse struct {
	Choices []struct {
		Message struct {
			Content   string                 `json:"content"`
			ToolCalls []openAICompatToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// ---- message/tool conversion ----

// buildOpenAICompatMessages converts the canonical conversation to the wire
// format, prepending the system prompt as a system message when present.
func buildOpenAICompatMessages(system string, msgs []Message) []openAICompatMessage {
	out := make([]openAICompatMessage, 0, len(msgs)+1)
	if system != "" {
		out = append(out, openAICompatMessage{Role: "system", Content: system})
	}
	for _, m := range msgs {
		switch m.Role {
		case RoleTool:
			for _, tr := range m.ToolResults {
				out = append(out, openAICompatMessage{
					Role:       "tool",
					Content:    tr.Content,
					ToolCallID: tr.ID,
				})
			}
		case RoleAssistant:
			msg := openAICompatMessage{Role: "assistant"}
			if len(m.ToolCalls) > 0 {
				msg.ToolCalls = make([]openAICompatToolCall, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					msg.ToolCalls = append(msg.ToolCalls, openAICompatToolCall{
						ID:   tc.ID,
						Type: "function",
					})
					msg.ToolCalls[len(msg.ToolCalls)-1].Function.Name = tc.Name
					msg.ToolCalls[len(msg.ToolCalls)-1].Function.Arguments = tc.Arguments
				}
			} else {
				msg.Content = m.Content
			}
			out = append(out, msg)
		default:
			out = append(out, openAICompatMessage{
				Role:    string(m.Role),
				Content: m.Content,
			})
		}
	}
	return out
}

func buildOpenAICompatTools(tools []Tool) []openAICompatTool {
	out := make([]openAICompatTool, 0, len(tools))
	for _, t := range tools {
		var tc openAICompatTool
		tc.Type = "function"
		tc.Function.Name = t.Name
		tc.Function.Description = t.Description
		tc.Function.Parameters = t.Parameters
		out = append(out, tc)
	}
	return out
}

func effectiveTemperature(t *float64) *float64 {
	if t == nil {
		return nil
	}
	return t
}

func intPtr(v int) *int { return &v }
