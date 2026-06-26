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

// LocalConfig holds configuration for the local OpenAI-compatible adapter.
// BaseURL points at any OpenAI-compatible /v1 endpoint (LM Studio, vLLM,
// Ollama, or a self-hosted server). The adapter does not assume a specific
// deployment or model family.
type LocalConfig struct {
	APIKey          string
	BaseURL         string  // required; e.g. "http://localhost:1234"
	Model           string  // optional; LM Studio uses the loaded model when empty
	MaxOutputTokens int     // max tokens to generate; 0 = let server decide
	Temperature     float64 // sampling temperature; 0 = omit (server default)
	Timeout         time.Duration
}

// LocalAdapter implements Provider against an OpenAI-compatible chat completions API.
type LocalAdapter struct {
	apiKey          string
	baseURL         string
	model           string
	maxOutputTokens int
	temperature     float64
	http            *http.Client
}

// NewLocalAdapter creates a LocalAdapter. BaseURL is required.
func NewLocalAdapter(cfg LocalConfig) *LocalAdapter {
	model := cfg.Model
	if model == "" {
		model = "local"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	return &LocalAdapter{
		apiKey:          cfg.APIKey,
		baseURL:         strings.TrimRight(cfg.BaseURL, "/"),
		model:           model,
		maxOutputTokens: cfg.MaxOutputTokens,
		temperature:     cfg.Temperature,
		http:            &http.Client{Timeout: timeout},
	}
}

// Name returns the provider identifier.
func (a *LocalAdapter) Name() string { return "local" }

// Model returns the configured model identifier.
func (a *LocalAdapter) Model() string { return a.model }

// NativeContextWindow returns a conservative floor for local/self-hosted models.
// The actual window depends on the server's load-time configuration; use
// config context_size to override.
func (a *LocalAdapter) NativeContextWindow() int { return 8192 }

// --- OpenAI Chat Completions wire types ---

type localChatRequest struct {
	Model       string             `json:"model"`
	Messages    []localChatMessage `json:"messages"`
	Tools       []localTool        `json:"tools,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
}

type localChatMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCalls  []localToolCall `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type localToolCall struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Function localToolCallFunc `json:"function"`
}

type localToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type localTool struct {
	Type     string        `json:"type"`
	Function localToolFunc `json:"function"`
}

type localToolFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type localChatResponse struct {
	Choices []localChoice `json:"choices"`
	Usage   localUsage    `json:"usage"`
}

type localChoice struct {
	Message      localRespMessage `json:"message"`
	FinishReason string           `json:"finish_reason"`
}

type localRespMessage struct {
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	ToolCalls []localToolCall `json:"tool_calls,omitempty"`
}

type localUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type localErrorEnvelope struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// localFlatErrorEnvelope matches servers (LM Studio) that return the error as a
// plain string instead of the OpenAI object form: {"error":"..."}.
type localFlatErrorEnvelope struct {
	Error string `json:"error"`
}

// Send converts our types to OpenAI chat format, POSTs to /v1/chat/completions,
// and converts the response back.
func (a *LocalAdapter) Send(
	ctx context.Context,
	system string,
	messages []Message,
	tools []ToolDef,
) (Response, error) {
	body, err := a.buildRequestBody(system, messages, tools)
	if err != nil {
		return Response{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("provider.LocalAdapter.Send: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	resp, err := a.http.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("provider.LocalAdapter.Send: http call: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("provider.LocalAdapter.Send: read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, decodeLocalError(resp.StatusCode, respBytes)
	}

	var parsed localChatResponse
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return Response{}, fmt.Errorf("provider.LocalAdapter.Send: decode response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("provider.LocalAdapter.Send: empty choices in response")
	}

	return buildLocalResponse(parsed), nil
}

func (a *LocalAdapter) buildRequestBody(system string, messages []Message, tools []ToolDef) ([]byte, error) {
	out := localChatRequest{
		Model:       a.model,
		MaxTokens:   a.maxOutputTokens, // 0 is omitted via omitempty; server chooses default
		Temperature: a.temperature,     // 0 is omitted via omitempty
	}
	if system != "" {
		out.Messages = append(out.Messages, localChatMessage{Role: "system", Content: system})
	}
	for _, msg := range messages {
		converted, err := convertMessageToLocal(msg)
		if err != nil {
			return nil, err
		}
		out.Messages = append(out.Messages, converted...)
	}
	for _, t := range tools {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		out.Tools = append(out.Tools, localTool{
			Type: "function",
			Function: localToolFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  schema,
			},
		})
	}
	body, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("provider.LocalAdapter.Send: marshal request: %w", err)
	}
	return body, nil
}

// convertMessageToLocal translates one tu-agent Message into one or more OpenAI messages.
// An assistant turn always becomes a single message (text + tool_calls fold together).
// A user turn may split into a user message (text blocks) plus one tool message per tool_result.
func convertMessageToLocal(msg Message) ([]localChatMessage, error) {
	if msg.Role == "assistant" {
		var text strings.Builder
		var calls []localToolCall
		for _, b := range msg.Blocks {
			switch b.Type {
			case "text":
				text.WriteString(b.Text)
			case "tool_use":
				args := string(b.Input)
				if args == "" {
					args = "{}"
				}
				calls = append(calls, localToolCall{
					ID:   b.ID,
					Type: "function",
					Function: localToolCallFunc{
						Name:      b.Name,
						Arguments: args,
					},
				})
			}
		}
		return []localChatMessage{{
			Role:      "assistant",
			Content:   text.String(),
			ToolCalls: calls,
		}}, nil
	}

	// user role: text blocks merge into one user message; each tool_result becomes its own tool message.
	var out []localChatMessage
	var userText strings.Builder
	for _, b := range msg.Blocks {
		switch b.Type {
		case "text":
			userText.WriteString(b.Text)
		case "tool_result":
			out = append(out, localChatMessage{
				Role:       "tool",
				ToolCallID: b.ID,
				Content:    b.Content,
			})
		}
	}
	if userText.Len() > 0 {
		out = append([]localChatMessage{{Role: "user", Content: userText.String()}}, out...)
	}
	return out, nil
}

func buildLocalResponse(resp localChatResponse) Response {
	choice := resp.Choices[0]
	blocks := make([]Block, 0, 1+len(choice.Message.ToolCalls))
	if choice.Message.Content != "" {
		blocks = append(blocks, Block{Type: "text", Text: choice.Message.Content})
	}
	for _, tc := range choice.Message.ToolCalls {
		args := tc.Function.Arguments
		if args == "" {
			args = "{}"
		}
		blocks = append(blocks, Block{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: json.RawMessage(args),
		})
	}
	return Response{
		Blocks:     blocks,
		StopReason: mapLocalStopReason(choice.FinishReason),
		Usage: Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
}

// mapLocalStopReason translates OpenAI finish_reason values to the tu-agent vocabulary.
// "stop" and "length" both terminate the turn; "tool_calls" signals the loop should run tools.
func mapLocalStopReason(reason string) string {
	switch reason {
	case "tool_calls":
		return "tool_use"
	case "stop", "length", "":
		return "end_turn"
	default:
		return "end_turn"
	}
}

func decodeLocalError(status int, body []byte) error {
	msg := ""
	var env localErrorEnvelope
	var flat localFlatErrorEnvelope
	switch {
	case json.Unmarshal(body, &env) == nil && env.Error.Message != "":
		msg = env.Error.Message
	case json.Unmarshal(body, &flat) == nil && flat.Error != "":
		// LM Studio (some versions): {"error":"Context size has been exceeded."}
		msg = flat.Error
	default:
		msg = string(body)
		if len(msg) > 200 {
			msg = msg[:200] + "..."
		}
	}
	if status == 400 && isPromptTooLargeMessage(msg) {
		return fmt.Errorf("provider.LocalAdapter.Send: http %d: %s: %w", status, msg, ErrPromptTooLarge)
	}
	return fmt.Errorf("provider.LocalAdapter.Send: http %d: %s", status, msg)
}
