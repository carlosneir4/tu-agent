package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRecordHook_FailureCarriesErrorMessage pins @s2: a failing hook must
// record the error MESSAGE, not just OK=false. We assert against the RAW
// telemetry line text (not a struct field) so the package still compiles in the
// RED phase, before the new Error field exists.
func TestRecordHook_FailureCarriesErrorMessage(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	withTelemetryLevel(t, "full")

	recordHook("graph update", time.Millisecond, errors.New("database is locked"))

	data, err := os.ReadFile(filepath.Join(root, ".tu-agent", "logs", "telemetry.jsonl"))
	if err != nil {
		t.Fatalf("reading telemetry.jsonl: %v", err)
	}
	line := string(data)

	if !strings.Contains(line, "database is locked") {
		t.Errorf("failed-hook row must carry the error message, got: %s", line)
	}
	if strings.Contains(line, `"ok":true`) {
		t.Errorf("failed-hook row must not report ok:true, got: %s", line)
	}
}
