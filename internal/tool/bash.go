package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const bashInputSchema = `{
    "type": "object",
    "properties": {
        "command": {
            "type": "string",
            "description": "The shell command to execute. Runs in bash -c."
        }
    },
    "required": ["command"]
}`

const defaultBashTimeout = 30 * time.Second

// BashTool executes shell commands via bash -c and returns combined stdout/stderr.
type BashTool struct{ timeout time.Duration }

// NewBashTool returns a BashTool with the default 30-second per-call timeout.
func NewBashTool() *BashTool { return &BashTool{timeout: defaultBashTimeout} }

// NewBashToolWithTimeout returns a BashTool with a custom timeout. Intended for tests.
func NewBashToolWithTimeout(d time.Duration) *BashTool { return &BashTool{timeout: d} }

func (b *BashTool) Name() string        { return "bash" }
func (b *BashTool) Description() string { return "Execute a bash command and return the output." }
func (b *BashTool) InputSchema() json.RawMessage {
	return json.RawMessage(bashInputSchema)
}

type bashRunInput struct {
	Command string `json:"command"`
}

// Run executes the given bash command with a per-call timeout.
// ANTHROPIC_API_KEY, LOCAL_API_KEY (and the legacy QWEN_API_KEY) are stripped from the child process
// environment to prevent credential exfiltration via prompt injection.
// Non-zero exit codes and timeouts are reported in the returned string rather
// than as errors, so the agent can reason about failures.
func (b *BashTool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in bashRunInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bash.Run: parsing input: %w", err)
	}
	if in.Command == "" {
		return "", fmt.Errorf("bash.Run: command is empty")
	}

	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", in.Command)
	cmd.Env = stripSensitiveEnv(os.Environ())
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Run()
	out := buf.String()

	if ctx.Err() != nil {
		if out == "" {
			out = fmt.Sprintf("command timed out or was killed: %v", ctx.Err())
		} else {
			out = fmt.Sprintf("%s\n[timed out: %v]", out, ctx.Err())
		}
		return out, nil
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			if out != "" {
				out = out + fmt.Sprintf("\n[exit code: %d]", exitErr.ExitCode())
			} else {
				out = fmt.Sprintf("[exit code: %d]", exitErr.ExitCode())
			}
		} else {
			return "", fmt.Errorf("bash.Run: exec: %w", runErr)
		}
	}

	return out, nil
}

// stripSensitiveEnv removes API key variables from the environment so they
// are not inherited by child processes spawned by the bash tool.
// QWEN_API_KEY is retained in the strip list for users upgrading from older configs.
func stripSensitiveEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "ANTHROPIC_API_KEY=") || strings.HasPrefix(e, "LOCAL_API_KEY=") || strings.HasPrefix(e, "QWEN_API_KEY=") {
			continue
		}
		out = append(out, e)
	}
	return out
}
