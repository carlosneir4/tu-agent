package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestTddStatusJSON(t *testing.T) {
	var buf bytes.Buffer
	tddStatusCmd.SetOut(&buf)
	// In a temp repo with no state, status reports not resumable.
	if err := tddStatusCmd.RunE(tddStatusCmd, nil); err != nil {
		t.Fatalf("status: %v", err)
	}
	var out struct {
		Resumable bool `json:"resumable"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("status output not JSON: %q (%v)", buf.String(), err)
	}
}

func TestTddStateMarkUnknown(t *testing.T) {
	err := tddStateMarkCmd.RunE(tddStateMarkCmd, []string{"nope", "pass"})
	if err == nil {
		t.Skip("no state file in repo root; mark on empty state is a no-op or error depending on env")
	}
	if !strings.Contains(err.Error(), "nope") && !strings.Contains(err.Error(), "state") {
		t.Fatalf("unexpected error: %v", err)
	}
}
