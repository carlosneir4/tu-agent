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
	}
	for _, p := range block {
		if !guardFromHook(strings.NewReader(p)) {
			t.Errorf("expected BLOCK for %s", p)
		}
	}
	allow := []string{
		`{"tool_input":{"file_path":"main.go"}}`,
		`{"tool_input":{"command":"go test ./..."}}`,
		`not json`,
	}
	for _, p := range allow {
		if guardFromHook(strings.NewReader(p)) {
			t.Errorf("expected ALLOW for %s", p)
		}
	}
}
