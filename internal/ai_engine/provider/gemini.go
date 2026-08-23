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

// GeminiProvider talks to the native Google Gemini generateContent API.
// Authentication uses the x-goog-api-key header, so keys never appear in URLs.
type GeminiProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewGeminiProvider creates a Gemini provider adapter.
func NewGeminiProvider(apiKey, baseURL string, timeout time.Duration) *GeminiProvider {
	return &GeminiProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: DurationFor(timeout)},
	}
}

// Name returns the adapter name.
func (p *GeminiProvider) Name() string { return string(AdapterGemini) }

// Complete performs a single generateContent call. When req.OnStream is set
// the streamGenerateContent endpoint is used and each text part is forwarded;
// usageMetadata is taken from the final chunk. Bodies with no SSE events fall
// back to a one-shot JSON parse.
func (p *GeminiProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	if p.baseURL == "" {
		return nil, fmt.Errorf("provider base URL is not configured")
	}

	payload := geminiRequest{
		Contents: buildGeminiContents(req.Messages),
	}
	if req.System != "" {
		payload.SystemInstruction = &geminiPartHolder{
			Parts: []geminiPart{{Text: req.System}},
		}
	}
	if len(req.Tools) > 0 {
		payload.Tools = []geminiTool{{FunctionDeclarations: buildGeminiTools(req.Tools)}}
	}
	if req.MaxOutputTokens > 0 || req.Temperature != nil {
		cfg := geminiGenConfig{MaxOutputTokens: req.MaxOutputTokens}
		if temp := effectiveTemperature(req.Temperature); temp != nil {
			cfg.Temperature = temp
		}
		payload.GenerationConfig = &cfg
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	path := fmt.Sprintf("/models/%s:generateContent", urlPathEscape(req.Model))
	if req.OnStream != nil {
		path = fmt.Sprintf("/models/%s:streamGenerateContent?alt=sse", urlPathEscape(req.Model))
	}
	resp, err := p.post(ctx, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if req.OnStream != nil {
		return p.completeStream(resp.Body, req)
	}

	return p.parseResponse(resp.Body, req.OnStream)
}

// completeStream parses the Gemini SSE stream incrementally; every chunk carries
// the same response shape as a non-streamed call, with usageMetadata on the
// final one. Capped at 8 MiB via LimitReader.
func (p *GeminiProvider) completeStream(body io.Reader, req Request) (*Response, error) {
	limited := io.LimitReader(body, 8<<20)
	var capture bytes.Buffer
	tee := io.TeeReader(limited, &capture)

	out := &Response{}
	callID := 0
	sawEvent := false

	scanSSE(tee, func(event, data string) {
		if data == "" {
			return
		}
		var parsed geminiResponse
		if err := json.Unmarshal([]byte(data), &parsed); err != nil {
			return
		}
		sawEvent = true
		if parsed.UsageMetadata.TotalTokenCount > 0 {
			out.Usage = Usage{
				PromptTokens:     parsed.UsageMetadata.PromptTokenCount,
				CompletionTokens: parsed.UsageMetadata.CandidatesTokenCount,
				TotalTokens:      parsed.UsageMetadata.TotalTokenCount,
			}
		}
		if len(parsed.Candidates) == 0 {
			return
		}
		for _, part := range parsed.Candidates[0].Content.Parts {
			switch {
			case part.Text != "":
				out.Text += part.Text
				req.OnStream(part.Text)
			case part.FunctionCall != nil:
				out.ToolCalls = append(out.ToolCalls, ToolCall{
					ID:        fmt.Sprintf("call_%d", callID),
					Name:      part.FunctionCall.Name,
					Arguments: string(part.FunctionCall.Args),
				})
				callID++
			}
		}
	})

	if !sawEvent {
		var parsed geminiResponse
		if err := json.Unmarshal(capture.Bytes(), &parsed); err != nil {
			return nil, fmt.Errorf("failed to decode provider response: %w", err)
		}
		return mapGeminiResponse(&parsed, req.OnStream), nil
	}
	return out, nil
}

func (p *GeminiProvider) parseResponse(body io.Reader, onStream func(string)) (*Response, error) {
	var parsed geminiResponse
	if err := json.NewDecoder(io.LimitReader(body, 4<<20)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("failed to decode provider response: %w", err)
	}
	return mapGeminiResponse(&parsed, onStream), nil
}

func mapGeminiResponse(parsed *geminiResponse, onStream func(string)) *Response {
	out := &Response{}
	callID := 0
	if len(parsed.Candidates) > 0 {
		for _, part := range parsed.Candidates[0].Content.Parts {
			switch {
			case part.Text != "":
				out.Text += part.Text
				if onStream != nil {
					onStream(part.Text)
				}
			case part.FunctionCall != nil:
				out.ToolCalls = append(out.ToolCalls, ToolCall{
					ID:        fmt.Sprintf("call_%d", callID),
					Name:      part.FunctionCall.Name,
					Arguments: string(part.FunctionCall.Args),
				})
				callID++
			}
		}
	}
	out.Usage = Usage{
		PromptTokens:     parsed.UsageMetadata.PromptTokenCount,
		CompletionTokens: parsed.UsageMetadata.CandidatesTokenCount,
		TotalTokens:      parsed.UsageMetadata.TotalTokenCount,
	}
	return out
}

// Ping verifies connectivity and authentication with a minimal completion.
func (p *GeminiProvider) Ping(ctx context.Context, model string) error {
	if p.baseURL == "" {
		return fmt.Errorf("provider base URL is not configured")
	}
	if model == "" {
		return fmt.Errorf("no model configured")
	}

	payload := geminiRequest{
		Contents: []geminiContent{
			{Role: "user", Parts: []geminiPart{{Text: "Reply with exactly: OK"}}},
		},
		GenerationConfig: &geminiGenConfig{MaxOutputTokens: 5},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	path := fmt.Sprintf("/models/%s:generateContent", urlPathEscape(model))
	resp, err := p.post(ctx, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return nil
}

func (p *GeminiProvider) post(ctx context.Context, path string, body []byte) (*http.Response, error) {
	url := p.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("x-goog-api-key", p.apiKey)
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

type geminiRequest struct {
	Contents          []geminiContent   `json:"contents"`
	SystemInstruction *geminiPartHolder `json:"systemInstruction,omitempty"`
	Tools             []geminiTool      `json:"tools,omitempty"`
	GenerationConfig  *geminiGenConfig  `json:"generationConfig,omitempty"`
}

type geminiPartHolder struct {
	Parts []geminiPart `json:"parts"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string        `json:"text,omitempty"`
	FunctionCall     *geminiFnCall `json:"functionCall,omitempty"`
	FunctionResponse *geminiFnResp `json:"functionResponse,omitempty"`
}

type geminiFnCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type geminiFnResp struct {
	Name     string `json:"name"`
	Response any    `json:"response"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
}

type geminiFunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type geminiGenConfig struct {
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// ---- message/tool conversion ----

// buildGeminiContents maps the canonical conversation to Gemini contents.
// Gemini requires alternating user/model roles and has no "tool" role:
// assistant messages become "model" and tool results become "user" messages
// with functionResponse parts. Consecutive messages of the same role are
// merged into a single content block.
func buildGeminiContents(msgs []Message) []geminiContent {
	var out []geminiContent
	var current *geminiContent

	flush := func() {
		if current != nil && len(current.Parts) > 0 {
			out = append(out, *current)
		}
		current = nil
	}
	ensure := func(role string) *geminiContent {
		if current == nil || current.Role != role {
			flush()
			current = &geminiContent{Role: role}
		}
		return current
	}

	for _, m := range msgs {
		switch m.Role {
		case RoleSystem:
			// System content is carried in systemInstruction; drop from contents.
		case RoleTool:
			u := ensure("user")
			for _, tr := range m.ToolResults {
				respValue := parseFunctionResponseValue(tr.Content)
				if tr.IsError {
					respValue = map[string]any{
						"error":   tr.Content,
						"isError": true,
					}
				}
				name := tr.Name
				if name == "" {
					name = "unknown"
				}
				u.Parts = append(u.Parts, geminiPart{
					FunctionResponse: &geminiFnResp{Name: name, Response: respValue},
				})
			}
		case RoleAssistant:
			a := ensure("model")
			if m.Content != "" {
				a.Parts = append(a.Parts, geminiPart{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				args := tc.Arguments
				if strings.TrimSpace(args) == "" {
					args = "{}"
				}
				a.Parts = append(a.Parts, geminiPart{
					FunctionCall: &geminiFnCall{Name: tc.Name, Args: json.RawMessage(args)},
				})
			}
		default:
			u := ensure("user")
			u.Parts = append(u.Parts, geminiPart{Text: m.Content})
		}
	}
	flush()
	return out
}

func buildGeminiTools(tools []Tool) []geminiFunctionDeclaration {
	out := make([]geminiFunctionDeclaration, 0, len(tools))
	for _, t := range tools {
		params := t.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, geminiFunctionDeclaration{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
		})
	}
	return out
}

func parseFunctionResponseValue(content string) any {
	trimmed := strings.TrimSpace(content)
	var v any
	if err := json.Unmarshal([]byte(trimmed), &v); err == nil {
		return v
	}
	return map[string]any{"result": trimmed}
}

func urlPathEscape(s string) string {
	// Model IDs are simple identifiers; escape nothing but "/" is not present.
	return strings.ReplaceAll(s, "/", "%2F")
}
