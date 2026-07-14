package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/stats"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

func TestPromptSubmitDecision_FullEmitsRow(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	if err := promptSubmitDecision(strings.NewReader(`{"session_id":"s1","prompt":"hello"}`)); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}

	entries, err := stats.ReadEntries(filepath.Join(root, ".tu-agent", "telemetry.jsonl"))
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Event != telemetry.EventPrompt {
		t.Errorf("Event = %q, want %q", e.Event, telemetry.EventPrompt)
	}
	if e.SessionID != "s1" {
		t.Errorf("SessionID = %q, want s1", e.SessionID)
	}
}

func TestPromptSubmitDecision_MinimalNoOp(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "minimal")

	if err := promptSubmitDecision(strings.NewReader(`{"session_id":"s1","prompt":"hello"}`)); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("minimal level must not write prompt rows, got: %s", data)
	}
}

func TestPromptSubmitDecision_BadJSONNoOp(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	if err := promptSubmitDecision(strings.NewReader("not json")); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("bad json must not write a row, got: %s", data)
	}
}

func TestPromptSubmitDecision_EmptyStdinNoOp(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	if err := promptSubmitDecision(strings.NewReader("")); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("empty stdin must not write a row, got: %s", data)
	}
}
