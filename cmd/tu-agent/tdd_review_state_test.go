package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tu/tu-agent/internal/tdd"
)

// writeReviewState writes a state.json with the given review value into dir and
// points the state/status commands at that dir via an absolute --base (which
// tddStateFile joins directly, ignoring repoRoot).
func writeReviewState(t *testing.T, dir, review string) {
	t.Helper()
	raw := `{
		"version": 1,
		"task": "t",
		"review": "` + review + `",
		"features": [{"name": "a", "status": "pass"}]
	}`
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	old := tddStateBaseFlag
	t.Cleanup(func() { tddStateBaseFlag = old })
	tddStateBaseFlag = dir // absolute path -> tddStateFile returns dir/state.json
}

// @s3 — `tdd status --base <dir>` JSON output exposes the review field with the
// on-disk value.
func TestTddStatusExposesReview(t *testing.T) {
	dir := t.TempDir()
	writeReviewState(t, dir, "pending")

	var buf bytes.Buffer
	tddStatusCmd.SetOut(&buf)
	if err := tddStatusCmd.RunE(tddStatusCmd, nil); err != nil {
		t.Fatalf("status: %v", err)
	}
	var out struct {
		Review string `json:"review"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("status output not JSON: %q (%v)", buf.String(), err)
	}
	if out.Review != "pending" {
		t.Fatalf("status review = %q, want %q; output=%s", out.Review, "pending", buf.String())
	}
}

// @s4 — `tdd state review pass --base <dir>` persists review "pass".
func TestTddStateReviewPersists(t *testing.T) {
	dir := t.TempDir()
	writeReviewState(t, dir, "pending")

	if err := tddStateReviewCmd.RunE(tddStateReviewCmd, []string{"pass"}); err != nil {
		t.Fatalf("state review pass: %v", err)
	}
	st, err := tdd.LoadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if st.Review != "pass" {
		t.Fatalf("persisted Review = %q, want %q", st.Review, "pass")
	}
}

// @s4 — an invalid value is rejected with an error naming the allowed values.
func TestTddStateReviewRejectsInvalid(t *testing.T) {
	err := tddStateReviewCmd.RunE(tddStateReviewCmd, []string{"bogus"})
	if err == nil {
		t.Fatalf("expected error for bogus review value, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"pending", "pass", "skipped"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q must name allowed value %q", msg, want)
		}
	}
}
