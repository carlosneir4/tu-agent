package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// writeFlowTelemetry chdirs the test into a fresh temp dir and writes raw
// JSONL fixture lines to .tu-agent/logs/telemetry.jsonl, mirroring the
// fixture style of TestStatsInsights_ReportsUnusedToolsAndZeroResultRate in
// stats_insights_test.go.
func writeFlowTelemetry(t *testing.T, lines string) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join(".tu-agent", "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".tu-agent", "logs", "telemetry.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setStatsFlowFlag drives `stats --flow` through a runtime flag lookup rather
// than a statsFlow package var, which does not exist pre-change (SummarizeFlow/
// FlowSummary/--flow are the GREEN change this test drives red against). A nil
// lookup is an honest RED failure, not a test bug. Restores the flag's prior
// value via t.Cleanup — package-level cobra flag vars leak between tests in
// the same package otherwise.
func setStatsFlowFlag(t *testing.T) {
	t.Helper()
	f := statsCmd.Flags().Lookup("flow")
	if f == nil {
		t.Fatal("stats command has no --flow flag registered yet (SummarizeFlow/FlowSummary/--flow not implemented)")
	}
	prev := f.Value.String()
	if err := f.Value.Set("true"); err != nil {
		t.Fatalf("setting --flow=true: %v", err)
	}
	f.Changed = true
	t.Cleanup(func() {
		_ = f.Value.Set(prev)
		f.Changed = false
	})
}

// featureLineCounts extracts the (red, green) attempt counts that follow the
// named feature on the funnel line, e.g. "  login-form    2   1  ...". It does
// not pin exact column widths, only that the two counts follow the feature
// name in order.
func featureLineCounts(t *testing.T, out, feature string) (red, green string) {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(feature) + `\s+(\d+)\s+(\d+)`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no funnel line found for feature %q in output:\n%s", feature, out)
	}
	return m[1], m[2]
}

// TestStatsFlow_BucketsGateAttemptsWithFailureReasons (@s1) — red/green
// gate_attempt rows for one feature are bucketed with per-Outcome failure
// reasons; an unrelated model row must not affect the counts.
func TestStatsFlow_BucketsGateAttemptsWithFailureReasons(t *testing.T) {
	resetStatsFlags(t)
	writeFlowTelemetry(t, `{"timestamp":"2026-01-01T00:00:00Z","event":"gate_attempt","feature":"login-form","stage":"red","ok":true}
{"timestamp":"2026-01-01T00:00:01Z","event":"gate_attempt","feature":"login-form","stage":"red","outcome":"test_failed","ok":false}
{"timestamp":"2026-01-01T00:00:02Z","event":"gate_attempt","feature":"login-form","stage":"green","outcome":"build_failed","ok":false}
{"timestamp":"2026-01-01T00:00:03Z","provider":"claude","model":"m","input_tokens":10,"output_tokens":5,"cost_usd":0.001}
`)
	setStatsFlowFlag(t)

	out, err := captureStdout(t, func() error { return statsCmd.RunE(statsCmd, nil) })
	if err != nil {
		t.Fatalf("stats --flow: %v", err)
	}

	if !strings.Contains(out, "login-form") {
		t.Fatalf("funnel must name feature login-form, got:\n%s", out)
	}
	red, green := featureLineCounts(t, out, "login-form")
	if red != "2" {
		t.Errorf("login-form red attempts = %s, want 2, got:\n%s", red, out)
	}
	if green != "1" {
		t.Errorf("login-form green attempts = %s, want 1, got:\n%s", green, out)
	}
	if !strings.Contains(out, "test_failed:1") {
		t.Errorf("funnel must show test_failed:1, got:\n%s", out)
	}
	if !strings.Contains(out, "build_failed:1") {
		t.Errorf("funnel must show build_failed:1, got:\n%s", out)
	}
}

