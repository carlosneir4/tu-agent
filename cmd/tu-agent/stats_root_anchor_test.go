package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

// seedRootTelemetry writes one model-call row to the repo-root telemetry log
// THROUGH the telemetryPath helper, so it survives the .tu-agent/logs relayout
// without hardcoding the path. root is the repo root (the dir holding .git).
func seedRootTelemetry(t *testing.T, root string) {
	t.Helper()
	logger, err := telemetry.NewLogger(telemetryPath(root))
	if err != nil {
		t.Fatalf("telemetry.NewLogger: %v", err)
	}
	if err := logger.Log(telemetry.Entry{
		Timestamp:    time.Now(),
		Provider:     "claude",
		Model:        "m",
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.00105,
	}); err != nil {
		t.Fatalf("logger.Log: %v", err)
	}
}

// resetStatsFlags pins statsLast/statsInsights to their zero values for a
// deterministic run and restores them afterward.
func resetStatsFlags(t *testing.T) {
	t.Helper()
	prevLast, prevInsights := statsLast, statsInsights
	statsLast, statsInsights = 0, false
	t.Cleanup(func() { statsLast, statsInsights = prevLast, prevInsights })
}

// TestStatsReadsRepoRootTelemetryFromSubdir (@s1) — running stats from a
// subdirectory must report the telemetry recorded at the repo root, not the
// (missing) telemetry at the current directory.
//
// RED now: stats reads telemetryPath(".") = <subdir>/.tu-agent/logs/... which
// does not exist, so ReadEntries returns nil and stats prints "No telemetry
// data found". GREEN anchors it at repoRoot() and finds the seeded row.
func TestStatsReadsRepoRootTelemetryFromSubdir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	seedRootTelemetry(t, root)

	sub := filepath.Join(root, "internal")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)
	resetStatsFlags(t)

	out, err := captureStdout(t, func() error { return statsCmd.RunE(statsCmd, nil) })
	if err != nil {
		t.Fatalf("stats from subdir: %v", err)
	}
	if strings.Contains(out, "No telemetry data found") {
		t.Errorf("stats reported no telemetry from a subdir while the repo root has a model-call row, got:\n%s", out)
	}
	if !strings.Contains(out, "Total model calls : 1") {
		t.Errorf("stats must report the model call recorded at the repo root, got:\n%s", out)
	}
}

// TestStatsEmptyRepoReportsEmptyFromSubdir (@s3, green guard) — a repo with no
// telemetry anywhere must still report empty and exit without error, even from a
// subdirectory. This passes both before and after GREEN: re-anchoring at the
// repo root must not turn an honestly-empty repo into a loud error.
func TestStatsEmptyRepoReportsEmptyFromSubdir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "internal")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)
	resetStatsFlags(t)

	out, err := captureStdout(t, func() error { return statsCmd.RunE(statsCmd, nil) })
	if err != nil {
		t.Fatalf("stats on an empty repo must not error, got: %v", err)
	}
	if !strings.Contains(out, "No telemetry data found") {
		t.Errorf("stats on an empty repo must report no telemetry, got:\n%s", out)
	}
}
