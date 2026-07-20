package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// promptExpansionCmd locates the `hook prompt-expansion` subcommand on
// hookCmd at runtime. It does not exist on the pre-change tree, so this
// lookup — not a direct symbol reference — is the RED signal for @s1-@s4:
// the file compiles against the old tree and fails at runtime via t.Fatal.
func promptExpansionCmd(t *testing.T) *cobra.Command {
	t.Helper()
	for _, c := range hookCmd.Commands() {
		if c.Use == "prompt-expansion" {
			return c
		}
	}
	t.Fatal("hook prompt-expansion subcommand not registered on hookCmd")
	return nil
}

// runPromptExpansion drives the prompt-expansion hook's RunE with stdin
// swapped to payload, returning the error RunE reports.
func runPromptExpansion(t *testing.T, payload string) error {
	t.Helper()
	c := promptExpansionCmd(t)
	c.SetIn(strings.NewReader(payload))
	return c.RunE(c, nil)
}

// countLinesContaining returns how many non-empty lines in data contain sub.
func countLinesContaining(data []byte, sub string) int {
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		if strings.Contains(line, sub) {
			n++
		}
	}
	return n
}

// @s1: full level records one skill_invoked row for a slash-command expansion.
func TestPromptExpansion_FullLevelRecordsSkillInvokedRow(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	if err := runPromptExpansion(t, `{"session_id":"sess-1","command_name":"tu-agent:tdd"}`); err != nil {
		t.Fatalf("RunE err = %v, want nil", err)
	}

	data := readTelemetryFile(t, root)
	if got := countLinesContaining(data, `"event":"skill_invoked"`); got != 1 {
		t.Fatalf("expected exactly one skill_invoked line, got %d in: %s", got, data)
	}

	line := string(data)
	if !strings.Contains(line, `"skill":"tu-agent:tdd"`) {
		t.Errorf("line missing skill field, got: %s", line)
	}
	if !strings.Contains(line, `"session_id":"sess-1"`) {
		t.Errorf("line missing session_id field, got: %s", line)
	}
}

// @s2: minimal level records nothing.
func TestPromptExpansion_MinimalLevelNoOp(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "minimal")

	if err := runPromptExpansion(t, `{"session_id":"sess-1","command_name":"tu-agent:tdd"}`); err != nil {
		t.Fatalf("RunE err = %v, want nil", err)
	}
	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("minimal level must not write a row, got: %s", data)
	}
}

// @s3: a plain prompt without command_name is a silent no-op.
func TestPromptExpansion_NoCommandNameNoOp(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	if err := runPromptExpansion(t, `{"session_id":"sess-1"}`); err != nil {
		t.Fatalf("RunE err = %v, want nil", err)
	}
	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("payload without command_name must not write a row, got: %s", data)
	}
}

// @s4: a malformed payload never fails the hook.
func TestPromptExpansion_MalformedPayloadNoOp(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	if err := runPromptExpansion(t, "not json"); err != nil {
		t.Fatalf("RunE err = %v, want nil", err)
	}
	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("malformed payload must not write a row, got: %s", data)
	}
}
