package main

import (
	"strings"
	"testing"
)

func TestGuardFromHook(t *testing.T) {
	block := []string{
		`{"tool_input":{"file_path":"./.env"}}`,
		`{"tool_input":{"file_path":"/home/u/.ssh/id_ed25519"}}`,
		`{"tool_input":{"command":"cat ~/.ssh/id_rsa"}}`,
		`{"tool_input":{"command":"ls -la ~/.ssh/ && cat ~/.ssh/*.pub"}}`,
		`{"tool_input":{"command":"echo \"key=${ANTHROPIC_API_KEY:-UNSET}\""}}`,
		`{"tool_input":{"command":"printenv OPENAI_API_KEY"}}`,
	}
	for _, p := range block {
		if d := guardFromHook(strings.NewReader(p)); !d.touched {
			t.Errorf("expected BLOCK for %s", p)
		}
	}
	allow := []string{
		`{"tool_input":{"file_path":"main.go"}}`,
		`{"tool_input":{"command":"go test ./..."}}`,
		`not json`,
	}
	for _, p := range allow {
		if d := guardFromHook(strings.NewReader(p)); d.touched {
			t.Errorf("expected ALLOW for %s", p)
		}
	}
}

func TestGuardFromHook_ExtractsSessionAndTool(t *testing.T) {
	d := guardFromHook(strings.NewReader(`{"session_id":"s1","tool_name":"Write","tool_input":{"file_path":"./.env"}}`))
	if !d.touched {
		t.Fatal("expected touched=true")
	}
	if d.sessionID != "s1" {
		t.Errorf("sessionID = %q, want s1", d.sessionID)
	}
	if d.tool != "Write" {
		t.Errorf("tool = %q, want Write", d.tool)
	}
}

func TestGuardFromHook_MalformedYieldsZeroValue(t *testing.T) {
	d := guardFromHook(strings.NewReader("not json"))
	if (d != guardDecision{}) {
		t.Errorf("expected zero-value guardDecision, got %+v", d)
	}
}
