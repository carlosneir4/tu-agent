package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tu/tu-agent/internal/provider"
)

// anthropicTextResp builds a minimal Anthropic Messages API text response.
func anthropicTextResp() map[string]any {
	return map[string]any{
		"id":          "msg_01",
		"type":        "message",
		"role":        "assistant",
		"content":     []map[string]any{{"type": "text", "text": "Hello from Claude!"}},
		"model":       "claude-sonnet-4-6",
		"stop_reason": "end_turn",
		"usage":       map[string]int{"input_tokens": 10, "output_tokens": 7},
	}
}

// anthropicToolUseResp builds a minimal Anthropic tool_use response.
func anthropicToolUseResp() map[string]any {
	return map[string]any{
		"id":   "msg_02",
		"type": "message",
		"role": "assistant",
		"content": []map[string]any{
			{"type": "text", "text": "Let me check the directory."},
			{"type": "tool_use", "id": "toolu_01", "name": "bash", "input": map[string]any{"command": "ls -la"}},
		},
		"model":       "claude-sonnet-4-6",
		"stop_reason": "tool_use",
		"usage":       map[string]int{"input_tokens": 30, "output_tokens": 20},
	}
}

func newAnthropicMockServer(t *testing.T, respBody any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(respBody); err != nil {
			t.Errorf("mock server encode: %v", err)
		}
	}))
}

func TestClaudeAdapter_Send_TextResponse(t *testing.T) {
	srv := newAnthropicMockServer(t, anthropicTextResp())
	defer srv.Close()

	adapter := provider.NewClaudeAdapter(provider.ClaudeConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "claude-sonnet-4-6",
	})

	resp, err := adapter.Send(
		context.Background(),
		"You are a helpful assistant.",
		[]provider.Message{{Role: "user", Blocks: []provider.Block{{Type: "text", Text: "Hi"}}}},
		nil,
	)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "end_turn")
	}
	if len(resp.Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1", len(resp.Blocks))
	}
	if resp.Blocks[0].Text != "Hello from Claude!" {
		t.Errorf("Blocks[0].Text = %q, want %q", resp.Blocks[0].Text, "Hello from Claude!")
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 7 {
		t.Errorf("Usage = %+v, want {10 7}", resp.Usage)
	}
}

func TestClaudeAdapter_Send_ToolUseResponse(t *testing.T) {
	srv := newAnthropicMockServer(t, anthropicToolUseResp())
	defer srv.Close()

	adapter := provider.NewClaudeAdapter(provider.ClaudeConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "claude-sonnet-4-6",
	})

	resp, err := adapter.Send(
		context.Background(),
		"system",
		[]provider.Message{{Role: "user", Blocks: []provider.Block{{Type: "text", Text: "List files"}}}},
		[]provider.ToolDef{{
			Name:        "bash",
			Description: "Run a bash command",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
		}},
	)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "tool_use")
	}
	if len(resp.Blocks) != 2 {
		t.Fatalf("len(Blocks) = %d, want 2", len(resp.Blocks))
	}
	tb := resp.Blocks[1]
	if tb.Type != "tool_use" {
		t.Errorf("Blocks[1].Type = %q, want %q", tb.Type, "tool_use")
	}
	if tb.ID != "toolu_01" {
		t.Errorf("Blocks[1].ID = %q, want %q", tb.ID, "toolu_01")
	}
	if tb.Name != "bash" {
		t.Errorf("Blocks[1].Name = %q, want %q", tb.Name, "bash")
	}
	var gotInput map[string]any
	if err := json.Unmarshal(tb.Input, &gotInput); err != nil {
		t.Fatalf("unmarshal tool input: %v", err)
	}
	if gotInput["command"] != "ls -la" {
		t.Errorf("tool input command = %q, want %q", gotInput["command"], "ls -la")
	}
}

// captureMaxTokensServer records the max_tokens of the incoming request body.
func captureMaxTokensServer(t *testing.T, got *float64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if v, ok := body["max_tokens"].(float64); ok {
			*got = v
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(anthropicTextResp()); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
}

func sendHi(t *testing.T, a *provider.ClaudeAdapter) {
	t.Helper()
	if _, err := a.Send(context.Background(), "sys",
		[]provider.Message{{Role: "user", Blocks: []provider.Block{{Type: "text", Text: "Hi"}}}}, nil); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
}

func TestClaudeAdapter_Send_UsesConfiguredMaxTokens(t *testing.T) {
	var got float64
	srv := captureMaxTokensServer(t, &got)
	defer srv.Close()

	adapter := provider.NewClaudeAdapter(provider.ClaudeConfig{
		APIKey: "test-key", BaseURL: srv.URL, Model: "claude-sonnet-4-6",
		MaxOutputTokens: 4096,
	})
	sendHi(t, adapter)
	if got != 4096 {
		t.Errorf("max_tokens in request = %v, want 4096", got)
	}
}

func TestClaudeAdapter_Send_DefaultMaxTokens(t *testing.T) {
	var got float64
	srv := captureMaxTokensServer(t, &got)
	defer srv.Close()

	adapter := provider.NewClaudeAdapter(provider.ClaudeConfig{
		APIKey: "test-key", BaseURL: srv.URL, Model: "claude-sonnet-4-6",
	})
	sendHi(t, adapter)
	if got != 8192 {
		t.Errorf("default max_tokens in request = %v, want 8192", got)
	}
}

func TestClaudeAdapter_Name(t *testing.T) {
	a := provider.NewClaudeAdapter(provider.ClaudeConfig{APIKey: "x", Model: "m"})
	if a.Name() != "claude" {
		t.Errorf("Name() = %q, want %q", a.Name(), "claude")
	}
}

func TestClaudeAdapter_DefaultModel(t *testing.T) {
	// When no model is set in config, the adapter should use the default.
	// We verify this indirectly by checking Name() still works and the
	// adapter constructs without panic. The model is internal state.
	adapter := provider.NewClaudeAdapter(provider.ClaudeConfig{APIKey: "test"})
	if adapter.Name() != "claude" {
		t.Errorf("Name() = %q, want %q", adapter.Name(), "claude")
	}
	// Confirm it's non-nil and usable
	if adapter == nil {
		t.Fatal("adapter should not be nil")
	}
}
