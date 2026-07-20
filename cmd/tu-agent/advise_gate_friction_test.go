package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/stats"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

// TestSummarizeInsights_GateFailures_BucketsOnlyFailedGateAttemptsByReason
// (@s1) verifies that stats.SummarizeInsights buckets gate_attempt rows with
// OK=false by Outcome reason into a GateFailures map, ignoring the
// successful gate_attempt row and the unrelated violation row. Red today:
// InsightsSummary has no GateFailures field at all, so FieldByName returns an
// invalid reflect.Value — accessed via reflection (not a struct literal)
// because the field does not exist pre-change and a literal reference would
// fail to compile.
func TestSummarizeInsights_GateFailures_BucketsOnlyFailedGateAttemptsByReason(t *testing.T) {
	entries := []telemetry.Entry{
		{Event: telemetry.EventGateAttempt, Stage: "red", Outcome: "build_failed", OK: false},
		{Event: telemetry.EventGateAttempt, Stage: "green", Outcome: "build_failed", OK: false},
		{Event: telemetry.EventGateAttempt, Stage: "green", Outcome: "pass", OK: true},
		{Event: telemetry.EventViolation, Outcome: "secret-guard"},
	}

	got := stats.SummarizeInsights(entries)

	fv := reflect.ValueOf(got).FieldByName("GateFailures")
	if !fv.IsValid() {
		t.Fatal("stats.InsightsSummary has no GateFailures field yet (gate-friction rule not implemented)")
	}
	gateFailures, ok := fv.Interface().(map[string]int)
	if !ok {
		t.Fatalf("GateFailures field has type %s, want map[string]int", fv.Type())
	}
	if gateFailures["build_failed"] != 2 {
		t.Errorf("GateFailures[\"build_failed\"] = %d, want 2 (two OK=false gate_attempt rows), got: %+v", gateFailures["build_failed"], gateFailures)
	}
	if _, present := gateFailures["pass"]; present {
		t.Errorf("GateFailures must have no bucket for the OK=true row's outcome %q, got: %+v", "pass", gateFailures)
	}
}

// writeGateAttemptTelemetry appends n gate_attempt rows with the given
// outcome reason and ok flag to the current directory's telemetry log,
// mirroring writeAdviseTelemetry's shape (advise_test.go) for gate_attempt
// events instead of violation events.
func writeGateAttemptTelemetry(t *testing.T, reason string, ok bool, n int) {
	t.Helper()
	lg, err := telemetry.NewLogger(telemetryPath(repoRoot()))
	if err != nil {
		t.Fatalf("telemetry.NewLogger: %v", err)
	}
	for i := 0; i < n; i++ {
		if err := lg.Log(telemetry.Entry{
			Event:   telemetry.EventGateAttempt,
			Stage:   "red",
			Feature: "login-form",
			Outcome: reason,
			OK:      ok,
		}); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}
}

// TestAdvise_GateFriction_FiresOnThreeSameReasonFailures (@s2) drives `tu-agent
// advise` end to end against a temp repo whose telemetry log has three
// build_failed gate_attempt failures, mirroring the fixture-and-run shape of
// TestAdvisePlain_SkillPending_MentionsPendingCommands (advise_skill_pending_test.go)
// and writeAdviseTelemetry (advise_test.go). Red today: SummarizeInsights does
// not populate GateFailures and internal/advise.Evaluate has no gate-friction
// rule, so plain advise's output never names the failing reason or the
// tdd.build_tags tip no matter what gate_attempt rows sit in telemetry.jsonl.
func TestAdvise_GateFriction_FiresOnThreeSameReasonFailures(t *testing.T) {
	t.Chdir(t.TempDir())
	writeGateAttemptTelemetry(t, "build_failed", false, 3)

	var out bytes.Buffer
	adviseCmd.SetOut(&out)
	t.Cleanup(func() { adviseCmd.SetOut(nil) })
	if err := runAdvisePlain(adviseCmd); err != nil {
		t.Fatalf("runAdvisePlain: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "build_failed") {
		t.Errorf("advise output missing the failing reason %q with 3 same-reason gate failures, got: %q", "build_failed", got)
	}
	if !strings.Contains(got, "tdd.build_tags") {
		t.Errorf("advise output missing the build_failed-specific tip mentioning tdd.build_tags, got: %q", got)
	}
	if !strings.Contains(got, "3") {
		t.Errorf("advise output missing the failure count 3, got: %q", got)
	}
}

// TestAdvise_GateFriction_MixedReasonsBelowThresholdStaySilent (@s3) verifies
// that two build_failed and two test_failed gate_attempt failures — each
// reason individually below evidenceThreshold (3) — produce no gate-friction
// nudge. This scenario is a regression pin rather than a discriminating red:
// with no gate-friction rule implemented at all, plain advise's output never
// contains the gate-friction message today either, the same way
// TestApprovalLocalOnly_ExportedChunkCarriesNoApprovalState
// (advise_skill_pending_test.go) is documented green-today. It becomes a real
// guard once the rule exists, locking "evidence is the max single-reason
// count, not the sum" (design.md).
func TestAdvise_GateFriction_MixedReasonsBelowThresholdStaySilent(t *testing.T) {
	t.Chdir(t.TempDir())
	writeGateAttemptTelemetry(t, "build_failed", false, 2)
	writeGateAttemptTelemetry(t, "test_failed", false, 2)

	var out bytes.Buffer
	adviseCmd.SetOut(&out)
	t.Cleanup(func() { adviseCmd.SetOut(nil) })
	if err := runAdvisePlain(adviseCmd); err != nil {
		t.Fatalf("runAdvisePlain: %v", err)
	}
	if got := out.String(); strings.Contains(got, "tdd gate failed") {
		t.Errorf("mixed reasons at 2 each (below the threshold of 3) must not fire the gate-friction nudge, got: %q", got)
	}
}

// TestAdviseDismiss_GateFriction_AcceptsKnownRule (@s4) mirrors
// TestAdviseDismiss_SkillPending_SuppressesFollowingRun's first assertion
// (advise_skill_pending_test.go): dismissing a rule id not yet in
// knownAdviseRules must succeed once the rule is registered. Red today:
// "gate-friction" is absent from knownAdviseRules, so runAdviseDismiss
// returns an "unknown rule" error before ever writing "dismissed
// gate-friction" to the output.
func TestAdviseDismiss_GateFriction_AcceptsKnownRule(t *testing.T) {
	t.Chdir(t.TempDir())

	var out bytes.Buffer
	adviseCmd.SetOut(&out)
	t.Cleanup(func() { adviseCmd.SetOut(nil) })
	if err := runAdviseDismiss(adviseCmd, "gate-friction"); err != nil {
		t.Fatalf("advise dismiss gate-friction: want it to succeed once the rule is known, got: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "dismissed gate-friction") {
		t.Errorf("advise dismiss gate-friction output = %q, want it to contain %q", got, "dismissed gate-friction")
	}
}
