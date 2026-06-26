package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tu/tu-agent/internal/provider"
)

// localTextResp builds a minimal OpenAI-compatible text response.
func localTextResp() map[string]any {
	return map[string]any{
		"id":     "chatcmpl-01",
		"object": "chat.completion",
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": "Hello from Local!",
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]int{
			"prompt_tokens":     12,
			"completion_tokens": 6,
			"total_tokens":      18,
		},
	}
}

// localToolCallResp builds an OpenAI-compatible tool_calls response.
func localToolCallResp() map[string]any {
	return map[string]any{
		"id":     "chatcmpl-02",
		"object": "chat.completion",
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": "Let me check the directory.",
				"tool_calls": []map[string]any{{
					"id":   "call_01",
					"type": "function",
					"function": map[string]any{
						"name":      "bash",
						"arguments": `{"command":"ls -la"}`,
					},
				}},
			},
			"finish_reason": "tool_calls",
		}},
		"usage": map[string]int{
			"prompt_tokens":     40,
			"completion_tokens": 22,
			"total_tokens":      62,
		},
	}
}

// capturingHandler records the last request body and returns a canned response.
type capturingHandler struct {
	t           *testing.T
	respBody    any
	statusCode  int
	gotBody     []byte
	gotAuth     string
	gotMethod   string
	gotPath     string
	requestSeen bool
}

func (h *capturingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.t.Helper()
	h.requestSeen = true
	h.gotMethod = r.Method
	h.gotPath = r.URL.Path
	h.gotAuth = r.Header.Get("Authorization")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.t.Errorf("read request body: %v", err)
	}
	h.gotBody = body

	status := h.statusCode
	if status == 0 {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(h.respBody); err != nil {
		h.t.Errorf("encode response: %v", err)
	}
}

func newLocalAdapter(t *testing.T, srvURL string) *provider.LocalAdapter {
	t.Helper()
	return provider.NewLocalAdapter(provider.LocalConfig{
		APIKey:  "lm-studio",
		BaseURL: srvURL,
		Model:   "qwen3-coder-30b",
	})
}

func TestLocalAdapter_Send_TextResponse(t *testing.T) {
	h := &capturingHandler{t: t, respBody: localTextResp()}
	srv := httptest.NewServer(h)
	defer srv.Close()

	adapter := newLocalAdapter(t, srv.URL)

	resp, err := adapter.Send(
		context.Background(),
		"You are a helpful assistant.",
		[]provider.Message{{Role: "user", Blocks: []provider.Block{{Type: "text", Text: "Hi"}}}},
		nil,
	)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if h.gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", h.gotMethod)
	}
	if h.gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", h.gotPath)
	}
	if h.gotAuth != "Bearer lm-studio" {
		t.Errorf("Authorization = %q, want %q", h.gotAuth, "Bearer lm-studio")
	}

	// Inspect request body: should contain system + user message.
	var sent map[string]any
	if err := json.Unmarshal(h.gotBody, &sent); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	msgs, ok := sent["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("messages = %v, want length 2", sent["messages"])
	}
	first := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "You are a helpful assistant." {
		t.Errorf("system message = %v, want role=system content set", first)
	}
	second := msgs[1].(map[string]any)
	if second["role"] != "user" || second["content"] != "Hi" {
		t.Errorf("user message = %v, want role=user content=Hi", second)
	}
	if sent["model"] != "qwen3-coder-30b" {
		t.Errorf("model = %v, want qwen3-coder-30b", sent["model"])
	}

	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", resp.StopReason)
	}
	if len(resp.Blocks) != 1 || resp.Blocks[0].Text != "Hello from Local!" {
		t.Errorf("Blocks = %+v, want one text block", resp.Blocks)
	}
	if resp.Usage.InputTokens != 12 || resp.Usage.OutputTokens != 6 {
		t.Errorf("Usage = %+v, want {12 6}", resp.Usage)
	}
}

