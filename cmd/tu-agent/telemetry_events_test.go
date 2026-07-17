package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/carlosneir4/tu-agent/internal/graph/extract"
	"github.com/carlosneir4/tu-agent/internal/stats"
)

// withTelemetryLevel sets cfg.Telemetry.Level for the duration of the test and
// restores it afterward.
func withTelemetryLevel(t *testing.T, level string) {
	t.Helper()
	prev := cfg.Telemetry.Level
	cfg.Telemetry.Level = level
	t.Cleanup(func() { cfg.Telemetry.Level = prev })
}

func readTelemetryFile(t *testing.T, root string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".tu-agent", "logs", "telemetry.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("reading telemetry.jsonl: %v", err)
	}
	return data
}

func TestTelemetryLevel_DefaultsToMinimal(t *testing.T) {
	withTelemetryLevel(t, "")
	if got := telemetryLevel(); got != "minimal" {
		t.Errorf("telemetryLevel() = %q, want minimal", got)
	}
}

func TestTelemetryLevel_Full(t *testing.T) {
	withTelemetryLevel(t, "full")
	if got := telemetryLevel(); got != "full" {
		t.Errorf("telemetryLevel() = %q, want full", got)
	}
}

func TestTelemetryLevel_UnknownValueDefaultsToMinimal(t *testing.T) {
	withTelemetryLevel(t, "bogus")
	if got := telemetryLevel(); got != "minimal" {
		t.Errorf("telemetryLevel() = %q, want minimal", got)
	}
}

func TestRecordGraphRefresh_MinimalWritesNothing(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "minimal")

	recordGraphRefresh(extract.BuildResult{Parsed: 3, Unchanged: 1, Deleted: 0, Failed: 0}, 10*time.Millisecond)

	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("minimal level must not write graph_refresh rows, got: %s", data)
	}
}

func TestRecordGraphRefresh_FullWritesRowWithCounts(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	recordGraphRefresh(extract.BuildResult{Parsed: 3, Unchanged: 1, Deleted: 2, Failed: 1}, 10*time.Millisecond)

	entries, err := stats.ReadEntries(filepath.Join(root, ".tu-agent", "logs", "telemetry.jsonl"))
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Event != "graph_refresh" {
		t.Errorf("Event = %q, want graph_refresh", e.Event)
	}
	if e.Parsed != 3 || e.Unchanged != 1 || e.Deleted != 2 || e.Failed != 1 {
		t.Errorf("counts mismatch: %+v", e)
	}
	if e.OK {
		t.Errorf("OK = true, want false (Failed > 0)")
	}
}

func TestRecordGraphRefresh_FullOKTrueWhenNoFailures(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	recordGraphRefresh(extract.BuildResult{Parsed: 3}, time.Millisecond)

	entries, err := stats.ReadEntries(filepath.Join(root, ".tu-agent", "logs", "telemetry.jsonl"))
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	if len(entries) != 1 || !entries[0].OK {
		t.Fatalf("expected OK=true row, got %+v", entries)
	}
}

func TestRecordHook_MinimalOnlyRecordsFailures(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "minimal")

	recordHook("graph update", time.Millisecond, nil)
	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("minimal level must not record a hook success row, got: %s", data)
	}

	recordHook("graph update", time.Millisecond, errors.New("boom"))
	entries, err := stats.ReadEntries(filepath.Join(root, ".tu-agent", "logs", "telemetry.jsonl"))
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (the failure), got %d", len(entries))
	}
	if entries[0].OK {
		t.Errorf("OK = true, want false for a failed hook")
	}
	if entries[0].Tool != "graph update" {
		t.Errorf("Tool = %q, want %q", entries[0].Tool, "graph update")
	}
}

func TestRecordViolation_MinimalWritesNothing(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "minimal")

	recordViolation("secret-guard", "Write", "s1")

	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("minimal level must not write violation rows, got: %s", data)
	}
}

func TestRecordViolation_FullWritesRow(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	recordViolation("secret-guard", "Write", "s1")

	entries, err := stats.ReadEntries(filepath.Join(root, ".tu-agent", "logs", "telemetry.jsonl"))
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Event != "violation" {
		t.Errorf("Event = %q, want violation", e.Event)
	}
	if e.Outcome != "secret-guard" {
		t.Errorf("Outcome = %q, want secret-guard", e.Outcome)
	}
	if e.Tool != "Write" {
		t.Errorf("Tool = %q, want Write", e.Tool)
	}
	if e.SessionID != "s1" {
		t.Errorf("SessionID = %q, want s1", e.SessionID)
	}
}

func TestRecordHook_FullRecordsBoth(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	recordHook("memory relink", time.Millisecond, nil)
	recordHook("memory relink", time.Millisecond, errors.New("boom"))

	entries, err := stats.ReadEntries(filepath.Join(root, ".tu-agent", "logs", "telemetry.jsonl"))
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if !entries[0].OK {
		t.Errorf("entries[0].OK = false, want true")
	}
	if entries[1].OK {
		t.Errorf("entries[1].OK = true, want false")
	}
}
