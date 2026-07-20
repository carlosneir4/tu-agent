package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RED-phase tests for the flow-emitters feature (C2 + B4): the cmd-layer
// emitters recordGateAttempt / recordTddStage that append gate_attempt and
// tdd_stage rows to .tu-agent/logs/telemetry.jsonl, plus the new
// `tdd state review --findings <code>` flag. None of these emitters exist
// yet, so every RunE below runs today exactly as it does in production —
// these tests drive the real cobra RunE wrappers directly (the same pattern
// as TestTddStatusCmdWiring/TestTddStateReviewCmdWiring in tdd_state_test.go)
// and assert on telemetry.jsonl content, never on the new Go symbols
// themselves (Entry.Feature, EventGateAttempt, recordGateAttempt,
// recordTddStage do not exist at compile time).

// newFlowEmittersRepo creates a temp repo with a .git entry (so repoRoot()
// resolves inside it) and chdirs into it for the test's duration.
func newFlowEmittersRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	t.Chdir(root)
	return root
}

// resetTddGateFlags snapshots and clears the tdd-gate package flag vars so a
// value set by one test can never leak into the next, restoring the
// snapshot on cleanup.
func resetTddGateFlags(t *testing.T) {
	t.Helper()
	prevFeature, prevCovered, prevExpect, prevNewTests, prevTicket, prevBase :=
		tddGateFeature, tddGateCovered, tddGateExpect, tddGateNewTests, tddGateTicket, tddGateBase
	t.Cleanup(func() {
		tddGateFeature, tddGateCovered, tddGateExpect, tddGateNewTests, tddGateTicket, tddGateBase =
			prevFeature, prevCovered, prevExpect, prevNewTests, prevTicket, prevBase
	})
	tddGateFeature, tddGateCovered, tddGateExpect, tddGateNewTests, tddGateTicket, tddGateBase = "", "", "", "", "", ""
}

// resetTddStateFlags snapshots and clears the tdd-state package flag vars
// (shared across begin/mark/review/status), restoring the snapshot on
// cleanup.
func resetTddStateFlags(t *testing.T) {
	t.Helper()
	prevFeatures, prevTask, prevBranch, prevTicket, prevBase :=
		tddStateFeatures, tddStateTask, tddStateBranch, tddStateTicket, tddStateBaseFlag
	t.Cleanup(func() {
		tddStateFeatures, tddStateTask, tddStateBranch, tddStateTicket, tddStateBaseFlag =
			prevFeatures, prevTask, prevBranch, prevTicket, prevBase
	})
	tddStateFeatures, tddStateTask, tddStateBranch, tddStateTicket, tddStateBaseFlag = nil, "", "", "", ""
}

// withTestCommand sets cfg.Tdd.TestCommand for the test's duration and
// restores it afterward — the same pattern as withTelemetryLevel.
func withTestCommand(t *testing.T, cmd string) {
	t.Helper()
	prev := cfg.Tdd.TestCommand
	cfg.Tdd.TestCommand = cmd
	t.Cleanup(func() { cfg.Tdd.TestCommand = prev })
}

// beginFlowEmittersRun starts a fresh run state with a single "count"
// feature, the precondition several scenarios describe as "a begun run
// state".
func beginFlowEmittersRun(t *testing.T) {
	t.Helper()
	resetTddStateFlags(t)
	tddStateFeatures = []string{"count"}
	tddStateTask = "t"
	tddStateComplexity = "trivial"
	if err := tddStateBeginCmd.RunE(tddStateBeginCmd, nil); err != nil {
		t.Fatalf("tdd state begin: %v", err)
	}
}