func TestLocalAdapter_Send_ToolCallResponse(t *testing.T) {
	h := &capturingHandler{t: t, respBody: localToolCallResp()}
	srv := httptest.NewServer(h)
	defer srv.Close()

	adapter := newLocalAdapter(t, srv.URL)

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

	// Verify tools were serialized as OpenAI function declarations.
	var sent map[string]any
	if err := json.Unmarshal(h.gotBody, &sent); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	toolsField, ok := sent["tools"].([]any)
	if !ok || len(toolsField) != 1 {
		t.Fatalf("tools = %v, want length 1", sent["tools"])
	}
	tool := toolsField[0].(map[string]any)
	if tool["type"] != "function" {
		t.Errorf("tool.type = %v, want function", tool["type"])
	}
	fn := tool["function"].(map[string]any)
	if fn["name"] != "bash" {
		t.Errorf("tool.function.name = %v, want bash", fn["name"])
	}

	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason = %q, want tool_use", resp.StopReason)
	}
	if len(resp.Blocks) != 2 {
		t.Fatalf("len(Blocks) = %d, want 2 (text + tool_use)", len(resp.Blocks))
	}
	tu := resp.Blocks[1]
	if tu.Type != "tool_use" || tu.ID != "call_01" || tu.Name != "bash" {
		t.Errorf("tool_use block = %+v, want {tool_use call_01 bash}", tu)
	}
	var args map[string]any
	if err := json.Unmarshal(tu.Input, &args); err != nil {
		t.Fatalf("unmarshal tool input: %v", err)
	}
	if args["command"] != "ls -la" {
		t.Errorf("tool args command = %v, want 'ls -la'", args["command"])
	}
}

func TestLocalAdapter_Send_ToolResultRoundTrip(t *testing.T) {
	h := &capturingHandler{t: t, respBody: localTextResp()}
	srv := httptest.NewServer(h)
	defer srv.Close()

	adapter := newLocalAdapter(t, srv.URL)

	// Replay a transcript where the assistant called bash and got a result back.
	toolInput := json.RawMessage(`{"command":"ls"}`)
	messages := []provider.Message{
		{Role: "user", Blocks: []provider.Block{{Type: "text", Text: "list please"}}},
		{Role: "assistant", Blocks: []provider.Block{
			{Type: "text", Text: "running it"},
			{Type: "tool_use", ID: "call_01", Name: "bash", Input: toolInput},
		}},
		{Role: "user", Blocks: []provider.Block{
			{Type: "tool_result", ID: "call_01", Content: "file1\nfile2", IsError: false},
		}},
	}

	if _, err := adapter.Send(context.Background(), "", messages, nil); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(h.gotBody, &sent); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	msgs := sent["messages"].([]any)
	// no system → 3 messages: user, assistant (with tool_calls), tool
	if len(msgs) != 3 {
		t.Fatalf("len(messages) = %d, want 3 (got %v)", len(msgs), msgs)
	}

	assistant := msgs[1].(map[string]any)
	if assistant["role"] != "assistant" {
		t.Errorf("msg[1].role = %v, want assistant", assistant["role"])
	}
	calls, ok := assistant["tool_calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("assistant.tool_calls = %v, want length 1", assistant["tool_calls"])
	}
	call := calls[0].(map[string]any)
	callFn := call["function"].(map[string]any)
	if callFn["arguments"] != `{"command":"ls"}` {
		t.Errorf("tool_calls[0].function.arguments = %v, want JSON string", callFn["arguments"])
	}

	toolMsg := msgs[2].(map[string]any)
	if toolMsg["role"] != "tool" {
		t.Errorf("msg[2].role = %v, want tool", toolMsg["role"])
	}
	if toolMsg["tool_call_id"] != "call_01" {
		t.Errorf("tool_call_id = %v, want call_01", toolMsg["tool_call_id"])
	}
	if toolMsg["content"] != "file1\nfile2" {
		t.Errorf("tool content = %v, want 'file1\\nfile2'", toolMsg["content"])
	}
}

