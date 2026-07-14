package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Thin cobra-wiring smoke tests for the tdd state/status wrappers. The moved
// logic (path resolution, begin guard, mark/review persistence, status JSON
// shape) is exercised in internal/tdd/statecmd_test.go; these only prove the
// RunE wrappers parse args/flags and delegate to internal/tdd (F8 item 5.3).

// TestTddStatusCmdWiring drives tddStatusCmd.RunE and asserts it emits JSON.
func TestTddStatusCmdWiring(t *testing.T) {
	var buf bytes.Buffer
	tddStatusCmd.SetOut(&buf)
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

// TestTddStateReviewCmdWiring drives tddStateReviewCmd.RunE with an invalid
// value and asserts the wrapper passes args[0] through to tdd.RunStateReview,
// which rejects it naming the allowed values.
func TestTddStateReviewCmdWiring(t *testing.T) {
	err := tddStateReviewCmd.RunE(tddStateReviewCmd, []string{"bogus"})
	if err == nil {
		t.Fatalf("expected error for bogus review value, got nil")
	}
	if !strings.Contains(err.Error(), "pending") {
		t.Fatalf("error %q must name allowed values", err.Error())
	}
}