// TestStatsFlow_FinalMarkAndReviewOutcomeAreLastRowWins (@s2) — the last mark
// row wins for a feature's final status, and the last run-level review row
// (here branch-review, arriving after an earlier plain review row) wins for
// the review outcome line.
func TestStatsFlow_FinalMarkAndReviewOutcomeAreLastRowWins(t *testing.T) {
	resetStatsFlags(t)
	writeFlowTelemetry(t, `{"timestamp":"2026-01-01T00:00:00Z","event":"tdd_stage","feature":"login-form","stage":"mark","outcome":"pending"}
{"timestamp":"2026-01-01T00:00:01Z","event":"tdd_stage","feature":"login-form","stage":"mark","outcome":"pass"}
{"timestamp":"2026-01-01T00:00:02Z","event":"tdd_stage","stage":"review","outcome":"skipped"}
{"timestamp":"2026-01-01T00:00:03Z","event":"tdd_stage","stage":"branch-review","outcome":"critical:0,important:1"}
`)
	setStatsFlowFlag(t)

	out, err := captureStdout(t, func() error { return statsCmd.RunE(statsCmd, nil) })
	if err != nil {
		t.Fatalf("stats --flow: %v", err)
	}

	if m := regexp.MustCompile(`login-form.*\bpass\b`); !m.MatchString(out) {
		t.Errorf("login-form line must show final status pass (last mark row wins over the earlier pending row), got:\n%s", out)
	}
	if !strings.Contains(out, "critical:0,important:1") {
		t.Errorf("run-level review line must show the last branch-review outcome critical:0,important:1, got:\n%s", out)
	}
	if strings.Contains(out, "skipped") {
		t.Errorf("review outcome must be last-row-wins (branch-review), the earlier skipped review row must not appear, got:\n%s", out)
	}
}

// TestStatsFlow_EmptyFeatureRowsDoNotInventPhantomFeatureLines (@s3) — rows
// that carry no Feature (a model row and a load_skill row, both merely
// mentioning an unrelated name in other fields) must not spawn a feature line;
// only the feature actually named on a gate_attempt row is listed.
func TestStatsFlow_EmptyFeatureRowsDoNotInventPhantomFeatureLines(t *testing.T) {
	resetStatsFlags(t)
	writeFlowTelemetry(t, `{"timestamp":"2026-01-01T00:00:00Z","event":"gate_attempt","feature":"login-form","stage":"red","ok":true}
{"timestamp":"2026-01-01T00:00:01Z","provider":"claude","model":"m","sub_agent":"other-feature","cost_usd":0.001}
{"timestamp":"2026-01-01T00:00:02Z","event":"load_skill","skill":"other-feature","found":true}
`)
	setStatsFlowFlag(t)

	out, err := captureStdout(t, func() error { return statsCmd.RunE(statsCmd, nil) })
	if err != nil {
		t.Fatalf("stats --flow: %v", err)
	}

	if got := strings.Count(out, "login-form"); got != 1 {
		t.Errorf("login-form must appear exactly once as a feature line, got %d occurrences in:\n%s", got, out)
	}
	if strings.Contains(out, "other-feature") {
		t.Errorf("rows with no Feature must not invent a phantom feature line from an unrelated name, got:\n%s", out)
	}
}

// TestStatsFlow_NoFlowEventsYieldsFriendlyEmptyMessage (@s4) — telemetry with
// only model and load_skill rows (no gate_attempt/tdd_stage rows) prints the
// friendly empty message and no funnel table header.
func TestStatsFlow_NoFlowEventsYieldsFriendlyEmptyMessage(t *testing.T) {
	resetStatsFlags(t)
	writeFlowTelemetry(t, `{"timestamp":"2026-01-01T00:00:00Z","provider":"claude","model":"m","cost_usd":0.001}
{"timestamp":"2026-01-01T00:00:01Z","event":"load_skill","skill":"foo","found":true}
`)
	setStatsFlowFlag(t)

	out, err := captureStdout(t, func() error { return statsCmd.RunE(statsCmd, nil) })
	if err != nil {
		t.Fatalf("stats --flow: %v", err)
	}

	if !strings.Contains(strings.ToLower(out), "no flow events recorded yet") {
		t.Errorf("telemetry with no flow rows must print the friendly no-flow-events message, got:\n%s", out)
	}
	if strings.Contains(out, "Flow funnel:") {
		t.Errorf("telemetry with no flow rows must not print the funnel table header, got:\n%s", out)
	}
}