func TestLocalAdapter_Send_ErrorResponse(t *testing.T) {
	h := &capturingHandler{
		t:          t,
		statusCode: http.StatusBadRequest,
		respBody: map[string]any{
			"error": map[string]any{
				"message": "model not loaded",
				"type":    "invalid_request_error",
			},
		},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	adapter := newLocalAdapter(t, srv.URL)

	_, err := adapter.Send(
		context.Background(),
		"",
		[]provider.Message{{Role: "user", Blocks: []provider.Block{{Type: "text", Text: "hi"}}}},
		nil,
	)
	if err == nil {
		t.Fatal("Send() should error on non-2xx, got nil")
	}
	if !strings.Contains(err.Error(), "model not loaded") {
		t.Errorf("error = %v, want it to contain 'model not loaded'", err)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error = %v, want it to contain status code 400", err)
	}
}

func TestLocalAdapter_Send_OmitsAuthHeaderWhenKeyEmpty(t *testing.T) {
	h := &capturingHandler{t: t, respBody: localTextResp()}
	srv := httptest.NewServer(h)
	defer srv.Close()

	adapter := provider.NewLocalAdapter(provider.LocalConfig{
		BaseURL: srv.URL,
		Model:   "any",
	})

	if _, err := adapter.Send(
		context.Background(),
		"",
		[]provider.Message{{Role: "user", Blocks: []provider.Block{{Type: "text", Text: "hi"}}}},
		nil,
	); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if h.gotAuth != "" {
		t.Errorf("Authorization = %q, want empty when API key is unset", h.gotAuth)
	}
}

func TestLocalAdapter_TimeoutFromConfig(t *testing.T) {
	// Server that sleeps longer than the configured timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(localTextResp())
	}))
	defer srv.Close()

	a := provider.NewLocalAdapter(provider.LocalConfig{
		BaseURL: srv.URL,
		Model:   "m",
		Timeout: 100 * time.Millisecond,
	})
	_, err := a.Send(context.Background(), "", nil, nil)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") &&
		!strings.Contains(err.Error(), "Client.Timeout") {
		t.Errorf("expected timeout-related error, got: %v", err)
	}
}

