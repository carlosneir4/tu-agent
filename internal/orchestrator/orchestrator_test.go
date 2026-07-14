package orchestrator_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/orchestrator"
	"github.com/carlosneir4/tu-agent/internal/provider"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
	"github.com/carlosneir4/tu-agent/internal/tool"
)

// loopProvider returns the same response (and optional error) on every call,
// so it can drive error and max-iteration paths without slice bounds.
type loopProvider struct {
	resp  provider.Response
	err   error
	calls int
}

func (m *loopProvider) Send(_ context.Context, _ string, _ []provider.Message, _ []provider.ToolDef) (provider.Response, error) {
	m.calls++
	return m.resp, m.err
}
func (m *loopProvider) Name() string             { return "mock" }
func (m *loopProvider) Model() string            { return "mock-model" }
func (m *loopProvider) NativeContextWindow() int { return 200000 }

// errTool is a Tool whose Run always fails, to exercise the tool-error path.
type errTool struct{}

func (errTool) Name() string                 { return "boom" }
func (errTool) Description() string          { return "always errors" }
func (errTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (errTool) Run(context.Context, json.RawMessage) (string, error) {
	return "", fmt.Errorf("boom failed")
}

// mockProvider replays a pre-loaded sequence of responses.
type mockProvider struct {
	responses []provider.Response
	callCount int
}

func (m *mockProvider) Send(
	_ context.Context,
	_ string,
	_ []provider.Message,
	_ []provider.ToolDef,
) (provider.Response, error) {
	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func (m *mockProvider) Name() string             { return "mock" }
func (m *mockProvider) Model() string            { return "mock-model" }
func (m *mockProvider) NativeContextWindow() int { return 200000 }

func newTestTelemetry(t *testing.T) *telemetry.Logger {
	t.Helper()
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	logger, err := telemetry.NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	return logger
}

func TestOrchestrator_Chat_SimpleTextResponse(t *testing.T) {
	p := &mockProvider{
		responses: []provider.Response{
			{
				Blocks:     []provider.Block{{Type: "text", Text: "I'm doing great!"}},
				StopReason: "end_turn",
				Usage:      provider.Usage{InputTokens: 10, OutputTokens: 5},
			},
		},
	}

	reg := tool.NewRegistry()
	orch := orchestrator.New(p, reg, newTestTelemetry(t), "system prompt", "")

	got, err := orch.Chat(context.Background(), "How are you?")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if got != "I'm doing great!" {
		t.Errorf("Chat() = %q, want %q", got, "I'm doing great!")
	}
	if p.callCount != 1 {
		t.Errorf("provider called %d times, want 1", p.callCount)
	}
}

func TestOrchestrator_Chat_ToolUseFlow(t *testing.T) {
	toolInput, _ := json.Marshal(map[string]string{"command": "echo hello"})
	p := &mockProvider{
		responses: []provider.Response{
			{
				Blocks: []provider.Block{
					{Type: "text", Text: "Let me run that."},
					{Type: "tool_use", ID: "toolu_01", Name: "bash", Input: toolInput},
				},
				StopReason: "tool_use",
				Usage:      provider.Usage{InputTokens: 20, OutputTokens: 10},
			},
			{
				Blocks:     []provider.Block{{Type: "text", Text: "The output was: hello"}},
				StopReason: "end_turn",
				Usage:      provider.Usage{InputTokens: 50, OutputTokens: 10},
			},
		},
	}

	reg := tool.NewRegistry()
	reg.Register(tool.NewBashTool())

	orch := orchestrator.New(p, reg, newTestTelemetry(t), "system prompt", "")

	got, err := orch.Chat(context.Background(), "Run echo hello")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if got != "The output was: hello" {
		t.Errorf("Chat() = %q, want %q", got, "The output was: hello")
	}
	if p.callCount != 2 {
		t.Errorf("provider called %d times, want 2", p.callCount)
	}
}

func TestOrchestrator_Chat_UnknownTool(t *testing.T) {
	toolInput, _ := json.Marshal(map[string]string{"command": "ls"})
	p := &mockProvider{
		responses: []provider.Response{
			{
				Blocks: []provider.Block{
					{Type: "tool_use", ID: "toolu_01", Name: "nonexistent", Input: toolInput},
				},
				StopReason: "tool_use",
			},
			{
				Blocks:     []provider.Block{{Type: "text", Text: "OK"}},
				StopReason: "end_turn",
			},
		},
	}

	reg := tool.NewRegistry() // no tools registered
	orch := orchestrator.New(p, reg, newTestTelemetry(t), "system prompt", "")

	_, err := orch.Chat(context.Background(), "do something")
	if err != nil {
		t.Fatalf("Chat() should not error on unknown tool, got: %v", err)
	}
}

func TestOrchestrator_Chat_ProviderError(t *testing.T) {
	p := &loopProvider{err: fmt.Errorf("network down")}
	orch := orchestrator.New(p, tool.NewRegistry(), newTestTelemetry(t), "sys", "")
	_, err := orch.Chat(context.Background(), "hi")
	if err == nil {
		t.Fatal("Chat() should return an error when the provider fails")
	}
	if !strings.Contains(err.Error(), "network down") {
		t.Errorf("error should wrap the provider error, got: %v", err)
	}
}

func TestOrchestrator_Chat_ToolError(t *testing.T) {
	input, _ := json.Marshal(map[string]string{})
	p := &mockProvider{responses: []provider.Response{
		{
			Blocks:     []provider.Block{{Type: "tool_use", ID: "t1", Name: "boom", Input: input}},
			StopReason: "tool_use",
		},
		{Blocks: []provider.Block{{Type: "text", Text: "recovered"}}, StopReason: "end_turn"},
	}}
	reg := tool.NewRegistry()
	reg.Register(errTool{})
	orch := orchestrator.New(p, reg, newTestTelemetry(t), "sys", "")

	got, err := orch.Chat(context.Background(), "run boom")
	if err != nil {
		t.Fatalf("Chat() must not surface a tool error as its own error: %v", err)
	}
	if got != "recovered" {
		t.Errorf("Chat() = %q, want %q", got, "recovered")
	}
	if p.callCount != 2 {
		t.Errorf("provider called %d times, want 2 (tool result fed back)", p.callCount)
	}
}

func TestOrchestrator_Chat_ExceedsMaxIterations(t *testing.T) {
	input, _ := json.Marshal(map[string]string{})
	// Always asks for a tool, never ends the turn → loop must bail out.
	p := &loopProvider{resp: provider.Response{
		Blocks:     []provider.Block{{Type: "tool_use", ID: "t1", Name: "boom", Input: input}},
		StopReason: "tool_use",
	}}
	reg := tool.NewRegistry()
	reg.Register(errTool{})
	orch := orchestrator.New(p, reg, newTestTelemetry(t), "sys", "")

	_, err := orch.Chat(context.Background(), "loop forever")
	if err == nil {
		t.Fatal("Chat() should error after exceeding max tool iterations")
	}
	if !strings.Contains(err.Error(), "max tool iterations") {
		t.Errorf("error should mention the iteration cap, got: %v", err)
	}
}

func TestOrchestrator_Run_ProcessesLinesUntilExit(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin, oldStdout := os.Stdin, os.Stdout
	os.Stdin = r
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = devnull
	t.Cleanup(func() {
		os.Stdin, os.Stdout = oldStdin, oldStdout
		devnull.Close()
	})

	go func() {
		// "hi" → one Chat; blank line skipped; /exit breaks the loop.
		_, _ = io.WriteString(w, "hi\n\n/exit\n")
		_ = w.Close()
	}()

	p := &loopProvider{resp: provider.Response{
		Blocks:     []provider.Block{{Type: "text", Text: "ok"}},
		StopReason: "end_turn",
	}}
	orch := orchestrator.New(p, tool.NewRegistry(), newTestTelemetry(t), "sys", "")
	if err := orch.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if p.calls != 1 {
		t.Errorf("provider calls = %d, want 1 (only the non-empty line)", p.calls)
	}
}

func TestOrchestrator_Chat_TelemetryWritten(t *testing.T) {
	dir := t.TempDir()
	telPath := filepath.Join(dir, "telemetry.jsonl")
	logger, _ := telemetry.NewLogger(telPath)

	p := &mockProvider{
		responses: []provider.Response{
			{Blocks: []provider.Block{{Type: "text", Text: "hi"}}, StopReason: "end_turn"},
		},
	}

	orch := orchestrator.New(p, tool.NewRegistry(), logger, "system", "")
	if _, err := orch.Chat(context.Background(), "hello"); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	data, err := os.ReadFile(telPath)
	if err != nil {
		t.Fatalf("reading telemetry: %v", err)
	}
	if len(data) == 0 {
		t.Error("telemetry file should have at least one entry after Chat()")
	}
}

func TestOrchestrator_Chat_SubAgentTelemetry(t *testing.T) {
	prov := &mockProvider{responses: []provider.Response{
		{
			Blocks:     []provider.Block{{Type: "text", Text: "done"}},
			StopReason: "end_turn",
		},
	}}
	tools := tool.NewRegistry()
	telPath := filepath.Join(t.TempDir(), "tel.jsonl")
	tel, err := telemetry.NewLogger(telPath)
	if err != nil {
		t.Fatal(err)
	}

	orch := orchestrator.New(prov, tools, tel, "sys", "codebase-explorer")
	if _, err := orch.Chat(context.Background(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(telPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"codebase-explorer"`) {
		t.Errorf("expected sub_agent in telemetry, got: %s", string(data))
	}
}
