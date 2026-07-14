package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/stats"
)

func TestRunGraphBuildQuiet_FullLevelRecordsGraphRefresh(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "A.java"), []byte("package p;\npublic class A {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	if _, err := captureStdout(t, func() error { return runGraphBuild("") }); err != nil {
		t.Fatalf("runGraphBuild: %v", err)
	}

	entries, err := stats.ReadEntries(filepath.Join(root, ".tu-agent", "telemetry.jsonl"))
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.Event == "graph_refresh" {
			found = true
			if e.Parsed == 0 {
				t.Errorf("graph_refresh row Parsed = 0, want > 0: %+v", e)
			}
			if !e.OK {
				t.Errorf("graph_refresh row OK = false, want true: %+v", e)
			}
		}
	}
	if !found {
		t.Fatalf("expected a graph_refresh row, got entries: %+v", entries)
	}
}

func TestRunGraphBuildQuiet_ManualErrorRecordsNoHookRow(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	// An out-of-root subpath makes runGraphBuildQuiet error with "outside the
	// repository root". quiet=false means a MANUAL run — no hook row must be
	// written (it would pollute the hook failure-rate / percentiles).
	outside := t.TempDir()
	if err := runGraphBuildQuiet(outside, false); err == nil {
		t.Fatal("expected an out-of-root error, got nil")
	}

	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("a manual (quiet=false) failed build must not record a hook row, got: %s", data)
	}
}

func TestRunGraphBuildQuiet_HookModeErrorRecordsOneHookFailure(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "A.java"), []byte("package p;\npublic class A {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	// Bootstrap a graph so the quiet no-op guard passes.
	if _, err := captureStdout(t, func() error { return runGraphBuild("") }); err != nil {
		t.Fatalf("bootstrap build: %v", err)
	}
	// Reset the telemetry log so only the error-path row is under test.
	if err := os.Remove(filepath.Join(root, ".tu-agent", "telemetry.jsonl")); err != nil {
		t.Fatal(err)
	}

	// quiet=true is hook mode; an out-of-root subpath forces an error.
	outside := t.TempDir()
	if err := runGraphBuildQuiet(outside, true); err == nil {
		t.Fatal("expected an out-of-root error, got nil")
	}

	entries, err := stats.ReadEntries(filepath.Join(root, ".tu-agent", "telemetry.jsonl"))
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	var hookRows int
	for _, e := range entries {
		if e.Event == "hook" {
			hookRows++
			if e.Tool != "graph update" {
				t.Errorf("hook row Tool = %q, want %q", e.Tool, "graph update")
			}
			if e.OK {
				t.Errorf("hook row OK = true, want false for a failed build")
			}
		}
	}
	if hookRows != 1 {
		t.Fatalf("expected exactly one hook row, got %d (entries: %+v)", hookRows, entries)
	}
}

func TestRunGraphBuildQuiet_MinimalLevelRecordsNoGraphRefresh(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "A.java"), []byte("package p;\npublic class A {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	withTelemetryLevel(t, "minimal")

	if _, err := captureStdout(t, func() error { return runGraphBuild("") }); err != nil {
		t.Fatalf("runGraphBuild: %v", err)
	}

	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("minimal level must not record graph_refresh rows, got: %s", data)
	}
}