func TestLocalAdapter_MaxOutputTokensZero_OmitsField(t *testing.T) {
	h := &capturingHandler{t: t, respBody: localTextResp()}
	srv := httptest.NewServer(h)
	defer srv.Close()

	a := provider.NewLocalAdapter(provider.LocalConfig{BaseURL: srv.URL, Model: "m", MaxOutputTokens: 0})
	_, _ = a.Send(context.Background(), "", nil, nil)

	var req map[string]any
	if err := json.Unmarshal(h.gotBody, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if _, ok := req["max_tokens"]; ok {
		t.Error("max_tokens must not be present in JSON when MaxOutputTokens=0")
	}
}

func TestLocalAdapter_MaxOutputTokensNonzero_SentInRequest(t *testing.T) {
	h := &capturingHandler{t: t, respBody: localTextResp()}
	srv := httptest.NewServer(h)
	defer srv.Close()

	a := provider.NewLocalAdapter(provider.LocalConfig{BaseURL: srv.URL, Model: "m", MaxOutputTokens: 1024})
	_, _ = a.Send(context.Background(), "", nil, nil)

	var req map[string]any
	if err := json.Unmarshal(h.gotBody, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	got, ok := req["max_tokens"]
	if !ok {
		t.Fatal("max_tokens must be present in JSON when MaxOutputTokens>0")
	}
	if int(got.(float64)) != 1024 {
		t.Errorf("max_tokens = %v, want 1024", got)
	}
}

func TestLocalAdapter_NameAndModel(t *testing.T) {
	tests := []struct {
		name    string
		cfg     provider.LocalConfig
		wantMdl string
	}{
		{"explicit model", provider.LocalConfig{BaseURL: "x", Model: "qwen3-coder-30b"}, "qwen3-coder-30b"},
		{"empty model falls back to 'local'", provider.LocalConfig{BaseURL: "x"}, "local"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := provider.NewLocalAdapter(tc.cfg)
			if a.Name() != "local" {
				t.Errorf("Name() = %q, want 'local'", a.Name())
			}
			if a.Model() != tc.wantMdl {
				t.Errorf("Model() = %q, want %q", a.Model(), tc.wantMdl)
			}
		})
	}
}

func TestBuildRequestBody_IncludesTemperatureWhenSet(t *testing.T) {
	h := &capturingHandler{t: t, respBody: localTextResp()}
	srv := httptest.NewServer(h)
	defer srv.Close()

	a := provider.NewLocalAdapter(provider.LocalConfig{BaseURL: srv.URL, Model: "m", Temperature: 0.2})
	_, err := a.Send(context.Background(), "", []provider.Message{{Role: "user", Blocks: []provider.Block{{Type: "text", Text: "hi"}}}}, nil)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if !strings.Contains(string(h.gotBody), "\"temperature\":0.2") {
		t.Errorf("expected temperature in body, got: %s", h.gotBody)
	}
}

func TestBuildRequestBody_OmitsTemperatureWhenZero(t *testing.T) {
	h := &capturingHandler{t: t, respBody: localTextResp()}
	srv := httptest.NewServer(h)
	defer srv.Close()

	a := provider.NewLocalAdapter(provider.LocalConfig{BaseURL: srv.URL, Model: "m"})
	_, err := a.Send(context.Background(), "", []provider.Message{{Role: "user", Blocks: []provider.Block{{Type: "text", Text: "hi"}}}}, nil)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if strings.Contains(string(h.gotBody), "temperature") {
		t.Errorf("expected temperature omitted when zero, got: %s", h.gotBody)
	}
}

func TestLocalSendWrapsPromptTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"This model's maximum context length is 32768 tokens"}}`)
	}))
	defer srv.Close()
	a := provider.NewLocalAdapter(provider.LocalConfig{BaseURL: srv.URL})
	_, err := a.Send(context.Background(), "sys", []provider.Message{{Role: "user", Blocks: []provider.Block{{Type: "text", Text: "hi"}}}}, nil)
	if !errors.Is(err, provider.ErrPromptTooLarge) {
		t.Errorf("400 context-length error not wrapped as ErrPromptTooLarge: %v", err)
	}
}

func TestLocalSendWrapsPromptTooLargeFlatJSON(t *testing.T) {
	// LM Studio (some versions) returns a flat string error, not the OpenAI
	// envelope: {"error":"Context size has been exceeded."}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"Context size has been exceeded."}`)
	}))
	defer srv.Close()
	a := provider.NewLocalAdapter(provider.LocalConfig{BaseURL: srv.URL})
	_, err := a.Send(context.Background(), "sys", []provider.Message{{Role: "user", Blocks: []provider.Block{{Type: "text", Text: "hi"}}}}, nil)
	if !errors.Is(err, provider.ErrPromptTooLarge) {
		t.Errorf("flat-JSON context-size error not wrapped as ErrPromptTooLarge: %v", err)
	}
	if err != nil && strings.Contains(err.Error(), `{"error"`) {
		t.Errorf("error message should contain the decoded message, not raw JSON: %v", err)
	}
}

func TestLocalSendOtherErrorsNotWrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer srv.Close()
	a := provider.NewLocalAdapter(provider.LocalConfig{BaseURL: srv.URL})
	_, err := a.Send(context.Background(), "sys", []provider.Message{{Role: "user", Blocks: []provider.Block{{Type: "text", Text: "hi"}}}}, nil)
	if err == nil || errors.Is(err, provider.ErrPromptTooLarge) {
		t.Errorf("401 must error without the sentinel: %v", err)
	}
}
