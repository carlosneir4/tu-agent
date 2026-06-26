package tool_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tu/tu-agent/internal/tool"
)

func bashInput(t *testing.T, command string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatalf("marshal bash input: %v", err)
	}
	return b
}

func TestBashTool_Name(t *testing.T) {
	bt := tool.NewBashTool()
	if bt.Name() != "bash" {
		t.Errorf("Name() = %q, want %q", bt.Name(), "bash")
	}
}

func TestBashTool_Run_Echo(t *testing.T) {
	bt := tool.NewBashTool()
	out, err := bt.Run(context.Background(), bashInput(t, `echo "hello world"`))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("output %q does not contain %q", out, "hello world")
	}
}

func TestBashTool_Run_NonZeroExit(t *testing.T) {
	bt := tool.NewBashTool()
	out, err := bt.Run(context.Background(), bashInput(t, "exit 42"))
	if err != nil {
		t.Fatalf("Run() should not return error on non-zero exit, got: %v", err)
	}
	if !strings.Contains(out, "42") {
		t.Errorf("output %q should mention exit code 42", out)
	}
}

func TestBashTool_Run_StderrCaptured(t *testing.T) {
	bt := tool.NewBashTool()
	out, err := bt.Run(context.Background(), bashInput(t, "echo error >&2"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(out, "error") {
		t.Errorf("output %q should contain stderr output", out)
	}
}

func TestBashTool_Run_Timeout(t *testing.T) {
	bt := tool.NewBashTool()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	out, err := bt.Run(ctx, bashInput(t, "sleep 10"))
	if err != nil {
		t.Fatalf("Run() should not return error on timeout, got: %v", err)
	}
	if !strings.Contains(out, "timed out") && !strings.Contains(out, "killed") {
		t.Errorf("output %q should indicate timeout/kill, but doesn't", out)
	}
}

func TestBashTool_InputSchema_Valid(t *testing.T) {
	bt := tool.NewBashTool()
	schema := bt.InputSchema()
	var obj map[string]any
	if err := json.Unmarshal(schema, &obj); err != nil {
		t.Fatalf("InputSchema is not valid JSON: %v", err)
	}
	if obj["type"] != "object" {
		t.Errorf("schema type = %q, want %q", obj["type"], "object")
	}
}

func TestBashTool_Run_EmptyCommand(t *testing.T) {
	bt := tool.NewBashTool()
	_, err := bt.Run(context.Background(), json.RawMessage(`{"command":""}`))
	if err == nil {
		t.Error("Run() should return error for empty command")
	}
}

func TestBashTool_Run_MalformedInput(t *testing.T) {
	bt := tool.NewBashTool()
	_, err := bt.Run(context.Background(), json.RawMessage(`{`))
	if err == nil {
		t.Error("Run() should return error for malformed JSON input")
	}
}

func TestBashTool_StripsAPIKeysFromEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-secret")
	t.Setenv("LOCAL_API_KEY", "local-secret")
	t.Setenv("QWEN_API_KEY", "qwen-secret") // legacy; still stripped for safety

	bt := tool.NewBashTool()
	out, err := bt.Run(context.Background(), bashInput(t, `echo "${ANTHROPIC_API_KEY}__${LOCAL_API_KEY}__${QWEN_API_KEY}"`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "sk-secret") || strings.Contains(out, "local-secret") || strings.Contains(out, "qwen-secret") {
		t.Errorf("API keys leaked into child process: %q", out)
	}
}

func TestBashTool_PerCallTimeoutKillsCommand(t *testing.T) {
	bt := tool.NewBashToolWithTimeout(100 * time.Millisecond)
	out, err := bt.Run(context.Background(), bashInput(t, "sleep 10"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "timed out") && !strings.Contains(out, "killed") && !strings.Contains(out, "signal") {
		t.Errorf("expected timeout indication, got: %q", out)
	}
}
