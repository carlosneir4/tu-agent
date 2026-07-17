package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/stats"
)

func TestMemoryRelinkCmd_HookMode_FullLevelRecordsHookRow(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	memRelinkQuiet = true
	t.Cleanup(func() { memRelinkQuiet = false })
	var buf bytes.Buffer
	memoryRelinkCmd.SetOut(&buf)
	t.Cleanup(func() { memoryRelinkCmd.SetOut(nil) })

	if err := memoryRelinkCmd.RunE(memoryRelinkCmd, nil); err != nil {
		t.Fatalf("relink: %v", err)
	}

	entries, err := stats.ReadEntries(filepath.Join(root, ".tu-agent", "logs", "telemetry.jsonl"))
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.Event == "hook" && e.Tool == "memory relink" {
			found = true
			if !e.OK {
				t.Errorf("hook row OK = false, want true: %+v", e)
			}
		}
	}
	if !found {
		t.Fatalf("expected a hook row for memory relink, got entries: %+v", entries)
	}
}

func TestMemoryRelinkCmd_NonHookMode_FullLevelRecordsNoHookRow(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	// memRelinkQuiet left false — an interactive/manual run, not a hook.
	if err := memoryRelinkCmd.RunE(memoryRelinkCmd, nil); err != nil {
		t.Fatalf("relink: %v", err)
	}

	if data := readTelemetryFile(t, root); len(data) != 0 {
		t.Errorf("a manual (non-hook-mode) run must not record a hook row, got: %s", data)
	}
}