// telemetryRows returns telemetry.jsonl split into its non-empty lines (one
// row per append), or nil if the file was never written.
func telemetryRows(t *testing.T, root string) []string {
	t.Helper()
	data := readTelemetryFile(t, root)
	if len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// findTelemetryRow returns the first telemetry.jsonl row containing sub,
// failing the test if none matches.
func findTelemetryRow(t *testing.T, root, sub string) string {
	t.Helper()
	rows := telemetryRows(t, root)
	for _, row := range rows {
		if strings.Contains(row, sub) {
			return row
		}
	}
	t.Fatalf("no telemetry.jsonl row contains %q; rows: %v", sub, rows)
	return ""
}

// TestFlowEmittersS1_GateAttemptRowAtMinimalLevel (@s1) drives `tdd gate
// --expect red` against a failing test command with no new-test files (a
// clean red confirmation, reason "ok") and asserts the gate_attempt row
// lands even at telemetry level minimal, carrying feature/stage/reason.
// Red today: tddGateCmd.RunE never emits anything, so telemetry.jsonl is
// never written.
func TestFlowEmittersS1_GateAttemptRowAtMinimalLevel(t *testing.T) {
	root := newFlowEmittersRepo(t)
	withTelemetryLevel(t, "minimal")
	withTestCommand(t, "false") // a failing check: the RED gate confirms red
	resetTddGateFlags(t)

	tddGateFeature = "count"
	tddGateExpect = "red"
	tddGateCmd.SetContext(context.Background())

	if err := tddGateCmd.RunE(tddGateCmd, nil); err != nil {
		t.Fatalf("tdd gate: %v", err)
	}

	row := findTelemetryRow(t, root, `"event":"gate_attempt"`)
	if !strings.Contains(row, `"feature":"count"`) {
		t.Errorf("gate_attempt row missing feature=count: %s", row)
	}
	if !strings.Contains(row, `"stage":"red"`) {
		t.Errorf("gate_attempt row missing stage=red: %s", row)
	}
	// A failing test command with no new-test files resolves RunGate's reason
	// to "ok" (red confirmed) — the row's outcome must carry that same reason.
	if !strings.Contains(row, `"outcome":"ok"`) {
		t.Errorf("gate_attempt row must carry the gate result's reason as outcome: %s", row)
	}
}

// TestFlowEmittersS2_GateRunnerErrorRow (@s2) drives `tdd gate` with no
// --feature, which makes RunGate return an error before running anything.
// The command must still return that error AND leave a gate_attempt row
// with outcome=runner_error. Red today: no row is ever written on the
// error path.
func TestFlowEmittersS2_GateRunnerErrorRow(t *testing.T) {
	root := newFlowEmittersRepo(t)
	withTelemetryLevel(t, "minimal")
	resetTddGateFlags(t)
	// tddGateFeature left empty: RunGate errors with "--feature is required"
	// before doing anything else.
	tddGateCmd.SetContext(context.Background())

	if err := tddGateCmd.RunE(tddGateCmd, nil); err == nil {
		t.Fatalf("expected tdd gate to return an error for a missing --feature")
	}

	row := findTelemetryRow(t, root, `"event":"gate_attempt"`)
	if !strings.Contains(row, `"outcome":"runner_error"`) {
		t.Errorf("gate_attempt row must record outcome=runner_error on a RunGate error: %s", row)
	}
}

// TestFlowEmittersS3_StateBeginAndMarkRows (@s3) drives `tdd state begin`
// then `tdd state mark count pass` and asserts both leave tdd_stage rows,
// the mark row carrying the feature slug and status. Red today: no
// tdd_stage row is ever written by these RunEs.
func TestFlowEmittersS3_StateBeginAndMarkRows(t *testing.T) {
	root := newFlowEmittersRepo(t)
	withTelemetryLevel(t, "minimal")
	beginFlowEmittersRun(t)

	if err := tddStateMarkCmd.RunE(tddStateMarkCmd, []string{"count", "pass"}); err != nil {
		t.Fatalf("tdd state mark: %v", err)
	}

	beginRow := findTelemetryRow(t, root, `"stage":"begin"`)
	if !strings.Contains(beginRow, `"event":"tdd_stage"`) {
		t.Errorf("begin row must have event=tdd_stage: %s", beginRow)
	}

	markRow := findTelemetryRow(t, root, `"stage":"mark"`)
	if !strings.Contains(markRow, `"event":"tdd_stage"`) {
		t.Errorf("mark row must have event=tdd_stage: %s", markRow)
	}
	if !strings.Contains(markRow, `"feature":"count"`) {
		t.Errorf("mark row must carry feature=count: %s", markRow)
	}
	if !strings.Contains(markRow, `"outcome":"pass"`) {
		t.Errorf("mark row must carry outcome=pass: %s", markRow)
	}
}

// TestFlowEmittersS4_StateReviewWithFindingsWritesBranchReviewRow (@s4)
// drives `tdd state review pass --findings critical:1,important:2` and
// asserts a branch-review row carries the findings code as its outcome.
// The --findings flag does not exist on tddStateReviewCmd yet, so
// Flags().Set returns an error today (an honest RUNTIME failure, never a
// compile-time reference to the flag) — the RunE still runs and the
// assertion on the resulting row is the actual red proof.
func TestFlowEmittersS4_StateReviewWithFindingsWritesBranchReviewRow(t *testing.T) {
	root := newFlowEmittersRepo(t)
	withTelemetryLevel(t, "minimal")
	beginFlowEmittersRun(t)

	if err := tddStateReviewCmd.Flags().Set("findings", "critical:1,important:2"); err != nil {
		t.Logf("tdd state review --findings not yet registered (expected pre-implementation): %v", err)
	}

	if err := tddStateReviewCmd.RunE(tddStateReviewCmd, []string{"pass"}); err != nil {
		t.Fatalf("tdd state review: %v", err)
	}

	row := findTelemetryRow(t, root, `"stage":"branch-review"`)
	if !strings.Contains(row, `"outcome":"critical:1,important:2"`) {
		t.Errorf("branch-review row must carry the findings code as outcome: %s", row)
	}
}

// TestFlowEmittersS5_StateReviewWithoutFindingsWritesPlainReviewRow (@s5)
// drives `tdd state review skipped` (no --findings) and asserts a plain
// review row with outcome=skipped. Red today: no row is written at all.
func TestFlowEmittersS5_StateReviewWithoutFindingsWritesPlainReviewRow(t *testing.T) {
	root := newFlowEmittersRepo(t)
	withTelemetryLevel(t, "minimal")
	beginFlowEmittersRun(t)

	if err := tddStateReviewCmd.RunE(tddStateReviewCmd, []string{"skipped"}); err != nil {
		t.Fatalf("tdd state review: %v", err)
	}

	row := findTelemetryRow(t, root, `"stage":"review"`)
	if !strings.Contains(row, `"outcome":"skipped"`) {
		t.Errorf("review row must carry outcome=skipped: %s", row)
	}
}

// TestFlowEmittersS6_UnwritableLogsDirNeverBreaksStateMark (@s6) blocks
// .tu-agent/logs with a regular file BEFORE any state command runs (so
// every telemetry write in the test — begin's and mark's — fails at mkdir
// time: os.MkdirAll(".tu-agent/logs") cannot create a directory where a
// regular file already sits) and asserts both `tdd state begin` and `tdd
// state mark count pass` still succeed and print their normal
// confirmations. The point is that a telemetry write failure never breaks
// the command, for every emitter call site, not just mark's.
func TestFlowEmittersS6_UnwritableLogsDirNeverBreaksStateMark(t *testing.T) {
	root := newFlowEmittersRepo(t)
	withTelemetryLevel(t, "minimal")

	if err := os.MkdirAll(filepath.Join(root, ".tu-agent"), 0o755); err != nil {
		t.Fatalf("mkdir .tu-agent: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".tu-agent", "logs"), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write logs blocker file: %v", err)
	}

	resetTddStateFlags(t)
	tddStateFeatures = []string{"count"}
	tddStateTask = "t"
	tddStateComplexity = "trivial"

	beginOut, err := captureStdout(t, func() error {
		return tddStateBeginCmd.RunE(tddStateBeginCmd, nil)
	})
	if err != nil {
		t.Fatalf("tdd state begin must succeed even when telemetry cannot be written, got: %v", err)
	}
	if !strings.Contains(beginOut, "began run with 1 feature(s)") {
		t.Errorf("expected begin output to contain %q, got %q", "began run with 1 feature(s)", beginOut)
	}

	markOut, err := captureStdout(t, func() error {
		return tddStateMarkCmd.RunE(tddStateMarkCmd, []string{"count", "pass"})
	})
	if err != nil {
		t.Fatalf("tdd state mark must succeed even when telemetry cannot be written, got: %v", err)
	}
	if !strings.Contains(markOut, "marked count pass") {
		t.Errorf("expected mark output to contain %q, got %q", "marked count pass", markOut)
	}
}
